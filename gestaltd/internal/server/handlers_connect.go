package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/discovery"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

var errManagedIdentityIntegrationNotFound = errors.New("managed identity integration not found")

type connectManualRequest struct {
	Integration      string            `json:"integration"`
	Connection       string            `json:"connection"`
	Instance         string            `json:"instance"`
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
	if p := PrincipalFromContext(r.Context()); p != nil && p.Kind == principal.KindWorkload {
		auditErr = errWorkloadForbidden
		writeError(w, http.StatusForbidden, "workload callers are not allowed on this route")
		return
	}

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

	auth := s.effectiveConnectionAuth(req.Integration, manualConnection)
	if !manualConnectionAllowed(prov, auth) {
		auditErr = errors.New("integration does not support manual auth")
		writeError(w, http.StatusBadRequest, fmt.Sprintf("integration %q does not support manual auth; use OAuth connect instead", req.Integration))
		return
	}

	dbUserID, manualInstance, err := s.resolveUserConnectionSetup(w, r, req.Instance)
	if err != nil {
		auditErr = err
		return
	}
	auditTarget = connectionAuditTarget(req.Integration, manualConnection, manualInstance)

	effectiveCredential, credErr := buildEffectiveManualCredential(req, auth)
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

	connParams, ok := resolveConnectionParams(w, prov, req.ConnectionParams)
	if !ok {
		auditErr = errors.New("invalid connection parameters")
		return
	}

	manualMeta, metaErr := buildConnectionMetadata(prov, connParams, nil)
	if metaErr != nil {
		auditErr = errors.New(metaErr.Error())
		writeError(w, http.StatusBadRequest, metaErr.Error())
		return
	}

	authSource := ""
	if p := PrincipalFromContext(r.Context()); p != nil {
		authSource = p.AuthSource()
	}
	tm := tokenMaterial{
		OwnerKind:       core.IntegrationTokenOwnerKindUser,
		OwnerID:         dbUserID,
		InitiatorUserID: dbUserID,
		AuthSource:      authSource,
		Integration:     req.Integration,
		Connection:      manualConnection,
		Instance:        manualInstance,
		AccessToken:     effectiveCredential,
		MetadataJSON:    manualMeta,
	}

	result, err := s.runPostConnect(r.Context(), prov, tm)
	if err != nil {
		auditErr = errors.New("connection setup failed")
		slog.ErrorContext(r.Context(), "post_connect failed", "provider", req.Integration, "error", err)
		writeError(w, http.StatusBadGateway, "connection setup failed")
		return
	}

	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, result)
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

