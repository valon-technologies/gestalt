package server_test

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

// newDevProxyUpstream returns a stand-in framework dev server. Normal requests
// echo the path they received (so tests can assert the full mount prefix is
// forwarded); WebSocket upgrades complete a minimal handshake and echo a line
// (so tests can assert HMR upgrades pass through gestaltd's middleware chain).
func newDevProxyUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			conn, buf, err := http.NewResponseController(w).Hijack()
			if err != nil {
				t.Errorf("upstream hijack: %v", err)
				return
			}
			defer func() { _ = conn.Close() }()
			io.WriteString(buf, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			_ = buf.Flush()
			line, err := buf.ReadString('\n')
			if err != nil {
				return
			}
			io.WriteString(buf, "echo:"+line)
			_ = buf.Flush()
			return
		}
		w.Header().Set("X-Upstream", "dev-server")
		fmt.Fprintf(w, "served:%s", r.URL.Path)
	}))
}

func newDevProxyTestServer(t *testing.T, mountPath, upstreamURL string) *httptest.Server {
	t.Helper()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.ProviderUIs = map[string]*config.UIEntry{
			"gIssues": {
				Path:     mountPath,
				DevProxy: &config.UIDevProxy{URL: upstreamURL},
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)
	return ts
}

func TestUIDevProxyForwardsFullPath(t *testing.T) {
	t.Parallel()

	upstream := newDevProxyUpstream(t)
	defer upstream.Close()
	ts := newDevProxyTestServer(t, "/g-issues", upstream.URL)

	resp, err := http.Get(ts.URL + "/g-issues/_next/static/app.js")
	if err != nil {
		t.Fatalf("GET dev-proxy mount: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Upstream"); got != "dev-server" {
		t.Fatalf("X-Upstream = %q, want dev-server (response not proxied)", got)
	}
	// The dev server must see the full mount-prefixed path so its basePath and
	// HMR client URLs line up; gestaltd must not strip the /g-issues prefix.
	if want := "served:/g-issues/_next/static/app.js"; string(body) != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestUIDevProxyForwardsWebSocketUpgrade(t *testing.T) {
	t.Parallel()

	upstream := newDevProxyUpstream(t)
	defer upstream.Close()
	ts := newDevProxyTestServer(t, "/g-issues", upstream.URL)

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatalf("dial gestaltd: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	req := "GET /g-issues/_next/webpack-hmr HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("write upgrade request: %v", err)
	}

	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read upgrade status: %v", err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("status line = %q, want 101 Switching Protocols", status)
	}
	// Drain the rest of the response headers.
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read upgrade headers: %v", err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}

	if _, err := io.WriteString(conn, "ping\n"); err != nil {
		t.Fatalf("write ws frame: %v", err)
	}
	echo, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read ws echo: %v", err)
	}
	if strings.TrimSpace(echo) != "echo:ping" {
		t.Fatalf("ws echo = %q, want echo:ping (bidirectional stream broken)", strings.TrimSpace(echo))
	}
}
