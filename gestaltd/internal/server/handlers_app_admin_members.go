package server

import (
	"context"
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
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Get("/apps/{app}/admin/members", s.listAppAdminMembers)
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
	appName = strings.TrimSpace(appName)
	rows := make([]appAdminMemberRow, 0)
	pageToken := ""
	for {
		resp, err := s.authorization.ListRelationships(ctx, &proto.ListRelationshipsRequest{
			Filter: &proto.RelationshipFilter{
				Resource: s.authorizationResource(appName),
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
