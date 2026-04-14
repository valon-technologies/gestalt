// Package cachetransport provides a minimal Cache gRPC server backed by an
// in-memory StubCache for SDK transport tests.
package cachetransport

import (
	"net"
	"os"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"google.golang.org/grpc"
)

// Server wraps a gRPC server backed by StubCache on a Unix socket.
type Server struct {
	srv    *grpc.Server
	lis    net.Listener
	Socket string
}

// Start creates a new Cache gRPC server on the given Unix socket path.
func Start(socketPath string) (*Server, error) {
	_ = os.Remove(socketPath)
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	stub := coretesting.NewStubCache()
	srv := grpc.NewServer()
	proto.RegisterCacheServer(srv, providerhost.NewCacheServer(stub, ""))
	go func() { _ = srv.Serve(lis) }()
	return &Server{srv: srv, lis: lis, Socket: socketPath}, nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	s.srv.GracefulStop()
	_ = s.lis.Close()
	_ = os.Remove(s.Socket)
}
