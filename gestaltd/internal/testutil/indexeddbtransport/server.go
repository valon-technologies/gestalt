// Package indexeddbtransport provides a minimal IndexedDB gRPC server
// backed by an in-memory StubIndexedDB for SDK transport tests.
package indexeddbtransport

import (
	"net"
	"os"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil/relaytest"
	"github.com/valon-technologies/gestalt/server/internal/testutil/transporttest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"google.golang.org/grpc"
)

// Server wraps a gRPC server backed by StubIndexedDB on a Unix socket.
type Server struct {
	srv    *grpc.Server
	lis    net.Listener
	target string
}

type Options struct {
	ExpectRelayToken string
}

// Start creates a new IndexedDB gRPC server on the supplied transport target.
// Supported forms are a plain Unix socket path, unix:///path, or tcp://host:port.
// The server starts empty; tests seed data through the SDK client.
func Start(target string, opts Options) (*Server, error) {
	network, address, err := transporttest.ParseTarget("indexeddb", target)
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
	stub := &coretesting.StubIndexedDB{}
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(relaytest.RequireRelayTokenUnary(opts.ExpectRelayToken)),
		grpc.ChainStreamInterceptor(relaytest.RequireRelayTokenStream(opts.ExpectRelayToken)),
	)
	proto.RegisterIndexedDBServer(srv, indexeddbservice.NewServer(stub, "", indexeddbservice.ServerOptions{}))
	go func() { _ = srv.Serve(lis) }()
	return &Server{srv: srv, lis: lis, target: target}, nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	s.srv.GracefulStop()
	_ = s.lis.Close()
	network, address, err := transporttest.ParseTarget("indexeddb", s.target)
	if err == nil && network == "unix" {
		_ = os.Remove(address)
	}
}
