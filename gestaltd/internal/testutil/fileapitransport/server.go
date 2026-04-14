// Package fileapitransport provides a minimal FileAPI gRPC server backed by an
// in-memory StubFileAPI for SDK transport tests.
package fileapitransport

import (
	"net"
	"os"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"google.golang.org/grpc"
)

type Server struct {
	srv    *grpc.Server
	lis    net.Listener
	Socket string
}

func Start(socketPath string) (*Server, error) {
	_ = os.Remove(socketPath)
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	stub := &coretesting.StubFileAPI{}
	srv := grpc.NewServer()
	proto.RegisterFileAPIServer(srv, providerhost.NewFileAPIServer(stub, ""))
	go func() { _ = srv.Serve(lis) }()
	return &Server{srv: srv, lis: lis, Socket: socketPath}, nil
}

func (s *Server) Stop() {
	s.srv.GracefulStop()
	_ = s.lis.Close()
	_ = os.Remove(s.Socket)
}
