package tunnel

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	frpcclient "github.com/fatedier/frp/client"
	frpproxy "github.com/fatedier/frp/client/proxy"
	frpsource "github.com/fatedier/frp/pkg/config/source"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	fmux "github.com/hashicorp/yamux"
	"golang.org/x/net/websocket"
)

// HostConfig runs the local side: an embedded frpc proxying the tunnel host to
// an in-process reverse listener that terminates inner mTLS.
type HostConfig struct {
	ServerURL    string // frps WSS endpoint from TunnelBootstrap.frps_address
	Identity     *Identity
	UpstreamSPKI string // pin for the upstream client identity from ListRemotes ServerIdentity
}

// Host runs the local tunnel side. Listener returns connections that have
// already passed mutual inner TLS; each net.Conn exposes the verified upstream
// identity via PeerIdentity.
type Host struct {
	frpService *frpcclient.Service
	tunnelHost string
	inner      net.Listener
	closed     bool
	mu         sync.Mutex
}

type wssConnector struct {
	ctx     context.Context
	cfg     *v1.ClientCommonConfig
	useTLS  bool
	session *fmux.Session
	conn    net.Conn
}

func (c *wssConnector) Open() error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	cfg := fmux.DefaultConfig()
	cfg.KeepAliveInterval = 30 * time.Second
	cfg.MaxStreamWindowSize = 6 * 1024 * 1024
	session, err := fmux.Client(conn, cfg)
	if err != nil {
		_ = conn.Close()
		return err
	}
	c.session = session
	c.conn = conn
	return nil
}

func (c *wssConnector) Connect() (net.Conn, error) {
	if c.session != nil {
		return c.session.OpenStream()
	}
	return c.dial()
}

func (c *wssConnector) Close() error {
	if c.session != nil {
		_ = c.session.Close()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	return nil
}

func (c *wssConnector) dial() (net.Conn, error) {
	if err := c.ctx.Err(); err != nil {
		return nil, fmt.Errorf("wss connector: %w", err)
	}
	host := c.cfg.ServerAddr
	port := c.cfg.ServerPort
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	d := net.Dialer{}
	rawConn, err := d.DialContext(c.ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("wss connector: dial tcp: %w", err)
	}

	conn := rawConn
	wsScheme := "ws"
	if c.useTLS {
		sn := c.cfg.Transport.TLS.ServerName
		if sn == "" {
			sn = host
		}
		tlsCfg := &tls.Config{
			ServerName: sn,
			NextProtos: []string{"http/1.1"},
		}
		tlsConn := tls.Client(rawConn, tlsCfg)
		if err := tlsConn.HandshakeContext(c.ctx); err != nil {
			_ = rawConn.Close()
			return nil, fmt.Errorf("wss connector: tls handshake: %w", err)
		}
		conn = tlsConn
		wsScheme = "wss"
	}

	hostport := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	wsURL := wsScheme + "://" + hostport + "/~!frp"
	origin := "http://" + hostport
	wsCfg, err := websocket.NewConfig(wsURL, origin)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("wss connector: websocket config: %w", err)
	}

	if err := c.ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("wss connector: %w", err)
	}

	wsConn, err := websocket.NewClient(wsCfg, conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("wss connector: websocket handshake: %w", err)
	}
	wsConn.PayloadType = websocket.BinaryFrame
	return wsConn, nil
}

