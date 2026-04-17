package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type createManagedIdentityTokenRequest struct {
	Name        string                  `json:"name"`
	Permissions []core.AccessPermission `json:"permissions"`
}

func (s *Server) listManagedIdentityTokens(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleViewer)
	if !ok {
		return
	}

	tokens, err := s.apiTokens.ListAPITokensByOwner(r.Context(), core.APITokenOwnerKindManagedIdentity, actor.Identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list identity tokens")
		return
	}
	grants, err := s.identityGrants.ListGrantsByIdentity(r.Context(), actor.Identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve identity grants")
		return
	}
	grantPermissions := principal.CompileManagedIdentityGrants(grants)

	viewer := managedIdentityGrantValidationPrincipal(PrincipalFromContext(r.Context()), actor.UserID)
	out := make([]apiTokenInfo, 0, len(tokens))
	for _, token := range tokens {
		info := apiTokenInfoFromCore(token)
		info.Permissions = s.filterAccessPermissionsForViewer(r.Context(), effectiveManagedIdentityTokenPermissions(token, grantPermissions), viewer)
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createManagedIdentityToken(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("identity token create failed")
	auditTarget := apiTokenAuditTarget("", "")
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "identity.token.create", auditAllowed, auditErr, auditTarget)
	}()

	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleViewer)
	if !ok {
		return
	}

	var req createManagedIdentityTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auditErr = errors.New("invalid JSON body")
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		auditErr = errors.New("name is required")
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	auditTarget = apiTokenAuditTarget("", req.Name)

	if err := validateAccessPermissionPayload(req.Permissions); err != nil {
		auditErr = err
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	permissions := normalizeAccessPermissions(req.Permissions)
	if len(permissions) == 0 {
		auditErr = errors.New("permissions are required")
		writeError(w, http.StatusBadRequest, "permissions are required")
		return
	}

	viewer := managedIdentityGrantValidationPrincipal(PrincipalFromContext(r.Context()), actor.UserID)
	s.managedIdentityMu.Lock()
	defer s.managedIdentityMu.Unlock()

	if _, err := s.managedIdentities.GetIdentity(r.Context(), actor.Identity.ID); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			auditErr = errors.New("identity not found")
			writeError(w, http.StatusNotFound, "identity not found")
			return
		}
		auditErr = errors.New("failed to resolve identity")
		writeError(w, http.StatusInternalServerError, "failed to resolve identity")
		return
	}

	if err := s.validateManagedIdentityTokenPermissions(r.Context(), actor.Identity.ID, permissions, viewer); err != nil {
		auditErr = err
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	apiToken, plaintext, err := s.issueManagedIdentityAPIToken(r.Context(), actor.Identity.ID, req.Name, permissions, false)
	if err != nil {
		auditErr = errors.New("failed to generate identity token")
		writeError(w, http.StatusInternalServerError, "failed to generate identity token")
		return
	}
	auditAllowed = true
	auditErr = nil
	auditTarget = apiTokenAuditTarget(apiToken.ID, apiToken.Name)

	writeJSON(w, http.StatusCreated, createTokenResponse{
		ID:          apiToken.ID,
		Name:        apiToken.Name,
		Token:       plaintext,
		Permissions: append([]core.AccessPermission(nil), apiToken.Permissions...),
		ExpiresAt:   apiToken.ExpiresAt,
	})
}

