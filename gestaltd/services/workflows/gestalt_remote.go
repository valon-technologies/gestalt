package workflows

import (
	"context"
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type gestaltRemoteWorkflow struct {
	client proto.WorkflowClient
	name   string
}

// NewGestaltRemoteProvider routes workflow provider calls through a remote gestaltd public Workflow API.
func NewGestaltRemoteProvider(client proto.WorkflowClient, name string) coreworkflow.Provider {
	name = strings.TrimSpace(name)
	if client == nil || name == "" {
		return nil
	}
	return &gestaltRemoteWorkflow{client: client, name: name}
}

func (r *gestaltRemoteWorkflow) ApplyDefinition(ctx context.Context, req *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	providerReq := cloneGestaltWorkflowReq(req, &proto.ApplyWorkflowProviderDefinitionRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.ApplyDefinition(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return resp, nil
}

func (r *gestaltRemoteWorkflow) GetDefinition(ctx context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	providerReq := cloneGestaltWorkflowReq(req, &proto.GetWorkflowProviderDefinitionRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetDefinition(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return resp, nil
}

func (r *gestaltRemoteWorkflow) ListDefinitions(ctx context.Context, req *proto.ListWorkflowProviderDefinitionsRequest) (*proto.ListWorkflowProviderDefinitionsResponse, error) {
	providerReq := cloneGestaltWorkflowReq(req, &proto.ListWorkflowProviderDefinitionsRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.ListDefinitions(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return resp, nil
}

func (r *gestaltRemoteWorkflow) SetDefinitionPaused(ctx context.Context, req *proto.SetWorkflowProviderDefinitionPausedRequest) (*proto.WorkflowDefinition, error) {
	providerReq := cloneGestaltWorkflowReq(req, &proto.SetWorkflowProviderDefinitionPausedRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.SetDefinitionPaused(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return resp, nil
}

func (r *gestaltRemoteWorkflow) SetActivationPaused(ctx context.Context, req *proto.SetWorkflowProviderActivationPausedRequest) (*proto.WorkflowDefinition, error) {
	providerReq := cloneGestaltWorkflowReq(req, &proto.SetWorkflowProviderActivationPausedRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.SetActivationPaused(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return resp, nil
}

func (r *gestaltRemoteWorkflow) DeleteDefinition(ctx context.Context, req *proto.DeleteWorkflowProviderDefinitionRequest) error {
	providerReq := cloneGestaltWorkflowReq(req, &proto.DeleteWorkflowProviderDefinitionRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	_, err := r.client.DeleteDefinition(ctx, providerReq)
	return remote.StatusError(err)
}

func (r *gestaltRemoteWorkflow) StartRun(ctx context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	providerReq := cloneGestaltWorkflowReq(req, &proto.StartWorkflowProviderRunRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.StartRun(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return resp, nil
}

func (r *gestaltRemoteWorkflow) GetRun(ctx context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	providerReq := cloneGestaltWorkflowReq(req, &proto.GetWorkflowProviderRunRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetRun(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return resp, nil
}

func (r *gestaltRemoteWorkflow) ListRuns(ctx context.Context, req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	providerReq := cloneGestaltWorkflowReq(req, &proto.ListWorkflowProviderRunsRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.ListRuns(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return resp, nil
}

func (r *gestaltRemoteWorkflow) GetRunEvents(ctx context.Context, req *proto.GetWorkflowProviderRunEventsRequest) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	providerReq := cloneGestaltWorkflowReq(req, &proto.GetWorkflowProviderRunEventsRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetRunEvents(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return resp, nil
}

func (r *gestaltRemoteWorkflow) GetRunOutput(ctx context.Context, req *proto.GetWorkflowProviderRunOutputRequest) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	providerReq := cloneGestaltWorkflowReq(req, &proto.GetWorkflowProviderRunOutputRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetRunOutput(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return resp, nil
}

func (r *gestaltRemoteWorkflow) CancelRun(ctx context.Context, req *proto.CancelWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	providerReq := cloneGestaltWorkflowReq(req, &proto.CancelWorkflowProviderRunRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.CancelRun(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return resp, nil
}

func (r *gestaltRemoteWorkflow) SignalRun(ctx context.Context, req *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	providerReq := cloneGestaltWorkflowReq(req, &proto.SignalWorkflowProviderRunRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.SignalRun(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return resp, nil
}

func (r *gestaltRemoteWorkflow) SignalOrStartRun(ctx context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	providerReq := cloneGestaltWorkflowReq(req, &proto.SignalOrStartWorkflowProviderRunRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.SignalOrStartRun(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return resp, nil
}

func (r *gestaltRemoteWorkflow) DeliverEvent(ctx context.Context, req *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	providerReq := cloneGestaltWorkflowReq(req, &proto.DeliverWorkflowProviderEventRequest{}, r.name)
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.DeliverEvent(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return resp, nil
}

func (r *gestaltRemoteWorkflow) Ping(ctx context.Context) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	_, err := r.client.ListDefinitions(ctx, &proto.ListWorkflowProviderDefinitionsRequest{})
	return remote.StatusError(err)
}

func (r *gestaltRemoteWorkflow) Close() error { return nil }

func cloneGestaltWorkflowReq[T gproto.Message](req T, empty T, providerName string) T {
	cloned := cloneWorkflowRequest(req, empty).(T)
	setWorkflowProviderName(cloned, providerName)
	return cloned
}

func setWorkflowProviderName(req gproto.Message, providerName string) {
	if req == nil {
		return
	}
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return
	}
	msg := req.ProtoReflect()
	field := msg.Descriptor().Fields().ByName("provider_name")
	if field == nil || field.Kind() != protoreflect.StringKind {
		return
	}
	if strings.TrimSpace(msg.Get(field).String()) != "" {
		return
	}
	msg.Set(field, protoreflect.ValueOfString(providerName))
}

var _ coreworkflow.Provider = (*gestaltRemoteWorkflow)(nil)
