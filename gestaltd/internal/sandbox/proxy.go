package sandbox

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	proxyDialTimeout     = 10 * time.Second
	proxyShutdownTimeout = 5 * time.Second
)

type ProxyServer struct {
	allowedHosts []string
	transport    *http.Transport
	server       *http.Server
	listener     net.Listener
}

func NewProxyServer(allowedHosts []string) *ProxyServer {
	normalized := make([]string, len(allowedHosts))
	for i, h := range allowedHosts {
		normalized[i] = strings.ToLower(h)
	}
	p := &ProxyServer{
		allowedHosts: normalized,
		transport:    &http.Transport{},
	}
	p.server = &http.Server{
		Handler:           p,
		ReadHeaderTimeout: proxyDialTimeout,
	}
	return p
}

func (p *ProxyServer) Start() (int, error) {
	var err error
	p.listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("proxy listen: %w", err)
	}
	go func() {
		_ = p.server.Serve(p.listener)
	}()
	return p.listener.Addr().(*net.TCPAddr).Port, nil
}

func (p *ProxyServer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), proxyShutdownTimeout)
	defer cancel()
	p.transport.CloseIdleConnections()
	return p.server.Shutdown(ctx)
}

func (p *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var host string
	if r.Method == http.MethodConnect {
		host = r.Host
	} else {
		host = r.URL.Hostname()
	}
	if host == "" {
		host = r.Host
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if !p.isHostAllowed(host) {
		http.Error(w, fmt.Sprintf("host %q is not in the allowed list", host), http.StatusForbidden)
		return
	}
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.handleHTTP(w, r)
}

func (p *ProxyServer) handleHTTP(w http.ResponseWriter, r *http.Request) {
	r.RequestURI = ""
	resp, err := p.transport.RoundTrip(r)
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

func (p *ProxyServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	targetConn, err := net.DialTimeout("tcp", r.Host, proxyDialTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.WriteHeader(http.StatusOK)

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		_ = targetConn.Close()
		return
	}

	deadline := time.Now().Add(10 * time.Minute)
	_ = clientConn.SetDeadline(deadline)
	_ = targetConn.SetDeadline(deadline)

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(targetConn, clientConn)
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

func (p *ProxyServer) isHostAllowed(host string) bool {
	if len(p.allowedHosts) == 0 {
		return true
	}
	lower := strings.ToLower(host)
	for _, pattern := range p.allowedHosts {
		if pattern == lower {
			return true
		}
		if strings.HasPrefix(pattern, "*.") && strings.HasSuffix(lower, pattern[1:]) {
			return true
		}
	}
	return false
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
}