func (s *Server) revokeManagedIdentityToken(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("identity token revoke failed")
	id := chi.URLParam(r, "id")
	auditTarget := apiTokenAuditTarget(id, "")
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "identity.token.revoke", auditAllowed, auditErr, auditTarget)
	}()

	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleEditor)
	if !ok {
		return
	}

	s.managedIdentityMu.Lock()
	defer s.managedIdentityMu.Unlock()

	if _, err := s.managedIdentities.GetIdentity(r.Context(), actor.Identity.ID); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			auditErr = errors.New("identity not found")
			writeError(w, http.StatusNotFound, "identity not found")
			return
		}
		auditErr = errors.New("failed to resolve identity")
		writeError(w, http.StatusInternalServerError, "failed to resolve identity")
		return
	}

	if err := s.apiTokens.RevokeAPITokenByOwner(r.Context(), core.APITokenOwnerKindManagedIdentity, actor.Identity.ID, id); err != nil {
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

func (s *Server) validateManagedIdentityTokenPermissions(ctx context.Context, identityID string, permissions []core.AccessPermission, viewer *principal.Principal) error {
	grants, err := s.identityGrants.ListGrantsByIdentity(ctx, identityID)
	if err != nil {
		return fmt.Errorf("failed to resolve identity grants")
	}
	visibleGrants := make(map[string]*core.ManagedIdentityGrant, len(grants))
	for _, grant := range grants {
		if grant == nil || !s.allowProviderContext(ctx, viewer, grant.Plugin) {
			continue
		}
		visibleGrants[grant.Plugin] = grant
	}

	for _, permission := range permissions {
		if !s.managedIdentityGrantPluginVisible(ctx, permission.Plugin, viewer) {
			return fmt.Errorf("plugin %q is not available", permission.Plugin)
		}
		if !s.managedIdentityInvocationSupported(permission.Plugin) {
			return fmt.Errorf("plugin %q does not yet support managed-identity invocation in this phase", permission.Plugin)
		}
		if len(permission.Operations) > 0 {
			if err := s.validateManagedIdentityPermissionOperations(ctx, permission.Plugin, permission.Operations, viewer); err != nil {
				return err
			}
		}
		grant, ok := visibleGrants[permission.Plugin]
		if !ok {
			return fmt.Errorf("plugin %q is not granted to this identity", permission.Plugin)
		}
		if !accessPermissionWithinGrant(permission, grant) {
			return fmt.Errorf("permissions for plugin %q must be within the identity grant", permission.Plugin)
		}
	}
	return nil
}

func validateAccessPermissionPayload(permissions []core.AccessPermission) error {
	for _, permission := range permissions {
		plugin := strings.TrimSpace(permission.Plugin)
		if plugin == "" || len(permission.Operations) == 0 {
			continue
		}
		hasOperation := false
		for _, operation := range permission.Operations {
			if strings.TrimSpace(operation) != "" {
				hasOperation = true
				break
			}
		}
		if !hasOperation {
			return fmt.Errorf("permissions for plugin %q must contain at least one non-blank operation", plugin)
		}
	}
	return nil
}

func normalizeAccessPermissions(permissions []core.AccessPermission) []core.AccessPermission {
	if len(permissions) == 0 {
		return nil
	}
	type permissionState struct {
		wildcard bool
		ops      map[string]struct{}
	}
	byPlugin := make(map[string]*permissionState, len(permissions))
	for _, permission := range permissions {
		plugin := strings.TrimSpace(permission.Plugin)
		if plugin == "" {
			continue
		}
		state := byPlugin[plugin]
		if state == nil {
			state = &permissionState{ops: map[string]struct{}{}}
			byPlugin[plugin] = state
		}
		if len(permission.Operations) == 0 {
			state.wildcard = true
			state.ops = nil
			continue
		}
		if state.wildcard {
			continue
		}
		for _, operation := range permission.Operations {
			operation = strings.TrimSpace(operation)
			if operation == "" {
				continue
			}
			state.ops[operation] = struct{}{}
		}
	}

	plugins := make([]string, 0, len(byPlugin))
	for plugin := range byPlugin {
		plugins = append(plugins, plugin)
	}
	sort.Strings(plugins)

	out := make([]core.AccessPermission, 0, len(plugins))
	for _, plugin := range plugins {
		state := byPlugin[plugin]
		permission := core.AccessPermission{Plugin: plugin}
		if state != nil && !state.wildcard && len(state.ops) > 0 {
			ops := make([]string, 0, len(state.ops))
			for operation := range state.ops {
				ops = append(ops, operation)
			}
			sort.Strings(ops)
			permission.Operations = ops
		}
		out = append(out, permission)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Server) filterAccessPermissionsForViewer(ctx context.Context, permissions []core.AccessPermission, viewer *principal.Principal) []core.AccessPermission {
	if len(permissions) == 0 {
		return nil
	}
	filtered := make([]core.AccessPermission, 0, len(permissions))
	for _, permission := range permissions {
		if viewer != nil && !s.allowProviderContext(ctx, viewer, permission.Plugin) {
			continue
		}
		filtered = append(filtered, core.AccessPermission{
			Plugin:     permission.Plugin,
			Operations: append([]string(nil), permission.Operations...),
		})
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func effectiveManagedIdentityTokenPermissions(token *core.APIToken, grantPermissions principal.PermissionSet) []core.AccessPermission {
	if token == nil {
		return nil
	}
	tokenPermissions := principal.CompilePermissions(token.Permissions)
	if tokenPermissions == nil {
		tokenPermissions = principal.PermissionsFromScopeString(token.Scopes)
	}
	if tokenPermissions == nil {
		tokenPermissions = principal.PermissionSet{}
	}
	effective := principal.IntersectPermissions(tokenPermissions, grantPermissions)
	if effective == nil {
		return nil
	}
	return principal.PermissionsToAccessPermissions(effective)
}

func accessPermissionWithinGrant(permission core.AccessPermission, grant *core.ManagedIdentityGrant) bool {
	if grant == nil {
		return false
	}
	if len(grant.Operations) == 0 {
		return true
	}
	if len(permission.Operations) == 0 {
		return false
	}
	allowed := make(map[string]struct{}, len(grant.Operations))
	for _, operation := range grant.Operations {
		allowed[operation] = struct{}{}
	}
	for _, operation := range permission.Operations {
		if _, ok := allowed[operation]; !ok {
			return false
		}
	}
	return true
}
