// Package indexeddbtransport provides a minimal IndexedDB gRPC server
// backed by an in-memory StubIndexedDB for SDK transport tests.
package indexeddbtransport

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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
	stub := &coretesting.StubIndexedDB{}
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(requireRelayTokenUnary(opts.ExpectRelayToken)),
		grpc.ChainStreamInterceptor(requireRelayTokenStream(opts.ExpectRelayToken)),
	)
	proto.RegisterIndexedDBServer(srv, providerhost.NewIndexedDBServer(stub, "", providerhost.IndexedDBServerOptions{}))
	go func() { _ = srv.Serve(lis) }()
	return &Server{srv: srv, lis: lis, target: target}, nil
}

func requireRelayTokenUnary(expected string) grpc.UnaryServerInterceptor {
	expected = strings.TrimSpace(expected)
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := validateRelayToken(ctx, expected); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

func requireRelayTokenStream(expected string) grpc.StreamServerInterceptor {
	expected = strings.TrimSpace(expected)
	return func(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := validateRelayToken(stream.Context(), expected); err != nil {
			return err
		}
		return handler(srv, stream)
	}
}

func validateRelayToken(ctx context.Context, expected string) error {
	if expected == "" {
		return nil
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing relay token metadata")
	}
	values := md.Get(providerhost.HostServiceRelayTokenHeader)
	if len(values) == 0 || strings.TrimSpace(values[0]) != expected {
		return status.Error(codes.Unauthenticated, "invalid relay token metadata")
	}
	return nil
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
		return "", "", fmt.Errorf("indexeddb transport target is required")
	}
	switch {
	case strings.HasPrefix(target, "tcp://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tcp://"))
		if address == "" {
			return "", "", fmt.Errorf("indexeddb tcp target %q is missing host:port", raw)
		}
		return "tcp", address, nil
	case strings.HasPrefix(target, "unix://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "unix://"))
		if address == "" {
			return "", "", fmt.Errorf("indexeddb unix target %q is missing a socket path", raw)
		}
		return "unix", address, nil
	case strings.Contains(target, "://"):
		return "", "", fmt.Errorf("unsupported indexeddb transport target %q", raw)
	default:
		return "unix", filepath.Clean(target), nil
	}
}
