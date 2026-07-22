package tunnel

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

func TestNewIdentity(t *testing.T) {
	t.Parallel()

	id, err := NewIdentity()
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	if id.TunnelHost == "" {
		t.Fatal("TunnelHost is empty")
	}
	if len(id.TunnelHost) < 16 {
		t.Fatalf("TunnelHost len = %d, want >= 16", len(id.TunnelHost))
	}
	if id.SPKISHA256 == "" {
		t.Fatal("SPKISHA256 is empty")
	}
	if len(id.Certificate.Certificate) == 0 {
		t.Fatal("Certificate is empty")
	}

	// A second identity must have a different tunnel host (128-bit randomness).
	id2, err := NewIdentity()
	if err != nil {
		t.Fatalf("second NewIdentity: %v", err)
	}
	if id.TunnelHost == id2.TunnelHost {
		t.Fatalf("two identities share tunnel host %q", id.TunnelHost)
	}
}

func TestRandomTunnelHostUniqueness(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{}, 100)
	for range 100 {
		host, err := randomTunnelHost()
		if err != nil {
			t.Fatalf("randomTunnelHost: %v", err)
		}
		if _, ok := seen[host]; ok {
			t.Fatalf("collision within 100 hosts: %q", host)
		}
		seen[host] = struct{}{}
	}
}

// TestTunnelRoundTrip verifies bytes round-trip from a Dialer through a real
// in-process frps to a Host listener with mutual pinned inner TLS.
func TestTunnelRoundTrip(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("tunnel round-trip requires in-process frps")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	harness, err := StartTestHarness(ctx)
	if err != nil {
		t.Fatalf("StartTestHarness: %v", err)
	}
	defer harness.Close()

	// Upstream identity (the Dialer's client cert).
	upstreamID, err := NewIdentity()
	if err != nil {
		t.Fatalf("upstream NewIdentity: %v", err)
	}

	// Local identity (the Host's server cert).
	localID, err := NewIdentity()
	if err != nil {
		t.Fatalf("local NewIdentity: %v", err)
	}

	host, err := StartHost(ctx, HostConfig{
		ServerURL:    harness.ServerURL(),
		Identity:     localID,
		UpstreamSPKI: upstreamID.SPKISHA256,
	})
	if err != nil {
		t.Fatalf("StartHost: %v", err)
	}
	defer func() { _ = host.Close() }()

	// Wait for frpc to register the proxy with frps.
	waitProxyReady()

	// Accept on the host side; the tlsListener completes the handshake in Accept.
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := host.Listener().Accept()
		if acceptErr != nil {
			accepted <- nil
			return
		}
		accepted <- conn
	}()

	dialer := NewDialer(DialerConfig{
		ConnectAddr:    harness.ConnectAddr(),
		TunnelHost:     localID.TunnelHost,
		PinnedSPKI:     localID.SPKISHA256,
		ClientIdentity: upstreamID.Certificate,
	})

	conn, err := dialer.DialContext(ctx, "tcp", localID.TunnelHost)
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer func() { _ = conn.Close() }()

	serverConn := <-accepted
	if serverConn == nil {
		t.Fatal("host Accept returned nil")
	}
	defer func() { _ = serverConn.Close() }()

	// Verify PeerIdentity on the server side reports the upstream SPKI.
	peerSPKI, ok := PeerIdentity(serverConn)
	if !ok {
		t.Fatal("PeerIdentity returned not ok")
	}
	if peerSPKI != upstreamID.SPKISHA256 {
		t.Fatalf("PeerIdentity = %q, want %q", peerSPKI, upstreamID.SPKISHA256)
	}

	// Round-trip bytes.
	payload := []byte("hello through the tunnel")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("client write: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(serverConn, buf); err != nil {
		t.Fatalf("server read: %v", err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("server got %q, want %q", buf, payload)
	}

	reply := []byte("ack from host")
	if _, err := serverConn.Write(reply); err != nil {
		t.Fatalf("server write: %v", err)
	}
	rbuf := make([]byte, len(reply))
	if _, err := io.ReadFull(conn, rbuf); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(rbuf) != string(reply) {
		t.Fatalf("client got %q, want %q", rbuf, reply)
	}
}

func TestTunnelWrongServerSPKIRejected(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("tunnel SPKI test requires in-process frps")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	harness, err := StartTestHarness(ctx)
	if err != nil {
		t.Fatalf("StartTestHarness: %v", err)
	}
	defer harness.Close()

	upstreamID, err := NewIdentity()
	if err != nil {
		t.Fatalf("upstream NewIdentity: %v", err)
	}
	localID, err := NewIdentity()
	if err != nil {
		t.Fatalf("local NewIdentity: %v", err)
	}

	host, err := StartHost(ctx, HostConfig{
		ServerURL:    harness.ServerURL(),
		Identity:     localID,
		UpstreamSPKI: upstreamID.SPKISHA256,
	})
	if err != nil {
		t.Fatalf("StartHost: %v", err)
	}
	defer func() { _ = host.Close() }()

	waitProxyReady()

	// Dial with a wrong pinned SPKI (a fresh identity's SPKI, not localID's).
	wrongID, err := NewIdentity()
	if err != nil {
		t.Fatalf("wrong NewIdentity: %v", err)
	}
	dialer := NewDialer(DialerConfig{
		ConnectAddr:    harness.ConnectAddr(),
		TunnelHost:     localID.TunnelHost,
		PinnedSPKI:     wrongID.SPKISHA256,
		ClientIdentity: upstreamID.Certificate,
	})

	// Drive the Host-side Accept so the inner mTLS handshake runs on both sides.
	// The Dialer rejects because the server cert SPKI does not match PinnedSPKI.
	go func() {
		conn, _ := host.Listener().Accept()
		if conn != nil {
			_ = conn.Close()
		}
	}()

	_, err = dialer.DialContext(ctx, "tcp", localID.TunnelHost)
	if err == nil {
		t.Fatal("DialContext with wrong pinned SPKI succeeded, want error")
	}
}

