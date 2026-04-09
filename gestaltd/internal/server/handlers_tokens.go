package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
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
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
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
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (s *Server) listAPITokens(w http.ResponseWriter, r *http.Request) {
	userID, err := s.resolveUserID(w, r)
	if err != nil {
		return
	}

	tokens, err := s.datastore.ListAPITokens(r.Context(), userID)
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
	if err := s.datastore.RevokeAPIToken(r.Context(), userID, id); err != nil {
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

	count, err := s.datastore.RevokeAllAPITokens(r.Context(), userID)
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
	intg, ok := s.integrationDefs[name]
	if !ok || intg.Plugin == nil {
		return nil
	}
	return connectionInfosForPlugin(intg.Plugin, integrationAuthTypes, defaultCredentialFields)
}

func connectionInfosForPlugin(plugin *config.ProviderDef, integrationAuthTypes []string, defaultCredentialFields []credentialFieldInfo) []connectionDefInfo {
	if plugin == nil {
		return nil
	}
	manifestProvider := plugin.ManifestPlugin()

	var infos []connectionDefInfo
	if info, ok := connectionInfoFromAuth(config.PluginConnectionAlias, config.EffectivePluginConnectionDef(plugin, manifestProvider).Auth, integrationAuthTypes, defaultCredentialFields); ok {
		infos = append(infos, info)
	}

	names := make([]string, 0, len(plugin.Connections))
	seen := make(map[string]struct{})
	addName := func(raw string) {
		resolved := config.ResolveConnectionAlias(raw)
		if resolved == "" || resolved == config.PluginConnectionName {
			return
		}
		if _, ok := seen[resolved]; ok {
			return
		}
		seen[resolved] = struct{}{}
		names = append(names, resolved)
	}
	if manifestProvider != nil {
		for name := range manifestProvider.Connections {
			addName(name)
		}
	}
	for name := range plugin.Connections {
		addName(name)
	}
	sort.Strings(names)

	for _, name := range names {
		conn, ok := config.EffectiveNamedConnectionDef(plugin, manifestProvider, name)
		if !ok {
			continue
		}
		if info, ok := connectionInfoFromAuth(name, conn.Auth, integrationAuthTypes, defaultCredentialFields); ok {
			infos = append(infos, info)
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

func integrationAuthTypesForProvider(prov core.Provider) []string {
	var authTypes []string
	if atl, ok := prov.(core.AuthTypeLister); ok {
		authTypes = atl.AuthTypes()
	} else if mp, ok := prov.(core.ManualProvider); ok && mp.SupportsManualAuth() {
		authTypes = []string{"manual"}
	} else {
		authTypes = []string{"oauth"}
	}
	return userFacingAuthTypes(authTypes)
}

func credentialFieldInfosFromProvider(prov core.Provider) []credentialFieldInfo {
	cfp, ok := prov.(core.CredentialFieldsProvider)
	if !ok {
		return nil
	}
	return credentialFieldInfos(cfp.CredentialFields(), func(field core.CredentialFieldDef) credentialFieldInfo {
		return credentialFieldInfo{
			Name:        field.Name,
			Label:       field.Label,
			Description: field.Description,
			HelpURL:     field.HelpURL,
		}
	})
}

func credentialFieldInfos[T any](fields []T, mapField func(T) credentialFieldInfo) []credentialFieldInfo {
	if len(fields) == 0 {
		return nil
	}
	infos := make([]credentialFieldInfo, len(fields))
	for i, field := range fields {
		infos[i] = mapField(field)
	}
	return infos
}

func connectionInfoFromAuth(name string, auth config.ConnectionAuthDef, integrationAuthTypes []string, defaultCredentialFields []credentialFieldInfo) (connectionDefInfo, bool) {
	authTypes := connectionAuthTypes(auth.Type, integrationAuthTypes)
	if len(authTypes) == 0 {
		return connectionDefInfo{}, false
	}

	info := connectionDefInfo{Name: name, AuthTypes: authTypes}
	if fields := credentialFieldInfos(auth.Credentials, func(field config.CredentialFieldDef) credentialFieldInfo {
		return credentialFieldInfo{
			Name:        field.Name,
			Label:       field.Label,
			Description: field.Description,
			HelpURL:     field.HelpURL,
		}
	}); len(fields) > 0 {
		info.CredentialFields = fields
	} else if authTypesContain(authTypes, "manual") && len(defaultCredentialFields) > 0 {
		info.CredentialFields = append([]credentialFieldInfo(nil), defaultCredentialFields...)
	}
	return info, true
}

func connectionAuthTypes(authType string, integrationAuthTypes []string) []string {
	if authType == "" {
		if len(integrationAuthTypes) == 0 {
			return nil
		}
		return append([]string(nil), integrationAuthTypes...)
	}

	authTypes := userFacingAuthTypes([]string{authType})
	if len(authTypes) == 0 {
		return nil
	}
	if authTypesContain(integrationAuthTypes, "manual") && !authTypesContain(authTypes, "manual") {
		authTypes = append(authTypes, "manual")
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
