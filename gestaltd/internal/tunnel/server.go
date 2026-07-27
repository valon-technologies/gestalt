package tunnel

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	frpserver "github.com/fatedier/frp/server"
	"golang.org/x/net/websocket"
)

const FrpsWebsocketPath = "/~!frp"

type Server struct {
	service     *frpserver.Service
	cancel      context.CancelFunc
	controlAddr string
	connectPort int
}

// StartServer launches an in-process frps. The control listener binds a
// random localhost port; HTTPHandler exposes it via WebSocket upgrade at
// /~!frp so frpc clients can connect through the existing HTTP ingress
// (wss://host:443/~!frp) without a separate TCP port. The tcpmux
// httpconnect listener binds a separate random localhost port for the
// upstream's internal dial-back.
func StartServer(ctx context.Context) (*Server, error) {
	controlLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("tunnel server: control listen: %w", err)
	}
	controlPort := controlLn.Addr().(*net.TCPAddr).Port
	_ = controlLn.Close()

	connectLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("tunnel server: connect listen: %w", err)
	}
	connectPort := connectLn.Addr().(*net.TCPAddr).Port
	_ = connectLn.Close()

	cfg := &v1.ServerConfig{
		BindAddr:              "127.0.0.1",
		BindPort:              controlPort,
		ProxyBindAddr:         "127.0.0.1",
		TCPMuxHTTPConnectPort: connectPort,
	}
	_ = cfg.Complete()
	cfg.BindPort = controlPort
	cfg.TCPMuxHTTPConnectPort = connectPort
	cfg.ProxyBindAddr = "127.0.0.1"

	service, err := frpserver.NewService(cfg)
	if err != nil {
		return nil, fmt.Errorf("tunnel server: NewService: %w", err)
	}

	serverCtx, cancel := context.WithCancel(ctx)
	go service.Run(serverCtx)

	return &Server{
		service:     service,
		cancel:      cancel,
		controlAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(controlPort)),
		connectPort: connectPort,
	}, nil
}

// HTTPHandler returns an http.Handler that upgrades WebSocket connections at
// /~!frp and pipes them to the frps control listener. Mount this on the
// existing HTTP server so frpc clients can reach frps through the same port
// as the public API.
func (s *Server) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(FrpsWebsocketPath, websocket.Server{
		Handler: func(c *websocket.Conn) {
			c.PayloadType = websocket.BinaryFrame
			backend, err := net.Dial("tcp", s.controlAddr)
			if err != nil {
				return
			}
			defer func() { _ = backend.Close() }()
			done := make(chan struct{}, 2)
			go func() { _, _ = io.Copy(backend, c); done <- struct{}{} }()
			go func() { _, _ = io.Copy(c, backend); done <- struct{}{} }()
			<-done
		},
		Handshake: func(_ *websocket.Config, _ *http.Request) error { return nil },
	})
	return mux
}

func (s *Server) ConnectAddr() string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(s.connectPort))
}

func (s *Server) ServerURL(rawurl string) string {
	return rawurl
}

func (s *Server) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.service != nil {
		_ = s.service.Close()
	}
}
