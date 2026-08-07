package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/core"
)

// appAdminIdentityRow is the read model for GET /apps/{app}/admin/identities.
// It is the agent-identity projection of app authorization relationships:
// service_account subjects with a grant on this app.
type appAdminIdentityRow struct {
	SubjectID   string `json:"subjectId"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	Source      string `json:"source"`
	Mutable     bool   `json:"mutable"`
	Effective   bool   `json:"effective"`
	ShadowedBy  string `json:"shadowedBy,omitempty"`
}

func (s *Server) mountAppAdminIdentitiesRoutes(r chi.Router) {
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Get("/apps/{app}/admin/identities", s.listAppAdminIdentities)
}

func (s *Server) listAppAdminIdentities(w http.ResponseWriter, r *http.Request) {
	appName := strings.TrimSpace(chi.URLParam(r, "app"))
	if appName == "" {
		writeError(w, http.StatusBadRequest, "app is required")
		return
	}
	if s.authorization == nil {
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}

	memberRows, err := s.listAppAuthorizationMemberRows(r.Context(), appName)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, projectAppAdminIdentityRows(memberRows))
}

func projectAppAdminIdentityRows(rows []appAdminMemberRow) []appAdminIdentityRow {
	out := make([]appAdminIdentityRow, 0)
	for _, row := range rows {
		if !isAppAdminServiceAccountRow(row) {
			continue
		}
		kind, localID, ok := core.ParseSubjectID(row.SubjectID)
		displayName := row.SubjectID
		if ok && kind == "service_account" && localID != "" {
			displayName = localID
		}
		out = append(out, appAdminIdentityRow{
			SubjectID:   row.SubjectID,
			DisplayName: displayName,
			Role:        row.Role,
			Source:      row.Source,
			Mutable:     row.Mutable,
			Effective:   row.Effective,
			ShadowedBy:  row.ShadowedBy,
		})
	}
	return out
}

func isAppAdminServiceAccountRow(row appAdminMemberRow) bool {
	if row.SelectorKind != "subject_id" || row.SubjectID == "" {
		return false
	}
	kind, _, ok := core.ParseSubjectID(row.SubjectID)
	return ok && kind == "service_account"
}
