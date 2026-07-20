package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func (s *Server) validateCreateGrantRequest(req createTokenRequest) (string, error) {
	if strings.TrimSpace(req.Name) == "" {
		return "", errors.New("name is required")
	}
	// Scopes are optional: an empty scope mints a grant that inherits the
	// caller's full scope (token-exchange attenuation), i.e. a token that acts
	// as the signed-in identity. Provided scopes must reference a known app.
	scope := strings.TrimSpace(req.Scopes)
	for _, part := range strings.Fields(scope) {
		appName, _, _ := strings.Cut(part, ":")
		if _, err := s.providers.Get(appName); err != nil {
			return "", fmt.Errorf("unknown scope %q", part)
		}
	}
	return scope, nil
}

type createTokenRequest struct {
	Name      string `json:"name"`
	Scopes    string `json:"scopes"`
	ExpiresIn *int64 `json:"expiresIn,omitempty"`
}

func (s *Server) createAPIToken(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("grant creation failed")
	auditTarget := auditTarget{}
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "grant.create", auditAllowed, auditErr, auditTarget)
	}()

	if err := requireUserCaller(w, PrincipalFromContext(r.Context())); err != nil {
		auditErr = err
		return
	}
	if p := PrincipalFromContext(r.Context()); p != nil {
		enriched, enrichErr := s.resolvePrincipalUserID(r.Context(), p)
		if enrichErr != nil {
			auditErr = errResolveUser
			writeError(w, http.StatusInternalServerError, "failed to resolve user")
			return
		}
		if enriched != nil {
			r = r.WithContext(principal.WithPrincipal(r.Context(), enriched))
		}
	}
	if s.auth == nil {
		auditErr = errors.New("auth is disabled")
		writeError(w, http.StatusNotFound, "auth is disabled")
		return
	}

	var req createTokenRequest
	if err := decodeCreateTokenRequest(r.Body, &req); err != nil {
		auditErr = errors.New("invalid JSON body")
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	auditTarget = apiTokenAuditTarget("", req.Name)

	scope, err := s.validateCreateGrantRequest(req)
	if err != nil {
		auditErr = err
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	expiresIn, err := tokenExpiresIn(req.ExpiresIn)
	if err != nil {
		auditErr = err
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := s.callerAuthContext(r.Context(), r)
	callerToken, err := s.callerBearerToken(r)
	if err != nil {
		auditErr = err
		writeError(w, http.StatusUnauthorized, "caller bearer token required")
		return
	}
	tokenResp, err := s.auth.Token(ctx, &core.TokenRequest{
		GrantType:        core.GrantTypeTokenExchange,
		SubjectToken:     callerToken,
		SubjectTokenType: core.SubjectTokenTypeAccessToken,
		Scope:            scope,
		ClientID:         core.DefaultOAuthClientID,
		ExpiresIn:        expiresIn,
	})
	grantID := ""
	if tokenResp != nil {
		grantID = strings.TrimSpace(tokenResp.GrantID)
	}
	if err != nil || tokenResp == nil || strings.TrimSpace(tokenResp.AccessToken) == "" || grantID == "" {
		auditErr = errors.New("failed to issue grant token")
		writeError(w, http.StatusInternalServerError, "failed to issue grant token")
		return
	}
	auditTarget = apiTokenAuditTarget(grantID, req.Name)

	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusCreated, createGrantResponse{
		ID:        grantID,
		Name:      grantID,
		Token:     tokenResp.AccessToken,
		Scopes:    principal.ParseScopeString(tokenResp.Scope),
		ExpiresAt: tokenExpiresAt(s.now, tokenResp.ExpiresIn),
	})
}

func decodeCreateTokenRequest(r io.Reader, req *createTokenRequest) error {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	return decoder.Decode(req)
}

func (s *Server) revokeAPIToken(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("grant revoke failed")
	id := chi.URLParam(r, "id")
	auditTarget := apiTokenAuditTarget(id, "")
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "grant.revoke", auditAllowed, auditErr, auditTarget)
	}()

	if err := requireUserCaller(w, PrincipalFromContext(r.Context())); err != nil {
		auditErr = err
		return
	}
	if s.auth == nil {
		auditErr = errors.New("auth is disabled")
		writeError(w, http.StatusNotFound, "auth is disabled")
		return
	}

	ctx := s.callerAuthContext(r.Context(), r)
	if _, err := s.auth.RevokeGrant(ctx, &core.RevokeGrantRequest{GrantID: id}); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			auditErr = errors.New("grant not found")
			writeError(w, http.StatusNotFound, "grant not found")
			return
		}
		auditErr = errors.New("failed to revoke grant")
		writeError(w, http.StatusInternalServerError, "failed to revoke grant")
		return
	}
	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) connectionInfosForPlugin(integration string, app *config.ProviderEntry, instances []instanceInfo, integrationAuthTypes []string, p *principal.Principal) []connectionDefInfo {
	if app == nil {
		return []connectionDefInfo{}
	}
	manifestSpec := app.ManifestSpec()
	plan, err := config.BuildStaticConnectionPlan(app, manifestSpec)
	if err != nil {
		return []connectionDefInfo{}
	}
	names := plan.AdvertisedConnectionNames()
	infos := make([]connectionDefInfo, 0, len(names))
	for _, name := range names {
		conn, ok := plan.LookupConnection(name)
		if !ok || shouldHidePassiveNamedConnection(plan, name, conn, integrationAuthTypes) {
			continue
		}
		if name == config.AppConnectionName {
			conn = displayAppConnectionDef(app, manifestSpec, conn)
		}
		if info, ok := s.connectionInfoFromAuth(integration, userFacingConnectionName(name), name, conn, instances, integrationAuthTypes, name != config.AppConnectionName, p); ok {
			infos = append(infos, info)
		}
	}

	return infos
}

