package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type adminAuthorizationPluginInfo struct {
	Name                string `json:"name"`
	AuthorizationPolicy string `json:"authorizationPolicy"`
	MountedUIPath       string `json:"mountedUiPath,omitempty"`
}

type adminAuthorizationMemberRow struct {
	Plugin        string `json:"plugin"`
	Role          string `json:"role"`
	Source        string `json:"source"`
	Effective     bool   `json:"effective"`
	Mutable       bool   `json:"mutable"`
	SelectorKind  string `json:"selectorKind"`
	SelectorValue string `json:"selectorValue"`
	Email         string `json:"email,omitempty"`
	ShadowedBy    string `json:"shadowedBy,omitempty"`
}

type putAdminAuthorizationMemberRequest struct {
	SubjectID string `json:"subjectId"`
	Email     string `json:"email"`
	Role      string `json:"role"`
}

const adminAuthorizationCanWriteHeader = "X-Gestalt-Can-Write"

func (s *Server) mountAdminAuthorizationRoutes(r chi.Router) {
	r.Get("/authorization/provider", s.getAdminAuthorizationProvider)
	r.Get("/authorization/models", s.listAdminAuthorizationModels)
	r.Get("/authorization/relationships", s.listAdminAuthorizationRelationships)
	r.Get("/authorization/admins/members", s.listAdminAuthorizationAdminMembers)
	r.Put("/authorization/admins/members", s.putAdminAuthorizationAdminMember)
	r.Delete("/authorization/admins/members/{subjectID}", s.deleteAdminAuthorizationAdminMember)
	r.Get("/authorization/plugins", s.listAdminAuthorizationPlugins)
	r.Get("/authorization/plugins/{plugin}/members", s.listAdminAuthorizationPluginMembers)
	r.Put("/authorization/plugins/{plugin}/members", s.putAdminAuthorizationPluginMember)
	r.Delete("/authorization/plugins/{plugin}/members/{subjectID}", s.deleteAdminAuthorizationPluginMember)
}

