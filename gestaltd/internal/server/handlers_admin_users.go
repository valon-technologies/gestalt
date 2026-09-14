package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
)

type adminUserSummary struct {
	ID    string `json:"id"`
	Email string `json:"email,omitempty"`
}

// These routes inherit the configured platform-admin policy. The workspace
// directory must not depend on membership in a particular console app.
func (s *Server) mountAdminUsersRoutes(r chi.Router) {
	r.Get("/users", s.listAdminUsers)
	r.Post("/users/lookup-emails", s.lookupAdminUserEmails)
}

func (s *Server) listAdminUsers(w http.ResponseWriter, r *http.Request) {
	if s.users == nil {
		writeError(w, http.StatusServiceUnavailable, "user directory is unavailable")
		return
	}
	users, err := s.users.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "user directory is unavailable")
		return
	}
	rows := make([]adminUserSummary, 0, len(users))
	for _, user := range users {
		rows = append(rows, adminUserSummary{ID: user.ID, Email: strings.ToLower(strings.TrimSpace(user.Email))})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Email != rows[j].Email {
			return rows[i].Email < rows[j].Email
		}
		return rows[i].ID < rows[j].ID
	})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		Users []adminUserSummary `json:"users"`
	}{Users: rows})
}

func (s *Server) lookupAdminUserEmails(w http.ResponseWriter, r *http.Request) {
	var request struct {
		UserIDs []string `json:"userIds"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil || len(request.UserIDs) > 1000 {
		writeError(w, http.StatusBadRequest, "provide up to 1000 user IDs")
		return
	}
	if s.users == nil {
		writeError(w, http.StatusServiceUnavailable, "user directory is unavailable")
		return
	}
	emails := make(map[string]string)
	seen := make(map[string]bool)
	for _, rawID := range request.UserIDs {
		id, err := uuid.Parse(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(rawID)), "user:"))
		if err != nil || seen[id.String()] {
			continue
		}
		seen[id.String()] = true
		user, err := s.users.GetUser(r.Context(), id.String())
		if errors.Is(err, core.ErrNotFound) {
			continue
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "user directory is unavailable")
			return
		}
		if email := strings.ToLower(strings.TrimSpace(user.Email)); email != "" {
			emails[id.String()] = email
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		Emails map[string]string `json:"emails"`
	}{Emails: emails})
}
