package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type minProtocolProviderServer struct {
	proto.UnimplementedProviderPluginServer
}

func (s *minProtocolProviderServer) GetMetadata(context.Context, *emptypb.Empty) (*proto.ProviderMetadata, error) {
	return &proto.ProviderMetadata{
		Name:               "minproto",
		DisplayName:        "Min Protocol Provider",
		Description:        "test fixture",
		ConnectionMode:     proto.ConnectionMode_CONNECTION_MODE_NONE,
		MinProtocolVersion: proto.CurrentProtocolVersion,
	}, nil
}

func (s *minProtocolProviderServer) StartProvider(_ context.Context, req *proto.StartProviderRequest) (*proto.StartProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	return &proto.StartProviderResponse{
		ProtocolVersion: proto.CurrentProtocolVersion,
	}, nil
}

func (s *minProtocolProviderServer) ListOperations(context.Context, *emptypb.Empty) (*proto.ListOperationsResponse, error) {
	return &proto.ListOperationsResponse{
		Operations: []*proto.Operation{
			{
				Name:        "ping",
				Description: "Ping",
				Method:      http.MethodGet,
			},
		},
	}, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	socket := os.Getenv(proto.EnvPluginSocket)
	if socket == "" {
		return fmt.Errorf("%s is required", proto.EnvPluginSocket)
	}
	if err := os.RemoveAll(socket); err != nil {
		return fmt.Errorf("remove stale socket %q: %w", socket, err)
	}

	lis, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("listen on plugin socket %q: %w", socket, err)
	}
	defer func() {
		_ = lis.Close()
		_ = os.Remove(socket)
	}()

	srv := grpc.NewServer()
	proto.RegisterProviderPluginServer(srv, &minProtocolProviderServer{})

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		srv.GracefulStop()
	}()

	err = srv.Serve(lis)
	if ctx.Err() != nil {
		<-stopped
		return nil
	}
	return err
}
