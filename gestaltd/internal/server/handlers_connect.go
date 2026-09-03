package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/apiexec"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

type connectManualRequest struct {
	Integration      string            `json:"integration"`
	Connection       string            `json:"connection"`
	Instance         string            `json:"instance"`
	ServiceAccountID string            `json:"serviceAccountId"`
	Credential       string            `json:"credential"`
	Credentials      map[string]string `json:"credentials"`
	ConnectionParams map[string]string `json:"connectionParams"`
}

func (s *Server) connectManual(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	auditAllowed := false
	auditErr := errors.New("manual connection failed")
	auditTarget := auditTarget{Kind: auditTargetKindConnection}
	providerName := ""
	metricProviderName := metricutil.UnknownAttrValue
	connectionMode := metricutil.UnknownAttrValue
	defer func() {
		metricutil.RecordConnectionAuthMetrics(r.Context(), startedAt, metricProviderName, "manual", "complete", connectionMode, auditErr != nil)
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), providerName, "connection.manual.connect", auditAllowed, auditErr, auditTarget)
	}()
	var req connectManualRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auditErr = errors.New("invalid JSON body")
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	providerName = req.Integration

	if req.Integration == "" {
		auditErr = errors.New("integration is required")
		writeError(w, http.StatusBadRequest, "integration is required")
		return
	}

	prov, manualConnection, err := s.resolveConnectionProvider(r.Context(), w, req.Integration, req.Connection)
	if err != nil {
		auditErr = err
		return
	}
	metricProviderName = req.Integration
	connectionMode = metricutil.NormalizeConnectionMode(prov.ConnectionMode())
	p := PrincipalFromContext(r.Context())

	selected, ok := s.requireConfiguredConnection(w, req.Integration, manualConnection)
	if !ok {
		auditErr = errors.New("connection is not configured")
		return
	}
	conn := selected.Def
	connectionMode = metricutil.NormalizeConnectionMode(selected.Mode)
	auth := conn.Auth
	if !manualConnectionAllowed(conn) {
		auditErr = errors.New("integration does not support manual auth")
		writeError(w, http.StatusBadRequest, fmt.Sprintf("integration %q does not support manual auth; use OAuth connect instead", req.Integration))
		return
	}

	subjectID, manualInstance, err := s.resolveCredentialConnectionSetup(w, r, req.Instance, req.ServiceAccountID)
	if err != nil {
		auditErr = err
		return
	}
	auditTarget = connectionAuditTarget(req.Integration, manualConnection, manualInstance)

	tokenExchange := manualTokenExchangeConfigured(auth)

	fields, rawCredential, credErr := buildEffectiveManualCredential(req, auth, tokenExchange)
	if credErr != nil {
		auditErr = credErr
		writeError(w, http.StatusBadRequest, credErr.Error())
		return
	}
	if len(fields) == 0 && rawCredential == "" {
		auditErr = errors.New("credential is required")
		writeError(w, http.StatusBadRequest, "credential is required")
		return
	}

	connParams, ok := resolveConnectionParams(w, selected.ParamDefs, req.ConnectionParams)
	if !ok {
		auditErr = errors.New("invalid connection parameters")
		return
	}

	credentialJSON := ""
	if len(fields) > 0 {
		credentialJSON, credErr = marshalManualCredentials(fields)
		if credErr != nil {
			auditErr = credErr
			writeError(w, http.StatusBadRequest, credErr.Error())
			return
		}
	}

	tokenResp, exchangeErr := s.exchangeManualCredential(r.Context(), req, manualConnection, conn.ConnectionID, subjectID, manualInstance, auth, credentialJSON, connParams, tokenExchange)
	if exchangeErr != nil {
		auditErr = errors.New("token exchange failed")
		slog.ErrorContext(r.Context(), "manual token exchange failed", "provider", req.Integration, "error", exchangeErr)
		writeError(w, http.StatusBadGateway, "token exchange failed")
		return
	}

	manualMeta, metaErr := buildConnectionMetadata(selected.ParamDefs, connParams, tokenResp)
	if metaErr != nil {
		auditErr = errors.New(metaErr.Error())
		writeError(w, http.StatusBadRequest, metaErr.Error())
		return
	}

	authSource := ""
	if p != nil {
		authSource = p.AuthSource()
	}
	tm := credentialMaterial{
		SubjectID:         subjectID,
		AuthSource:        authSource,
		ConnectionID:      conn.ConnectionID,
		Integration:       req.Integration,
		Connection:        manualConnection,
		Instance:          manualInstance,
		Fields:            fields,
		AccessToken:       rawCredential,
		MetadataJSON:      manualMeta,
		ProviderAccountID: providerAccountIDFromTokenResponse(selected.ParamDefs, tokenResp),
	}
	credentialActorFromPrincipal(p, subjectID).applyTo(&tm)

	// Exchange-minted tokens are not persisted; the provider re-mints from
	// the stored fields at resolve time.
	discoveryToken := rawCredential
	if tokenResp != nil {
		discoveryToken = tokenResp.AccessToken
	}

	result, err := s.runConnectionSetup(r.Context(), prov, tm, discoveryToken)
	if err != nil {
		auditErr = errors.New("connection setup failed")
		slog.ErrorContext(r.Context(), "connection setup failed", "provider", req.Integration, "error", err)
		writeError(w, http.StatusBadGateway, "connection setup failed")
		return
	}

	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, result)
}

