package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

// appAdminMemberRow is the shared authorization grant roster row used before
// Members / Identities projections. Field names match the Members UI contract.
type appAdminMemberRow struct {
	Email         string `json:"email,omitempty"`
	Role          string `json:"role"`
	Source        string `json:"source"`
	Mutable       bool   `json:"mutable"`
	Effective     bool   `json:"effective"`
	ShadowedBy    string `json:"shadowedBy,omitempty"`
	SelectorKind  string `json:"selectorKind,omitempty"`
	SelectorValue string `json:"selectorValue,omitempty"`
	SubjectID     string `json:"subjectId,omitempty"`
}

func (s *Server) mountAppAdminMembersRoutes(r chi.Router) {
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminUIObservabilityMiddleware, s.appAdminAuthorizationMiddleware).
		Get("/apps/{app}/admin/members", s.listAppAdminMembers)
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminUIObservabilityMiddleware, s.appAdminAuthorizationMiddleware).
		Post("/apps/{app}/admin/members", s.setAppAdminMember)
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminUIObservabilityMiddleware, s.appAdminAuthorizationMiddleware).
		Delete("/apps/{app}/admin/members", s.removeAppAdminMember)
}

type appAdminMemberSetRequest struct {
	SubjectID string `json:"subjectId"`
	Role      string `json:"role"`
}

type appAdminMemberRemoveRequest struct {
	SubjectID string `json:"subjectId"`
	Role      string `json:"role,omitempty"`
}

type appAdminMemberSetResponse struct {
	App       string `json:"app"`
	SubjectID string `json:"subjectId"`
	Role      string `json:"role"`
	Changed   bool   `json:"changed"`
}

type appAdminMemberRemoveResponse struct {
	App          string   `json:"app"`
	SubjectID    string   `json:"subjectId"`
	RemovedRoles []string `json:"removedRoles"`
}

func (s *Server) listAppAdminMembers(w http.ResponseWriter, r *http.Request) {
	appName := strings.TrimSpace(chi.URLParam(r, "app"))
	if appName == "" {
		writeError(w, http.StatusBadRequest, "app is required")
		return
	}
	if s.authorization == nil {
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}

	rows, err := s.listAppAuthorizationMemberRows(r.Context(), appName)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}
	// Members is the human/group access roster. Service-account grants are
	// owned by GET /apps/{app}/admin/identities.
	writeJSON(w, http.StatusOK, s.projectAppAdminHumanMemberRows(r.Context(), rows))
}

