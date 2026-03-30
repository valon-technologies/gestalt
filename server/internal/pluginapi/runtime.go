package pluginapi

import (
	"context"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"google.golang.org/protobuf/types/known/emptypb"
)

type RemoteRuntime struct {
	name   string
	client pluginapiv1.RuntimePluginClient
	start  *pluginapiv1.StartRuntimeRequest
}

func NewRemoteRuntime(name string, client pluginapiv1.RuntimePluginClient, config map[string]any, initialCapabilities []core.Capability) (*RemoteRuntime, error) {
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
		start: &pluginapiv1.StartRuntimeRequest{
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
	pluginapiv1.UnimplementedRuntimeHostServer
	Invoker          invocation.Invoker
	CapabilityLister invocation.CapabilityLister
}

func NewRuntimeHostServer(invoker invocation.Invoker, lister invocation.CapabilityLister) *RuntimeHostServer {
	return &RuntimeHostServer{
		Invoker:          invoker,
		CapabilityLister: lister,
	}
}

func (s *RuntimeHostServer) Invoke(ctx context.Context, req *pluginapiv1.InvokeRequest) (*pluginapiv1.OperationResult, error) {
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
	return &pluginapiv1.OperationResult{
		Status: int32(result.Status),
		Body:   result.Body,
	}, nil
}

func (s *RuntimeHostServer) ListCapabilities(context.Context, *emptypb.Empty) (*pluginapiv1.ListCapabilitiesResponse, error) {
	if s.CapabilityLister == nil {
		return &pluginapiv1.ListCapabilitiesResponse{}, nil
	}
	caps, err := capabilitiesToProto(s.CapabilityLister.ListCapabilities())
	if err != nil {
		return nil, err
	}
	return &pluginapiv1.ListCapabilitiesResponse{Capabilities: caps}, nil
}