func (s *Server) adminAPIAuthMiddleware(next http.Handler) http.Handler {
	if s.adminRoute.AuthorizationPolicy == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authorizer == nil {
			writeError(w, http.StatusInternalServerError, "admin authorization is unavailable")
			return
		}

		p, authenticated, err := s.resolveMountedUIPrincipal(r, s.adminMountedUI())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve user")
			return
		}
		if !authenticated {
			writeError(w, http.StatusUnauthorized, "missing authorization")
			return
		}
		if p.Kind == principal.KindWorkload {
			writeError(w, http.StatusForbidden, "workload callers are not allowed on this route")
			return
		}

		access, allowed := s.authorizer.ResolveAdminAccess(r.Context(), p, s.adminRoute.AuthorizationPolicy)
		if !allowed || !mountedUIRoleAllowed(access.Role, s.adminRoute.AllowedRoles) {
			writeError(w, http.StatusForbidden, "admin access denied")
			return
		}

		ctx := r.Context()
		if p != nil {
			ctx = principal.WithPrincipal(ctx, p)
		}
		if access.Policy != "" || access.Role != "" {
			ctx = invocation.WithAccessContext(ctx, access)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) listAdminAuthorizationPlugins(w http.ResponseWriter, r *http.Request) {
	names := make([]string, 0, len(s.pluginDefs))
	for name, entry := range s.pluginDefs {
		if entry == nil || strings.TrimSpace(entry.AuthorizationPolicy) == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]adminAuthorizationPluginInfo, 0, len(names))
	for _, name := range names {
		entry := s.pluginDefs[name]
		out = append(out, adminAuthorizationPluginInfo{
			Name:                name,
			AuthorizationPolicy: strings.TrimSpace(entry.AuthorizationPolicy),
			MountedUIPath:       strings.TrimSpace(entry.MountPath),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listAdminAuthorizationPluginMembers(w http.ResponseWriter, r *http.Request) {
	plugin, _, err := s.adminAuthorizationPluginEntry(chi.URLParam(r, "plugin"))
	if err != nil {
		s.writeAdminAuthorizationPluginError(w, err)
		return
	}
	if !s.ensureAdminDynamicAuthorizationAvailable(w) {
		return
	}

	rows, err := s.adminAuthorizationMemberRows(r.Context(), plugin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list authorization members")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) putAdminAuthorizationPluginMember(w http.ResponseWriter, r *http.Request) {
	plugin, _, err := s.adminAuthorizationPluginEntry(chi.URLParam(r, "plugin"))
	if err != nil {
		s.writeAdminAuthorizationPluginError(w, err)
		return
	}
	if !s.ensureAdminDynamicAuthorizationAvailable(w) {
		return
	}

	var req putAdminAuthorizationMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Role) == "" {
		writeError(w, http.StatusBadRequest, "role is required")
		return
	}
	user, status, message := s.resolveAdminAuthorizationWriteUser(r.Context(), req)
	if status != 0 {
		writeError(w, status, message)
		return
	}
	if access, ok := s.authorizer.StaticRoleForProviderIdentity(plugin, principal.UserSubjectID(user.ID)); ok && access.Role != "" {
		writeError(w, http.StatusConflict, "user already has static authorization for this plugin")
		return
	}

	membership, err := s.upsertProviderPluginAuthorization(r.Context(), user, plugin, req.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist authorization member")
		return
	}

	row := adminAuthorizationMemberRow{
		Plugin:        membership.Plugin,
		Role:          membership.Role,
		Source:        "dynamic",
		Effective:     true,
		Mutable:       true,
		SelectorKind:  "subject_id",
		SelectorValue: principal.UserSubjectID(strings.TrimSpace(membership.UserID)),
		Email:         strings.TrimSpace(user.Email),
	}
	if err := s.reloadAuthorizationState(r.Context()); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":     "persisted_pending_reload",
			"persisted":  true,
			"reloaded":   false,
			"membership": row,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"persisted":  true,
		"reloaded":   true,
		"membership": row,
	})
}

func (s *Server) deleteAdminAuthorizationPluginMember(w http.ResponseWriter, r *http.Request) {
	plugin, _, err := s.adminAuthorizationPluginEntry(chi.URLParam(r, "plugin"))
	if err != nil {
		s.writeAdminAuthorizationPluginError(w, err)
		return
	}
	if !s.ensureAdminDynamicAuthorizationAvailable(w) {
		return
	}
	userID, err := adminAuthorizationUserIDFromSubjectID(strings.TrimSpace(chi.URLParam(r, "subjectID")))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	deleteErr := s.deleteProviderPluginAuthorization(r.Context(), plugin, userID)
	if deleteErr != nil {
		if errors.Is(deleteErr, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "authorization member not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete authorization member")
		return
	}

	if err := s.reloadAuthorizationState(r.Context()); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":    "persisted_pending_reload",
			"persisted": true,
			"reloaded":  false,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "deleted",
		"persisted": true,
		"reloaded":  true,
	})
}

