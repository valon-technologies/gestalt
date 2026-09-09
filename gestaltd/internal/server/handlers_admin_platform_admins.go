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

func (s *Server) configuredPlatformAdminRole() string {
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
	role := s.configuredPlatformAdminRole()
	rows, err := s.listAuthorizationMemberRows(r.Context(), resource)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}
	filtered := make([]appAdminMemberRow, 0, len(rows))
	for i := range rows {
		if strings.TrimSpace(rows[i].Role) == role {
			filtered = append(filtered, rows[i])
		}
	}
	writeJSON(w, http.StatusOK, adminPlatformAdminsResponse{
		Resource: adminPlatformAdminResource{
			Type: resource.GetType(),
			ID:   resource.GetId(),
		},
		Role:    role,
		Members: s.projectAppAdminHumanMemberRows(r.Context(), filtered),
	})
}
