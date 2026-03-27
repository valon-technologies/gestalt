package proxy

import (
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/internal/egress"
	"github.com/valon-technologies/gestalt/plugins/bindings/internal/httpjson"
)

const (
	connectDialTimeout = 10 * time.Second
	connectIdleTimeout = 5 * time.Minute
)

func (b *Binding) handleConnect(w http.ResponseWriter, r *http.Request) {
	// For CONNECT, the canonical target is the request-URI (host:port),
	// not the Host header which a client could set independently.
	targetHost := r.RequestURI
	if targetHost == "" && r.URL != nil {
		targetHost = r.URL.Host
	}
	if targetHost == "" {
		targetHost = r.Host
	}
	if targetHost == "" {
		httpjson.WriteError(w, http.StatusBadRequest, "missing CONNECT target host")
		return
	}

	target := egress.Target{
		Provider: b.provider,
		Method:   http.MethodConnect,
		Host:     targetHost,
	}

	ctx := b.subjectContext(r.Context())
	subject, _ := egress.SubjectFromContext(ctx)

	if err := egress.EvaluatePolicy(ctx, b.resolver.Policy, egress.PolicyInput{
		Subject: subject,
		Target:  target,
	}); err != nil {
		httpjson.WriteError(w, http.StatusForbidden, err.Error())
		return
	}

	upstream, err := net.DialTimeout("tcp", targetHost, connectDialTimeout)
	if err != nil {
		httpjson.WriteError(w, http.StatusBadGateway, "failed to connect to upstream")
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		httpjson.WriteError(w, http.StatusInternalServerError, "server does not support connection hijacking")
		return
	}
	clientConn, bufrw, err := hj.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}

	_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// Drain any bytes the HTTP server buffered beyond the CONNECT headers
	// before switching to raw bidirectional copy.
	if bufrw != nil && bufrw.Reader.Buffered() > 0 {
		_, _ = io.CopyN(upstream, bufrw.Reader, int64(bufrw.Reader.Buffered()))
	}

	tunnel(clientConn, upstream)
}

func tunnel(client, upstream net.Conn) {
	defer func() { _ = client.Close() }()
	defer func() { _ = upstream.Close() }()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		copyWithIdleTimeout(upstream, client, connectIdleTimeout)
		closeWrite(upstream)
	}()

	go func() {
		defer wg.Done()
		copyWithIdleTimeout(client, upstream, connectIdleTimeout)
		closeWrite(client)
	}()

	wg.Wait()
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
}

func copyWithIdleTimeout(dst, src net.Conn, idle time.Duration) {
	buf := make([]byte, 32*1024)
	for {
		_ = src.SetReadDeadline(time.Now().Add(idle))
		n, readErr := src.Read(buf)
		if n > 0 {
			_ = dst.SetWriteDeadline(time.Now().Add(idle))
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				break
			}
		}
		if readErr != nil {
			break
		}
	}
}
