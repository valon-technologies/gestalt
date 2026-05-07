package server

import (
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

const (
	proxyAuthorizationHeader = "Proxy-Authorization"
	onBehalfOfHeader         = "X-On-Behalf-Of"
	egressProxyDialTimeout   = 10 * time.Second
)

func (s *Server) egressProxyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.egressProxyToken(r)
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		if s.egressProxyTokens == nil || !isEgressProxyRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		target, err := s.egressProxyTokens.ResolveToken(token)
		if err != nil {
			http.Error(w, "invalid egress proxy token", http.StatusProxyAuthRequired)
			return
		}
		host := proxyTargetHost(r)
		if host == "" {
			http.Error(w, "proxy target host is required", http.StatusBadRequest)
			return
		}
		if err := egress.CheckHost(target.AllowedHosts, host, target.DefaultAction); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		// G1: if the token authorizes impersonation, the X-On-Behalf-Of header
		// names the user the request is being made for. We populate a RunAs
		// audit context so downstream credential resolution + audit logging
		// pick up the correct subject.
		if target.MayImpersonate {
			onBehalf := strings.TrimSpace(r.Header.Get(onBehalfOfHeader))
			if onBehalf == "" {
				http.Error(w, "X-On-Behalf-Of header required for impersonating tokens", http.StatusBadRequest)
				return
			}
			runAs := core.NormalizeRunAsSubject(&core.RunAsSubject{
				SubjectID:           "user:" + onBehalf,
				SubjectKind:         "user",
				CredentialSubjectID: "user:" + onBehalf,
				DisplayName:         onBehalf,
			})
			caller := core.NormalizeRunAsSubject(&core.RunAsSubject{
				SubjectID:   target.CallerSubjectID,
				SubjectKind: subjectKindFromID(target.CallerSubjectID, "service_account"),
			})
			r = r.WithContext(invocation.WithRunAsAudit(r.Context(), caller, runAs))
		}

		// Always strip the on-behalf-of header before forwarding to the
		// upstream — it is for gestaltd's consumption only.
		r.Header.Del(onBehalfOfHeader)

		s.newEgressProxyHandler().ServeHTTP(w, r)
	})
}

func (s *Server) egressProxyToken(r *http.Request) string {
	if s == nil || r == nil {
		return ""
	}
	return extractProxyAuthorizationToken(r.Header.Get(proxyAuthorizationHeader))
}

func isEgressProxyRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.Method == http.MethodConnect {
		return true
	}
	return r.URL != nil && r.URL.IsAbs()
}

func proxyTargetHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	var host string
	switch {
	case r.Method == http.MethodConnect:
		host = strings.TrimSpace(r.Host)
	case r.URL != nil && r.URL.Host != "":
		host = strings.TrimSpace(r.URL.Hostname())
	default:
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func extractProxyAuthorizationToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	if token, ok := strings.CutPrefix(header, "Bearer "); ok {
		return strings.TrimSpace(token)
	}
	if token, ok := strings.CutPrefix(header, "Basic "); ok {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(token))
		if err != nil {
			return ""
		}
		user, pass, found := strings.Cut(string(decoded), ":")
		if found && strings.TrimSpace(pass) != "" {
			return strings.TrimSpace(pass)
		}
		return strings.TrimSpace(user)
	}
	return ""
}

func subjectKindFromID(subjectID, fallback string) string {
	if kind, _, ok := core.ParseSubjectID(subjectID); ok {
		return kind
	}
	return fallback
}

func (s *Server) newEgressProxyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			s.handleEgressProxyConnect(w, r)
			return
		}
		s.handleEgressProxyHTTP(w, r)
	})
}

