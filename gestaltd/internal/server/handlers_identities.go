package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/emailutil"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

const (
	managedIdentityRoleViewer = "viewer"
	managedIdentityRoleEditor = "editor"
	managedIdentityRoleAdmin  = "admin"
)

var managedIdentityRoleRank = map[string]int{
	managedIdentityRoleViewer: 1,
	managedIdentityRoleEditor: 2,
	managedIdentityRoleAdmin:  3,
}

type managedIdentityInfo struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type managedIdentityMemberInfo struct {
	UserID    string    `json:"userId"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type managedIdentityGrantInfo struct {
	Plugin     string    `json:"plugin"`
	Operations []string  `json:"operations"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type createManagedIdentityRequest struct {
	DisplayName string `json:"displayName"`
}

type updateManagedIdentityRequest struct {
	DisplayName string `json:"displayName"`
}

type putManagedIdentityMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type putManagedIdentityGrantRequest struct {
	Operations []string `json:"operations"`
}

type managedIdentityActor struct {
	UserID     string
	Identity   *core.ManagedIdentity
	Membership *core.ManagedIdentityMembership
}

func (s *Server) listManagedIdentities(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := s.resolveManagedIdentityUser(w, r)
	if !ok {
		return
	}

	memberships, err := s.identityMemberships.ListMembershipsByUser(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list identities")
		return
	}

	identityIDs := make([]string, 0, len(memberships))
	roleByIdentityID := make(map[string]string, len(memberships))
	for _, membership := range memberships {
		identityIDs = append(identityIDs, membership.IdentityID)
		roleByIdentityID[membership.IdentityID] = membership.Role
	}

	identities, err := s.managedIdentities.ListIdentitiesByIDs(r.Context(), identityIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list identities")
		return
	}

	out := make([]managedIdentityInfo, 0, len(identities))
	for _, identity := range identities {
		out = append(out, managedIdentityInfo{
			ID:          identity.ID,
			DisplayName: identity.DisplayName,
			Role:        roleByIdentityID[identity.ID],
			CreatedAt:   identity.CreatedAt,
			UpdatedAt:   identity.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DisplayName != out[j].DisplayName {
			return out[i].DisplayName < out[j].DisplayName
		}
		return out[i].ID < out[j].ID
	})

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createManagedIdentity(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("identity create failed")
	auditTarget := managedIdentityAuditTarget("", "")
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "identity.create", auditAllowed, auditErr, auditTarget)
	}()

	userID, user, ok := s.resolveManagedIdentityUser(w, r)
	if !ok {
		return
	}

	var req createManagedIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.DisplayName == "" {
		auditErr = errors.New("displayName is required")
		writeError(w, http.StatusBadRequest, "displayName is required")
		return
	}
	auditTarget = managedIdentityAuditTarget("", req.DisplayName)

	s.managedIdentityMu.Lock()
	defer s.managedIdentityMu.Unlock()

	now := s.nowUTCSecond()
	identity := &core.ManagedIdentity{
		ID:                  uuid.NewString(),
		DisplayName:         req.DisplayName,
		CreatedByIdentityID: userID,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.managedIdentities.CreateIdentity(r.Context(), identity); err != nil {
		auditErr = errors.New("failed to create identity")
		writeError(w, http.StatusInternalServerError, "failed to create identity")
		return
	}
	membership, err := s.identityMemberships.UpsertMembership(r.Context(), &core.ManagedIdentityMembership{
		IdentityID: identity.ID,
		UserID:     userID,
		Email:      user.Email,
		Role:       managedIdentityRoleAdmin,
	})
	if err != nil {
		membership = &core.ManagedIdentityMembership{
			ID:         uuid.NewString(),
			IdentityID: identity.ID,
			UserID:     userID,
			Email:      user.Email,
			Role:       managedIdentityRoleAdmin,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		membership, err = s.recoverManagedIdentityCreateMembership(r.Context(), identity, membership, err)
		if err != nil {
			auditErr = err
			writeError(w, http.StatusInternalServerError, "failed to create identity membership")
			return
		}
	}
	auditAllowed = true
	auditErr = nil
	auditTarget = managedIdentityAuditTarget(identity.ID, identity.DisplayName)

	writeJSON(w, http.StatusCreated, managedIdentityInfo{
		ID:          identity.ID,
		DisplayName: identity.DisplayName,
		Role:        membership.Role,
		CreatedAt:   identity.CreatedAt,
		UpdatedAt:   identity.UpdatedAt,
	})
}

func (s *Server) getManagedIdentity(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleViewer)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, managedIdentityInfo{
		ID:          actor.Identity.ID,
		DisplayName: actor.Identity.DisplayName,
		Role:        actor.Membership.Role,
		CreatedAt:   actor.Identity.CreatedAt,
		UpdatedAt:   actor.Identity.UpdatedAt,
	})
}

func (s *Server) routeManagedIdentityOrExecuteIdentitiesOperation(
	w http.ResponseWriter,
	r *http.Request,
	identityHandler func(http.ResponseWriter, *http.Request),
) {
	identityID := strings.TrimSpace(chi.URLParam(r, "identityID"))
	if identityID == "" {
		identityHandler(w, r)
		return
	}
	if s.managedIdentities != nil {
		if _, err := s.managedIdentities.GetIdentity(r.Context(), identityID); err == nil {
			identityHandler(w, r)
			return
		} else if !errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "failed to resolve identity")
			return
		}
	}
	if s.identitiesProviderHasOperation(r.Context(), identityID, r.Method, PrincipalFromContext(r.Context())) {
		s.executeIdentitiesOperation(w, r)
		return
	}
	identityHandler(w, r)
}

func (s *Server) getManagedIdentityOrExecuteIdentitiesOperation(w http.ResponseWriter, r *http.Request) {
	s.routeManagedIdentityOrExecuteIdentitiesOperation(w, r, s.getManagedIdentity)
}

