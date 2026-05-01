package server

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/services/egress"
)

const (
	proxyAuthorizationHeader = "Proxy-Authorization"
	egressProxyDialTimeout   = 10 * time.Second
)

type egressProxyRequestPolicy struct {
	destination egress.DestinationPolicy
}

func (s *Server) egressProxyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.egressProxyToken(r)
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		if s.routeProfile == RouteProfileManagement || s.egressProxyTokens == nil || !isEgressProxyRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		target, err := s.egressProxyTokens.ResolveToken(token)
		if err != nil {
			http.Error(w, "invalid egress proxy token", http.StatusProxyAuthRequired)
			return
		}
		endpoint, err := proxyTargetEndpoint(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if endpoint.host == "" {
			http.Error(w, "proxy target host is required", http.StatusBadRequest)
			return
		}
		if err := egress.CheckEndpoint(target.AllowedHosts, endpoint.hostport(), target.DefaultAction, endpoint.defaultPort); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		requestPolicy := egressProxyRequestPolicy{
			destination: egress.DestinationPolicy{
				AllowLoopback: proxyTargetAllowsExplicitLoopback(target.AllowedHosts, endpoint.host),
			},
		}
		if err := egress.RejectUnsafeHostLiteral(endpoint.host, requestPolicy.destination); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		newEgressProxyHandler(requestPolicy).ServeHTTP(w, r)
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

type proxyEndpoint struct {
	host        string
	port        string
	defaultPort string
}

func (e proxyEndpoint) hostport() string {
	if e.port == "" {
		return e.host
	}
	return net.JoinHostPort(e.host, e.port)
}

func proxyTargetEndpoint(r *http.Request) (proxyEndpoint, error) {
	if r == nil {
		return proxyEndpoint{}, nil
	}
	var host, port, defaultPort string
	switch {
	case r.Method == http.MethodConnect:
		host, port = splitProxyHostPort(strings.TrimSpace(r.Host))
		defaultPort = "443"
	case r.URL != nil && r.URL.Host != "":
		host = strings.TrimSpace(r.URL.Hostname())
		port = strings.TrimSpace(r.URL.Port())
		defaultPort = defaultPortForScheme(r.URL.Scheme)
	default:
		host, port = splitProxyHostPort(strings.TrimSpace(r.Host))
		defaultPort = "80"
	}
	if host == "" {
		return proxyEndpoint{}, nil
	}
	if port == "" {
		resolvedHost, resolvedPort, err := egress.SplitHostPortDefault(host, defaultPort)
		if err != nil {
			return proxyEndpoint{}, err
		}
		host, port = resolvedHost, resolvedPort
	}
	if _, _, err := egress.SplitHostPortDefault(net.JoinHostPort(host, port), defaultPort); err != nil {
		return proxyEndpoint{}, err
	}
	return proxyEndpoint{
		host:        strings.TrimSpace(host),
		port:        port,
		defaultPort: defaultPort,
	}, nil
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

func splitProxyHostPort(hostport string) (string, string) {
	if h, p, err := net.SplitHostPort(hostport); err == nil {
		return h, p
	}
	return hostport, ""
}

func defaultPortForScheme(scheme string) string {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "https":
		return "443"
	default:
		return "80"
	}
}

func proxyTargetAllowsExplicitLoopback(allowedHosts []string, host string) bool {
	if !egress.IsLocalhostName(host) {
		return false
	}
	for _, allowed := range allowedHosts {
		allowedHost, _, _, err := splitAllowedProxyHost(allowed)
		if err == nil && egress.IsLocalhostName(allowedHost) {
			return true
		}
	}
	return false
}

func splitAllowedProxyHost(pattern string) (string, string, bool, error) {
	u := &url.URL{Host: strings.TrimSpace(pattern)}
	host := u.Hostname()
	port := u.Port()
	if host == "" {
		host, port = splitProxyHostPort(pattern)
	}
	if port == "" {
		return host, "", false, nil
	}
	if _, _, err := egress.SplitHostPortDefault(net.JoinHostPort(host, port), ""); err != nil {
		return "", "", false, err
	}
	return host, port, true, nil
}

func newEgressProxyHandler(policy egressProxyRequestPolicy) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			handleEgressProxyConnect(w, r, policy)
			return
		}
		handleEgressProxyHTTP(w, r, policy)
	})
}

func handleEgressProxyHTTP(w http.ResponseWriter, r *http.Request, policy egressProxyRequestPolicy) {
	transport := egress.CloneDefaultTransport()
	transport.Proxy = nil
	transport.DialContext = egress.SafeDialContext(policy.destination)
	defer transport.CloseIdleConnections()

	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Header = out.Header.Clone()
	out.Header.Del(proxyAuthorizationHeader)
	if out.URL == nil || !out.URL.IsAbs() {
		http.Error(w, "proxy target URL is required", http.StatusBadRequest)
		return
	}
	out.Host = out.URL.Host

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

func handleEgressProxyConnect(w http.ResponseWriter, r *http.Request, policy egressProxyRequestPolicy) {
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

	ctx, cancel := context.WithTimeout(r.Context(), egressProxyDialTimeout)
	defer cancel()
	safeTargetAddr, err := egress.ResolveSafeTCPAddr(ctx, "tcp", targetAddr, policy.destination)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	var dialer net.Dialer
	targetConn, err := dialer.DialContext(ctx, "tcp", safeTargetAddr)
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
