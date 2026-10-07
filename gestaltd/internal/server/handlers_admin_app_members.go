package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// mountAdminAppMembersRoutes lets a platform admin read any app's access
// roster. Reading is all it grants: changing a roster stays with that app's own
// admins on /apps/{app}/admin/members.
func (s *Server) mountAdminAppMembersRoutes(r chi.Router) {
	r.Get("/apps/{app}/members", s.listAdminAppMembers)
}

func (s *Server) listAdminAppMembers(w http.ResponseWriter, r *http.Request) {
	s.writeAppAdminMembers(w, r, strings.TrimSpace(chi.URLParam(r, "app")))
}
