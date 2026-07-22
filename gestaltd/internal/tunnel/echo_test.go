package tunnel

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// TestTunnelRawEcho verifies the frpc→frps relay path without inner TLS, to
// isolate relay issues from TLS handshake issues. It uses a plain TCP echo
// listener as the Host's backend.
func TestTunnelRawEcho(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("raw echo requires in-process frps")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	harness, err := StartTestHarness(ctx)
	if err != nil {
		t.Fatalf("StartTestHarness: %v", err)
	}
	defer harness.Close()

	// Plain TCP echo listener (no TLS).
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("backend listen: %v", err)
	}
	go func() {
		for {
			conn, err := backend.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()

	localID, err := NewIdentity()
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}

	// Start frpc directly (not via StartHost, which adds TLS).
	host, err := startRawHost(ctx, HostConfig{
		ServerURL:    harness.ServerURL(),
		Identity:     localID,
		UpstreamSPKI: "",
	}, backend)
	if err != nil {
		t.Fatalf("startRawHost: %v", err)
	}
	defer func() { _ = host.Close() }()

	waitProxyReady()

	// Dial via CONNECT (no inner TLS).
	conn, err := net.DialTimeout("tcp", harness.ConnectAddr(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial frps: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := sendConnectRequest(ctx, conn, localID.TunnelHost); err != nil {
		t.Fatalf("CONNECT: %v", err)
	}

	// Echo round-trip.
	payload := []byte("raw echo through frp")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(payload))
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("got %q, want %q", buf, payload)
	}
}