func manualTokenExchangeConfigured(auth config.ConnectionAuthDef) bool {
	return (auth.Type == "" || auth.Type == providermanifestv1.AuthTypeManual) && strings.TrimSpace(auth.TokenURL) != ""
}

func (s *Server) exchangeManualCredential(ctx context.Context, req connectManualRequest, connection, connectionID, subjectID, instance string, auth config.ConnectionAuthDef, credential string, connParams map[string]string, tokenExchange bool) (*core.OAuthTokenResponse, error) {
	if !tokenExchange {
		return nil, nil
	}
	resp, err := s.externalCredentials.ExchangeCredential(ctx, &core.ExchangeExternalCredentialRequest{
		Provider:            req.Integration,
		Connection:          connection,
		ConnectionID:        connectionID,
		CredentialSubjectID: subjectID,
		Instance:            instance,
		Auth:                bootstrap.ExternalCredentialAuthConfig(auth),
		CredentialJSON:      credential,
		ConnectionParams:    connParams,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.TokenResponse == nil {
		return nil, errors.New("external credential provider returned no token response")
	}
	tokenResp := resp.TokenResponse
	refreshToken := tokenResp.RefreshSource
	if refreshToken == "" {
		refreshToken = tokenResp.RefreshToken
	}
	return &core.OAuthTokenResponse{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
		TokenType:    tokenResp.TokenType,
		Extra:        tokenResp.Extra,
	}, nil
}

func (s *Server) resolveConnectionProvider(ctx context.Context, w http.ResponseWriter, integration, requestedConnection string) (core.Provider, string, error) {
	prov, ok := s.getProvider(ctx, w, integration)
	if !ok {
		return nil, "", errors.New("integration not found")
	}
	connection, ok := s.resolveRequestedConnection(w, integration, requestedConnection)
	if !ok {
		return nil, "", errors.New("invalid connection")
	}
	return prov, connection, nil
}

func (s *Server) resolveCredentialConnectionSetup(w http.ResponseWriter, r *http.Request, requestedInstance, serviceAccountID string) (string, string, error) {
	subjectID, err := s.resolveCredentialSubjectID(w, r, serviceAccountID)
	if err != nil {
		return "", "", err
	}
	instance, ok := resolveRequestedInstance(w, requestedInstance)
	if !ok {
		return "", "", errors.New("invalid instance")
	}
	return subjectID, instance, nil
}

func validateConnectionParams(defs map[string]core.ConnectionParamDef, provided map[string]string) (map[string]string, error) {
	for key := range provided {
		if _, ok := defs[key]; !ok {
			return nil, fmt.Errorf("unknown connection parameter: %s", key)
		}
	}
	result := make(map[string]string)
	for name, def := range defs {
		if def.From != "" {
			continue
		}
		if v, ok := provided[name]; ok && v != "" {
			if !safeParamValue.MatchString(v) {
				return nil, fmt.Errorf("connection parameter %q contains invalid characters (allowed: letters, digits, hyphens, dots, underscores)", name)
			}
			result[name] = v
		} else if def.Default != "" {
			result[name] = def.Default
		} else if def.Required {
			return nil, fmt.Errorf("missing required connection parameter: %s", name)
		}
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func buildConnectionMetadata(defs map[string]core.ConnectionParamDef, userParams map[string]string, tokenResp *core.OAuthTokenResponse) (string, error) {
	metadata := make(map[string]string)
	for k, v := range userParams {
		metadata[k] = v
	}

	for name, def := range defs {
		if def.From == "token_response" {
			if tokenResp == nil {
				continue
			}
			field := def.Field
			if field == "" {
				field = name
			}
			val, ok := apiexec.ExtractJSONPath(tokenResp.Extra, field)
			if !ok {
				if def.Required {
					return "", fmt.Errorf("token response missing required field %q for connection param %q", field, name)
				}
				continue
			}
			s := fmt.Sprintf("%v", val)
			if !safeTokenResponseValue.MatchString(s) {
				return "", fmt.Errorf("token response field %q for connection param %q contains invalid characters", field, name)
			}
			metadata[name] = s
		}
	}

	if len(metadata) == 0 {
		return "", nil
	}
	b, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal connection metadata: %w", err)
	}
	return string(b), nil
}

// providerAccountIDFromTokenResponse extracts the provider-owned identifier
// when a registry connection declares the conventional account_id metadata
// parameter. The parameter may map to a nested token-response field such as
// account.id. Unmarked connection parameters remain runtime-only metadata and
// are never treated as account identity.
func providerAccountIDFromTokenResponse(defs map[string]core.ConnectionParamDef, tokenResp *core.OAuthTokenResponse) string {
	if tokenResp == nil || len(tokenResp.Extra) == 0 {
		return ""
	}
	var names []string
	for name, def := range defs {
		if def.From == "token_response" && isProviderAccountIDParam(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		def := defs[name]
		field := strings.TrimSpace(def.Field)
		if field == "" {
			field = name
		}
		value, ok := apiexec.ExtractJSONPath(tokenResp.Extra, field)
		if !ok {
			continue
		}
		candidate := fmt.Sprintf("%v", value)
		if safeTokenResponseValue.MatchString(candidate) {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

func isProviderAccountIDParam(name string) bool {
	name = strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(strings.TrimSpace(name)))
	return name == "accountid" || name == "provideraccountid"
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", core.BearerScheme+t.token)
	}
	return t.base.RoundTrip(req)
}

type credentialMaterial struct {
	SubjectID    string
	AuthSource   string
	ConnectionID string
	Integration  string
	Connection   string
	Instance     string
	// Fields holds named manual credentials, stored as an Opaque credential.
	// When empty, the token fields below are stored as a Grant.
	Fields            map[string]string
	AccessToken       string
	RefreshToken      string
	TokenExpiresAt    *time.Time
	MetadataJSON      string
	ProviderAccountID string
	ActorSubjectID    string
	ActorUserID       string
	ActorAuthSource   string
}

type credentialActor struct {
	SubjectID  string
	UserID     string
	AuthSource string
}

func credentialActorFromPrincipal(p *principal.Principal, credentialSubjectID string) credentialActor {
	p = principal.Canonicalized(p)
	if p == nil {
		return credentialActor{}
	}
	actorSubjectID := strings.TrimSpace(p.SubjectID)
	if actorSubjectID == "" || actorSubjectID == strings.TrimSpace(credentialSubjectID) {
		return credentialActor{}
	}
	return credentialActor{
		SubjectID:  actorSubjectID,
		UserID:     strings.TrimSpace(p.UserID),
		AuthSource: p.AuthSource(),
	}
}

func (a credentialActor) applyTo(tm *credentialMaterial) {
	if tm == nil {
		return
	}
	tm.ActorSubjectID = strings.TrimSpace(a.SubjectID)
	tm.ActorUserID = strings.TrimSpace(a.UserID)
	tm.ActorAuthSource = strings.TrimSpace(a.AuthSource)
}

func credentialMaterialContext(ctx context.Context, p *principal.Principal, tm credentialMaterial) context.Context {
	if cred := principalForCredentialMaterial(p, tm); cred != nil {
		return principal.WithPrincipal(ctx, cred)
	}
	return ctx
}

func principalForCredentialMaterial(p *principal.Principal, tm credentialMaterial) *principal.Principal {
	if subjectID := strings.TrimSpace(tm.SubjectID); subjectID != "" {
		cred := &principal.Principal{
			SubjectID: subjectID,
			Source:    principal.SourceBearer,
		}
		if suffix := principal.UserIDFromSubjectID(subjectID); suffix != "" {
			if strings.Contains(suffix, "@") {
				cred.Identity = &core.UserIdentity{Email: suffix}
				cred.Kind = principal.KindUser
			} else {
				cred.UserID = suffix
				cred.Kind = principal.KindUser
			}
		} else if kind := principal.KindFromSubjectID(subjectID); kind != "" {
			cred.Kind = kind
		}
		return principal.Canonicalize(cred)
	}
	if p := principal.Canonicalized(p); p != nil {
		return p
	}
	actorSubjectID := strings.TrimSpace(tm.ActorSubjectID)
	if actorSubjectID == "" {
		return nil
	}
	actor := &principal.Principal{
		SubjectID: actorSubjectID,
		UserID:    strings.TrimSpace(tm.ActorUserID),
	}
	if kind, _, ok := core.ParseSubjectID(actor.SubjectID); ok {
		actor.Kind = principal.Kind(kind)
	}
	return principal.Canonicalize(actor)
}

type connectionSetupResult struct {
	Status       string                   `json:"status"`
	Integration  string                   `json:"integration,omitempty"`
	SelectionURL string                   `json:"selectionUrl,omitempty"`
	PendingToken string                   `json:"pendingToken,omitempty"`
	Candidates   []discoveryCandidateInfo `json:"candidates,omitempty"`
}

type discoveryCandidateInfo struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

func (s *Server) storeCredentialFromMaterial(ctx context.Context, tm credentialMaterial) (*core.ExternalCredential, error) {
	now := s.now().UTC().Truncate(time.Second)
	audience := strings.TrimSpace(tm.ConnectionID)
	if audience == "" {
		audience = tm.Integration + ":" + tm.Connection
	}
	tok := &core.ExternalCredential{
		ID:           uuid.NewString(),
		Subject:      tm.SubjectID,
		Audience:     audience,
		Qualifier:    tm.Instance,
		MetadataJSON: tm.MetadataJSON,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if len(tm.Fields) > 0 {
		tok.Opaque = &core.ExternalCredentialOpaque{Fields: tm.Fields}
	} else {
		tok.Grant = &core.ExternalCredentialGrant{
			AccessToken:     tm.AccessToken,
			RefreshToken:    tm.RefreshToken,
			ExpiresAt:       tm.TokenExpiresAt,
			LastRefreshedAt: &now,
		}
	}
	accountKey := accountKeyStoredInMetadataJSON(tok.MetadataJSON)
	if accountKey != "" {
		unlock := s.credentialReconciliation.lock(strings.Join([]string{tok.Subject, tok.Audience, accountKey}, "\x00"))
		defer unlock()
	}
	duplicates, _, err := s.reconcileCanonicalAccountCredential(ctx, tok)
	if err != nil {
		return nil, err
	}
	if err := s.externalCredentials.UpsertCredential(ctx, tok); err != nil {
		return nil, err
	}
	s.deleteCanonicalDuplicates(ctx, tok, duplicates)

	// A second read closes the race with another server instance that may have
	// inserted a duplicate between the initial read and upsert. The provider
	// contract does not offer compare-and-swap, so cleanup is deliberately
	// retry-safe rather than reported as a failed save.
	if accountKey != "" {
		postDuplicates, changed, reconcileErr := s.reconcileCanonicalAccountCredential(ctx, tok)
		if reconcileErr != nil {
			slog.WarnContext(ctx, "canonical duplicate cleanup deferred", "subject", tok.Subject, "audience", tok.Audience, "error", reconcileErr)
			return tok, nil
		}
		if changed {
			if err := s.externalCredentials.UpsertCredential(ctx, tok); err != nil {
				slog.WarnContext(ctx, "canonical duplicate reconciliation deferred", "subject", tok.Subject, "audience", tok.Audience, "error", err)
				return tok, nil
			}
		}
		s.deleteCanonicalDuplicates(ctx, tok, postDuplicates)
	}
	return tok, nil
}

func (s *Server) reconcileCanonicalAccountCredential(ctx context.Context, candidate *core.ExternalCredential) ([]string, bool, error) {
	if s == nil || candidate == nil {
		return nil, false, nil
	}
	accountKey := accountKeyStoredInMetadataJSON(candidate.MetadataJSON)
	legacyCandidateKey := legacyAccountKeyFromMetadataJSON(candidate.MetadataJSON)
	if accountKey == "" || strings.TrimSpace(candidate.Subject) == "" || strings.TrimSpace(candidate.Audience) == "" {
		return nil, false, nil
	}
	credentials, err := s.externalCredentials.ListCredentials(ctx, candidate.Subject, candidate.Audience)
	if err != nil {
		return nil, false, fmt.Errorf("list credentials for canonical account merge: %w", err)
	}
	matches := []*core.ExternalCredential{candidate}
	for _, credential := range credentials {
		if credential == nil || credential.ID == candidate.ID {
			continue
		}
		credentialKey := credentialCanonicalAccountKey(credential)
		if credentialKey == accountKey ||
			(credentialKey == "" && legacyCandidateKey != "" && legacyAccountKeyFromMetadataJSON(credential.MetadataJSON) == legacyCandidateKey) {
			matches = append(matches, credential)
		}
	}

	preferred := s.preferredInstanceForConnection(ctx, candidate.Subject, candidate.Audience)
	keep := chooseCanonicalCredential(matches, preferred)
	previousID := candidate.ID
	previousQualifier := candidate.Qualifier
	previousCreatedAt := candidate.CreatedAt
	if keep == nil {
		return nil, false, nil
	}
	candidate.ID = keep.ID
	candidate.Qualifier = keep.Qualifier
	candidate.CreatedAt = keep.CreatedAt

	duplicates := make([]string, 0, len(matches)-1)
	for _, duplicate := range matches {
		if duplicate.ID == keep.ID || strings.TrimSpace(duplicate.ID) == "" {
			continue
		}
		duplicates = append(duplicates, duplicate.ID)
	}
	return duplicates, previousID != candidate.ID || previousQualifier != candidate.Qualifier || !previousCreatedAt.Equal(candidate.CreatedAt), nil
}

func (s *Server) deleteCanonicalDuplicates(ctx context.Context, candidate *core.ExternalCredential, duplicateIDs []string) {
	if s == nil || candidate == nil {
		return
	}
	for _, duplicateID := range duplicateIDs {
		if err := s.externalCredentials.DeleteCredential(ctx, duplicateID); err != nil {
			slog.WarnContext(ctx, "canonical duplicate cleanup deferred", "credential_id", duplicateID, "subject", candidate.Subject, "audience", candidate.Audience, "error", err)
		}
	}
}

func credentialCanonicalAccountKey(credential *core.ExternalCredential) string {
	if credential == nil {
		return ""
	}
	return accountKeyStoredInMetadataJSON(credential.MetadataJSON)
}

func legacyAccountKeyFromMetadataJSON(metadataJSON string) string {
	identity, err := parseAccountIdentity(metadataJSON)
	if err != nil {
		return ""
	}
	return accountKeyFromIdentity(identity)
}

func chooseCanonicalCredential(credentials []*core.ExternalCredential, preferred string) *core.ExternalCredential {
	var keep *core.ExternalCredential
	for _, credential := range credentials {
		if credential == nil {
			continue
		}
		if keep == nil || credential.Qualifier == preferred ||
			(keep.Qualifier != preferred && credential.CreatedAt.Before(keep.CreatedAt)) ||
			(keep.Qualifier != preferred && credential.CreatedAt.Equal(keep.CreatedAt) && credential.ID < keep.ID) {
			keep = credential
		}
	}
	return keep
}

func validateProviderMetadata(source string, metadata map[string]string) error {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "provider"
	}
	for k, v := range metadata {
		if k == accountIdentityMetadataKey || k == accountKeyMetadataKey {
			return fmt.Errorf("%s must not set reserved metadata key %q", source, k)
		}
		if !safeParamValue.MatchString(k) || !safeTokenResponseValue.MatchString(v) {
			return fmt.Errorf("%s returned invalid key or value for %q", source, k)
		}
	}
	return nil
}

func validateDiscoveryMetadata(metadata map[string]string) error {
	return validateProviderMetadata("discovery", metadata)
}

func mergeMetadataJSON(existing string, extra map[string]string) (string, error) {
	m := make(map[string]string)
	if existing != "" {
		if err := json.Unmarshal([]byte(existing), &m); err != nil {
			return "", fmt.Errorf("corrupt MetadataJSON: %w", err)
		}
	}
	for k, v := range extra {
		if k == accountIdentityMetadataKey || k == accountKeyMetadataKey {
			// Host-owned; never accept from provider/discovery overlays.
			continue
		}
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshal merged metadata: %w", err)
	}
	return string(b), nil
}

func (s *Server) runConnectionSetup(ctx context.Context, prov core.Provider, tm credentialMaterial, discoveryToken string) (*connectionSetupResult, error) {
	if cfg := prov.DiscoveryConfig(); cfg != nil {
		client := &http.Client{
			Timeout:   30 * time.Second,
			Transport: &bearerTransport{token: discoveryToken, base: http.DefaultTransport},
		}
		candidates, err := runDiscovery(ctx, cfg, client)
		if err != nil {
			return nil, fmt.Errorf("discovery: %w", err)
		}
		if len(candidates) == 0 {
			return nil, fmt.Errorf("no resources discovered")
		}
		if len(candidates) == 1 {
			if err := validateDiscoveryMetadata(candidates[0].Metadata); err != nil {
				return nil, err
			}
			merged, err := mergeMetadataJSON(tm.MetadataJSON, candidates[0].Metadata)
			if err != nil {
				return nil, err
			}
			merged, err = mergeDiscoveryCandidateIdentity(merged, candidates[0].Name)
			if err != nil {
				return nil, err
			}
			tm.MetadataJSON = merged
			return s.completeConnection(ctx, prov, tm)
		}

		pendingToken, err := s.encodePendingConnectionToken(tm, candidates)
		if err != nil {
			return nil, fmt.Errorf("encode pending connection: %w", err)
		}
		return &connectionSetupResult{
			Status:       "selection_required",
			Integration:  tm.Integration,
			SelectionURL: pendingConnectionPath,
			PendingToken: pendingToken,
			Candidates:   discoveryCandidateInfos(candidates),
		}, nil
	}

	return s.completeConnection(ctx, prov, tm)
}

func (s *Server) completeConnection(ctx context.Context, prov core.Provider, tm credentialMaterial) (*connectionSetupResult, error) {
	enriched := s.enrichAccountIdentity(ctx, tm)
	if err := s.ensureAppAccessDefaults(ctx, enriched, prov); err != nil {
		return nil, err
	}
	stored, err := s.storeCredentialFromMaterial(ctx, enriched)
	if err != nil {
		return nil, err
	}
	s.maybeSetDefaultInstancePreference(ctx, enriched.SubjectID, enriched.Integration, enriched.Connection, stored.Qualifier)
	return &connectionSetupResult{Status: "connected", Integration: enriched.Integration}, nil
}

func (s *Server) ensureAppAccessDefaults(ctx context.Context, tm credentialMaterial, prov core.Provider) error {
	if s == nil || s.appAccessProfiles == nil || prov == nil {
		return nil
	}
	credentialPrincipal := principalForCredentialMaterial(nil, tm)
	if credentialPrincipal == nil || principal.IsNonUserPrincipal(credentialPrincipal) {
		return nil
	}
	if principal.ClassifyUserSubjectID(credentialPrincipal.SubjectID) == principal.UserSubjectFormOpaque {
		// Account identity can bridge a provider-opaque subject to the
		// persisted user, but it must not replace a meaningful user subject.
		// The connected account may belong to someone else than the Gestalt
		// user who owns this credential.
		if identity := identityFromMetadataJSON(tm.MetadataJSON); identity != nil {
			for _, fact := range identity.Facts {
				if fact.Kind == "email" {
					clone := *credentialPrincipal
					clone.Identity = &core.UserIdentity{Email: fact.Value}
					credentialPrincipal = principal.Canonicalize(&clone)
					break
				}
			}
		}
	}
	subjectID, err := principal.ResolveAuthorizationSubjectID(ctx, s.credentialUserResolver(), credentialPrincipal)
	if err != nil {
		if errors.Is(err, principal.ErrOpaqueCredentialSubject) {
			// A legacy/local subject without a canonical user identity cannot
			// safely own a persisted profile; leave it uninitialized so the
			// connection itself remains usable.
			return nil
		}
		return fmt.Errorf("resolve app access subject: %w", err)
	}
	app := strings.TrimSpace(tm.Integration)
	if app == "" {
		return nil
	}
	staticCat := s.publicCatalog(app, prov, prov.Catalog())
	if core.SupportsSessionCatalog(prov) {
		// Session catalogs are resolved with the connected user's credential at
		// request time. Do not turn an empty static catalog into a permanent
		// empty allow list before those operations are discoverable.
		return nil
	}
	cat := appAccessCapabilityCatalog(prov, staticCat)
	_, err = s.appAccessProfiles.EnsureAppAccessDefaults(ctx, subjectID, app, defaultAppAccessOperationsForProvider(prov, cat))
	return err
}

func manualConnectionAllowed(conn config.ConnectionDef) bool {
	return authTypesContain(connectionAuthTypes(conn.Auth, nil), "manual")
}

type configuredConnectionInfo struct {
	Def       config.ConnectionDef
	Mode      core.ConnectionMode
	ParamDefs map[string]core.ConnectionParamDef
}

func (s *Server) configuredConnectionInfo(integration, connection string) (configuredConnectionInfo, error) {
	conn, ok := s.effectiveConnectionDef(integration, connection)
	if !ok {
		return configuredConnectionInfo{}, fmt.Errorf("connection %q is not configured for integration %q", connection, integration)
	}
	return configuredConnectionInfo{
		Def:       conn,
		Mode:      config.ConnectionModeForConnection(conn),
		ParamDefs: connectionParamDefsFromConfig(conn.ConnectionParams),
	}, nil
}

func connectionParamDefsFromConfig(defs map[string]config.ConnectionParamDef) map[string]core.ConnectionParamDef {
	if len(defs) == 0 {
		return nil
	}
	out := make(map[string]core.ConnectionParamDef, len(defs))
	for name, def := range defs {
		out[name] = core.ConnectionParamDef{
			Required:    def.Required,
			Description: def.Description,
			Default:     def.Default,
			From:        def.From,
			Field:       def.Field,
		}
	}
	return out
}

func (s *Server) requireConfiguredConnection(w http.ResponseWriter, integration, connection string) (configuredConnectionInfo, bool) {
	info, err := s.configuredConnectionInfo(integration, connection)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return configuredConnectionInfo{}, false
	}
	return info, true
}

func discoveryCandidateInfos(candidates []core.DiscoveryCandidate) []discoveryCandidateInfo {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]discoveryCandidateInfo, len(candidates))
	for i, candidate := range candidates {
		out[i] = discoveryCandidateInfo{
			ID:   candidate.ID,
			Name: candidate.Name,
		}
	}
	return out
}

// buildEffectiveManualCredential returns the named credential fields (stored
// as an Opaque credential) or the single unstructured secret (stored as a
// bearer Grant); exactly one of the two is non-empty on success.
func buildEffectiveManualCredential(req connectManualRequest, auth config.ConnectionAuthDef, tokenExchange bool) (map[string]string, string, error) {
	if tokenExchange {
		if req.Credential != "" {
			return nil, "", errors.New("manual token exchange requires named credentials")
		}
		if len(auth.Credentials) == 0 {
			return nil, "", errors.New("manual token exchange requires declared credentials")
		}
		fields, err := declaredManualCredentials(req.Credentials, auth.Credentials)
		return fields, "", err
	}

	if len(req.Credentials) > 0 {
		if err := validateManualCredentialValues(req.Credentials); err != nil {
			return nil, "", err
		}
		return req.Credentials, "", nil
	}

	structured := auth.AuthMapping != nil && (len(auth.AuthMapping.Headers) > 0 || auth.AuthMapping.Basic != nil)
	if !structured {
		return nil, req.Credential, nil
	}

	switch {
	case req.Credential != "" && len(auth.Credentials) == 1:
		fields := map[string]string{auth.Credentials[0].Name: req.Credential}
		if err := validateManualCredentialValues(fields); err != nil {
			return nil, "", err
		}
		return fields, "", nil
	case req.Credential != "":
		return nil, "", errors.New("manual connection requires named credentials")
	}
	return nil, "", nil
}

func declaredManualCredentials(provided map[string]string, fields []config.CredentialFieldDef) (map[string]string, error) {
	declared := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if field.Name != "" {
			declared[field.Name] = struct{}{}
		}
	}
	if len(declared) == 0 {
		return nil, errors.New("manual token exchange requires declared credentials")
	}
	for name := range provided {
		if _, ok := declared[name]; !ok {
			return nil, fmt.Errorf("unknown credential: %s", name)
		}
	}
	for _, field := range fields {
		if field.Name == "" {
			continue
		}
		if _, ok := provided[field.Name]; !ok {
			return nil, fmt.Errorf("missing required credential: %s", field.Name)
		}
	}
	if err := validateManualCredentialValues(provided); err != nil {
		return nil, err
	}
	return provided, nil
}

func validateManualCredentialValues(creds map[string]string) error {
	for name, value := range creds {
		if value == "" {
			return fmt.Errorf("credential %q must not be empty", name)
		}
	}
	return nil
}

func marshalManualCredentials(creds map[string]string) (string, error) {
	if len(creds) == 0 {
		return "", nil
	}
	if err := validateManualCredentialValues(creds); err != nil {
		return "", err
	}
	data, err := json.Marshal(creds)
	if err != nil {
		return "", errors.New("invalid credentials map")
	}
	return string(data), nil
}
