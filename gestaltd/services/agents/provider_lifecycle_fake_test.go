package agents

import (
	"context"
	"errors"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeProviderLifecycleClient struct{}

func (*fakeProviderLifecycleClient) GetProviderIdentity(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.ProviderIdentity, error) {
	return nil, errors.New("unexpected GetProviderIdentity call")
}

func (*fakeProviderLifecycleClient) ConfigureProvider(context.Context, *proto.ConfigureProviderRequest, ...grpc.CallOption) (*proto.ConfigureProviderResponse, error) {
	return nil, errors.New("unexpected ConfigureProvider call")
}

func (*fakeProviderLifecycleClient) HealthCheck(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.HealthCheckResponse, error) {
	return &proto.HealthCheckResponse{Ready: true}, nil
}

func (*fakeProviderLifecycleClient) StartProvider(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.StartRuntimeProviderResponse, error) {
	return nil, errors.New("unexpected StartProvider call")
}
