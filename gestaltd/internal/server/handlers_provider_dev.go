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
	if !s.providerDevAvailable(w) {
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

func (s *Server) listProviderDevAttachments(w http.ResponseWriter, r *http.Request) {
	if !s.providerDevAvailable(w) {
		return
	}
	resp, err := s.providerDevSessions.ListSessions(PrincipalFromContext(r.Context()))
	if err != nil {
		writeProviderDevError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attachments": resp})
}

func (s *Server) getProviderDevAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.providerDevAvailable(w) {
		return
	}
	resp, err := s.providerDevSessions.GetSession(PrincipalFromContext(r.Context()), providerDevAttachmentID(r))
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
	if !s.providerDevAvailable(w) {
		return
	}
	sessionID := providerDevAttachmentID(r)
	ctx, cancel := context.WithTimeout(r.Context(), providerdev.DefaultPollTimeout)
	defer cancel()
	resp, ok, err := s.providerDevSessions.PollSessionWithDispatcherSecret(ctx, PrincipalFromContext(r.Context()), sessionID, providerDevDispatcherSecret(r))
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
	if !s.providerDevAvailable(w) {
		return
	}
	p := PrincipalFromContext(r.Context())
	sessionID := providerDevAttachmentID(r)
	dispatcherSecret := providerDevDispatcherSecret(r)
	if err := s.providerDevSessions.VerifyDispatcherSecret(p, sessionID, dispatcherSecret); err != nil {
		writeProviderDevError(w, err)
		return
	}
	var req providerdev.CompleteCallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid provider dev call response")
		return
	}
	err := s.providerDevSessions.CompleteCall(
		p,
		sessionID,
		providerDevCallID(r),
		req,
	)
	if err != nil {
		writeProviderDevError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) closeProviderDevSession(w http.ResponseWriter, r *http.Request) {
	if !s.providerDevAvailable(w) {
		return
	}
	err := s.providerDevSessions.CloseSession(PrincipalFromContext(r.Context()), providerDevAttachmentID(r))
	if err != nil {
		writeProviderDevError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

func (s *Server) providerDevAvailable(w http.ResponseWriter) bool {
	if s.providerDevSessions == nil {
		writeError(w, http.StatusNotFound, "provider dev is not configured")
		return false
	}
	if !s.providerDevAttach {
		writeError(w, http.StatusForbidden, "provider dev remote attach is disabled")
		return false
	}
	if s.noAuth {
		writeError(w, http.StatusForbidden, "provider dev remote attach requires server authentication")
		return false
	}
	return true
}

func providerDevAttachmentID(r *http.Request) string {
	if id := chi.URLParam(r, "attachmentID"); id != "" {
		return id
	}
	return chi.URLParam(r, "sessionID")
}

func providerDevCallID(r *http.Request) string {
	if id := chi.URLParam(r, "callID"); id != "" {
		return id
	}
	return chi.URLParam(r, "call")
}

func providerDevDispatcherSecret(r *http.Request) string {
	return r.Header.Get(providerdev.HeaderDispatcherSecret)
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