func (s *Server) updateManagedIdentityOrExecuteIdentitiesOperation(w http.ResponseWriter, r *http.Request) {
	s.routeManagedIdentityOrExecuteIdentitiesOperation(w, r, s.updateManagedIdentity)
}

func (s *Server) deleteManagedIdentityOrExecuteIdentitiesOperation(w http.ResponseWriter, r *http.Request) {
	s.routeManagedIdentityOrExecuteIdentitiesOperation(w, r, s.deleteManagedIdentity)
}

func (s *Server) executeIdentitiesOperation(w http.ResponseWriter, r *http.Request) {
	operation := strings.TrimSpace(chi.URLParam(r, "identityID"))
	routeCtx := chi.RouteContext(r.Context())
	if routeCtx == nil {
		routeCtx = chi.NewRouteContext()
	} else {
		cloned := chi.NewRouteContext()
		*cloned = *routeCtx
		routeCtx = cloned
	}
	routeCtx.URLParams.Add("integration", "identities")
	routeCtx.URLParams.Add("operation", operation)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx))
	s.executeOperation(w, r)
}

func (s *Server) identitiesProviderHasOperation(ctx context.Context, operation, method string, p *principal.Principal) bool {
	prov, err := s.providers.Get("identities")
	if err != nil {
		return false
	}
	var resolver invocation.TokenResolver
	if tr, ok := s.invoker.(invocation.TokenResolver); ok {
		resolver = tr
	}
	ctx = invocation.WithAccessContext(ctx, s.providerAccessContextWithContext(ctx, p, "identities"))
	targets, err := s.managedIdentityGrantCatalogTargets(ctx, "identities", p)
	if err != nil {
		return false
	}
	var firstErr error
	for _, target := range targets {
		cat, err := s.managedIdentityGrantCatalogForConnection(ctx, prov, "identities", resolver, p, target.connection, target.instance)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if cat == nil {
			continue
		}
		for i := range cat.Operations {
			op := cat.Operations[i]
			if op.ID != operation {
				continue
			}
			if strings.TrimSpace(op.Method) == "" || strings.EqualFold(op.Method, method) {
				return true
			}
		}
	}
	return false
}

func (s *Server) updateManagedIdentity(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("identity update failed")
	auditTarget := managedIdentityAuditTarget(chi.URLParam(r, "identityID"), "")
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "identity.update", auditAllowed, auditErr, auditTarget)
	}()

	var req updateManagedIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.DisplayName == "" {
		auditErr = errors.New("displayName is required")
		writeError(w, http.StatusBadRequest, "displayName is required")
		return
	}

	s.managedIdentityMu.Lock()
	defer s.managedIdentityMu.Unlock()

	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleAdmin)
	if !ok {
		return
	}
	auditTarget = managedIdentityAuditTarget(actor.Identity.ID, actor.Identity.DisplayName)

	actor.Identity.DisplayName = req.DisplayName
	actor.Identity.UpdatedAt = s.nowUTCSecond()
	if err := s.managedIdentities.UpdateIdentity(r.Context(), actor.Identity); err != nil {
		auditErr = errors.New("failed to update identity")
		writeError(w, http.StatusInternalServerError, "failed to update identity")
		return
	}
	auditAllowed = true
	auditErr = nil
	auditTarget = managedIdentityAuditTarget(actor.Identity.ID, actor.Identity.DisplayName)

	writeJSON(w, http.StatusOK, managedIdentityInfo{
		ID:          actor.Identity.ID,
		DisplayName: actor.Identity.DisplayName,
		Role:        actor.Membership.Role,
		CreatedAt:   actor.Identity.CreatedAt,
		UpdatedAt:   actor.Identity.UpdatedAt,
	})
}

