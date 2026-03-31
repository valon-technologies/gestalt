package pluginhost

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"google.golang.org/protobuf/types/known/emptypb"
)

type RemoteRuntime struct {
	name   string
	client proto.RuntimePluginClient
	start  *proto.StartRuntimeRequest
}

func NewRemoteRuntime(name string, client proto.RuntimePluginClient, config map[string]any, initialCapabilities []core.Capability) (*RemoteRuntime, error) {
	cfg, err := structFromMap(config)
	if err != nil {
		return nil, err
	}
	caps, err := capabilitiesToProto(initialCapabilities)
	if err != nil {
		return nil, err
	}
	return &RemoteRuntime{
		name:   name,
		client: client,
		start: &proto.StartRuntimeRequest{
			Name:                name,
			Config:              cfg,
			InitialCapabilities: caps,
		},
	}, nil
}

func (r *RemoteRuntime) Name() string { return r.name }

func (r *RemoteRuntime) Start(ctx context.Context) error {
	_, err := r.client.Start(ctx, r.start)
	return err
}

func (r *RemoteRuntime) Stop(ctx context.Context) error {
	_, err := r.client.Stop(ctx, &emptypb.Empty{})
	return err
}

type RuntimeHostServer struct {
	proto.UnimplementedRuntimeHostServer
	Invoker          invocation.Invoker
	CapabilityLister invocation.CapabilityLister
}

func NewRuntimeHostServer(invoker invocation.Invoker, lister invocation.CapabilityLister) *RuntimeHostServer {
	return &RuntimeHostServer{
		Invoker:          invoker,
		CapabilityLister: lister,
	}
}

func (s *RuntimeHostServer) Invoke(ctx context.Context, req *proto.InvokeRequest) (*proto.OperationResult, error) {
	result, err := s.Invoker.Invoke(
		ctx,
		principalFromProto(req.GetPrincipal()),
		req.GetProvider(),
		req.GetInstance(),
		req.GetOperation(),
		mapFromStruct(req.GetParams()),
	)
	if err != nil {
		return nil, err
	}
	return &proto.OperationResult{
		Status: int32(result.Status),
		Body:   result.Body,
	}, nil
}

func (s *RuntimeHostServer) ListCapabilities(context.Context, *emptypb.Empty) (*proto.ListCapabilitiesResponse, error) {
	if s.CapabilityLister == nil {
		return &proto.ListCapabilitiesResponse{}, nil
	}
	caps, err := capabilitiesToProto(s.CapabilityLister.ListCapabilities())
	if err != nil {
		return nil, err
	}
	return &proto.ListCapabilitiesResponse{Capabilities: caps}, nil
}
