package tunnel

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// DialerConfig runs the upstream side: HTTP CONNECT to the internal frps port,
// then inner mTLS pinned to the registration's certificate.
type DialerConfig struct {
	ConnectAddr    string          // cluster-internal frps CONNECT service
	TunnelHost     string          // from the registration
	PinnedSPKI     string          // registration's server_spki_sha256
	ClientIdentity tls.Certificate // upstream client identity (ServerIdentity keypair)
}

// Dialer opens tunneled connections to a reverse-remote host. It satisfies
// grpc.WithContextDialer and http.Transport.DialContext.
type Dialer struct {
	cfg DialerConfig
}

// NewDialer returns a Dialer for the given configuration.
func NewDialer(cfg DialerConfig) *Dialer {
	return &Dialer{cfg: cfg}
}

// DialContext connects to the tunnel host via HTTP CONNECT over the frps
// internal port, then upgrades to inner mutual TLS. The returned net.Conn has
// passed mutual TLS with the server SPKI pinned to cfg.PinnedSPKI.
func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if d == nil {
		return nil, errors.New("tunnel dialer: not configured")
	}
	if strings.TrimSpace(d.cfg.ConnectAddr) == "" {
		return nil, errors.New("tunnel dialer: connect address is required")
	}
	if strings.TrimSpace(d.cfg.TunnelHost) == "" {
		return nil, errors.New("tunnel dialer: tunnel host is required")
	}

	var dialer net.Dialer
	rawConn, err := dialer.DialContext(ctx, "tcp", d.cfg.ConnectAddr)
	if err != nil {
		return nil, fmt.Errorf("tunnel dialer: connect to frps: %w", err)
	}
	br, err := sendConnectRequest(ctx, rawConn, d.cfg.TunnelHost)
	if err != nil {
		_ = rawConn.Close()
		return nil, fmt.Errorf("tunnel dialer: CONNECT %s: %w", d.cfg.TunnelHost, err)
	}

	tlsCfg := &tls.Config{
		Certificates:       []tls.Certificate{d.cfg.ClientIdentity},
		ServerName:         d.cfg.TunnelHost,
		InsecureSkipVerify: true, // SPKI pinning below; we do not use system roots.
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{"h2", "http/1.1"},
	}
	// Wrap the bufio.Reader so any bytes buffered after the 200 response are not
	// lost when the connection is handed to the TLS client.
	tlsConn := tls.Client(&bufConn{Conn: rawConn, br: br}, tlsCfg)
	_ = tlsConn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = tlsConn.Close()
		return nil, fmt.Errorf("tunnel dialer: inner TLS handshake: %w", err)
	}
	_ = tlsConn.SetDeadline(time.Time{})
	state := tlsConn.ConnectionState()
	peerSPKI, err := PeerSPKISHA256(state)
	if err != nil {
		_ = tlsConn.Close()
		return nil, fmt.Errorf("tunnel dialer: peer SPKI: %w", err)
	}
	if peerSPKI != d.cfg.PinnedSPKI {
		_ = tlsConn.Close()
		return nil, fmt.Errorf("tunnel dialer: server SPKI %q does not match pinned %q", peerSPKI, d.cfg.PinnedSPKI)
	}
	return tlsConn, nil
}
