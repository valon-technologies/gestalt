package server

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type putAdminAuthorizationMemberRequest struct {
	SubjectID string `json:"subjectId"`
	Email     string `json:"email"`
	Role      string `json:"role"`
}

type adminAuthorizationWriteSubject struct {
	SubjectID string
	User      *core.User
}

func (s *Server) mountAdminAuthorizationRoutes(r chi.Router) {
}

func (s *Server) adminAPIAuthMiddleware(next http.Handler) http.Handler {
	if s.adminRoute.AuthorizationPolicy == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authorizer == nil {
			writeError(w, http.StatusInternalServerError, "admin authorization is unavailable")
			return
		}

		p, authenticated, err := s.resolveMountedUIPrincipal(r, s.adminMountedUI())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve user")
			return
		}
		if !authenticated {
			writeError(w, http.StatusUnauthorized, "missing authorization")
			return
		}
		if err := requireUserCaller(w, p); err != nil {
			return
		}

		if access, allowed := s.authorizer.ResolveAdminAccess(r.Context(), p, s.adminRoute.AuthorizationPolicy); allowed && mountedUIRoleAllowed(access.Role, s.adminRoute.AllowedRoles) {
			s.serveAdminAPIWithAccess(next, w, r, p, access)
			return
		}

		writeError(w, http.StatusForbidden, "admin access denied")
	})
}

func (s *Server) serveAdminAPIWithAccess(next http.Handler, w http.ResponseWriter, r *http.Request, p *principal.Principal, access invocation.AccessContext) {
	ctx := r.Context()
	if p != nil {
		ctx = principal.WithPrincipal(ctx, p)
	}
	if access.Policy != "" || access.Role != "" {
		ctx = invocation.WithAccessContext(ctx, access)
	}
	next.ServeHTTP(w, r.WithContext(ctx))
}

func (s *Server) reloadAuthorizationState(ctx context.Context) error {
	if s.authorizer == nil {
		return nil
	}

	var lastErr error
	for i := 0; i < 3; i++ {
		if err := s.authorizer.ReloadAuthorizationState(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i == 2 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i+1) * 50 * time.Millisecond):
		}
	}
	return lastErr
}

func (s *Server) adminAuthorizationEmailForSubjectID(ctx context.Context, subjectID string) string {
	userID := strings.TrimSpace(principal.UserIDFromSubjectID(subjectID))
	if userID == "" || s.users == nil {
		return ""
	}
	user, err := s.users.GetUser(ctx, userID)
	if err != nil || user == nil {
		return ""
	}
	return strings.TrimSpace(user.Email)
}

func (s *Server) resolveAdminAuthorizationWriteSubject(ctx context.Context, req putAdminAuthorizationMemberRequest) (*adminAuthorizationWriteSubject, int, string) {
	subjectID := strings.TrimSpace(req.SubjectID)
	email := strings.TrimSpace(req.Email)
	switch {
	case subjectID != "" && email != "":
		return nil, http.StatusBadRequest, "provide either subjectId or email, not both"
	case subjectID != "":
		subjectID, err := adminAuthorizationSubjectID(subjectID)
		if err != nil {
			return nil, http.StatusBadRequest, err.Error()
		}
		userID := strings.TrimSpace(principal.UserIDFromSubjectID(subjectID))
		if userID == "" {
			return &adminAuthorizationWriteSubject{SubjectID: subjectID}, 0, ""
		}
		user, err := s.users.GetUser(ctx, userID)
		switch {
		case err == nil:
			return &adminAuthorizationWriteSubject{SubjectID: subjectID, User: user}, 0, ""
		case errors.Is(err, core.ErrNotFound):
			return nil, http.StatusNotFound, "subject not found"
		default:
			return nil, http.StatusInternalServerError, "failed to resolve user"
		}
	case email != "":
		parsed, err := mail.ParseAddress(email)
		if err != nil || strings.TrimSpace(parsed.Address) == "" {
			return nil, http.StatusBadRequest, "email must be a valid email address"
		}
		user, err := s.users.FindOrCreateUser(ctx, parsed.Address)
		if err != nil {
			return nil, http.StatusInternalServerError, "failed to resolve user"
		}
		return &adminAuthorizationWriteSubject{SubjectID: principal.UserSubjectID(user.ID), User: user}, 0, ""
	default:
		return nil, http.StatusBadRequest, "subjectId or email is required"
	}
}

func adminAuthorizationSubjectEmail(subject *adminAuthorizationWriteSubject) string {
	if subject == nil || subject.User == nil {
		return ""
	}
	return strings.TrimSpace(subject.User.Email)
}

func adminAuthorizationSubjectID(subjectID string) (string, error) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return "", errors.New("subjectID is required")
	}
	kind, id, ok := core.ParseSubjectID(subjectID)
	if !ok {
		return "", errors.New("subjectID must be a canonical subject ID")
	}
	subjectID = kind + ":" + id
	if kind == "system" {
		return "", errors.New("subjectID must not use system:<id>")
	}
	return subjectID, nil
}

func adminAuthorizationValidSubjectID(subjectID string) bool {
	_, err := adminAuthorizationSubjectID(subjectID)
	return err == nil
}

var (
	errAdminAuthorizationUnavailable = errors.New("dynamic authorization is unavailable")
)

func (s *Server) ensureAdminDynamicAuthorizationAvailable(w http.ResponseWriter) bool {
	if s.authorizer == nil || s.authorizationProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "dynamic authorization requires an authorization provider")
		return false
	}
	if s.authzFragments == nil {
		writeError(w, http.StatusServiceUnavailable, "dynamic authorization requires indexeddb source state")
		return false
	}
	return true
}
