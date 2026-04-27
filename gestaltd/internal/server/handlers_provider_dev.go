package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerdev"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) createProviderDevSession(w http.ResponseWriter, r *http.Request) {
	if s.providerDevSessions == nil {
		writeError(w, http.StatusNotFound, "provider dev is not configured")
		return
	}
	var req providerdev.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid provider dev session request")
		return
	}
	p := PrincipalFromContext(r.Context())
	if err := s.authorizeProviderDevSessionRequest(w, r, p, req); err != nil {
		return
	}
	resp, err := s.providerDevSessions.CreateSession(r.Context(), p, req)
	if err != nil {
		writeProviderDevError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) resolveProviderDevSession(w http.ResponseWriter, r *http.Request) {
	if s.providerDevSessions == nil {
		writeError(w, http.StatusNotFound, "provider dev is not configured")
		return
	}
	var req providerdev.ResolveAttachRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid provider dev resolve request")
		return
	}
	p := PrincipalFromContext(r.Context())
	if err := s.authorizeProviderDevSessionRequest(w, r, p, req); err != nil {
		return
	}
	resp, err := s.providerDevSessions.ResolveAttachProviders(req)
	if err != nil {
		writeProviderDevError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) authorizeProviderDevSessionRequest(w http.ResponseWriter, r *http.Request, p *principal.Principal, req providerdev.CreateSessionRequest) error {
	if err := requireUserCaller(w, p); err != nil {
		return err
	}
	names, err := s.providerDevSessions.ResolveAttachProviderNames(req)
	if err != nil {
		writeProviderDevError(w, err)
		return err
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !principal.AllowsProviderPermission(p, name) || !s.allowProviderContext(r.Context(), p, name) {
			writeError(w, http.StatusForbidden, errOperationAccess.Error())
			return errOperationAccess
		}
	}
	return nil
}

func (s *Server) pollProviderDevSession(w http.ResponseWriter, r *http.Request) {
	if s.providerDevSessions == nil {
		writeError(w, http.StatusNotFound, "provider dev is not configured")
		return
	}
	sessionID := chi.URLParam(r, "sessionID")
	ctx, cancel := context.WithTimeout(r.Context(), providerdev.DefaultPollTimeout)
	defer cancel()
	resp, ok, err := s.providerDevSessions.PollSession(ctx, PrincipalFromContext(r.Context()), sessionID)
	if err != nil {
		writeProviderDevError(w, err)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) completeProviderDevCall(w http.ResponseWriter, r *http.Request) {
	if s.providerDevSessions == nil {
		writeError(w, http.StatusNotFound, "provider dev is not configured")
		return
	}
	var req providerdev.CompleteCallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid provider dev call response")
		return
	}
	err := s.providerDevSessions.CompleteCall(
		PrincipalFromContext(r.Context()),
		chi.URLParam(r, "sessionID"),
		chi.URLParam(r, "callID"),
		req,
	)
	if err != nil {
		writeProviderDevError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) closeProviderDevSession(w http.ResponseWriter, r *http.Request) {
	if s.providerDevSessions == nil {
		writeError(w, http.StatusNotFound, "provider dev is not configured")
		return
	}
	err := s.providerDevSessions.CloseSession(PrincipalFromContext(r.Context()), chi.URLParam(r, "sessionID"))
	if err != nil {
		writeProviderDevError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

func writeProviderDevError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, err.Error())
		return
	}
	st, ok := status.FromError(err)
	if !ok {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch st.Code() {
	case codes.InvalidArgument:
		writeError(w, http.StatusBadRequest, st.Message())
	case codes.Unauthenticated:
		writeError(w, http.StatusUnauthorized, st.Message())
	case codes.PermissionDenied:
		writeError(w, http.StatusForbidden, st.Message())
	case codes.NotFound:
		writeError(w, http.StatusNotFound, st.Message())
	case codes.FailedPrecondition:
		writeError(w, http.StatusPreconditionFailed, st.Message())
	case codes.DeadlineExceeded:
		writeError(w, http.StatusGatewayTimeout, st.Message())
	case codes.Unavailable:
		writeError(w, http.StatusServiceUnavailable, st.Message())
	default:
		writeError(w, http.StatusInternalServerError, st.Message())
	}
}