func (s *Server) handleEgressProxyHTTP(w http.ResponseWriter, r *http.Request) {
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Header = out.Header.Clone()
	out.Header.Del(proxyAuthorizationHeader)
	out.Header.Del(onBehalfOfHeader)
	if out.URL == nil || !out.URL.IsAbs() {
		http.Error(w, "proxy target URL is required", http.StatusBadRequest)
		return
	}
	out.Host = out.URL.Host

	// G3: if the request is on behalf of a user and the upstream host maps to
	// a known provider, swap any inbound Authorization for the per-user token
	// gestaltd has stored for that provider.
	if injection, err := s.injectImpersonationCredential(out); err != nil {
		s.writeReconnectRequiredOrError(w, err, injection.providerName)
		return
	}

	resp, err := transport.RoundTrip(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

type impersonationInjection struct {
	providerName string
	injected     bool
}

// injectImpersonationCredential resolves the upstream host to a configured
// provider, looks up the impersonated user's credential for that provider, and
// rewrites the outbound Authorization header. Returns the (possibly empty)
// provider name even on error so the caller can surface a reconnect URL.
func (s *Server) injectImpersonationCredential(out *http.Request) (impersonationInjection, error) {
	if s == nil || out == nil {
		return impersonationInjection{}, nil
	}
	audit := invocation.RunAsAuditFromContext(out.Context())
	runAs := audit.RunAsSubject
	if runAs == nil || strings.TrimSpace(runAs.CredentialSubjectID) == "" {
		return impersonationInjection{}, nil
	}
	if core.ExternalCredentialProviderMissing(s.externalCredentials) {
		return impersonationInjection{}, nil
	}
	host := strings.TrimSpace(out.URL.Hostname())
	if host == "" {
		return impersonationInjection{}, nil
	}
	providerName := s.providerForHost(host)
	if providerName == "" {
		return impersonationInjection{}, nil
	}

	connection := core.PluginConnectionName
	authConfig := core.ExternalCredentialAuthConfig{}
	var connectionParams map[string]string
	if s.connectionRuntime != nil {
		if info, ok := s.connectionRuntime(providerName, connection); ok {
			authConfig = info.AuthConfig
			connectionParams = info.Params
		}
	}

	actorID := ""
	if audit.AgentSubject != nil {
		actorID = audit.AgentSubject.SubjectID
	}

	resp, err := s.externalCredentials.ResolveCredential(out.Context(), &core.ResolveExternalCredentialRequest{
		Provider:            providerName,
		Connection:          connection,
		Mode:                core.ConnectionModeUser,
		CredentialSubjectID: runAs.CredentialSubjectID,
		ActorSubjectID:      actorID,
		Auth:                authConfig,
		ConnectionParams:    connectionParams,
	})
	if err != nil {
		return impersonationInjection{providerName: providerName}, err
	}
	if resp == nil {
		return impersonationInjection{providerName: providerName}, nil
	}
	token := strings.TrimSpace(resp.Token)
	if token == "" && resp.Credential != nil {
		token = strings.TrimSpace(resp.Credential.AccessToken)
	}
	if token == "" {
		return impersonationInjection{providerName: providerName}, nil
	}
	out.Header.Set("Authorization", "Bearer "+token)
	return impersonationInjection{providerName: providerName, injected: true}, nil
}

// providerForHost returns the provider name whose egress.allowedHosts list
// matches the upstream host, or "" if no plugin matches.
func (s *Server) providerForHost(host string) string {
	if s == nil || host == "" {
		return ""
	}
	host = strings.ToLower(strings.TrimSpace(host))
	for name, entry := range s.pluginDefs {
		if entry == nil {
			continue
		}
		hosts := entry.EffectiveAllowedHosts()
		if len(hosts) == 0 {
			continue
		}
		if egress.CheckHost(hosts, host, egress.PolicyDeny) == nil {
			return name
		}
	}
	return ""
}

func (s *Server) writeReconnectRequiredOrError(w http.ResponseWriter, err error, providerName string) {
	switch {
	case errors.Is(err, core.ErrReconnectRequired):
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error":         "reconnect_required",
			"provider":      providerName,
			"reconnect_url": s.reconnectURL(providerName),
		})
	case errors.Is(err, core.ErrNotFound):
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error":         "no_credential",
			"provider":      providerName,
			"reconnect_url": s.reconnectURL(providerName),
		})
	default:
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
}

// reconnectURL points the caller at the gestalt UI's connect flow for the
// named provider. The user still has to authenticate to gestalt to complete
// the OAuth dance — the egress proxy doesn't carry the user's session.
func (s *Server) reconnectURL(providerName string) string {
	base := strings.TrimRight(s.publicBaseURL, "/")
	return base + "/auth/connect?integration=" + providerName
}

func (s *Server) handleEgressProxyConnect(w http.ResponseWriter, r *http.Request) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	targetAddr := strings.TrimSpace(r.Host)
	if targetAddr == "" {
		http.Error(w, "proxy target address is required", http.StatusBadRequest)
		return
	}
	if _, _, err := net.SplitHostPort(targetAddr); err != nil {
		targetAddr = net.JoinHostPort(targetAddr, "443")
	}

	var dialer net.Dialer
	targetConn, err := dialer.DialContext(r.Context(), "tcp", targetAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	clientConn, clientRW, err := hijacker.Hijack()
	if err != nil {
		_ = targetConn.Close()
		return
	}
	if _, err := clientRW.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		_ = clientConn.Close()
		_ = targetConn.Close()
		return
	}
	if err := clientRW.Flush(); err != nil {
		_ = clientConn.Close()
		_ = targetConn.Close()
		return
	}

	deadline := time.Now().Add(10 * time.Minute)
	_ = clientConn.SetDeadline(deadline)
	_ = targetConn.SetDeadline(deadline)

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(targetConn, clientRW)
		closeWrite(targetConn)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(clientConn, targetConn)
		closeWrite(clientConn)
		done <- struct{}{}
	}()
	<-done
	<-done
	_ = clientConn.Close()
	_ = targetConn.Close()
}

func closeWrite(c net.Conn) {
	if closeWriter, ok := c.(interface{ CloseWrite() error }); ok {
		_ = closeWriter.CloseWrite()
	}
}