func (s *Server) deleteManagedIdentity(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("identity delete failed")
	auditTarget := managedIdentityAuditTarget(chi.URLParam(r, "identityID"), "")
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "identity.delete", auditAllowed, auditErr, auditTarget)
	}()

	s.managedIdentityMu.Lock()
	defer s.managedIdentityMu.Unlock()

	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleAdmin)
	if !ok {
		return
	}
	auditTarget = managedIdentityAuditTarget(actor.Identity.ID, actor.Identity.DisplayName)

	snapshot, err := s.loadManagedIdentityDeleteSnapshot(r.Context(), actor.Identity.ID)
	if err != nil {
		auditErr = errors.New("failed to load identity delete snapshot")
		writeError(w, http.StatusInternalServerError, "failed to delete identity")
		return
	}

	if err := s.deleteManagedIdentityChildren(r.Context(), snapshot, actor.UserID); err != nil {
		auditErr = fmt.Errorf("failed to delete identity children: %w", err)
		writeError(w, http.StatusInternalServerError, "failed to delete identity")
		return
	}

	if err := s.managedIdentities.DeleteIdentity(r.Context(), actor.Identity.ID); err != nil {
		if restoreErr := s.restoreManagedIdentityChildren(r.Context(), snapshot, actor.UserID); restoreErr != nil {
			auditErr = fmt.Errorf("failed to delete identity and restore children: %w", restoreErr)
			writeError(w, http.StatusInternalServerError, "failed to delete identity")
			return
		}
		auditErr = errors.New("failed to delete identity")
		writeError(w, http.StatusInternalServerError, "failed to delete identity")
		return
	}

	response := map[string]any{"status": "deleted"}
	var warnings []string
	if _, err := s.apiTokens.RevokeAllAPITokensByOwner(r.Context(), core.APITokenOwnerKindManagedIdentity, actor.Identity.ID); err != nil {
		warnings = append(warnings, "failed to delete identity api tokens")
	}
	if len(warnings) > 0 {
		response["cleanup"] = "partial"
		response["warnings"] = warnings
	}
	auditAllowed = true
	auditErr = nil

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) listManagedIdentityMembers(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleViewer)
	if !ok {
		return
	}

	memberships, err := s.identityMemberships.ListMembershipsByIdentity(r.Context(), actor.Identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list identity members")
		return
	}

	out := make([]managedIdentityMemberInfo, 0, len(memberships))
	for _, membership := range memberships {
		out = append(out, managedIdentityMemberInfo{
			UserID:    membership.UserID,
			Email:     membership.Email,
			Role:      membership.Role,
			CreatedAt: membership.CreatedAt,
			UpdatedAt: membership.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if managedIdentityRoleRank[out[i].Role] != managedIdentityRoleRank[out[j].Role] {
			return managedIdentityRoleRank[out[i].Role] > managedIdentityRoleRank[out[j].Role]
		}
		if out[i].Email != out[j].Email {
			return out[i].Email < out[j].Email
		}
		return out[i].UserID < out[j].UserID
	})

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putManagedIdentityMember(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("identity member update failed")
	auditTarget := managedIdentityMemberAuditTarget(chi.URLParam(r, "identityID"), "")
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "identity.member.put", auditAllowed, auditErr, auditTarget)
	}()

	var req putManagedIdentityMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	role := normalizeManagedIdentityRole(req.Role)
	if role == "" {
		auditErr = errors.New("invalid role")
		writeError(w, http.StatusBadRequest, "role must be viewer, editor, or admin")
		return
	}
	email := emailutil.Normalize(req.Email)
	if email == "" {
		auditErr = errors.New("email is required")
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	s.managedIdentityMu.Lock()
	defer s.managedIdentityMu.Unlock()

	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleAdmin)
	if !ok {
		return
	}
	auditTarget = managedIdentityMemberAuditTarget(actor.Identity.ID, email)

	user, err := s.users.FindOrCreateUser(r.Context(), email)
	if err != nil {
		auditErr = errors.New("failed to resolve user")
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return
	}

	existing, err := s.identityMemberships.GetMembership(r.Context(), actor.Identity.ID, user.ID)
	if err != nil && !errors.Is(err, core.ErrNotFound) {
		auditErr = errors.New("failed to resolve existing membership")
		writeError(w, http.StatusInternalServerError, "failed to resolve existing membership")
		return
	}
	if existing != nil && existing.Role == managedIdentityRoleAdmin && role != managedIdentityRoleAdmin {
		if !s.ensureManagedIdentityHasAnotherAdmin(w, r, actor.Identity.ID, user.ID) {
			return
		}
	}

	membership, err := s.identityMemberships.UpsertMembership(r.Context(), &core.ManagedIdentityMembership{
		IdentityID: actor.Identity.ID,
		UserID:     user.ID,
		Email:      user.Email,
		Role:       role,
	})
	if err != nil {
		auditErr = errors.New("failed to persist identity member")
		writeError(w, http.StatusInternalServerError, "failed to persist identity member")
		return
	}
	auditAllowed = true
	auditErr = nil
	auditTarget = managedIdentityMemberAuditTarget(actor.Identity.ID, membership.Email)

	writeJSON(w, http.StatusOK, managedIdentityMemberInfo{
		UserID:    membership.UserID,
		Email:     membership.Email,
		Role:      membership.Role,
		CreatedAt: membership.CreatedAt,
		UpdatedAt: membership.UpdatedAt,
	})
}

func (s *Server) deleteManagedIdentityMember(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("identity member delete failed")
	auditTarget := managedIdentityMemberAuditTarget(chi.URLParam(r, "identityID"), "")
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "identity.member.delete", auditAllowed, auditErr, auditTarget)
	}()

	rawEmail, err := url.PathUnescape(strings.TrimSpace(chi.URLParam(r, "email")))
	if err != nil {
		auditErr = errors.New("invalid email path")
		writeError(w, http.StatusBadRequest, "invalid email path")
		return
	}
	email := emailutil.Normalize(rawEmail)
	if email == "" {
		auditErr = errors.New("email is required")
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	s.managedIdentityMu.Lock()
	defer s.managedIdentityMu.Unlock()

	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleAdmin)
	if !ok {
		return
	}
	auditTarget = managedIdentityMemberAuditTarget(actor.Identity.ID, email)

	user, err := s.users.FindUserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			auditErr = errors.New("identity member not found")
			writeError(w, http.StatusNotFound, "identity member not found")
			return
		}
		auditErr = errors.New("failed to resolve identity member")
		writeError(w, http.StatusInternalServerError, "failed to resolve identity member")
		return
	}

	membership, err := s.identityMemberships.GetMembership(r.Context(), actor.Identity.ID, user.ID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			auditErr = errors.New("identity member not found")
			writeError(w, http.StatusNotFound, "identity member not found")
			return
		}
		auditErr = errors.New("failed to resolve identity member")
		writeError(w, http.StatusInternalServerError, "failed to resolve identity member")
		return
	}
	if membership.Role == managedIdentityRoleAdmin && !s.ensureManagedIdentityHasAnotherAdmin(w, r, actor.Identity.ID, membership.UserID) {
		return
	}
	if err := s.identityMemberships.DeleteMembership(r.Context(), actor.Identity.ID, membership.UserID); err != nil {
		auditErr = errors.New("failed to delete identity member")
		writeError(w, http.StatusInternalServerError, "failed to delete identity member")
		return
	}
	auditAllowed = true
	auditErr = nil
	auditTarget = managedIdentityMemberAuditTarget(actor.Identity.ID, membership.Email)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) listManagedIdentityGrants(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleViewer)
	if !ok {
		return
	}

	grants, err := s.identityGrants.ListGrantsByIdentity(r.Context(), actor.Identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list identity grants")
		return
	}

	p := PrincipalFromContext(r.Context())
	out := make([]managedIdentityGrantInfo, 0, len(grants))
	for _, grant := range grants {
		if !s.managedIdentityGrantVisibleToActor(r.Context(), grant.Plugin, p, actor.Membership.Role) {
			continue
		}
		out = append(out, managedIdentityGrantInfo{
			Plugin:     grant.Plugin,
			Operations: managedIdentityGrantOperationsResponse(grant.Operations),
			CreatedAt:  grant.CreatedAt,
			UpdatedAt:  grant.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Plugin < out[j].Plugin })

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putManagedIdentityGrant(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("identity grant update failed")
	auditTarget := managedIdentityGrantAuditTarget(chi.URLParam(r, "identityID"), chi.URLParam(r, "plugin"))
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "identity.grant.put", auditAllowed, auditErr, auditTarget)
	}()

	plugin := strings.TrimSpace(chi.URLParam(r, "plugin"))

	s.managedIdentityMu.Lock()
	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleEditor)
	s.managedIdentityMu.Unlock()
	if !ok {
		return
	}

	var req putManagedIdentityGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		auditErr = errors.New("invalid JSON body")
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	p := managedIdentityGrantValidationPrincipal(PrincipalFromContext(r.Context()), actor.UserID)
	if !s.managedIdentityGrantPluginVisible(r.Context(), plugin, p) {
		auditErr = errors.New("plugin not found")
		writeError(w, http.StatusNotFound, "plugin not found")
		return
	}
	operations := normalizeManagedIdentityOperations(req.Operations)
	if len(req.Operations) > 0 && len(operations) == 0 {
		auditErr = errors.New("operations must contain at least one non-blank operation")
		writeError(w, http.StatusBadRequest, "operations must contain at least one non-blank operation")
		return
	}
	if err := s.validateManagedIdentityGrantOperations(r.Context(), plugin, operations, p); err != nil {
		auditErr = err
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.managedIdentityMu.Lock()
	defer s.managedIdentityMu.Unlock()

	actor, ok = s.resolveManagedIdentityActor(w, r, managedIdentityRoleEditor)
	if !ok {
		return
	}
	if !s.managedIdentityGrantPluginVisible(r.Context(), plugin, p) {
		auditErr = errors.New("plugin not found")
		writeError(w, http.StatusNotFound, "plugin not found")
		return
	}
	if !s.managedIdentityInvocationSupported(plugin) {
		auditErr = fmt.Errorf("plugin %q does not yet support managed-identity invocation in this phase", plugin)
		writeError(w, http.StatusBadRequest, auditErr.Error())
		return
	}
	auditTarget = managedIdentityGrantAuditTarget(actor.Identity.ID, plugin)

	grant, err := s.identityGrants.UpsertGrant(r.Context(), &core.ManagedIdentityGrant{
		IdentityID: actor.Identity.ID,
		Plugin:     plugin,
		Operations: operations,
	})
	if err != nil {
		auditErr = errors.New("failed to persist identity grant")
		writeError(w, http.StatusInternalServerError, "failed to persist identity grant")
		return
	}
	auditAllowed = true
	auditErr = nil

	writeJSON(w, http.StatusOK, managedIdentityGrantInfo{
		Plugin:     grant.Plugin,
		Operations: managedIdentityGrantOperationsResponse(grant.Operations),
		CreatedAt:  grant.CreatedAt,
		UpdatedAt:  grant.UpdatedAt,
	})
}

