// Package s3transport provides a minimal S3 gRPC server backed by an
// in-memory StubS3 for SDK transport tests.
package s3transport

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil/relaytest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/s3"
	"google.golang.org/grpc"
)

// Server wraps a gRPC server backed by StubS3 on a transport target.
type Server struct {
	srv    *grpc.Server
	lis    net.Listener
	target string
}

type Options struct {
	ExpectRelayToken string
}

// Start creates a new S3 gRPC server on the supplied transport target. The
// target can be a plain Unix socket path, unix:///path, or tcp://host:port.
// The server starts empty; tests seed data through the SDK client.
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
	stub := &coretesting.StubS3{}
	accessURLs, err := s3.NewObjectAccessURLManager(
		[]byte("0123456789abcdef0123456789abcdef"),
		"https://gestalt.example.test",
	)
	if err != nil {
		_ = lis.Close()
		return nil, err
	}
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(relaytest.RequireRelayTokenUnary(opts.ExpectRelayToken)),
		grpc.ChainStreamInterceptor(relaytest.RequireRelayTokenStream(opts.ExpectRelayToken)),
	)
	proto.RegisterS3Server(srv, s3.NewServer(stub, ""))
	proto.RegisterS3ObjectAccessServer(srv, s3.NewObjectAccessServer(accessURLs, "sdk-test", "default"))
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
		return "", "", fmt.Errorf("s3 transport target is required")
	}
	switch {
	case strings.HasPrefix(target, "tcp://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tcp://"))
		if address == "" {
			return "", "", fmt.Errorf("s3 tcp target %q is missing host:port", raw)
		}
		return "tcp", address, nil
	case strings.HasPrefix(target, "unix://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "unix://"))
		if address == "" {
			return "", "", fmt.Errorf("s3 unix target %q is missing a socket path", raw)
		}
		return "unix", address, nil
	case strings.Contains(target, "://"):
		return "", "", fmt.Errorf("unsupported s3 transport target %q", raw)
	default:
		return "unix", filepath.Clean(target), nil
	}
}