func displayAppConnectionDef(app *config.ProviderEntry, manifestSpec *providermanifestv1.Spec, conn config.ConnectionDef) config.ConnectionDef {
	if app == nil || manifestSpec == nil || manifestSpec.IsManifestBacked() {
		return conn
	}
	def := manifestSpec.DefaultConnectionDef()
	if def == nil {
		return conn
	}

	merged := config.ConnectionDef{}
	if def.Auth != nil {
		config.MergeConnectionAuth(&merged.Auth, config.ManifestAuthToConnectionAuthDef(def.Auth))
	}
	if app.Auth != nil {
		config.MergeConnectionAuth(&merged.Auth, *app.Auth)
	}
	if len(merged.Auth.Credentials) == 0 {
		return conn
	}
	conn.Auth = merged.Auth
	return conn
}

func userFacingConnectionName(name string) string {
	if name == config.AppConnectionName {
		return config.AppConnectionAlias
	}
	return name
}

func (s *Server) populateIntegrationSettings(info *integrationInfo, instances []instanceInfo, p *principal.Principal) []string {
	authTypes := []string{}
	info.Connections = s.connectionInfosForPlugin(info.Name, s.pluginDefs[info.Name], instances, authTypes, p)
	resolvedAuthTypes := resolvedIntegrationAuthTypes(info.Connections)
	if len(authTypes) == 0 && len(resolvedAuthTypes) > 0 {
		info.Connections = s.connectionInfosForPlugin(info.Name, s.pluginDefs[info.Name], instances, resolvedAuthTypes, p)
	}
	return resolvedIntegrationAuthTypes(info.Connections)
}

func connectionParamInfosFromConnection(conn config.ConnectionDef) map[string]connectionParamInfo {
	if len(conn.ConnectionParams) == 0 {
		return map[string]connectionParamInfo{}
	}
	infos := make(map[string]connectionParamInfo, len(conn.ConnectionParams))
	for name, def := range conn.ConnectionParams {
		if def.From != "" {
			continue
		}
		infos[name] = connectionParamInfo{
			Required:    def.Required,
			Description: def.Description,
			Default:     def.Default,
		}
	}
	return infos
}

func credentialFieldInfos[T any](fields []T, mapField func(T) credentialFieldInfo) []credentialFieldInfo {
	if len(fields) == 0 {
		return []credentialFieldInfo{}
	}
	infos := make([]credentialFieldInfo, len(fields))
	for i, field := range fields {
		infos[i] = mapField(field)
	}
	return infos
}

func (s *Server) connectionInfoFromAuth(integration, name, instanceConnection string, conn config.ConnectionDef, instances []instanceInfo, integrationAuthTypes []string, includeWithoutAuth bool, p *principal.Principal) (connectionDefInfo, bool) {
	mode := config.ConnectionModeForConnection(conn)
	connectionInstances := groupInstancesForConnection(instances, instanceConnection)
	connectionParams := connectionParamInfosFromConnection(conn)
	authTypes := connectionAuthTypes(conn.Auth, nil)
	authTypes = s.supportedConnectionAuthTypes(integration, name, authTypes)
	if len(authTypes) == 0 && !includeWithoutAuth {
		return connectionDefInfo{}, false
	}
	displayMode := mode
	if displayMode == core.ConnectionModeNone && len(authTypes) > 0 {
		displayMode = core.ConnectionModeSubject
	}
	status := noAuthConnectionStatus()
	if displayMode != core.ConnectionModeNone {
		status = subjectConnectionStatus(connectionInstances, len(authTypes) > 0, ownerKindForPrincipal(p))
	}

	info := connectionDefInfo{
		DisplayName:      connectionDisplayName(name, conn.DisplayName),
		Name:             name,
		Mode:             string(displayMode),
		AuthTypes:        []string{},
		ConnectionParams: connectionParams,
		CredentialFields: []credentialFieldInfo{},
		Status:           status.Status,
		CredentialState:  status.CredentialState,
		HealthState:      status.HealthState,
		Actions:          status.Actions,
		CredentialMode:   status.CredentialMode,
		OwnerKind:        status.OwnerKind,
		Instances:        connectionInstances,
		StatusCode:       status.StatusCode,
		StatusReason:     status.StatusReason,
		connected:        status.Connected,
		connectable:      len(authTypes) > 0,
		disconnectable:   status.Disconnectable,
	}
	if len(authTypes) > 0 {
		info.AuthTypes = authTypes
	}
	if fields := credentialFieldInfos(conn.Auth.Credentials, func(field config.CredentialFieldDef) credentialFieldInfo {
		return credentialFieldInfo{
			Name:        field.Name,
			Label:       field.Label,
			Description: field.Description,
		}
	}); len(fields) > 0 {
		info.CredentialFields = fields
	} else if authTypesContain(authTypes, "manual") {
		info.CredentialFields = defaultManualCredentialFieldInfos()
	}
	return info, true
}