func managedIdentityGrantOperationsResponse(operations []string) []string {
	if len(operations) == 0 {
		return []string{}
	}
	return append([]string(nil), operations...)
}

func (s *Server) deleteManagedIdentityGrant(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("identity grant delete failed")
	auditTarget := managedIdentityGrantAuditTarget(chi.URLParam(r, "identityID"), chi.URLParam(r, "plugin"))
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "identity.grant.delete", auditAllowed, auditErr, auditTarget)
	}()

	plugin := strings.TrimSpace(chi.URLParam(r, "plugin"))

	s.managedIdentityMu.Lock()
	defer s.managedIdentityMu.Unlock()

	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleEditor)
	if !ok {
		return
	}
	if _, err := s.providers.Get(plugin); err != nil {
		if _, grantErr := s.identityGrants.GetGrant(r.Context(), actor.Identity.ID, plugin); grantErr != nil {
			if errors.Is(grantErr, core.ErrNotFound) {
				auditErr = errors.New("identity grant not found")
				writeError(w, http.StatusNotFound, "identity grant not found")
				return
			}
			auditErr = errors.New("failed to resolve identity grant")
			writeError(w, http.StatusInternalServerError, "failed to resolve identity grant")
			return
		}
	} else if !s.allowProviderContext(r.Context(), PrincipalFromContext(r.Context()), plugin) {
		auditErr = errors.New("plugin not found")
		writeError(w, http.StatusNotFound, "plugin not found")
		return
	}
	auditTarget = managedIdentityGrantAuditTarget(actor.Identity.ID, plugin)
	if err := s.identityGrants.DeleteGrant(r.Context(), actor.Identity.ID, plugin); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			auditErr = errors.New("identity grant not found")
			writeError(w, http.StatusNotFound, "identity grant not found")
			return
		}
		auditErr = errors.New("failed to delete identity grant")
		writeError(w, http.StatusInternalServerError, "failed to delete identity grant")
		return
	}
	auditAllowed = true
	auditErr = nil

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) resolveManagedIdentityUser(w http.ResponseWriter, r *http.Request) (string, *core.User, bool) {
	if !s.ensureManagedIdentityRoutesAvailable(w) {
		return "", nil, false
	}
	userID, err := s.resolveUserID(w, r)
	if err != nil {
		return "", nil, false
	}
	user, err := s.users.GetUser(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return "", nil, false
	}
	return userID, user, true
}

func (s *Server) resolveManagedIdentityActor(w http.ResponseWriter, r *http.Request, requiredRole string) (*managedIdentityActor, bool) {
	userID, _, ok := s.resolveManagedIdentityUser(w, r)
	if !ok {
		return nil, false
	}

	identityID := strings.TrimSpace(chi.URLParam(r, "identityID"))
	if identityID == "" {
		writeError(w, http.StatusBadRequest, "identityID is required")
		return nil, false
	}
	identity, err := s.managedIdentities.GetIdentity(r.Context(), identityID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "identity not found")
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, "failed to resolve identity")
		return nil, false
	}
	membership, err := s.identityMemberships.GetMembership(r.Context(), identityID, userID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "identity not found")
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, "failed to resolve identity membership")
		return nil, false
	}
	if !managedIdentityRoleAllows(membership.Role, requiredRole) {
		writeError(w, http.StatusForbidden, "identity access denied")
		return nil, false
	}
	return &managedIdentityActor{
		UserID:     userID,
		Identity:   identity,
		Membership: membership,
	}, true
}