func (s *Server) listAdminAuthorizationAdminMembers(w http.ResponseWriter, r *http.Request) {
	if !s.ensureAdminDynamicAdminAvailable(w) {
		return
	}

	rows, err := s.adminAuthorizationAdminRows(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list admin members")
		return
	}
	access := invocation.AccessContextFromContext(r.Context())
	w.Header().Set(adminAuthorizationCanWriteHeader, strconv.FormatBool(s.adminRoleCanMutate(access.Role)))
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) putAdminAuthorizationAdminMember(w http.ResponseWriter, r *http.Request) {
	if !s.ensureAdminDynamicAdminAvailable(w) {
		return
	}
	if !s.ensureAdminAuthorizationWriteAccess(w, r) {
		return
	}

	var req putAdminAuthorizationMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Role) == "" {
		writeError(w, http.StatusBadRequest, "role is required")
		return
	}
	user, status, message := s.resolveAdminAuthorizationWriteUser(r.Context(), req)
	if status != 0 {
		writeError(w, status, message)
		return
	}
	if access, ok := s.authorizer.StaticRoleForPolicyIdentity(s.adminRoute.AuthorizationPolicy, principal.UserSubjectID(user.ID)); ok && access.Role != "" {
		writeError(w, http.StatusConflict, "user already has static admin authorization")
		return
	}

	membership, err := s.upsertProviderAdminAuthorization(r.Context(), user, req.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist admin member")
		return
	}

	row := adminAuthorizationMemberRow{
		Role:          membership.Role,
		Source:        "dynamic",
		Effective:     true,
		Mutable:       true,
		SelectorKind:  "subject_id",
		SelectorValue: principal.UserSubjectID(strings.TrimSpace(membership.UserID)),
		Email:         strings.TrimSpace(user.Email),
	}
	if err := s.reloadAuthorizationState(r.Context()); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":     "persisted_pending_reload",
			"persisted":  true,
			"reloaded":   false,
			"membership": row,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"persisted":  true,
		"reloaded":   true,
		"membership": row,
	})
}

func (s *Server) deleteAdminAuthorizationAdminMember(w http.ResponseWriter, r *http.Request) {
	if !s.ensureAdminDynamicAdminAvailable(w) {
		return
	}
	if !s.ensureAdminAuthorizationWriteAccess(w, r) {
		return
	}
	userID, err := adminAuthorizationUserIDFromSubjectID(strings.TrimSpace(chi.URLParam(r, "subjectID")))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	deleteErr := s.deleteProviderAdminAuthorization(r.Context(), userID)
	if deleteErr != nil {
		if errors.Is(deleteErr, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "admin member not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete admin member")
		return
	}

	if err := s.reloadAuthorizationState(r.Context()); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":    "persisted_pending_reload",
			"persisted": true,
			"reloaded":  false,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "deleted",
		"persisted": true,
		"reloaded":  true,
	})
}

func (s *Server) adminAuthorizationPluginEntry(plugin string) (string, *config.ProviderEntry, error) {
	plugin = strings.TrimSpace(plugin)
	if plugin == "" {
		return "", nil, errAdminAuthorizationPluginMissing
	}
	entry := s.pluginDefs[plugin]
	if entry == nil {
		return "", nil, errAdminAuthorizationPluginUnknown
	}
	if strings.TrimSpace(entry.AuthorizationPolicy) == "" {
		return "", nil, errAdminAuthorizationPluginUnbound
	}
	return plugin, entry, nil
}

func (s *Server) writeAdminAuthorizationPluginError(w http.ResponseWriter, err error) {
	switch err {
	case errAdminAuthorizationPluginMissing:
		writeError(w, http.StatusBadRequest, "plugin is required")
	case errAdminAuthorizationPluginUnknown:
		writeError(w, http.StatusNotFound, "plugin not found")
	case errAdminAuthorizationPluginUnbound:
		writeError(w, http.StatusBadRequest, "plugin does not declare authorizationPolicy")
	default:
		writeError(w, http.StatusInternalServerError, "plugin authorization failed")
	}
}

func (s *Server) adminAuthorizationMemberRows(ctx context.Context, plugin string) ([]adminAuthorizationMemberRow, error) {
	if s.authorizer == nil || s.authorizationProvider == nil {
		return nil, errAdminAuthorizationUnavailable
	}
	staticRows := s.adminAuthorizationStaticRows(ctx, plugin)
	dynamicRows, err := s.providerPluginAuthorizationRows(ctx, plugin)
	if err != nil {
		return nil, err
	}
	return mergeAdminAuthorizationRows(staticRows, dynamicRows), nil
}

