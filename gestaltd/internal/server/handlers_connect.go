package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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

	prov, manualConnection, err := s.resolveConnectionProvider(w, req.Integration, req.Connection)
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

	effectiveCredential, credErr := buildEffectiveManualCredential(req, auth, tokenExchange)
	if credErr != nil {
		auditErr = credErr
		writeError(w, http.StatusBadRequest, credErr.Error())
		return
	}
	if effectiveCredential == "" {
		auditErr = errors.New("credential is required")
		writeError(w, http.StatusBadRequest, "credential is required")
		return
	}

	connParams, ok := resolveConnectionParams(w, selected.ParamDefs, req.ConnectionParams)
	if !ok {
		auditErr = errors.New("invalid connection parameters")
		return
	}

	tokenResp, exchangeErr := s.exchangeManualCredential(r.Context(), req, manualConnection, conn.ConnectionID, subjectID, manualInstance, auth, effectiveCredential, connParams, tokenExchange)
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
		SubjectID:    subjectID,
		AuthSource:   authSource,
		ConnectionID: conn.ConnectionID,
		Integration:  req.Integration,
		Connection:   manualConnection,
		Instance:     manualInstance,
		AccessToken:  effectiveCredential,
		MetadataJSON: manualMeta,
	}
	if tokenResp != nil {
		tm.AccessToken = tokenResp.AccessToken
		tm.RefreshToken = tokenResp.RefreshToken
		if tokenResp.RefreshToken == "" {
			tm.RefreshToken = effectiveCredential
		}
		if tokenResp.ExpiresIn > 0 {
			t := s.now().UTC().Truncate(time.Second).Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
			tm.TokenExpiresAt = &t
		}
	}
	credentialActorFromPrincipal(p, subjectID).applyTo(&tm)

	result, err := s.runConnectionSetup(r.Context(), prov, tm)
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

func (s *Server) exchangeManualCredential(ctx context.Context, req connectManualRequest, connection, connectionID, subjectID, instance string, auth config.ConnectionAuthDef, credential string, connParams map[string]string, tokenExchange bool) (*core.TokenResponse, error) {
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
	return &core.TokenResponse{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
		TokenType:    tokenResp.TokenType,
		Extra:        tokenResp.Extra,
	}, nil
}

func (s *Server) resolveConnectionProvider(w http.ResponseWriter, integration, requestedConnection string) (core.Provider, string, error) {
	prov, ok := s.getProvider(w, integration)
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

func buildConnectionMetadata(defs map[string]core.ConnectionParamDef, userParams map[string]string, tokenResp *core.TokenResponse) (string, error) {
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
	SubjectID       string
	AuthSource      string
	ConnectionID    string
	Integration     string
	Connection      string
	Instance        string
	AccessToken     string
	RefreshToken    string
	TokenExpiresAt  *time.Time
	MetadataJSON    string
	ActorSubjectID  string
	ActorUserID     string
	ActorAuthSource string
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
	if p != nil {
		p = principal.Canonicalized(p)
		if p != nil {
			p.CredentialSubjectID = strings.TrimSpace(tm.SubjectID)
			return principal.WithPrincipal(ctx, p)
		}
	}
	if strings.TrimSpace(tm.ActorSubjectID) == "" {
		return ctx
	}
	actor := &principal.Principal{
		SubjectID:           strings.TrimSpace(tm.ActorSubjectID),
		UserID:              strings.TrimSpace(tm.ActorUserID),
		CredentialSubjectID: strings.TrimSpace(tm.SubjectID),
	}
	if kind, _, ok := core.ParseSubjectID(actor.SubjectID); ok {
		actor.Kind = principal.Kind(kind)
	}
	return principal.WithPrincipal(ctx, principal.Canonicalize(actor))
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
	connectionID := strings.TrimSpace(tm.ConnectionID)
	if connectionID == "" {
		connectionID = tm.Integration + ":" + tm.Connection
	}
	tok := &core.ExternalCredential{
		ID:              uuid.NewString(),
		SubjectID:       tm.SubjectID,
		ConnectionID:    connectionID,
		Integration:     tm.Integration,
		Connection:      tm.Connection,
		Instance:        tm.Instance,
		AccessToken:     tm.AccessToken,
		RefreshToken:    tm.RefreshToken,
		ExpiresAt:       tm.TokenExpiresAt,
		LastRefreshedAt: &now,
		MetadataJSON:    tm.MetadataJSON,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.externalCredentials.PutCredential(ctx, tok); err != nil {
		return nil, err
	}
	return tok, nil
}

func validateProviderMetadata(source string, metadata map[string]string) error {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "provider"
	}
	for k, v := range metadata {
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
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshal merged metadata: %w", err)
	}
	return string(b), nil
}

func (s *Server) runConnectionSetup(ctx context.Context, prov core.Provider, tm credentialMaterial) (*connectionSetupResult, error) {
	if cfg := prov.DiscoveryConfig(); cfg != nil {
		client := &http.Client{
			Timeout:   30 * time.Second,
			Transport: &bearerTransport{token: tm.AccessToken, base: http.DefaultTransport},
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

func (s *Server) completeConnection(ctx context.Context, _ core.Provider, tm credentialMaterial) (*connectionSetupResult, error) {
	if _, err := s.storeCredentialFromMaterial(ctx, tm); err != nil {
		return nil, err
	}
	return &connectionSetupResult{Status: "connected", Integration: tm.Integration}, nil
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

func buildEffectiveManualCredential(req connectManualRequest, auth config.ConnectionAuthDef, tokenExchange bool) (string, error) {
	if tokenExchange {
		if req.Credential != "" {
			return "", errors.New("manual token exchange requires named credentials")
		}
		if len(auth.Credentials) == 0 {
			return "", errors.New("manual token exchange requires declared credentials")
		}
		return marshalDeclaredManualCredentials(req.Credentials, auth.Credentials)
	}

	if len(req.Credentials) > 0 {
		return marshalManualCredentials(req.Credentials)
	}

	structured := auth.AuthMapping != nil && (len(auth.AuthMapping.Headers) > 0 || auth.AuthMapping.Basic != nil)
	if !structured {
		return req.Credential, nil
	}

	switch {
	case req.Credential != "" && len(auth.Credentials) == 1:
		return marshalManualCredentials(map[string]string{auth.Credentials[0].Name: req.Credential})
	case req.Credential != "":
		return "", errors.New("manual connection requires named credentials")
	}
	return "", nil
}

func marshalDeclaredManualCredentials(provided map[string]string, fields []config.CredentialFieldDef) (string, error) {
	declared := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if field.Name != "" {
			declared[field.Name] = struct{}{}
		}
	}
	if len(declared) == 0 {
		return "", errors.New("manual token exchange requires declared credentials")
	}
	for name := range provided {
		if _, ok := declared[name]; !ok {
			return "", fmt.Errorf("unknown credential: %s", name)
		}
	}
	for _, field := range fields {
		if field.Name == "" {
			continue
		}
		if _, ok := provided[field.Name]; !ok {
			return "", fmt.Errorf("missing required credential: %s", field.Name)
		}
	}
	return marshalManualCredentials(provided)
}

func marshalManualCredentials(creds map[string]string) (string, error) {
	if len(creds) == 0 {
		return "", nil
	}
	for name, value := range creds {
		if value == "" {
			return "", fmt.Errorf("credential %q must not be empty", name)
		}
	}
	data, err := json.Marshal(creds)
	if err != nil {
		return "", errors.New("invalid credentials map")
	}
	return string(data), nil
}