func (s *Server) ensureManagedIdentityRoutesAvailable(w http.ResponseWriter) bool {
	if s.noAuth {
		writeError(w, http.StatusServiceUnavailable, "managed identities require auth to be enabled")
		return false
	}
	if s.managedIdentities == nil || s.identityMemberships == nil || s.identityGrants == nil {
		writeError(w, http.StatusServiceUnavailable, "managed identities are unavailable")
		return false
	}
	return true
}

func (s *Server) ensureManagedIdentityHasAnotherAdmin(w http.ResponseWriter, r *http.Request, identityID, excludedUserID string) bool {
	memberships, err := s.identityMemberships.ListMembershipsByIdentity(r.Context(), identityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve identity admins")
		return false
	}
	for _, membership := range memberships {
		if membership.UserID == excludedUserID {
			continue
		}
		if membership.Role == managedIdentityRoleAdmin {
			return true
		}
	}
	writeError(w, http.StatusConflict, "identity must retain at least one admin")
	return false
}

type managedIdentityDeleteSnapshot struct {
	Grants      []*core.ManagedIdentityGrant
	Memberships []*core.ManagedIdentityMembership
}

func (s *Server) loadManagedIdentityDeleteSnapshot(ctx context.Context, identityID string) (*managedIdentityDeleteSnapshot, error) {
	grants, err := s.identityGrants.ListGrantsByIdentity(ctx, identityID)
	if err != nil {
		return nil, err
	}
	memberships, err := s.identityMemberships.ListMembershipsByIdentity(ctx, identityID)
	if err != nil {
		return nil, err
	}
	return &managedIdentityDeleteSnapshot{
		Grants:      cloneManagedIdentityGrants(grants),
		Memberships: cloneManagedIdentityMemberships(memberships),
	}, nil
}

func (s *Server) deleteManagedIdentityChildren(ctx context.Context, snapshot *managedIdentityDeleteSnapshot, actorUserID string) error {
	deleted := &managedIdentityDeleteSnapshot{}

	for _, grant := range snapshot.Grants {
		if err := s.identityGrants.DeleteGrant(ctx, grant.IdentityID, grant.Plugin); err != nil {
			if restoreErr := s.restoreManagedIdentityChildren(ctx, deleted, actorUserID); restoreErr != nil {
				return fmt.Errorf("delete managed identity grant: %w (restore deleted children: %v)", err, restoreErr)
			}
			return err
		}
		deleted.Grants = append(deleted.Grants, cloneManagedIdentityGrant(grant))
	}

	for _, membership := range managedIdentityDeleteMembershipOrder(snapshot.Memberships, actorUserID) {
		if err := s.identityMemberships.DeleteMembership(ctx, membership.IdentityID, membership.UserID); err != nil {
			if restoreErr := s.restoreManagedIdentityChildren(ctx, deleted, actorUserID); restoreErr != nil {
				return fmt.Errorf("delete managed identity membership: %w (restore deleted children: %v)", err, restoreErr)
			}
			return err
		}
		deleted.Memberships = append(deleted.Memberships, cloneManagedIdentityMembership(membership))
	}

	return nil
}

func (s *Server) restoreManagedIdentityChildren(ctx context.Context, snapshot *managedIdentityDeleteSnapshot, actorUserID string) error {
	if snapshot == nil {
		return nil
	}
	failedMemberships := s.restoreManagedIdentityMemberships(ctx, managedIdentityRestoreMembershipOrder(snapshot.Memberships, actorUserID))
	if len(failedMemberships) > 0 {
		failedMemberships = s.restoreManagedIdentityMemberships(ctx, failedMemberships)
	}
	failedGrants := s.restoreManagedIdentityGrants(ctx, snapshot.Grants)
	if len(failedGrants) > 0 {
		failedGrants = s.restoreManagedIdentityGrants(ctx, failedGrants)
	}

	var restoreErr error
	for _, membership := range failedMemberships {
		restoreErr = errors.Join(restoreErr, fmt.Errorf("restore managed identity membership %s: failed after retry", membership.UserID))
	}
	for _, grant := range failedGrants {
		restoreErr = errors.Join(restoreErr, fmt.Errorf("restore managed identity grant %s: failed after retry", grant.Plugin))
	}
	return restoreErr
}

func managedIdentityDeleteMembershipOrder(memberships []*core.ManagedIdentityMembership, actorUserID string) []*core.ManagedIdentityMembership {
	if len(memberships) == 0 {
		return nil
	}
	ordered := cloneManagedIdentityMemberships(memberships)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].UserID == actorUserID {
			return false
		}
		if ordered[j].UserID == actorUserID {
			return true
		}
		return ordered[i].UserID < ordered[j].UserID
	})
	return ordered
}

func managedIdentityRestoreMembershipOrder(memberships []*core.ManagedIdentityMembership, actorUserID string) []*core.ManagedIdentityMembership {
	if len(memberships) == 0 {
		return nil
	}
	ordered := cloneManagedIdentityMemberships(memberships)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].UserID == actorUserID {
			return true
		}
		if ordered[j].UserID == actorUserID {
			return false
		}
		return ordered[i].UserID < ordered[j].UserID
	})
	return ordered
}

func (s *Server) restoreManagedIdentityMemberships(ctx context.Context, memberships []*core.ManagedIdentityMembership) []*core.ManagedIdentityMembership {
	if len(memberships) == 0 {
		return nil
	}
	failed := make([]*core.ManagedIdentityMembership, 0)
	for _, membership := range memberships {
		if err := s.identityMemberships.RestoreMembership(ctx, cloneManagedIdentityMembership(membership)); err != nil {
			failed = append(failed, cloneManagedIdentityMembership(membership))
		}
	}
	return failed
}