func (s *Server) adminAuthorizationAdminRows(ctx context.Context) ([]adminAuthorizationMemberRow, error) {
	if s.authorizer == nil || s.authorizationProvider == nil {
		return nil, errAdminAuthorizationUnavailable
	}
	staticRows := s.adminAuthorizationStaticAdminRows(ctx)
	dynamicRows, err := s.providerAdminAuthorizationRows(ctx)
	if err != nil {
		return nil, err
	}
	return mergeAdminAuthorizationRows(staticRows, dynamicRows), nil
}

func (s *Server) adminAuthorizationStaticRows(ctx context.Context, plugin string) []adminAuthorizationMemberRow {
	_, members, ok := s.authorizer.StaticMembersForProvider(plugin)
	if !ok {
		return nil
	}
	return s.adminAuthorizationRowsFromStaticMembers(ctx, plugin, members)
}

func (s *Server) adminAuthorizationStaticAdminRows(ctx context.Context) []adminAuthorizationMemberRow {
	members, ok := s.authorizer.StaticMembersForPolicy(s.adminRoute.AuthorizationPolicy)
	if !ok {
		return nil
	}
	return s.adminAuthorizationRowsFromStaticMembers(ctx, "", members)
}

func (s *Server) adminAuthorizationRowsFromStaticMembers(ctx context.Context, plugin string, members []authorization.StaticHumanMember) []adminAuthorizationMemberRow {
	rows := make([]adminAuthorizationMemberRow, 0, len(members))
	for _, member := range members {
		rows = append(rows, adminAuthorizationMemberRow{
			Plugin:        plugin,
			Role:          member.Role,
			Source:        "static",
			Effective:     true,
			Mutable:       false,
			SelectorKind:  "subject_id",
			SelectorValue: member.SubjectID,
			Email:         s.adminAuthorizationEmailForSubjectID(ctx, member.SubjectID),
		})
	}
	return rows
}

func mergeAdminAuthorizationRows(staticRows, dynamicRows []adminAuthorizationMemberRow) []adminAuthorizationMemberRow {
	staticBySubjectID := make(map[string]string, len(staticRows))
	rows := make([]adminAuthorizationMemberRow, 0, len(staticRows)+len(dynamicRows))
	for i := range staticRows {
		row := &staticRows[i]
		rows = append(rows, *row)
		staticBySubjectID[strings.TrimSpace(row.SelectorValue)] = row.adminAuthorizationRowKey()
	}
	for i := range dynamicRows {
		row := dynamicRows[i]
		if shadow, ok := staticBySubjectID[strings.TrimSpace(row.SelectorValue)]; ok {
			row.Effective = false
			row.ShadowedBy = shadow
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Source != rows[j].Source {
			return rows[i].Source < rows[j].Source
		}
		if rows[i].SelectorKind != rows[j].SelectorKind {
			return rows[i].SelectorKind < rows[j].SelectorKind
		}
		if rows[i].SelectorValue != rows[j].SelectorValue {
			return rows[i].SelectorValue < rows[j].SelectorValue
		}
		return rows[i].Role < rows[j].Role
	})
	return rows
}

func (s *Server) reloadAuthorizationState(ctx context.Context) error {
	if s.authorizer == nil {
		return nil
	}

	var lastErr error
	for i := 0; i < 3; i++ {
		if err := s.authorizer.ReloadAuthorizationState(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i == 2 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i+1) * 50 * time.Millisecond):
		}
	}
	return lastErr
}

func (r adminAuthorizationMemberRow) adminAuthorizationRowKey() string {
	return r.Source + ":" + r.SelectorKind + ":" + r.SelectorValue
}

func (s *Server) adminAuthorizationEmailForSubjectID(ctx context.Context, subjectID string) string {
	userID := strings.TrimSpace(principal.UserIDFromSubjectID(subjectID))
	if userID == "" || s.users == nil {
		return ""
	}
	user, err := s.users.GetUser(ctx, userID)
	if err != nil || user == nil {
		return ""
	}
	return strings.TrimSpace(user.Email)
}