func (s *Server) setAppAdminMember(w http.ResponseWriter, r *http.Request) {
	appName := strings.TrimSpace(chi.URLParam(r, "app"))
	if appName == "" {
		writeError(w, http.StatusBadRequest, "app is required")
		return
	}
	request, ok := decodeAppAdminMemberSetRequest(w, r)
	if !ok {
		return
	}
	subjectID := strings.TrimSpace(request.SubjectID)
	role := strings.TrimSpace(request.Role)
	if subjectID == "" {
		writeError(w, http.StatusBadRequest, "subjectId is required")
		return
	}
	if err := validateAppAdminMemberSubjectID(subjectID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	recordAppAdminUITargetSubject(r.Context(), subjectID)
	if role == "" {
		writeError(w, http.StatusBadRequest, "role is required")
		return
	}
	if err := validateAppAdminMemberRole(role); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	existing, err := s.mutableAppAdminMemberRoles(r.Context(), appName, subjectID)
	if err != nil {
		slog.Error("app admin member roles load failed", "app", appName, "subject_id", subjectID, "error", err)
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}
	addRole := true
	for _, existingRole := range existing {
		if existingRole == role {
			addRole = false
			continue
		}
		if err := s.deleteAppAdminMemberRole(r.Context(), appName, existingRole, subjectID); err != nil {
			slog.Error("app admin member grant remove failed", "app", appName, "subject_id", subjectID, "role", existingRole, "error", err)
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
	}
	if addRole {
		if err := s.addAppAdminMemberRole(r.Context(), appName, role, subjectID); err != nil {
			slog.Error("app admin member grant add failed", "app", appName, "subject_id", subjectID, "role", role, "error", err)
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
	}
	writeJSON(w, http.StatusOK, appAdminMemberSetResponse{
		App:       appName,
		SubjectID: subjectID,
		Role:      role,
		Changed:   addRole || len(existing) > 1,
	})
}

func (s *Server) removeAppAdminMember(w http.ResponseWriter, r *http.Request) {
	appName := strings.TrimSpace(chi.URLParam(r, "app"))
	if appName == "" {
		writeError(w, http.StatusBadRequest, "app is required")
		return
	}
	request, ok := decodeAppAdminMemberRemoveRequest(w, r)
	if !ok {
		return
	}
	subjectID := strings.TrimSpace(request.SubjectID)
	if subjectID == "" {
		writeError(w, http.StatusBadRequest, "subjectId is required")
		return
	}
	if err := validateAppAdminMemberSubjectID(subjectID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	recordAppAdminUITargetSubject(r.Context(), subjectID)
	existing, err := s.mutableAppAdminMemberRoles(r.Context(), appName, subjectID)
	if err != nil {
		slog.Error("app admin member roles load failed", "app", appName, "subject_id", subjectID, "error", err)
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}
	roles := existing
	if role := strings.TrimSpace(request.Role); role != "" {
		if err := validateAppAdminMemberRole(role); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		found := false
		for _, existingRole := range existing {
			if existingRole == role {
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusBadRequest, "no mutable "+role+" grant found for "+subjectID+" on "+appName)
			return
		}
		roles = []string{role}
	}
	if len(roles) == 0 {
		writeError(w, http.StatusBadRequest, "no mutable member grants found for "+subjectID+" on "+appName)
		return
	}
	for _, role := range roles {
		if err := s.deleteAppAdminMemberRole(r.Context(), appName, role, subjectID); err != nil {
			slog.Error("app admin member grant remove failed", "app", appName, "subject_id", subjectID, "role", role, "error", err)
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
	}
	writeJSON(w, http.StatusOK, appAdminMemberRemoveResponse{
		App:          appName,
		SubjectID:    subjectID,
		RemovedRoles: roles,
	})
}

// isAppAdminServiceAccountRow partitions the shared app authorization grant
// roster: direct service_account subject_id grants belong on identities;
// humans, other subjects, and subject_set selectors belong on members.
func isAppAdminServiceAccountRow(row appAdminMemberRow) bool {
	if row.SelectorKind != "subject_id" || row.SubjectID == "" {
		return false
	}
	kind, _, ok := core.ParseSubjectID(row.SubjectID)
	return ok && kind == coredata.ManagedSubjectKindServiceAccount
}

// projectAppAdminHumanMemberRows is the human/group projection of the shared
// grant roster. Email enrichment belongs here — not in the shared mapper —
// so identities listing does not resolve user emails it will discard.
//
// Turning a subject ID into an email is user lookup, so it is gated on the
// explicit employee operator role rather than on the app-scoped admin grant
// that reached this handler. An app administrator without that role still sees
// the full grant roster - which subjects and groups hold which roles on their
// own app - but cannot use it to enumerate the directory.
func (s *Server) projectAppAdminHumanMemberRows(ctx context.Context, rows []appAdminMemberRow) []appAdminMemberRow {
	allowLookup := s.userLookupAllowed(ctx)
	out := make([]appAdminMemberRow, 0, len(rows))
	for _, row := range rows {
		if isAppAdminServiceAccountRow(row) {
			continue
		}
		if allowLookup && row.SelectorKind == "subject_id" && row.SubjectID != "" {
			row.Email = s.resolveAppAdminMemberEmail(ctx, row.SubjectID)
		}
		out = append(out, row)
	}
	return out
}

func (s *Server) listAppAuthorizationMemberRows(ctx context.Context, appName string) ([]appAdminMemberRow, error) {
	return s.listAuthorizationMemberRows(ctx, s.authorizationResource(strings.TrimSpace(appName)))
}

func (s *Server) listAuthorizationMemberRows(ctx context.Context, resource *proto.Resource) ([]appAdminMemberRow, error) {
	if resource == nil || strings.TrimSpace(resource.GetId()) == "" {
		return nil, fmt.Errorf("authorization resource is required")
	}
	rows := make([]appAdminMemberRow, 0)
	pageToken := ""
	for {
		resp, err := s.authorization.ListRelationships(ctx, &proto.ListRelationshipsRequest{
			Filter: &proto.RelationshipFilter{
				Resource: resource,
			},
			PageSize:  500,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, relationship := range resp.GetRelationships() {
			if row, ok := s.appAdminMemberRowFromRelationship(ctx, relationship); ok {
				rows = append(rows, row)
			}
		}
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			return projectAppAdminMemberRoster(rows), nil
		}
	}
}

func decodeAppAdminMemberSetRequest(w http.ResponseWriter, r *http.Request) (appAdminMemberSetRequest, bool) {
	var request appAdminMemberSetRequest
	if !decodeStrictJSONBody(w, r, &request) {
		return appAdminMemberSetRequest{}, false
	}
	return request, true
}

func decodeAppAdminMemberRemoveRequest(w http.ResponseWriter, r *http.Request) (appAdminMemberRemoveRequest, bool) {
	var request appAdminMemberRemoveRequest
	if !decodeStrictJSONBody(w, r, &request) {
		return appAdminMemberRemoveRequest{}, false
	}
	return request, true
}

func decodeStrictJSONBody(w http.ResponseWriter, r *http.Request, out any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func (s *Server) mutableAppAdminMemberRoles(ctx context.Context, appName, subjectID string) ([]string, error) {
	rows, err := s.listAppAuthorizationMemberRows(ctx, appName)
	if err != nil {
		return nil, err
	}
	roles := make([]string, 0)
	for _, row := range rows {
		if !row.Mutable || !appAdminMemberRowMatchesSubject(row, subjectID) {
			continue
		}
		roles = append(roles, row.Role)
	}
	return roles, nil
}

func appAdminMemberRowMatchesSubject(row appAdminMemberRow, subjectID string) bool {
	subjectID = strings.TrimSpace(subjectID)
	return subjectID != "" && row.SelectorKind == "subject_id" && strings.TrimSpace(row.SubjectID) == subjectID
}

func (s *Server) addAppAdminMemberRole(ctx context.Context, appName, role, subjectID string) error {
	if s == nil || s.authorization == nil {
		return errors.New("authorization is unavailable")
	}
	_, err := s.authorization.AddRelationship(ctx, &proto.AddRelationshipRequest{
		Relationship: &proto.Relationship{
			Tuple:       appAdminMemberRelationshipTuple(appName, role, subjectID),
			SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
		},
	})
	return err
}

func (s *Server) deleteAppAdminMemberRole(ctx context.Context, appName, role, subjectID string) error {
	if s == nil || s.authorization == nil {
		return errors.New("authorization is unavailable")
	}
	_, err := s.authorization.DeleteRelationship(ctx, &proto.DeleteRelationshipRequest{
		RelationshipTuple: appAdminMemberRelationshipTuple(appName, role, subjectID),
	})
	return err
}

func appAdminMemberRelationshipTuple(appName, role, subjectID string) *proto.RelationshipTuple {
	return &proto.RelationshipTuple{
		Resource: &proto.Resource{Type: "app", Id: strings.TrimSpace(appName)},
		Relation: strings.TrimSpace(role),
		Target: &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
				Type: "subject",
				Id:   strings.TrimSpace(subjectID),
			}},
		},
	}
}

func validateAppAdminMemberSubjectID(subjectID string) error {
	if strings.Contains(subjectID, "#") {
		return errors.New("subjectId must be a direct subject, not a subject-set selector")
	}
	if _, _, ok := core.ParseSubjectID(subjectID); !ok {
		return errors.New("subjectId must include a subject kind prefix")
	}
	return nil
}

func validateAppAdminMemberRole(role string) error {
	switch strings.TrimSpace(role) {
	case "admin", "viewer", "editor":
		return nil
	default:
		return errors.New("role must be admin, viewer, or editor")
	}
}

// memberGrantKey identifies one logical grant for static-over-runtime shadowing.
func memberGrantKey(row appAdminMemberRow) string {
	return row.SelectorKind + "\x00" + row.SelectorValue + "\x00" + row.Role
}

// projectAppAdminMemberRoster owns roster layering: when the same selector+role
// exists as both static and runtime, the runtime row is marked shadowed.
func projectAppAdminMemberRoster(rows []appAdminMemberRow) []appAdminMemberRow {
	staticKeys := make(map[string]struct{})
	for _, row := range rows {
		if row.Source == "static" {
			staticKeys[memberGrantKey(row)] = struct{}{}
		}
	}
	out := make([]appAdminMemberRow, 0, len(rows))
	for _, row := range rows {
		if row.Source == "dynamic" {
			if _, ok := staticKeys[memberGrantKey(row)]; ok {
				row.Effective = false
				row.ShadowedBy = "static " + row.Role + " grant"
			}
		}
		out = append(out, row)
	}
	return out
}

func (s *Server) appAdminMemberRowFromRelationship(_ context.Context, relationship *proto.Relationship) (appAdminMemberRow, bool) {
	if relationship == nil || relationship.GetTuple() == nil {
		return appAdminMemberRow{}, false
	}
	tuple := relationship.GetTuple()
	role := strings.TrimSpace(tuple.GetRelation())
	if role == "" {
		return appAdminMemberRow{}, false
	}

	source, mutable := appAdminMemberSource(relationship.GetSourceLayer())
	row := appAdminMemberRow{
		Role:      role,
		Source:    source,
		Mutable:   mutable,
		Effective: true,
	}

	switch target := tuple.GetTarget().GetKind().(type) {
	case *proto.RelationshipTarget_Subject:
		subjectID := strings.TrimSpace(target.Subject.GetId())
		if subjectID == "" {
			return appAdminMemberRow{}, false
		}
		row.SubjectID = subjectID
		row.SelectorKind = "subject_id"
		row.SelectorValue = subjectID
		return row, true
	case *proto.RelationshipTarget_SubjectSet:
		resourceType := strings.TrimSpace(target.SubjectSet.GetResource().GetType())
		resourceID := strings.TrimSpace(target.SubjectSet.GetResource().GetId())
		relation := strings.TrimSpace(target.SubjectSet.GetRelation())
		if resourceType == "" || resourceID == "" {
			return appAdminMemberRow{}, false
		}
		selector := resourceType + ":" + resourceID
		if relation != "" {
			selector += "#" + relation
		}
		row.SelectorKind = "subject_set"
		row.SelectorValue = selector
		return row, true
	default:
		return appAdminMemberRow{}, false
	}
}

func appAdminMemberSource(layer proto.SourceLayer) (source string, mutable bool) {
	switch layer {
	case proto.SourceLayer_SOURCE_LAYER_RUNTIME:
		return "dynamic", true
	default:
		// Static config and unspecified (legacy) grants are treated as locked policy.
		return "static", false
	}
}

func (s *Server) resolveAppAdminMemberEmail(ctx context.Context, subjectID string) string {
	kind, id, ok := core.ParseSubjectID(subjectID)
	if !ok || kind != string(principal.KindUser) || s.users == nil {
		return ""
	}
	if strings.Contains(id, "@") {
		return id
	}
	user, err := s.users.GetUser(ctx, id)
	if err != nil || user == nil {
		return ""
	}
	return strings.TrimSpace(user.Email)
}