func (s *Server) resolveUserConnectionSetup(w http.ResponseWriter, r *http.Request, requestedInstance string) (string, string, error) {
	userID, err := s.resolveUserID(w, r)
	if err != nil {
		return "", "", err
	}
	instance, ok := resolveRequestedInstance(w, requestedInstance)
	if !ok {
		return "", "", errors.New("invalid instance")
	}
	return userID, instance, nil
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

func buildConnectionMetadata(prov core.Provider, userParams map[string]string, tokenResp *core.TokenResponse) (string, error) {
	metadata := make(map[string]string)
	for k, v := range userParams {
		metadata[k] = v
	}

	if defs := prov.ConnectionParamDefs(); tokenResp != nil && tokenResp.Extra != nil {
		for name, def := range defs {
			if def.From == "token_response" {
				field := def.Field
				if field == "" {
					field = name
				}
				val, ok := tokenResp.Extra[field]
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

type tokenMaterial struct {
	OwnerKind       string
	OwnerID         string
	InitiatorUserID string
	LegacyUserID    string `json:"UserID,omitempty"`
	AuthSource      string
	Integration     string
	Connection      string
	Instance        string
	AccessToken     string
	RefreshToken    string
	TokenExpiresAt  *time.Time
	MetadataJSON    string
}

type postConnectResult struct {
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

func (s *Server) storeTokenFromMaterial(ctx context.Context, tm tokenMaterial) (*core.IntegrationToken, error) {
	if tm.OwnerKind == core.IntegrationTokenOwnerKindManagedIdentity {
		s.managedIdentityMu.Lock()
		defer s.managedIdentityMu.Unlock()
		if err := s.validateManagedIdentityConnectionWrite(ctx, tm); err != nil {
			return nil, err
		}
	}

	now := s.now().UTC().Truncate(time.Second)
	tok := &core.IntegrationToken{
		ID:              uuid.NewString(),
		UserID:          core.IntegrationTokenStoredUserID(tm.OwnerKind, tm.OwnerID),
		OwnerKind:       tm.OwnerKind,
		OwnerID:         tm.OwnerID,
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
	if err := s.tokens.StoreToken(ctx, tok); err != nil {
		return nil, err
	}
	return tok, nil
}

func (s *Server) validateManagedIdentityConnectionWrite(ctx context.Context, tm tokenMaterial) error {
	if tm.OwnerID == "" {
		return core.ErrNotFound
	}
	if _, err := s.managedIdentityActor(ctx, tm.OwnerID, tm.InitiatorUserID, managedIdentityRoleEditor); err != nil {
		return err
	}
	if !s.managedIdentityGrantPluginVisible(tm.Integration, PrincipalFromContext(ctx)) {
		return errManagedIdentityIntegrationNotFound
	}
	return nil
}

func (s *Server) writeManagedIdentityConnectionWriteError(w http.ResponseWriter, integration string, err error) bool {
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeError(w, http.StatusNotFound, "identity not found")
	case errors.Is(err, errManagedIdentityAccessDenied):
		writeError(w, http.StatusForbidden, "identity access denied")
	case errors.Is(err, errManagedIdentityIntegrationNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", integration))
	default:
		return false
	}
	return true
}

func validateDiscoveryMetadata(metadata map[string]string) error {
	for k, v := range metadata {
		if !safeParamValue.MatchString(k) || !safeTokenResponseValue.MatchString(v) {
			return fmt.Errorf("discovery returned invalid key or value for %q", k)
		}
	}
	return nil
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

func (s *Server) runPostConnect(ctx context.Context, prov core.Provider, tm tokenMaterial) (*postConnectResult, error) {
	if cfg := prov.DiscoveryConfig(); cfg != nil {
		client := &http.Client{
			Timeout:   30 * time.Second,
			Transport: &bearerTransport{token: tm.AccessToken, base: http.DefaultTransport},
		}
		candidates, err := discovery.Run(ctx, cfg, client)
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
			if _, err := s.storeTokenFromMaterial(ctx, tm); err != nil {
				return nil, err
			}
			return &postConnectResult{Status: "connected", Integration: tm.Integration}, nil
		}

		pendingToken, err := s.encodePendingConnectionToken(tm, candidates)
		if err != nil {
			return nil, fmt.Errorf("encode pending connection: %w", err)
		}
		return &postConnectResult{
			Status:       "selection_required",
			Integration:  tm.Integration,
			SelectionURL: pendingConnectionPath,
			PendingToken: pendingToken,
			Candidates:   discoveryCandidateInfos(candidates),
		}, nil
	}

	if _, err := s.storeTokenFromMaterial(ctx, tm); err != nil {
		return nil, err
	}
	return &postConnectResult{Status: "connected", Integration: tm.Integration}, nil
}

func manualConnectionAllowed(prov core.Provider, auth config.ConnectionAuthDef) bool {
	if authTypesContain(connectionAuthTypes(auth, nil), "manual") {
		return true
	}
	return authTypesContain(userFacingAuthTypes(prov.AuthTypes()), "manual")
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

func (s *Server) effectiveConnectionAuth(integration, connection string) config.ConnectionAuthDef {
	entry, ok := s.pluginDefs[integration]
	if !ok || entry == nil {
		return config.ConnectionAuthDef{}
	}
	plan, err := config.BuildStaticConnectionPlan(entry, entry.ManifestSpec())
	if err != nil {
		return config.ConnectionAuthDef{}
	}
	conn, ok := plan.LookupConnection(connection)
	if !ok {
		return config.ConnectionAuthDef{}
	}
	return conn.Auth
}

func buildEffectiveManualCredential(req connectManualRequest, auth config.ConnectionAuthDef) (string, error) {
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
