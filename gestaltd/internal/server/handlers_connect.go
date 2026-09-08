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
	CredentialID     string            `json:"credentialId,omitempty"`
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
	expectedCredentialID, err := s.validateRequestedCredentialID(r.Context(), subjectID, credentialAudience(req.Integration, manualConnection, conn.ConnectionID), manualInstance, req.CredentialID)
	if err != nil {
		auditErr = err
		status, message := connectionSetupFailure(err)
		writeError(w, status, message)
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
		SubjectID:            subjectID,
		AuthSource:           authSource,
		ConnectionID:         conn.ConnectionID,
		Integration:          req.Integration,
		Connection:           manualConnection,
		Instance:             manualInstance,
		Fields:               fields,
		AccessToken:          rawCredential,
		MetadataJSON:         manualMeta,
		ProviderAccountID:    providerAccountIDFromTokenResponse(selected.ParamDefs, tokenResp),
		ExpectedCredentialID: expectedCredentialID,
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
		status, message := connectionSetupFailure(err)
		auditErr = errors.New(message)
		slog.ErrorContext(r.Context(), "connection setup failed", "provider", req.Integration, "error", err)
		writeError(w, status, message)
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
	for name := range defs {
		if isHostOwnedMetadataKey(name) {
			return "", fmt.Errorf("connection parameter %q is reserved for host-owned account metadata", name)
		}
	}
	for k, v := range userParams {
		if isHostOwnedMetadataKey(k) {
			return "", fmt.Errorf("connection parameter %q is reserved for host-owned account metadata", k)
		}
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

func isHostOwnedMetadataKey(key string) bool {
	key = strings.TrimSpace(key)
	return key == accountIdentityMetadataKey || key == accountKeyMetadataKey
}

// providerAccountIDFromTokenResponse extracts the provider-owned identifier
// from a connection parameter explicitly marked accountIdentity. The field
// may map to a nested token-response value such as account.id. The name-based
// fallback exists only for manifests written before accountIdentity was added.
func providerAccountIDFromTokenResponse(defs map[string]core.ConnectionParamDef, tokenResp *core.OAuthTokenResponse) string {
	if tokenResp == nil || len(tokenResp.Extra) == 0 {
		return ""
	}
	var names []string
	for name, def := range defs {
		if def.From == "token_response" && def.AccountIdentity {
			names = append(names, name)
		}
	}
	// Keep existing manifests working while they migrate to the explicit
	// accountIdentity declaration. New manifests must declare the role rather
	// than relying on a parameter name.
	if len(names) == 0 {
		for name, def := range defs {
			if def.From == "token_response" && isProviderAccountIDParam(name) {
				names = append(names, name)
			}
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
	AccountKey        string
	ProviderAccountID string
	ActorSubjectID    string
	ActorUserID       string
	ActorAuthSource   string
	// ExpectedCredentialID is set only for an explicit reconnect. It prevents
	// an identity lookup failure from authorizing an update to a different
	// credential that happens to use the same display label.
	ExpectedCredentialID string `json:"expectedCredentialId,omitempty"`
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
	Status           string                   `json:"status"`
	Integration      string                   `json:"integration,omitempty"`
	AlreadyConnected bool                     `json:"alreadyConnected,omitempty"`
	SelectionURL     string                   `json:"selectionUrl,omitempty"`
	PendingToken     string                   `json:"pendingToken,omitempty"`
	Candidates       []discoveryCandidateInfo `json:"candidates,omitempty"`
}

type discoveryCandidateInfo struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

func (s *Server) storeCredentialFromMaterial(ctx context.Context, tm credentialMaterial) (*core.ExternalCredential, error) {
	now := s.now().UTC().Truncate(time.Second)
	audience := credentialAudience(tm.Integration, tm.Connection, tm.ConnectionID)
	tok := &core.ExternalCredential{
		ID:           uuid.NewString(),
		Subject:      tm.SubjectID,
		Audience:     audience,
		Qualifier:    tm.Instance,
		AccountKey:   strings.TrimSpace(tm.AccountKey),
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
	if err := s.normalizeAccountKeyForStorage(tok); err != nil {
		return nil, err
	}
	if err := s.storeCredentialAtInstance(ctx, tok, tm.ExpectedCredentialID); err != nil {
		return nil, err
	}
	return tok, nil
}

// storeCredentialAtInstance makes the exact (subject, audience, qualifier)
// record the provider's concurrency boundary. AccountKey is only used to
// validate an existing record or upgrade a legacy keyless record; it never
// triggers a list-and-delete operation across other qualifiers.
func (s *Server) storeCredentialAtInstance(ctx context.Context, candidate *core.ExternalCredential, expectedCredentialID string) error {
	expectedCredentialID = strings.TrimSpace(expectedCredentialID)
	existing, err := s.externalCredentials.GetCredential(ctx, candidate.Subject, candidate.Audience, candidate.Qualifier)
	if err == nil {
		if expectedCredentialID != "" && existing.ID != expectedCredentialID {
			return credentialTargetMismatch(candidate.Qualifier)
		}
		if err := s.upsertCredentialAtInstance(ctx, candidate, existing, expectedCredentialID); err != nil {
			return err
		}
		return nil
	}
	if !errors.Is(err, core.ErrNotFound) {
		return fmt.Errorf("get credential at instance %q: %w", candidate.Qualifier, err)
	}
	if expectedCredentialID != "" {
		return credentialTargetMismatch(candidate.Qualifier)
	}
	if err := s.externalCredentials.CreateCredential(ctx, candidate); err == nil {
		return nil
	} else if !errors.Is(err, core.ErrAlreadyExists) {
		return err
	}

	// A different server won the insert between Get and Create. Read the
	// provider-owned record and apply the same account-ownership rule.
	existing, err = s.externalCredentials.GetCredential(ctx, candidate.Subject, candidate.Audience, candidate.Qualifier)
	if err != nil {
		return fmt.Errorf("read credential after instance conflict: %w", err)
	}
	if expectedCredentialID != "" && existing.ID != expectedCredentialID {
		return credentialTargetMismatch(candidate.Qualifier)
	}
	if err := s.upsertCredentialAtInstance(ctx, candidate, existing, expectedCredentialID); err != nil {
		return err
	}
	return nil
}

func credentialAudience(integration, connection, connectionID string) string {
	if audience := strings.TrimSpace(connectionID); audience != "" {
		return audience
	}
	return strings.TrimSpace(integration) + ":" + strings.TrimSpace(connection)
}

func (s *Server) upsertCredentialAtInstance(ctx context.Context, candidate, existing *core.ExternalCredential, expectedCredentialID string) error {
	existingKey := core.AccountKeyForCredential(existing)
	candidateKey := core.AccountKeyForCredential(candidate)
	if existingKey != "" && candidateKey == "" {
		if strings.TrimSpace(expectedCredentialID) == "" || strings.TrimSpace(expectedCredentialID) != existing.ID {
			return credentialOwnershipUnknown(candidate.Qualifier)
		}
		// An explicit reconnect may retain the existing key when the identity
		// probe cannot provide one. The credential ID is the ownership proof.
		candidate.AccountKey = existingKey
		candidateKey = existingKey
		if !core.ExternalCredentialProviderPersistsAccountKey(s.externalCredentials) {
			metadata, err := setAccountKeyMetadata(candidate.MetadataJSON, existingKey)
			if err != nil {
				return fmt.Errorf("persist account key compatibility metadata: %w", err)
			}
			candidate.MetadataJSON = metadata
		}
	}
	if existingKey != "" && existingKey != candidateKey {
		return &core.CredentialInstanceConflictError{Instance: candidate.Qualifier, DifferentAccount: true}
	}
	if err := s.normalizeAccountKeyForStorage(candidate); err != nil {
		return err
	}

	candidate.ID = existing.ID
	candidate.CreatedAt = existing.CreatedAt
	if expectedCredentialID != "" {
		conditional, ok := s.externalCredentials.(core.ExternalCredentialConditionalUpserter)
		if !ok {
			return core.ErrConditionalUpsertUnsupported
		}
		if err := conditional.UpsertCredentialIfID(ctx, candidate, expectedCredentialID); err != nil {
			if errors.Is(err, core.ErrAlreadyExists) {
				return credentialTargetMismatch(candidate.Qualifier)
			}
			return err
		}
		return nil
	}
	return s.externalCredentials.UpsertCredential(ctx, candidate)
}

// normalizeAccountKeyForStorage applies the one storage-boundary policy for
// typed account keys and the compatibility metadata used by legacy providers.
// Keeping this after ownership validation prevents a compatibility write from
// becoming an independent read-after-write repair operation.
func (s *Server) normalizeAccountKeyForStorage(credential *core.ExternalCredential) error {
	accountKey := core.AccountKeyForCredential(credential)
	credential.AccountKey = accountKey
	if accountKey == "" {
		return nil
	}
	if core.ExternalCredentialProviderPersistsAccountKey(s.externalCredentials) {
		if cleaned, err := removeAccountKeyMetadata(credential.MetadataJSON); err == nil {
			credential.MetadataJSON = cleaned
		}
		return nil
	}
	metadata, err := setAccountKeyMetadata(credential.MetadataJSON, accountKey)
	if err != nil {
		return fmt.Errorf("persist account key compatibility metadata: %w", err)
	}
	credential.MetadataJSON = metadata
	return nil
}

type credentialConflictReason uint8

const (
	credentialConflictTargetMismatch credentialConflictReason = iota + 1
	credentialConflictOwnershipUnknown
)

type credentialConflictError struct {
	Instance string
	Reason   credentialConflictReason
}

func (e *credentialConflictError) Error() string {
	if e == nil {
		return core.ErrAlreadyExists.Error()
	}
	if e.Reason == credentialConflictOwnershipUnknown {
		return "could not verify credential ownership for instance " + e.Instance
	}
	return "credential target no longer matches instance " + e.Instance
}

func (e *credentialConflictError) Unwrap() error {
	return core.ErrAlreadyExists
}

func credentialTargetMismatch(instance string) error {
	return &credentialConflictError{Instance: instance, Reason: credentialConflictTargetMismatch}
}

func credentialOwnershipUnknown(instance string) error {
	return &credentialConflictError{Instance: instance, Reason: credentialConflictOwnershipUnknown}
}

func (s *Server) validateRequestedCredentialID(ctx context.Context, subjectID, audience, instance, requestedID string) (string, error) {
	requestedID = strings.TrimSpace(requestedID)
	if requestedID == "" {
		return "", nil
	}
	credential, err := s.externalCredentials.GetCredential(ctx, subjectID, audience, instance)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return "", credentialTargetMismatch(instance)
		}
		return "", fmt.Errorf("validate requested credential: %w", err)
	}
	if credential == nil || credential.ID != requestedID {
		return "", credentialTargetMismatch(instance)
	}
	return requestedID, nil
}

func connectionSetupFailure(err error) (int, string) {
	var credentialConflict *credentialConflictError
	if errors.As(err, &credentialConflict) {
		if credentialConflict.Reason == credentialConflictOwnershipUnknown {
			return http.StatusConflict, fmt.Sprintf("Gestalt could not verify account ownership for instance %q. Refresh and try again.", credentialConflict.Instance)
		}
		return http.StatusConflict, fmt.Sprintf("The connection changed before instance %q could be updated. Refresh and try again.", credentialConflict.Instance)
	}
	var conflict *core.CredentialInstanceConflictError
	if errors.As(err, &conflict) {
		if conflict.DifferentAccount {
			return http.StatusConflict, fmt.Sprintf("The instance name %q is already linked to another account. Choose a different instance name and try again.", conflict.Instance)
		}
		return http.StatusConflict, fmt.Sprintf("The instance name %q is already in use. Choose a different instance name and try again.", conflict.Instance)
	}
	return http.StatusBadGateway, "connection setup failed"
}

func validateProviderMetadata(source string, metadata map[string]string) error {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "provider"
	}
	for k, v := range metadata {
		if isHostOwnedMetadataKey(k) {
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
		if isHostOwnedMetadataKey(k) {
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
	alreadyConnected := s.accountAlreadyConnected(ctx, enriched)
	if err := s.ensureAppAccessDefaults(ctx, enriched, prov); err != nil {
		return nil, err
	}
	stored, err := s.storeCredentialFromMaterial(ctx, enriched)
	if err != nil {
		return nil, err
	}
	s.maybeSetDefaultInstancePreference(ctx, enriched.SubjectID, enriched.Integration, enriched.Connection, stored.Qualifier)
	return &connectionSetupResult{
		Status:           "connected",
		Integration:      enriched.Integration,
		AlreadyConnected: alreadyConnected,
	}, nil
}

// accountAlreadyConnected reports whether the provider account was already
// linked before this connection attempt. It is presentation metadata only: a
// failed lookup must not turn a successful connection into an error.
func (s *Server) accountAlreadyConnected(ctx context.Context, tm credentialMaterial) bool {
	accountKey := strings.TrimSpace(tm.AccountKey)
	if accountKey == "" {
		return false
	}
	audience := credentialAudience(tm.Integration, tm.Connection, tm.ConnectionID)
	credentials, err := s.externalCredentials.ListCredentials(ctx, tm.SubjectID, audience)
	if err != nil {
		slog.WarnContext(ctx, "could not determine whether account was already connected", "integration", tm.Integration, "error", err)
		return false
	}
	for _, credential := range credentials {
		if core.AccountKeyForCredential(credential) == accountKey {
			return true
		}
	}
	return false
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
	paramDefs := connectionParamDefsFromConfig(conn.ConnectionParams)
	if err := core.ValidateConnectionParamDefs(paramDefs); err != nil {
		return configuredConnectionInfo{}, fmt.Errorf("invalid connection parameters: %w", err)
	}
	return configuredConnectionInfo{
		Def:       conn,
		Mode:      config.ConnectionModeForConnection(conn),
		ParamDefs: paramDefs,
	}, nil
}

func connectionParamDefsFromConfig(defs map[string]config.ConnectionParamDef) map[string]core.ConnectionParamDef {
	if len(defs) == 0 {
		return nil
	}
	out := make(map[string]core.ConnectionParamDef, len(defs))
	for name, def := range defs {
		out[name] = core.ConnectionParamDef{
			Required:        def.Required,
			Description:     def.Description,
			Default:         def.Default,
			From:            def.From,
			Field:           def.Field,
			AccountIdentity: def.AccountIdentity,
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
		// The UI historically sends a single credential as the raw
		// `credential` field. Preserve that API shape when the token exchange
		// declares exactly one named field, while keeping multi-field exchanges
		// explicit and unambiguous.
		if req.Credential != "" && len(auth.Credentials) == 1 {
			fields := map[string]string{auth.Credentials[0].Name: req.Credential}
			if err := validateManualCredentialValues(fields); err != nil {
				return nil, "", err
			}
			return fields, "", nil
		}
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