func (s *Server) restoreManagedIdentityGrants(ctx context.Context, grants []*core.ManagedIdentityGrant) []*core.ManagedIdentityGrant {
	if len(grants) == 0 {
		return nil
	}
	failed := make([]*core.ManagedIdentityGrant, 0)
	for _, grant := range grants {
		if err := s.identityGrants.RestoreGrant(ctx, cloneManagedIdentityGrant(grant)); err != nil {
			failed = append(failed, cloneManagedIdentityGrant(grant))
		}
	}
	return failed
}

func cloneManagedIdentityGrants(grants []*core.ManagedIdentityGrant) []*core.ManagedIdentityGrant {
	if len(grants) == 0 {
		return nil
	}
	cloned := make([]*core.ManagedIdentityGrant, 0, len(grants))
	for _, grant := range grants {
		cloned = append(cloned, cloneManagedIdentityGrant(grant))
	}
	return cloned
}

func cloneManagedIdentityGrant(grant *core.ManagedIdentityGrant) *core.ManagedIdentityGrant {
	if grant == nil {
		return nil
	}
	clone := *grant
	clone.Operations = append([]string(nil), grant.Operations...)
	return &clone
}

func cloneManagedIdentityMemberships(memberships []*core.ManagedIdentityMembership) []*core.ManagedIdentityMembership {
	if len(memberships) == 0 {
		return nil
	}
	cloned := make([]*core.ManagedIdentityMembership, 0, len(memberships))
	for _, membership := range memberships {
		cloned = append(cloned, cloneManagedIdentityMembership(membership))
	}
	return cloned
}

func cloneManagedIdentityMembership(membership *core.ManagedIdentityMembership) *core.ManagedIdentityMembership {
	if membership == nil {
		return nil
	}
	clone := *membership
	return &clone
}

func (s *Server) recoverManagedIdentityCreateMembership(ctx context.Context, identity *core.ManagedIdentity, membership *core.ManagedIdentityMembership, createErr error) (*core.ManagedIdentityMembership, error) {
	if membership == nil {
		return nil, fmt.Errorf("failed to create identity membership: %w", createErr)
	}
	if err := s.identityMemberships.RestoreMembership(ctx, cloneManagedIdentityMembership(membership)); err == nil {
		return membership, nil
	} else {
		restoreErr := err
		if deleteErr := s.managedIdentities.DeleteIdentity(ctx, identity.ID); deleteErr == nil {
			return nil, fmt.Errorf("failed to create identity membership: %w", createErr)
		} else {
			if err := s.identityMemberships.RestoreMembership(ctx, cloneManagedIdentityMembership(membership)); err == nil {
				return membership, nil
			}
			return nil, fmt.Errorf("failed to create identity membership and roll back identity: %w", errors.Join(createErr, restoreErr, deleteErr))
		}
	}
}

func (s *Server) managedIdentityGrantPluginVisible(ctx context.Context, plugin string, p *principal.Principal) bool {
	if _, err := s.providers.Get(plugin); err != nil {
		return false
	}
	return s.allowProviderContext(ctx, p, plugin)
}

func (s *Server) managedIdentityGrantVisibleToActor(ctx context.Context, plugin string, p *principal.Principal, role string) bool {
	if _, err := s.providers.Get(plugin); err != nil {
		return errors.Is(err, core.ErrNotFound) && managedIdentityRoleAllows(role, managedIdentityRoleEditor)
	}
	return s.allowProviderContext(ctx, p, plugin)
}

func (s *Server) managedIdentityInvocationSupported(plugin string) bool {
	if entry, ok := s.pluginDefs[plugin]; ok && entry != nil {
		return managedIdentityPluginConnectionMode(entry) == core.ConnectionModeNone
	}
	prov, err := s.providers.Get(plugin)
	if err != nil || prov == nil {
		return false
	}
	return prov.ConnectionMode() == core.ConnectionModeNone
}

func managedIdentityPluginConnectionMode(entry *config.ProviderEntry) core.ConnectionMode {
	needUser := false
	needIdentity := false

	addMode := func(mode core.ConnectionMode) {
		switch mode {
		case core.ConnectionModeUser:
			needUser = true
		case core.ConnectionModeIdentity:
			needIdentity = true
		}
	}

	addMode(managedIdentityConnectionModeForDef(config.EffectivePluginConnectionDef(entry, entry.ManifestSpec())))

	for name := range managedIdentityGrantNamedConnectionNames(entry) {
		conn, ok := config.EffectiveNamedConnectionDef(entry, entry.ManifestSpec(), name)
		if !ok {
			continue
		}
		addMode(managedIdentityConnectionModeForDef(conn))
	}

	switch {
	case needUser:
		return core.ConnectionModeUser
	case needIdentity:
		return core.ConnectionModeIdentity
	default:
		return core.ConnectionModeNone
	}
}

func managedIdentityGrantNamedConnectionNames(entry *config.ProviderEntry) map[string]struct{} {
	names := make(map[string]struct{})
	if entry == nil {
		return names
	}
	if spec := entry.ManifestSpec(); spec != nil {
		for name := range spec.Connections {
			resolved := config.ResolveConnectionAlias(name)
			if resolved != "" && resolved != config.PluginConnectionName {
				names[resolved] = struct{}{}
			}
		}
	}
	for name := range entry.Connections {
		resolved := config.ResolveConnectionAlias(name)
		if resolved != "" && resolved != config.PluginConnectionName {
			names[resolved] = struct{}{}
		}
	}
	return names
}

func managedIdentityConnectionModeForDef(conn config.ConnectionDef) core.ConnectionMode {
	if conn.Mode != "" {
		return core.ConnectionMode(conn.Mode)
	}
	switch conn.Auth.Type {
	case "", "none":
		return core.ConnectionModeNone
	default:
		return core.ConnectionModeUser
	}
}