func (s *Server) resolveAdminAuthorizationWriteUser(ctx context.Context, req putAdminAuthorizationMemberRequest) (*core.User, int, string) {
	subjectID := strings.TrimSpace(req.SubjectID)
	email := strings.TrimSpace(req.Email)
	switch {
	case subjectID != "" && email != "":
		return nil, http.StatusBadRequest, "provide either subjectId or email, not both"
	case subjectID != "":
		userID, err := adminAuthorizationUserIDFromSubjectID(subjectID)
		if err != nil {
			return nil, http.StatusBadRequest, err.Error()
		}
		user, err := s.users.GetUser(ctx, userID)
		switch {
		case err == nil:
			return user, 0, ""
		case errors.Is(err, core.ErrNotFound):
			return nil, http.StatusNotFound, "subject not found"
		default:
			return nil, http.StatusInternalServerError, "failed to resolve user"
		}
	case email != "":
		parsed, err := mail.ParseAddress(email)
		if err != nil || strings.TrimSpace(parsed.Address) == "" {
			return nil, http.StatusBadRequest, "email must be a valid email address"
		}
		user, err := s.users.FindOrCreateUser(ctx, parsed.Address)
		if err != nil {
			return nil, http.StatusInternalServerError, "failed to resolve user"
		}
		return user, 0, ""
	default:
		return nil, http.StatusBadRequest, "subjectId or email is required"
	}
}

func adminAuthorizationUserIDFromSubjectID(subjectID string) (string, error) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return "", errors.New("subjectID is required")
	}
	userID := strings.TrimSpace(principal.UserIDFromSubjectID(subjectID))
	if userID == "" {
		return "", errors.New("subjectID must use user:<id>")
	}
	return userID, nil
}

var (
	errAdminAuthorizationPluginMissing = errors.New("plugin is required")
	errAdminAuthorizationPluginUnknown = errors.New("plugin not found")
	errAdminAuthorizationPluginUnbound = errors.New("plugin does not declare authorizationPolicy")
	errAdminAuthorizationUnavailable   = errors.New("dynamic authorization is unavailable")
)

func (s *Server) ensureAdminDynamicAuthorizationAvailable(w http.ResponseWriter) bool {
	if s.authorizer == nil || s.authorizationProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "dynamic authorization requires an authorization provider")
		return false
	}
	return true
}

func (s *Server) ensureAdminDynamicAdminAvailable(w http.ResponseWriter) bool {
	if strings.TrimSpace(s.adminRoute.AuthorizationPolicy) == "" {
		writeError(w, http.StatusServiceUnavailable, "dynamic admin authorization is unavailable")
		return false
	}
	if s.authorizer == nil {
		writeError(w, http.StatusServiceUnavailable, "dynamic admin authorization is unavailable")
		return false
	}
	if s.authorizationProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "dynamic admin authorization requires an authorization provider")
		return false
	}
	members, ok := s.authorizer.StaticMembersForPolicy(s.adminRoute.AuthorizationPolicy)
	if !ok || len(members) == 0 {
		writeError(w, http.StatusServiceUnavailable, "dynamic admin authorization requires at least one static admin member")
		return false
	}
	hasSeedAdmin := false
	for _, member := range members {
		if s.adminRoleCanMutate(member.Role) {
			hasSeedAdmin = true
			break
		}
	}
	if !hasSeedAdmin {
		writeError(w, http.StatusServiceUnavailable, "dynamic admin authorization requires at least one static admin member")
		return false
	}
	return true
}

func (s *Server) ensureAdminAuthorizationWriteAccess(w http.ResponseWriter, r *http.Request) bool {
	access := invocation.AccessContextFromContext(r.Context())
	if !s.adminRoleCanMutate(access.Role) {
		writeError(w, http.StatusForbidden, "admin membership changes require an allowed admin role")
		return false
	}
	return true
}

func (s *Server) adminRoleCanMutate(role string) bool {
	return mountedUIRoleAllowed(strings.TrimSpace(role), s.adminRoute.AllowedRoles)
}