func (s *Server) invocationConnectionMode(prov core.Provider, integration, connection string) core.ConnectionMode {
	if conn, ok := s.effectiveConnectionDef(integration, connection); ok {
		if mode := config.ConnectionModeForConnection(conn); mode != "" {
			return mode
		}
	}
	if prov == nil {
		return ""
	}
	return core.NormalizeConnectionMode(prov.ConnectionMode())
}

func (s *Server) effectiveConnectionDef(integration, connection string) (config.ConnectionDef, bool) {
	entry, ok := s.pluginDefs[integration]
	if !ok || entry == nil {
		return config.ConnectionDef{}, false
	}
	plan, err := config.BuildStaticConnectionPlan(entry, entry.ManifestSpec())
	if err != nil {
		return config.ConnectionDef{}, false
	}
	return plan.LookupConnection(connection)
}

func shouldHidePassiveNamedConnection(plan config.StaticConnectionPlan, name string, conn config.ConnectionDef, integrationAuthTypes []string) bool {
	if len(plan.NamedConnectionNames()) != 1 {
		return false
	}
	if config.ResolveConnectionAlias(name) != plan.AuthDefaultConnection() {
		return false
	}
	if conn.Mode != providermanifestv1.ConnectionModeNone {
		return false
	}
	if strings.TrimSpace(conn.DisplayName) != "" {
		return false
	}
	if len(connectionAuthTypes(conn.Auth, nil)) != 0 {
		return false
	}
	if len(conn.Auth.Credentials) != 0 {
		return false
	}
	for _, def := range conn.ConnectionParams {
		if strings.TrimSpace(def.From) == "" {
			return false
		}
	}
	return true
}

func defaultManualCredentialFieldInfos() []credentialFieldInfo {
	return []credentialFieldInfo{{
		Name:  "credential",
		Label: "Credential",
	}}
}

func (s *Server) supportedConnectionAuthTypes(integration, connection string, authTypes []string) []string {
	if !authTypesContain(authTypes, "oauth") || s.connectionAuth == nil {
		return authTypes
	}

	connMap := s.connectionAuth()[integration]
	if connMap == nil {
		return removeAuthType(authTypes, "oauth")
	}

	if _, ok := connMap[config.ResolveConnectionAlias(connection)]; ok {
		return authTypes
	}
	return removeAuthType(authTypes, "oauth")
}

func removeAuthType(authTypes []string, drop string) []string {
	filtered := make([]string, 0, len(authTypes))
	for _, authType := range authTypes {
		if authType != drop {
			filtered = append(filtered, authType)
		}
	}
	return filtered
}

func connectionDisplayName(name, configured string) string {
	if strings.TrimSpace(configured) != "" {
		return configured
	}
	return userFacingConnectionName(name)
}

func resolvedIntegrationAuthTypes(connections []connectionDefInfo) []string {
	combined := make([]string, 0, 2)
	for i := range connections {
		connection := &connections[i]
		combined = append(combined, connection.AuthTypes...)
	}
	if authTypes := userFacingAuthTypes(combined); len(authTypes) > 0 {
		return authTypes
	}
	return []string{}
}

func connectionAuthTypes(auth config.ConnectionAuthDef, integrationAuthTypes []string) []string {
	if auth.Type == "" {
		if len(integrationAuthTypes) == 0 {
			return nil
		}
		return append([]string(nil), integrationAuthTypes...)
	}

	authTypes := userFacingAuthTypes([]string{string(auth.Type)})
	if len(authTypes) == 0 {
		return nil
	}
	return authTypes
}

func authTypesContain(authTypes []string, want string) bool {
	for _, authType := range authTypes {
		if authType == want {
			return true
		}
	}
	return false
}

func userFacingAuthTypes(authTypes []string) []string {
	if len(authTypes) == 0 {
		return nil
	}
	var out []string
	for _, authType := range authTypes {
		normalized, ok := userFacingAuthType(authType)
		if !ok || authTypesContain(out, normalized) {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func userFacingAuthType(authType string) (string, bool) {
	switch authType {
	case "", "oauth", "oauth2", "mcp_oauth":
		return "oauth", true
	case "manual", "bearer":
		return "manual", true
	default:
		return "", false
	}
}