func (s *Server) managedIdentityGrantCatalog(ctx context.Context, plugin string, p *principal.Principal) (*catalog.Catalog, error) {
	prov, err := s.providers.Get(plugin)
	if err != nil {
		return nil, err
	}
	var resolver invocation.TokenResolver
	if tr, ok := s.invoker.(invocation.TokenResolver); ok {
		resolver = tr
	}
	ctx = invocation.WithAccessContext(ctx, s.providerAccessContextWithContext(ctx, p, plugin))
	targets, err := s.managedIdentityGrantCatalogTargets(ctx, plugin, p)
	if err != nil {
		return nil, err
	}
	var firstErr error
	for _, target := range targets {
		cat, err := s.managedIdentityGrantCatalogForConnection(ctx, prov, plugin, resolver, p, target.connection, target.instance)
		if err == nil && cat != nil {
			return cat, nil
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

type managedIdentityGrantCatalogTarget struct {
	connection string
	instance   string
}

func (s *Server) managedIdentityGrantCatalogTargets(ctx context.Context, plugin string, p *principal.Principal) ([]managedIdentityGrantCatalogTarget, error) {
	targets := make([]managedIdentityGrantCatalogTarget, 0, 4)
	seen := map[string]struct{}{}
	addTarget := func(connection, instance string) {
		connection = config.ResolveConnectionAlias(strings.TrimSpace(connection))
		instance = strings.TrimSpace(instance)
		key := connection + "\x00" + instance
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, managedIdentityGrantCatalogTarget{
			connection: connection,
			instance:   instance,
		})
	}

	for _, connection := range s.sessionCatalogConnections(plugin, p, "") {
		connection, instance := s.workloadBindingSelectors(p, plugin, connection, "")
		addTarget(connection, instance)
	}
	if p == nil || !p.HasUserContext() || strings.TrimSpace(p.UserID) == "" {
		if len(targets) == 0 {
			addTarget("", "")
		}
		return targets, nil
	}

	tokens, err := s.tokens.ListTokensForIntegration(ctx, p.UserID, plugin)
	if err != nil {
		return nil, fmt.Errorf("list integration tokens for grant validation: %w", err)
	}
	byConnection := make(map[string][]*core.IntegrationToken, len(tokens))
	for _, token := range tokens {
		byConnection[token.Connection] = append(byConnection[token.Connection], token)
	}
	connections := make([]string, 0, len(byConnection))
	for connection := range byConnection {
		connections = append(connections, connection)
	}
	sort.Strings(connections)
	for _, connection := range connections {
		connectionTokens := byConnection[connection]
		if len(connectionTokens) <= 1 {
			addTarget(connection, "")
			continue
		}
		sort.Slice(connectionTokens, func(i, j int) bool {
			return connectionTokens[i].Instance < connectionTokens[j].Instance
		})
		for _, token := range connectionTokens {
			addTarget(connection, token.Instance)
		}
	}
	if len(targets) == 0 {
		addTarget("", "")
	}
	return targets, nil
}

func (s *Server) managedIdentityGrantCatalogForConnection(
	ctx context.Context,
	prov core.Provider,
	plugin string,
	resolver invocation.TokenResolver,
	p *principal.Principal,
	connection string,
	instance string,
) (*catalog.Catalog, error) {
	cat, attempted, err := s.managedIdentityGrantSessionCatalog(ctx, prov, plugin, resolver, p, connection, instance)
	if err != nil {
		return nil, err
	}
	if attempted && cat != nil {
		return cat, nil
	}
	cat, _, err = invocation.ResolveCatalogStrictWithMetadata(ctx, prov, plugin, resolver, p, connection, instance)
	return cat, err
}

func (s *Server) managedIdentityGrantSessionCatalog(
	ctx context.Context,
	prov core.Provider,
	plugin string,
	resolver invocation.TokenResolver,
	p *principal.Principal,
	connection string,
	instance string,
) (*catalog.Catalog, bool, error) {
	if !core.SupportsSessionCatalog(prov) {
		return nil, false, nil
	}
	if prov.ConnectionMode() == core.ConnectionModeNone {
		if enrichedCtx, token, ok, err := s.managedIdentityGrantUserSessionContext(ctx, prov, plugin, resolver, p, connection, instance); err != nil {
			return nil, true, err
		} else if ok {
			cat, _, err := core.CatalogForRequest(enrichedCtx, prov, token)
			return cat, true, err
		}
		if resolver != nil && p != nil {
			enrichedCtx, token, err := resolver.ResolveToken(ctx, p, plugin, connection, instance)
			if err != nil {
				return nil, true, err
			}
			if token != "" {
				cat, _, err := core.CatalogForRequest(enrichedCtx, prov, token)
				return cat, true, err
			}
		}
		ctx = invocation.WithCredentialContext(ctx, invocation.CredentialContext{Mode: core.ConnectionModeNone})
		cat, _, err := core.CatalogForRequest(ctx, prov, "")
		return cat, true, err
	}
	if resolver == nil || p == nil {
		return nil, false, nil
	}

	enrichedCtx, token, err := resolver.ResolveToken(ctx, p, plugin, connection, instance)
	if err != nil {
		return nil, true, err
	}
	cat, _, err := core.CatalogForRequest(enrichedCtx, prov, token)
	return cat, true, err
}

func managedIdentityGrantValidationPrincipal(p *principal.Principal, userID string) *principal.Principal {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return p
	}
	if p == nil {
		return &principal.Principal{
			Kind:      principal.KindUser,
			UserID:    userID,
			SubjectID: principal.UserSubjectID(userID),
		}
	}
	if p.UserID == userID {
		return p
	}
	clone := *p
	clone.UserID = userID
	if strings.TrimSpace(clone.SubjectID) == "" {
		clone.SubjectID = principal.UserSubjectID(userID)
	}
	return &clone
}

type managedIdentitySessionTokenResolver interface {
	ResolveUserToken(ctx context.Context, prov core.Provider, userID, providerName, connection, instance string) (context.Context, string, error)
}

func (s *Server) managedIdentityGrantUserSessionContext(ctx context.Context, prov core.Provider, plugin string, resolver invocation.TokenResolver, p *principal.Principal, connection string, instance string) (context.Context, string, bool, error) {
	if p == nil || strings.TrimSpace(p.UserID) == "" {
		return ctx, "", false, nil
	}
	if userResolver, ok := resolver.(managedIdentitySessionTokenResolver); ok {
		enrichedCtx, token, err := userResolver.ResolveUserToken(ctx, prov, p.UserID, plugin, connection, instance)
		if err != nil {
			if errors.Is(err, invocation.ErrNoToken) {
				return ctx, "", false, nil
			}
			return ctx, "", true, err
		}
		return enrichedCtx, token, true, nil
	}
	if s.tokens == nil {
		return ctx, "", false, nil
	}

	var (
		storedToken *core.IntegrationToken
		err         error
	)
	if strings.TrimSpace(instance) != "" {
		storedToken, err = s.tokens.Token(ctx, p.UserID, plugin, connection, instance)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				return ctx, "", false, nil
			}
			return ctx, "", true, err
		}
	} else {
		tokens, listErr := s.tokens.ListTokensForConnection(ctx, p.UserID, plugin, connection)
		if listErr != nil {
			return ctx, "", true, listErr
		}
		switch len(tokens) {
		case 0:
			return ctx, "", false, nil
		case 1:
			storedToken = tokens[0]
		default:
			return ctx, "", true, fmt.Errorf("integration %q has %d connections; specify which instance to use with the %q parameter", plugin, len(tokens), "_instance")
		}
	}
	if storedToken == nil {
		return ctx, "", false, nil
	}

	ctx = invocation.WithCredentialContext(ctx, invocation.CredentialContext{
		Mode:       core.ConnectionModeUser,
		SubjectID:  principal.UserSubjectID(p.UserID),
		Connection: storedToken.Connection,
		Instance:   storedToken.Instance,
	})
	if storedToken.MetadataJSON != "" {
		var connParams map[string]string
		if err := json.Unmarshal([]byte(storedToken.MetadataJSON), &connParams); err == nil && len(connParams) > 0 {
			ctx = core.WithConnectionParams(ctx, connParams)
		}
	}
	return ctx, storedToken.AccessToken, true, nil
}