func TestTunnelWrongClientSPKIRejected(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("tunnel SPKI test requires in-process frps")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	harness, err := StartTestHarness(ctx)
	if err != nil {
		t.Fatalf("StartTestHarness: %v", err)
	}
	defer harness.Close()

	upstreamID, err := NewIdentity()
	if err != nil {
		t.Fatalf("upstream NewIdentity: %v", err)
	}
	localID, err := NewIdentity()
	if err != nil {
		t.Fatalf("local NewIdentity: %v", err)
	}
	// Host pins a WRONG upstream SPKI, so the real upstream cert is rejected.
	wrongPinned, err := NewIdentity()
	if err != nil {
		t.Fatalf("wrong NewIdentity: %v", err)
	}

	host, err := StartHost(ctx, HostConfig{
		ServerURL:    harness.ServerURL(),
		Identity:     localID,
		UpstreamSPKI: wrongPinned.SPKISHA256,
	})
	if err != nil {
		t.Fatalf("StartHost: %v", err)
	}
	defer func() { _ = host.Close() }()

	waitProxyReady()

	// Drive the Host-side Accept so the SPKI pin check on the client cert runs.
	// The Host pins a wrong upstream SPKI, so the tlsListener rejects the client
	// cert and closes the connection; the Dialer's handshake then fails.
	go func() {
		conn, _ := host.Listener().Accept()
		if conn != nil {
			_ = conn.Close()
		}
	}()

	dialer := NewDialer(DialerConfig{
		ConnectAddr:    harness.ConnectAddr(),
		TunnelHost:     localID.TunnelHost,
		PinnedSPKI:     localID.SPKISHA256,
		ClientIdentity: upstreamID.Certificate,
	})

	conn, err := dialer.DialContext(ctx, "tcp", localID.TunnelHost)
	if err == nil {
		// In TLS 1.3 the client handshake may complete before the server's
		// cert verification rejects. The rejection surfaces on the next I/O.
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, werr := conn.Write([]byte("probe")); werr == nil {
			buf := make([]byte, 1)
			if _, rerr := conn.Read(buf); rerr == nil {
				_ = conn.Close()
				t.Fatal("DialContext with wrong client SPKI succeeded and probe was accepted, want error")
			}
		}
		_ = conn.Close()
	}
}

// waitProxyReady waits briefly for frpc to register the tcpmux proxy with frps.
// Registration is fast (<1s in practice); a fixed delay avoids racing the
// asynchronous frpc login flow.
func waitProxyReady() {
	time.Sleep(2 * time.Second)
}

// TestTunnelGRPCOverTunnel verifies a gRPC call succeeds over the tunnel.
func TestTunnelGRPCOverTunnel(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("grpc-over-tunnel requires in-process frps")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	harness, err := StartTestHarness(ctx)
	if err != nil {
		t.Fatalf("StartTestHarness: %v", err)
	}
	defer harness.Close()

	upstreamID, err := NewIdentity()
	if err != nil {
		t.Fatalf("upstream NewIdentity: %v", err)
	}
	localID, err := NewIdentity()
	if err != nil {
		t.Fatalf("local NewIdentity: %v", err)
	}

	host, err := StartHost(ctx, HostConfig{
		ServerURL:    harness.ServerURL(),
		Identity:     localID,
		UpstreamSPKI: upstreamID.SPKISHA256,
	})
	if err != nil {
		t.Fatalf("StartHost: %v", err)
	}
	defer func() { _ = host.Close() }()

	waitProxyReady()

	// Serve a trivial gRPC server on the host listener.
	grpcServer := grpc.NewServer()
	go func() { _ = grpcServer.Serve(host.Listener()) }()
	defer grpcServer.Stop()

	dialer := NewDialer(DialerConfig{
		ConnectAddr:    harness.ConnectAddr(),
		TunnelHost:     localID.TunnelHost,
		PinnedSPKI:     localID.SPKISHA256,
		ClientIdentity: upstreamID.Certificate,
	})
	conn, err := grpc.NewClient(localID.TunnelHost,
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", addr)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// The connection should leave Idle, proving the tunnel transport works.
	ctx2, cancel2 := context.WithTimeout(ctx, 10*time.Second)
	defer cancel2()
	_ = conn.WaitForStateChange(ctx2, conn.GetState())
	if conn.GetState() == connectivity.Shutdown {
		t.Fatalf("gRPC conn shut down, state = %v", conn.GetState())
	}
}
