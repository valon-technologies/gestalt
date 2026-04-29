package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerdev"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var errProviderDevAttachAccess = errors.New("provider dev attach access denied")

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

func (s *Server) createProviderDevAttachAuthorization(w http.ResponseWriter, r *http.Request) {
	if !s.providerDevAvailable(w) {
		return
	}
	var req providerdev.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid provider dev attach authorization request")
		return
	}
	info, clientSecret, verificationCode, err := s.providerDevSessions.CreateAttachAuthorization(req, s.now())
	if err != nil {
		writeProviderDevError(w, err)
		return
	}
	approvalPath := providerdev.PathAttachAuthorizations + "/" + url.PathEscape(info.AuthorizationID)
	approvalURL, err := s.resolvePublicURLPreserveBasePath(r, approvalPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve provider dev attach approval URL")
		return
	}
	writeJSON(w, http.StatusCreated, providerdev.CreateAttachAuthorizationResponse{
		AuthorizationID:    info.AuthorizationID,
		ClientSecret:       clientSecret,
		VerificationCode:   verificationCode,
		ApprovalURL:        approvalURL,
		ExpiresAt:          info.ExpiresAt,
		PollIntervalMillis: 1000,
	})
}

func (s *Server) showProviderDevAttachAuthorization(w http.ResponseWriter, r *http.Request) {
	if !s.providerDevAvailable(w) {
		return
	}
	p, ok := s.providerDevBrowserPrincipal(w, r)
	if !ok {
		return
	}
	info, err := s.providerDevSessions.GetAttachAuthorization(providerDevAuthorizationID(r))
	if err != nil {
		writeProviderDevError(w, err)
		return
	}
	for _, name := range info.Providers {
		if !s.allowProviderActionContext(r.Context(), p, name, core.ProviderActionDevAttach) {
			writeError(w, http.StatusForbidden, errProviderDevAttachAccess.Error())
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	approvalAction := "./" + url.PathEscape(info.AuthorizationID) + "/approve"
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html>
<head><meta charset="utf-8"><title>Approve provider dev attach</title></head>
<body>
<main style="font-family: sans-serif; max-width: 42rem; margin: 3rem auto; line-height: 1.5">
<h1>Approve local provider development?</h1>
<p>This allows the requesting local CLI process to handle provider calls for your browser/session only.</p>
<p>Only approve if you started this request and the provider list matches what you expected.</p>
<p><strong>Providers:</strong> %s</p>
<form method="post" action="%s">
<label for="verificationCode">Enter the verification code shown in your terminal:</label>
<input id="verificationCode" name="verificationCode" autocomplete="off" inputmode="numeric" pattern="[0-9 -]*" required>
<button type="submit">Approve attach</button>
</form>
</main>
</body>
</html>`, html.EscapeString(strings.Join(info.Providers, ", ")), html.EscapeString(approvalAction))
}

func (s *Server) approveProviderDevAttachAuthorization(w http.ResponseWriter, r *http.Request) {
	if !s.providerDevAvailable(w) {
		return
	}
	p, ok := s.providerDevBrowserPrincipal(w, r)
	if !ok {
		return
	}
	info, err := s.providerDevSessions.GetAttachAuthorization(providerDevAuthorizationID(r))
	if err != nil {
		writeProviderDevError(w, err)
		return
	}
	for _, name := range info.Providers {
		if !s.allowProviderActionContext(r.Context(), p, name, core.ProviderActionDevAttach) {
			writeError(w, http.StatusForbidden, errProviderDevAttachAccess.Error())
			return
		}
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid provider dev attach approval request")
		return
	}
	if err := s.providerDevSessions.ApproveAttachAuthorization(info.AuthorizationID, p, r.Form.Get("verificationCode")); err != nil {
		writeProviderDevError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, `<!doctype html>
<html>
<head><meta charset="utf-8"><title>Provider dev attach approved</title></head>
<body>
<main style="font-family: sans-serif; max-width: 42rem; margin: 3rem auto; line-height: 1.5">
<h1>Provider dev attach approved</h1>
<p>You can return to your terminal. Keep the local process running while you develop.</p>
</main>
</body>
</html>`)
}

func (s *Server) pollProviderDevAttachAuthorization(w http.ResponseWriter, r *http.Request) {
	if !s.providerDevAvailable(w) {
		return
	}
	resp, err := s.providerDevSessions.PollAttachAuthorization(providerDevAuthorizationID(r), providerDevAuthorizationSecret(r))
	if err != nil {
		writeProviderDevError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) createAuthorizedProviderDevSession(w http.ResponseWriter, r *http.Request) {
	if !s.providerDevAvailable(w) {
		return
	}
	var req providerdev.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid provider dev session request")
		return
	}
	p, err := s.providerDevSessions.ConsumeAttachAuthorization(providerDevAuthorizationID(r), providerDevAuthorizationSecret(r), req)
	if err != nil {
		writeProviderDevError(w, err)
		return
	}
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
		if !principal.AllowsActionPermission(p, name, core.ProviderActionDevAttach) || !s.allowProviderActionContext(r.Context(), p, name, core.ProviderActionDevAttach) {
			writeError(w, http.StatusForbidden, errProviderDevAttachAccess.Error())
			return errProviderDevAttachAccess
		}
	}
	return nil
}

func (s *Server) providerDevBrowserPrincipal(w http.ResponseWriter, r *http.Request) (*principal.Principal, bool) {
	browserRequest := r.Clone(r.Context())
	browserRequest.Header = r.Header.Clone()
	browserRequest.Header.Del("Authorization")
	p, err := s.resolveRequestPrincipalWithUserID(browserRequest)
	if err == nil && p != nil && p.Source == principal.SourceSession && !principal.IsNonUserPrincipal(p) {
		return p, true
	}
	if r.Method == http.MethodGet {
		http.Redirect(w, r, browserLoginPath+"?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return nil, false
	}
	writeError(w, http.StatusUnauthorized, "provider dev attach approval requires browser authentication")
	return nil, false
}

func (s *Server) resolvePublicURLPreserveBasePath(r *http.Request, raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.IsAbs() {
		return parsed.String(), nil
	}
	base := s.publicBaseURL
	if base == "" {
		return s.resolvePublicURL(r, raw)
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	basePath := strings.TrimRight(baseURL.Path, "/")
	refPath := parsed.EscapedPath()
	if refPath == "" {
		refPath = parsed.Path
	}
	refPath = "/" + strings.TrimLeft(refPath, "/")
	baseURL.Path = basePath + refPath
	baseURL.RawPath = ""
	baseURL.RawQuery = parsed.RawQuery
	baseURL.Fragment = parsed.Fragment
	return baseURL.String(), nil
}

func (s *Server) pollProviderDevSessionByDispatcher(w http.ResponseWriter, r *http.Request) {
	if !s.providerDevAvailable(w) {
		return
	}
	sessionID := providerDevAttachmentID(r)
	ctx, cancel := context.WithTimeout(r.Context(), providerdev.DefaultPollTimeout)
	defer cancel()
	resp, ok, err := s.providerDevSessions.PollSessionWithDispatcherSecretOnly(ctx, sessionID, providerDevDispatcherSecret(r))
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

func (s *Server) completeProviderDevCallByDispatcher(w http.ResponseWriter, r *http.Request) {
	if !s.providerDevAvailable(w) {
		return
	}
	if err := s.providerDevSessions.VerifyDispatcherSecretOnly(providerDevAttachmentID(r), providerDevDispatcherSecret(r)); err != nil {
		writeProviderDevError(w, err)
		return
	}
	var req providerdev.CompleteCallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid provider dev call response")
		return
	}
	err := s.providerDevSessions.CompleteCallWithDispatcherSecretOnly(
		providerDevAttachmentID(r),
		providerDevCallID(r),
		providerDevDispatcherSecret(r),
		req,
	)
	if err != nil {
		writeProviderDevError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) closeProviderDevSessionByDispatcherOrOwner(w http.ResponseWriter, r *http.Request) {
	if !s.providerDevAvailable(w) {
		return
	}
	if strings.TrimSpace(providerDevDispatcherSecret(r)) != "" {
		err := s.providerDevSessions.CloseSessionWithDispatcherSecret(providerDevAttachmentID(r), providerDevDispatcherSecret(r))
		if err != nil {
			writeProviderDevError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
		return
	}
	p, err := s.resolveRequestPrincipalWithUserID(r)
	if err != nil || p == nil {
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return
	}
	err = s.providerDevSessions.CloseSession(p, providerDevAttachmentID(r))
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

func providerDevAuthorizationID(r *http.Request) string {
	return chi.URLParam(r, "authorizationID")
}

func providerDevAuthorizationSecret(r *http.Request) string {
	return r.Header.Get(providerdev.HeaderAuthorizationSecret)
}

func providerDevAttachmentID(r *http.Request) string {
	return chi.URLParam(r, "attachmentID")
}

func providerDevCallID(r *http.Request) string {
	return chi.URLParam(r, "callID")
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
