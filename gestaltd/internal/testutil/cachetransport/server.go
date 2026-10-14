// Package cachetransport provides a minimal Cache gRPC server backed by an
// in-memory StubCache for SDK transport tests.
package cachetransport

import (
	"net"
	"os"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil/relaytest"
	"github.com/valon-technologies/gestalt/server/internal/testutil/transporttest"
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
	network, address, err := transporttest.ParseTarget("cache", target)
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
	network, address, err := transporttest.ParseTarget("cache", s.target)
	if err == nil && network == "unix" {
		_ = os.Remove(address)
	}
}