func (s *Server) managedIdentityGrantableOperations(ctx context.Context, plugin string, p *principal.Principal) (*catalog.Catalog, []catalog.CatalogOperation, map[string]struct{}, error) {
	cat, err := s.managedIdentityGrantCatalog(ctx, plugin, p)
	if err != nil || cat == nil {
		return nil, nil, nil, fmt.Errorf("plugin %q does not expose operations for validation", plugin)
	}
	visible := grantableCatalogOperations(cat.Operations)
	if len(visible) == 0 {
		return nil, nil, nil, fmt.Errorf("plugin %q does not expose operations for validation", plugin)
	}
	known := make(map[string]struct{}, len(visible))
	for i := range visible {
		known[visible[i].ID] = struct{}{}
	}
	return cat, visible, known, nil
}

func (s *Server) validateManagedIdentityGrantOperations(ctx context.Context, plugin string, operations []string, p *principal.Principal) error {
	cat, visible, known, err := s.managedIdentityGrantableOperations(ctx, plugin, p)
	if err != nil {
		return err
	}
	allowedVisible := visible
	if s.authorizer != nil {
		filtered := invocation.FilterCatalogForPrincipal(ctx, cat, plugin, p, s.authorizer)
		allowedVisible = grantableCatalogOperations(filtered.Operations)
	}
	allowed := make(map[string]struct{}, len(allowedVisible))
	for i := range allowedVisible {
		allowed[allowedVisible[i].ID] = struct{}{}
	}
	if len(operations) == 0 {
		for operation := range known {
			if _, ok := allowed[operation]; !ok {
				return fmt.Errorf("plugin-wide access is not authorized for plugin %q", plugin)
			}
		}
		return nil
	}
	for _, operation := range operations {
		if _, ok := known[operation]; !ok {
			return fmt.Errorf("unknown operation %q for plugin %q", operation, plugin)
		}
		if _, ok := allowed[operation]; !ok {
			return fmt.Errorf("operation %q is not authorized for plugin %q", operation, plugin)
		}
	}
	return nil
}

func (s *Server) validateManagedIdentityPermissionOperations(ctx context.Context, plugin string, operations []string, p *principal.Principal) error {
	_, _, known, err := s.managedIdentityGrantableOperations(ctx, plugin, p)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		if _, ok := known[operation]; !ok {
			return fmt.Errorf("unknown operation %q for plugin %q", operation, plugin)
		}
	}
	return nil
}

func grantableCatalogOperations(ops []catalog.CatalogOperation) []catalog.CatalogOperation {
	filtered := make([]catalog.CatalogOperation, 0, len(ops))
	for i := range ops {
		if strings.TrimSpace(ops[i].ID) == "" {
			continue
		}
		filtered = append(filtered, ops[i])
	}
	return filtered
}

func normalizeManagedIdentityRole(role string) string {
	role = strings.TrimSpace(strings.ToLower(role))
	if _, ok := managedIdentityRoleRank[role]; !ok {
		return ""
	}
	return role
}

func managedIdentityRoleAllows(actualRole, requiredRole string) bool {
	if requiredRole == "" {
		return true
	}
	actualRank := managedIdentityRoleRank[normalizeManagedIdentityRole(actualRole)]
	requiredRank := managedIdentityRoleRank[normalizeManagedIdentityRole(requiredRole)]
	return actualRank >= requiredRank
}

func normalizeManagedIdentityOperations(operations []string) []string {
	if len(operations) == 0 {
		return nil
	}
	out := make([]string, 0, len(operations))
	for _, operation := range operations {
		operation = strings.TrimSpace(operation)
		if operation == "" || slices.Contains(out, operation) {
			continue
		}
		out = append(out, operation)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}
