package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

type createTokenRequest struct {
	Name   string `json:"name"`
	Scopes string `json:"scopes"`
}

type createTokenResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Token     string     `json:"token"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func (s *Server) createAPIToken(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("api token creation failed")
	defer func() {
		s.auditHTTPEvent(r.Context(), PrincipalFromContext(r.Context()), "", "api_token.create", auditAllowed, auditErr)
	}()

	userID, err := s.resolveUserID(w, r)
	if err != nil {
		auditErr = err
		return
	}

	var req createTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auditErr = errors.New("invalid JSON body")
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Name == "" {
		auditErr = errors.New("name is required")
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if req.Scopes != "" {
		for _, scope := range strings.Fields(req.Scopes) {
			if _, err := s.providers.Get(scope); err != nil {
				auditErr = fmt.Errorf("unknown scope %q", scope)
				writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown scope %q", scope))
				return
			}
		}
	}

	apiToken, plaintext, err := s.issueAPIToken(r.Context(), userID, req.Name, req.Scopes, false)
	if err != nil {
		auditErr = errors.New("failed to generate token")
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusCreated, createTokenResponse{
		ID:        apiToken.ID,
		Name:      apiToken.Name,
		Token:     plaintext,
		ExpiresAt: apiToken.ExpiresAt,
	})
}

type apiTokenInfo struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Scopes    string     `json:"scopes"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func (s *Server) listAPITokens(w http.ResponseWriter, r *http.Request) {
	userID, err := s.resolveUserID(w, r)
	if err != nil {
		return
	}

	tokens, err := s.apiTokens.ListAPITokens(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}

	out := make([]apiTokenInfo, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, apiTokenInfoFromCore(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) revokeAPIToken(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("api token revoke failed")
	defer func() {
		s.auditHTTPEvent(r.Context(), PrincipalFromContext(r.Context()), "", "api_token.revoke", auditAllowed, auditErr)
	}()

	userID, err := s.resolveUserID(w, r)
	if err != nil {
		auditErr = err
		return
	}

	id := chi.URLParam(r, "id")
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
	defer func() {
		s.auditHTTPEvent(r.Context(), PrincipalFromContext(r.Context()), "", "api_token.revoke_all", auditAllowed, auditErr)
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

func (s *Server) integrationConnectionInfos(name string, integrationAuthTypes []string, defaultCredentialFields []credentialFieldInfo) []connectionDefInfo {
	entry, ok := s.pluginDefs[name]
	if !ok || entry == nil {
		return nil
	}
	return s.connectionInfosForPlugin(name, entry, integrationAuthTypes, defaultCredentialFields)
}

func (s *Server) connectionInfosForPlugin(integration string, plugin *config.ProviderEntry, integrationAuthTypes []string, defaultCredentialFields []credentialFieldInfo) []connectionDefInfo {
	if plugin == nil {
		return []connectionDefInfo{}
	}
	manifestProvider := plugin.ManifestSpec()

	names, err := bootstrap.AdvertisedConnectionNames(plugin)
	if err != nil {
		return []connectionDefInfo{}
	}
	infos := make([]connectionDefInfo, 0, len(names))
	for _, name := range names {
		if name == config.PluginConnectionName {
			effectivePluginConn := config.EffectivePluginConnectionDef(plugin, manifestProvider)
			if info, ok := s.connectionInfoFromAuth(integration, config.PluginConnectionAlias, effectivePluginConn, integrationAuthTypes, defaultCredentialFields); ok {
				infos = append(infos, info)
			}
			continue
		}
		conn, ok := config.EffectiveNamedConnectionDef(plugin, manifestProvider, name)
		if ok {
			if info, ok := s.connectionInfoFromAuth(integration, name, conn, integrationAuthTypes, defaultCredentialFields); ok {
				infos = append(infos, info)
			}
		}
	}

	return infos
}

func userFacingConnectionName(name string) string {
	if name == config.PluginConnectionName {
		return config.PluginConnectionAlias
	}
	return name
}

func providerSupportsManualAuth(prov core.Provider) bool {
	mp, ok := prov.(core.ManualProvider)
	return ok && mp.SupportsManualAuth()
}

func providerSupportsOAuth(prov core.Provider) bool {
	_, ok := prov.(core.OAuthProvider)
	return ok
}

func integrationAuthTypesForProvider(prov core.Provider) []string {
	var authTypes []string
	if atl, ok := prov.(core.AuthTypeLister); ok {
		authTypes = atl.AuthTypes()
	} else if providerSupportsManualAuth(prov) {
		authTypes = []string{"manual"}
	} else if providerSupportsOAuth(prov) {
		authTypes = []string{"oauth"}
	}
	return userFacingAuthTypes(authTypes)
}

func credentialFieldInfosFromProvider(prov core.Provider) []credentialFieldInfo {
	cfp, ok := prov.(core.CredentialFieldsProvider)
	if !ok {
		return []credentialFieldInfo{}
	}
	return credentialFieldInfos(cfp.CredentialFields(), func(field core.CredentialFieldDef) credentialFieldInfo {
		return credentialFieldInfo{
			Name:        field.Name,
			Label:       field.Label,
			Description: field.Description,
		}
	})
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

func (s *Server) connectionInfoFromAuth(integration, name string, conn config.ConnectionDef, integrationAuthTypes []string, defaultCredentialFields []credentialFieldInfo) (connectionDefInfo, bool) {
	authTypes := connectionAuthTypes(conn.Auth, integrationAuthTypes)
	authTypes = s.supportedConnectionAuthTypes(integration, name, authTypes)
	if len(authTypes) == 0 {
		return connectionDefInfo{}, false
	}

	info := connectionDefInfo{
		DisplayName:      connectionDisplayName(name, conn.DisplayName),
		Name:             name,
		AuthTypes:        authTypes,
		CredentialFields: []credentialFieldInfo{},
	}
	if fields := credentialFieldInfos(conn.Auth.Credentials, func(field config.CredentialFieldDef) credentialFieldInfo {
		return credentialFieldInfo{
			Name:        field.Name,
			Label:       field.Label,
			Description: field.Description,
		}
	}); len(fields) > 0 {
		info.CredentialFields = fields
	} else if authTypesContain(authTypes, "manual") && len(defaultCredentialFields) > 0 {
		info.CredentialFields = append([]credentialFieldInfo(nil), defaultCredentialFields...)
	}
	return info, true
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

func authTypesFromConnections(connections []connectionDefInfo) []string {
	combined := make([]string, 0, 2)
	for _, connection := range connections {
		combined = append(combined, connection.AuthTypes...)
	}
	return userFacingAuthTypes(combined)
}

func fallbackAuthTypesForProvider(prov core.Provider) []string {
	if providerSupportsManualAuth(prov) {
		return []string{"manual"}
	}
	if providerSupportsOAuth(prov) {
		return []string{"oauth"}
	}
	return nil
}

func connectionDisplayName(name, configured string) string {
	if strings.TrimSpace(configured) != "" {
		return configured
	}
	return userFacingConnectionName(name)
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
