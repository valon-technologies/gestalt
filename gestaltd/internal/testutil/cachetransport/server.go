// Package cachetransport provides a minimal Cache gRPC server backed by an
// in-memory StubCache for SDK transport tests.
package cachetransport

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil/relaytest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	cacheservice "github.com/valon-technologies/gestalt/server/services/cache"
	"google.golang.org/grpc"
)

// Server wraps a gRPC server backed by StubCache on a transport target.
type Server struct {
	srv    *grpc.Server
	lis    net.Listener
	target string
}

type Options struct {
	ExpectRelayToken string
}

// Start creates a new Cache gRPC server on the supplied transport target.
// Supported forms are a plain Unix socket path, unix:///path, or tcp://host:port.
func Start(target string, opts Options) (*Server, error) {
	network, address, err := parseTarget(target)
	if err != nil {
		return nil, err
	}
	if network == "unix" {
		_ = os.Remove(address)
	}
	lis, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}
	stub := coretesting.NewStubCache()
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(relaytest.RequireRelayTokenUnary(opts.ExpectRelayToken)),
	)
	proto.RegisterCacheServer(srv, cacheservice.NewServer(stub, ""))
	go func() { _ = srv.Serve(lis) }()
	return &Server{srv: srv, lis: lis, target: target}, nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	s.srv.GracefulStop()
	_ = s.lis.Close()
	network, address, err := parseTarget(s.target)
	if err == nil && network == "unix" {
		_ = os.Remove(address)
	}
}

func parseTarget(raw string) (network string, address string, err error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", "", fmt.Errorf("cache transport target is required")
	}
	switch {
	case strings.HasPrefix(target, "tcp://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tcp://"))
		if address == "" {
			return "", "", fmt.Errorf("cache tcp target %q is missing host:port", raw)
		}
		return "tcp", address, nil
	case strings.HasPrefix(target, "unix://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "unix://"))
		if address == "" {
			return "", "", fmt.Errorf("cache unix target %q is missing a socket path", raw)
		}
		return "unix", address, nil
	case strings.Contains(target, "://"):
		return "", "", fmt.Errorf("unsupported cache transport target %q", raw)
	default:
		return "unix", filepath.Clean(target), nil
	}
}
