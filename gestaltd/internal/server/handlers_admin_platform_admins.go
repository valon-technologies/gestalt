package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type adminPlatformAdminsResponse struct {
	Resource adminPlatformAdminResource `json:"resource"`
	Role     string                     `json:"role"`
	Members  []appAdminMemberRow        `json:"members"`
}

type adminPlatformAdminResource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func (s *Server) mountAdminPlatformAdminsRoutes(r chi.Router) {
	r.Get("/platform-admins", s.listAdminPlatformAdmins)
}

func (s *Server) platformAdminResource() *proto.Resource {
	name := strings.TrimSpace(s.adminRoute.AuthorizationPolicy)
	if name == "" {
		name = defaultAdminAuthorizationResource
	}
	return s.authorizationResource(name)
}

func (s *Server) platformAdminRole() string {
	for _, role := range s.adminRoute.AllowedRoles {
		role = strings.TrimSpace(role)
		if role != "" {
			return role
		}
	}
	return "admin"
}

func (s *Server) listAdminPlatformAdmins(w http.ResponseWriter, r *http.Request) {
	if s.authorization == nil {
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}
	resource := s.platformAdminResource()
	rows, err := s.listAuthorizationMemberRows(r.Context(), resource)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, adminPlatformAdminsResponse{
		Resource: adminPlatformAdminResource{
			Type: resource.GetType(),
			ID:   resource.GetId(),
		},
		Role:    s.platformAdminRole(),
		Members: s.projectAppAdminHumanMemberRows(r.Context(), rows),
	})
}
