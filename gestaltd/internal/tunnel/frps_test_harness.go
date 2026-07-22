package tunnel

import (
	"context"
	"fmt"
	"net"
	"strconv"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	frpserver "github.com/fatedier/frp/server"
)

// TestHarness runs an in-process frps for functional tests. It exposes the
// control address (frpc connects here) and the internal CONNECT address (the
// Dialer targets here).
type TestHarness struct {
	service     *frpserver.Service
	bindAddr    string
	bindPort    int
	connectPort int
	cancel      context.CancelFunc
}

// StartTestHarness launches an in-process frps bound to 127.0.0.1 with a
// tcpmux httpconnect port and no authentication.
func StartTestHarness(ctx context.Context) (*TestHarness, error) {
	bindPort, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("test harness: bind port: %w", err)
	}
	connectPort, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("test harness: connect port: %w", err)
	}

	cfg := &v1.ServerConfig{
		BindAddr:              "127.0.0.1",
		BindPort:              bindPort,
		TCPMuxHTTPConnectPort: connectPort,
	}
	_ = cfg.Complete()

	service, err := frpserver.NewService(cfg)
	if err != nil {
		return nil, fmt.Errorf("test harness: frps NewService: %w", err)
	}

	harnessCtx, cancel := context.WithCancel(ctx)
	go service.Run(harnessCtx)

	h := &TestHarness{
		service:     service,
		bindAddr:    "127.0.0.1",
		bindPort:    bindPort,
		connectPort: connectPort,
		cancel:      cancel,
	}
	return h, nil
}

// ControlAddr is the frpc control address (host:port).
func (h *TestHarness) ControlAddr() string {
	return net.JoinHostPort(h.bindAddr, strconv.Itoa(h.bindPort))
}

// ConnectAddr is the internal CONNECT address the Dialer targets.
func (h *TestHarness) ConnectAddr() string {
	return net.JoinHostPort(h.bindAddr, strconv.Itoa(h.connectPort))
}

// ServerURL returns the control address as a URL-like string for HostConfig.
func (h *TestHarness) ServerURL() string {
	return "http://" + h.ControlAddr()
}

// Close stops the frps service.
func (h *TestHarness) Close() {
	if h.cancel != nil {
		h.cancel()
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port, nil
}
