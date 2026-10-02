package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

// appAdminAccessResponse is the read model for GET /apps/{app}/admin/access.
// An app access profile is per-user state that only its owner can read or
// write, which leaves an admin debugging a denial with no way to see the
// record causing it. Legacy is true when the profile is still enforced as the
// old allow-list because it could not be converted exactly. ProfileExists separates "allowed everything because no
// record exists" from "allowed exactly this list".
type appAdminAccessResponse struct {
	App                 string   `json:"app"`
	SubjectID           string   `json:"subjectId"`
	Email               string   `json:"email,omitempty"`
	ProfileExists       bool     `json:"profileExists"`
	Legacy              bool     `json:"legacy"`
	DefaultsInitialized bool     `json:"defaultsInitialized"`
	EnabledOperations   []string `json:"enabledOperations"`
	DeniedOperations    []string `json:"deniedOperations"`
	UpdatedAt           string   `json:"updatedAt,omitempty"`
}

func (s *Server) mountAppAdminAccessRoutes(r chi.Router) {
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Get("/apps/{app}/admin/access", s.getAppAdminAccess)
}

func (s *Server) getAppAdminAccess(w http.ResponseWriter, r *http.Request) {
	appName := strings.TrimSpace(chi.URLParam(r, "app"))
	if appName == "" {
		writeError(w, http.StatusBadRequest, "app is required")
		return
	}
	if s.appAccessProfiles == nil {
		writeError(w, http.StatusServiceUnavailable, "app access settings are unavailable")
		return
	}
	prov, ok := s.getProvider(r.Context(), w, appName)
	if !ok {
		return
	}
	subjectID, email, ok := s.resolveAppAdminAccessSubject(w, r)
	if !ok {
		return
	}

	response := &appAdminAccessResponse{
		App:               appName,
		SubjectID:         subjectID,
		Email:             email,
		EnabledOperations: make([]string, 0),
		DeniedOperations:  make([]string, 0),
	}
	profile, err := s.appAccessProfiles.GetAppAccessProfile(r.Context(), subjectID, appName)
	if err != nil {
		if !errors.Is(err, core.ErrNotFound) {
			s.writeAppAccessError(w, err)
			return
		}
		// No record: nothing is gated for this user.
		writeJSON(w, http.StatusOK, response)
		return
	}
	profile = s.appAccess.Resolve(r.Context(), prov, profile)
	response.ProfileExists = true
	response.Legacy = profile.Legacy
	response.DefaultsInitialized = profile.DefaultsInitialized
	cat := prov.Catalog()
	defaults := core.AppAccessDefaultsFor(prov)
	response.EnabledOperations = append(response.EnabledOperations, profile.EnabledOperations(cat, defaults)...)
	if !profile.UpdatedAt.IsZero() {
		response.UpdatedAt = profile.UpdatedAt.UTC().Format(time.RFC3339)
	}
	// Every operation the record withholds, including the private ones the
	// owner's own page never offers them.
	response.DeniedOperations = append(response.DeniedOperations, profile.DeniedOperations(cat, defaults)...)
	writeJSON(w, http.StatusOK, response)
}

// resolveAppAdminAccessSubject accepts the subject id a profile is keyed on, or
// an email, which is what an admin reading a denial in an audit log has.
func (s *Server) resolveAppAdminAccessSubject(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	query := r.URL.Query()
	subjectID := strings.TrimSpace(query.Get("subject_id"))
	email := strings.TrimSpace(query.Get("email"))
	if subjectID == "" && email == "" {
		writeError(w, http.StatusBadRequest, "subject_id or email is required")
		return "", "", false
	}
	if subjectID != "" {
		return subjectID, email, true
	}
	if s.users == nil {
		writeError(w, http.StatusServiceUnavailable, "user lookup is unavailable")
		return "", "", false
	}
	user, err := s.users.FindUserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no user with that email")
			return "", "", false
		}
		writeError(w, http.StatusServiceUnavailable, "user lookup is unavailable")
		return "", "", false
	}
	return principal.UserSubjectID(user.ID), user.Email, true
}
