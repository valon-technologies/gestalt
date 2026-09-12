package workflows

import (
	"context"
	"strings"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type recordingLifecycleClient struct {
	promoteWorkers func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.PromoteWorkersResponse, error)
}

func (c *recordingLifecycleClient) GetProviderIdentity(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.ProviderIdentity, error) {
	return nil, status.Error(codes.Unimplemented, "not used")
}

func (c *recordingLifecycleClient) ConfigureProvider(context.Context, *proto.ConfigureProviderRequest, ...grpc.CallOption) (*proto.ConfigureProviderResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not used")
}

func (c *recordingLifecycleClient) HealthCheck(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.HealthCheckResponse, error) {
	return &proto.HealthCheckResponse{Ready: true}, nil
}

func (c *recordingLifecycleClient) StartProvider(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.StartRuntimeProviderResponse, error) {
	return &proto.StartRuntimeProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func (c *recordingLifecycleClient) PromoteWorkers(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*proto.PromoteWorkersResponse, error) {
	if c.promoteWorkers != nil {
		return c.promoteWorkers(ctx, in, opts...)
	}
	return &proto.PromoteWorkersResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func TestRemoteWorkflowPromoteWorkersInvokesLifecycleRPC(t *testing.T) {
	t.Parallel()

	calls := 0
	provider := &remoteWorkflow{
		name:    "local",
		runtime: &recordingLifecycleClient{
			promoteWorkers: func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.PromoteWorkersResponse, error) {
				calls++
				return &proto.PromoteWorkersResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
			},
		},
	}
	if err := provider.PromoteWorkers(context.Background()); err != nil {
		t.Fatalf("PromoteWorkers: %v", err)
	}
	if calls != 1 {
		t.Fatalf("PromoteWorkers RPC calls = %d, want 1", calls)
	}
}

func TestRemoteWorkflowPromoteWorkersPropagatesProviderError(t *testing.T) {
	t.Parallel()

	provider := &remoteWorkflow{
		name: "local",
		runtime: &recordingLifecycleClient{
			promoteWorkers: func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.PromoteWorkersResponse, error) {
				return nil, status.Error(codes.Unknown, "set worker deployment current version: boom")
			},
		},
	}
	err := provider.PromoteWorkers(context.Background())
	if err == nil {
		t.Fatal("expected provider error")
	}
	if !strings.Contains(err.Error(), "set worker deployment current version: boom") {
		t.Fatalf("PromoteWorkers error = %v", err)
	}
}

func TestRemoteWorkflowPromoteWorkersHonorsCancellation(t *testing.T) {
	t.Parallel()

	provider := &remoteWorkflow{
		name: "local",
		runtime: &recordingLifecycleClient{
			promoteWorkers: func(ctx context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*proto.PromoteWorkersResponse, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := provider.PromoteWorkers(ctx); err == nil {
		t.Fatal("expected cancellation error")
	}
}
