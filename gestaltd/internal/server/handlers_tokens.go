package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type createTokenRequest struct {
	Name        string                  `json:"name"`
	Scopes      string                  `json:"scopes"`
	Permissions []core.AccessPermission `json:"permissions,omitempty"`
}

type createTokenResponse struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Token       string                  `json:"token"`
	Permissions []core.AccessPermission `json:"permissions,omitempty"`
	ExpiresAt   *time.Time              `json:"expiresAt,omitempty"`
}

func (s *Server) createAPIToken(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("api token creation failed")
	auditTarget := auditTarget{}
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "api_token.create", auditAllowed, auditErr, auditTarget)
	}()

	userID, err := s.resolveUserID(w, r)
	if err != nil {
		auditErr = err
		return
	}

	var req createTokenRequest
	if err := decodeCreateTokenRequest(r.Body, &req); err != nil {
		auditErr = errors.New("invalid JSON body")
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	auditTarget = apiTokenAuditTarget("", req.Name)

	permissions, err := s.validateCreateAPITokenRequest(req)
	if err != nil {
		auditErr = err
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	apiToken, plaintext, err := s.issueAPITokenWithPermissions(r.Context(), userID, req.Name, req.Scopes, permissions, false)
	if err != nil {
		auditErr = errors.New("failed to generate token")
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	auditTarget = apiTokenAuditTarget(apiToken.ID, apiToken.Name)

	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusCreated, createTokenResponse{
		ID:          apiToken.ID,
		Name:        apiToken.Name,
		Token:       plaintext,
		Permissions: cloneAccessPermissions(apiToken.Permissions),
		ExpiresAt:   apiToken.ExpiresAt,
	})
}

func (s *Server) validateCreateAPITokenRequest(req createTokenRequest) ([]core.AccessPermission, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("name is required")
	}
	if req.Scopes != "" && len(req.Permissions) > 0 {
		return nil, errors.New("scopes and permissions are mutually exclusive")
	}
	if req.Scopes != "" {
		for _, scope := range strings.Fields(req.Scopes) {
			if _, err := s.providers.Get(scope); err != nil {
				return nil, fmt.Errorf("unknown scope %q", scope)
			}
		}
	}
	return s.normalizeAPITokenPermissions(req.Permissions)
}

func (s *Server) normalizeAPITokenPermissions(values []core.AccessPermission) ([]core.AccessPermission, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]core.AccessPermission, 0, len(values))
	for i, value := range values {
		app := strings.TrimSpace(value.App)
		if app == "" {
			return nil, fmt.Errorf("permissions[%d].app is required", i)
		}
		if _, err := s.providers.Get(app); err != nil {
			return nil, fmt.Errorf("unknown permission app %q", app)
		}
		operations, err := normalizeAPITokenPermissionNames(fmt.Sprintf("permissions[%d].operations", i), value.Operations)
		if err != nil {
			return nil, err
		}
		out = append(out, core.AccessPermission{
			App:        app,
			Operations: operations,
		})
	}
	return out, nil
}

func decodeCreateTokenRequest(r io.Reader, req *createTokenRequest) error {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	return decoder.Decode(req)
}

func normalizeAPITokenPermissionNames(label string, values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := values[:0]
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s[%d] is required", label, i)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out, nil
}

type apiTokenInfo struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Scopes      string                  `json:"scopes"`
	Permissions []core.AccessPermission `json:"permissions,omitempty"`
	CreatedAt   time.Time               `json:"createdAt"`
	ExpiresAt   *time.Time              `json:"expiresAt,omitempty"`
}

func (s *Server) listAPITokens(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("api token list failed")
	defer func() {
		s.auditHTTPEvent(r.Context(), PrincipalFromContext(r.Context()), "", "api_token.list", auditAllowed, auditErr)
	}()

	userID, err := s.resolveUserID(w, r)
	if err != nil {
		auditErr = err
		return
	}

	tokens, err := s.apiTokens.ListAPITokens(r.Context(), userID)
	if err != nil {
		auditErr = errors.New("failed to list tokens")
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}

	out := make([]apiTokenInfo, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, apiTokenInfoFromCore(t))
	}
	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) revokeAPIToken(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("api token revoke failed")
	id := chi.URLParam(r, "id")
	auditTarget := apiTokenAuditTarget(id, "")
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "api_token.revoke", auditAllowed, auditErr, auditTarget)
	}()

	userID, err := s.resolveUserID(w, r)
	if err != nil {
		auditErr = err
		return
	}

	if err := s.apiTokens.RevokeAPIToken(r.Context(), userID, id); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			auditErr = errors.New("token not found")
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		auditErr = errors.New("failed to revoke token")
		writeError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}
	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) revokeAllAPITokens(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("api token revoke all failed")
	auditTarget := apiTokenCollectionAuditTarget()
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "api_token.revoke_all", auditAllowed, auditErr, auditTarget)
	}()

	userID, err := s.resolveUserID(w, r)
	if err != nil {
		auditErr = err
		return
	}

	count, err := s.apiTokens.RevokeAllAPITokens(r.Context(), userID)
	if err != nil {
		auditErr = errors.New("failed to revoke tokens")
		writeError(w, http.StatusInternalServerError, "failed to revoke tokens")
		return
	}
	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked", "count": count})
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