// StartHost starts the embedded frpc and the inner mTLS listener. The frpc
// registers a tcpmux httpconnect proxy for the tunnel host; bytes forwarded by
// frps arrive on the inner listener, which terminates mutual TLS pinning the
// upstream SPKI.
func StartHost(ctx context.Context, cfg HostConfig) (*Host, error) {
	if strings.TrimSpace(cfg.ServerURL) == "" {
		return nil, errors.New("tunnel host: server url is required")
	}
	if cfg.Identity == nil {
		return nil, errors.New("tunnel host: identity is required")
	}

	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("tunnel host: inner listener: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cfg.Identity.Certificate},
		MinVersion:   tls.VersionTLS13,
		ClientAuth:   tls.RequireAnyClientCert,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("no client certificate")
			}
			spki, err := spkiSHA256(rawCerts[0])
			if err != nil {
				return err
			}
			if spki != cfg.UpstreamSPKI {
				return fmt.Errorf("client SPKI %q does not match pinned upstream %q", spki, cfg.UpstreamSPKI)
			}
			return nil
		},
	}

	// Build the frpc config: connect to ServerURL, register a tcpmux httpconnect
	// proxy whose CustomDomains is the tunnel host, forwarding to the inner listener.
	proxyCfg := &v1.TCPMuxProxyConfig{}
	proxyCfg.Name = cfg.Identity.TunnelHost
	proxyCfg.Type = string(v1.ProxyTypeTCPMUX)
	proxyCfg.LocalIP = "127.0.0.1"
	proxyCfg.LocalPort = innerListener.Addr().(*net.TCPAddr).Port
	proxyCfg.Multiplexer = string(v1.TCPMultiplexerHTTPConnect)
	proxyCfg.CustomDomains = []string{cfg.Identity.TunnelHost}

	common := &v1.ClientCommonConfig{}
	common.ServerAddr = hostFromURL(cfg.ServerURL)
	common.ServerPort = portFromURL(cfg.ServerURL)
	useTLS := isWssURL(cfg.ServerURL)
	common.Transport.TCPMux = ptr(true)
	if isWebsocketURL(cfg.ServerURL) {
		common.Transport.Protocol = "websocket"
	}

	configSource := frpsource.NewConfigSource()
	if err := configSource.ReplaceAll([]v1.ProxyConfigurer{proxyCfg}, nil); err != nil {
		_ = innerListener.Close()
		return nil, fmt.Errorf("tunnel host: set proxy config: %w", err)
	}

	service, err := frpcclient.NewService(frpcclient.ServiceOptions{
		Common:                 common,
		ConfigSourceAggregator: frpsource.NewAggregator(configSource),
		ConnectorCreator: func(ctx context.Context, cfg *v1.ClientCommonConfig) frpcclient.Connector {
			return &wssConnector{ctx: ctx, cfg: cfg, useTLS: useTLS}
		},
	})
	if err != nil {
		_ = innerListener.Close()
		return nil, fmt.Errorf("tunnel host: frpc service: %w", err)
	}

	go func() { _ = service.Run(ctx) }()

	return &Host{
		frpService: service,
		tunnelHost: cfg.Identity.TunnelHost,
		inner:      &tlsListener{Listener: innerListener, tlsCfg: tlsCfg},
	}, nil
}

// WaitReady blocks until the frpc proxy is running or ctx is canceled. It
// polls the frpc status exporter for the proxy phase.
func (h *Host) WaitReady(ctx context.Context) error {
	exporter := h.frpService.StatusExporter()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if status, ok := exporter.GetProxyStatus(h.tunnelHost); ok && status.Phase == frpproxy.ProxyPhaseRunning {
			return nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Listener returns connections that have already passed mutual inner TLS. Each
// connection's TLS state exposes the verified upstream identity via
// PeerIdentity.
func (h *Host) Listener() net.Listener {
	return h.inner
}

// Close stops the frpc service and the inner listener.
func (h *Host) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	if h.frpService != nil {
		h.frpService.Close()
	}
	return h.inner.Close()
}

// PeerIdentity extracts the verified upstream SPKI from an accepted inner-TLS
// connection. The connection must come from Host.Listener.
func PeerIdentity(conn net.Conn) (string, bool) {
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return "", false
	}
	state := tlsConn.ConnectionState()
	spki, err := PeerSPKISHA256(state)
	if err != nil {
		return "", false
	}
	return spki, true
}

func ptr[T any](v T) *T { return &v }

// isWebsocketURL reports whether the URL uses ws:// or wss://.
func isWssURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "wss://")
}

func isWebsocketURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "ws://") || strings.HasPrefix(rawURL, "wss://")
}

// hostFromURL extracts the host from a URL, handling bracketed IPv6 addresses.
func hostFromURL(rawURL string) string {
	host, _ := splitHostPort(rawURL)
	return host
}

// portFromURL extracts the port from a URL, defaulting to 443 for wss/https
// and 80 for ws/http when no explicit port is given.
func portFromURL(rawURL string) int {
	_, port := splitHostPort(rawURL)
	if port != 0 {
		return port
	}
	if strings.HasPrefix(rawURL, "https://") || strings.HasPrefix(rawURL, "wss://") {
		return 443
	}
	return 80
}

// splitHostPort strips the scheme and splits into host:port using net.SplitHostPort,
// which correctly handles bracketed IPv6 addresses like [2001:db8::1]:443.
func splitHostPort(rawURL string) (string, int) {
	hostport := rawURL
	for _, scheme := range []string{"https://", "http://", "wss://", "ws://"} {
		if strings.HasPrefix(hostport, scheme) {
			hostport = strings.TrimPrefix(hostport, scheme)
			break
		}
	}
	// Trim any trailing path.
	if i := strings.Index(hostport, "/"); i >= 0 {
		hostport = hostport[:i]
	}
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		// No port: strip IPv6 brackets if present.
		host = strings.TrimPrefix(strings.TrimSuffix(hostport, "]"), "[")
		return host, 0
	}
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	return host, port
}
