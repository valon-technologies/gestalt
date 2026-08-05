package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type userLookupResponse struct {
	SubjectID   string `json:"subjectId"`
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName,omitempty"`
}

func (s *Server) lookupUserByEmail(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireUserLookupAccess(w, r); !ok {
		return
	}
	if s.users == nil {
		writeError(w, http.StatusServiceUnavailable, "user store is unavailable")
		return
	}

	email := strings.TrimSpace(r.URL.Query().Get("email"))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email query parameter is required")
		return
	}

	user, err := s.users.FindUserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up user")
		return
	}

	writeJSON(w, http.StatusOK, userLookupResponse{
		SubjectID:   principal.UserSubjectID(user.ID),
		ID:          user.ID,
		Email:       user.Email,
		DisplayName: strings.TrimSpace(user.DisplayName),
	})
}

func (s *Server) requireGestaltAdmin(w http.ResponseWriter, r *http.Request) (*principal.Principal, bool) {
	p, ok := s.requireAuthenticatedUserCaller(w, r)
	if !ok {
		return nil, false
	}

	_, allowed, err := s.authorizeMountedAppAccess(r.Context(), p, s.adminMountedUI())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to authorize request")
		return nil, false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "gestalt admin access required")
		return nil, false
	}
	return p, true
}

func (s *Server) requireUserLookupAccess(w http.ResponseWriter, r *http.Request) (*principal.Principal, bool) {
	p, ok := s.requireAuthenticatedUserCaller(w, r)
	if !ok {
		return nil, false
	}

	_, gestaltAdmin, err := s.authorizeMountedAppAccess(r.Context(), p, s.adminMountedUI())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to authorize request")
		return nil, false
	}
	if gestaltAdmin {
		return p, true
	}

	subjectID := strings.TrimSpace(principal.Canonicalized(p).SubjectID)
	appAdmin, err := s.hasAnyExplicitAppAdmin(r.Context(), subjectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to authorize request")
		return nil, false
	}
	if !appAdmin {
		writeError(w, http.StatusForbidden, "gestalt admin or app admin access required")
		return nil, false
	}
	return p, true
}

func (s *Server) requireAuthenticatedUserCaller(w http.ResponseWriter, r *http.Request) (*principal.Principal, bool) {
	p, err := s.resolveRequestPrincipalWithUserID(r)
	switch {
	case errors.Is(err, errInvalidAuthorizationHeader):
		writeError(w, http.StatusUnauthorized, "invalid authorization header format")
		return nil, false
	case errors.Is(err, principal.ErrInvalidToken):
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return nil, false
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return nil, false
	case p == nil:
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return nil, false
	}
	if err := requireUserCaller(w, p); err != nil {
		return nil, false
	}
	return p, true
}
