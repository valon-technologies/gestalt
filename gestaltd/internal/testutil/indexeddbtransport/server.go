// Package indexeddbtransport provides a minimal IndexedDB gRPC server
// backed by an in-memory StubIndexedDB for SDK transport tests.
package indexeddbtransport

import (
	"net"
	"os"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"google.golang.org/grpc"
)

// Server wraps a gRPC server backed by StubIndexedDB on a Unix socket.
type Server struct {
	srv    *grpc.Server
	lis    net.Listener
	Socket string
}

// Start creates a new IndexedDB gRPC server on the given Unix socket path.
// The server starts empty; tests seed data through the SDK client.
func Start(socketPath string) (*Server, error) {
	_ = os.Remove(socketPath)
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	stub := &coretesting.StubIndexedDB{}
	srv := grpc.NewServer()
	proto.RegisterIndexedDBServer(srv, providerhost.NewIndexedDBServer(stub, "", providerhost.IndexedDBServerOptions{}))
	go func() { _ = srv.Serve(lis) }()
	return &Server{srv: srv, lis: lis, Socket: socketPath}, nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	s.srv.GracefulStop()
	_ = s.lis.Close()
	_ = os.Remove(s.Socket)
}
