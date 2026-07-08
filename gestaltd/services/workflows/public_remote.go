package workflows

import (
	"context"
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// publicRemoteWorkflow routes workflow provider calls to a remote gestaltd public Workflow API.
type publicRemoteWorkflow struct {
	name   string
	client proto.WorkflowClient
}

// NewPublicRemote constructs a gestaltd-to-gestaltd workflow provider without runtime lifecycle.
func NewPublicRemote(name string, client proto.WorkflowClient) coreworkflow.Provider {
	return &publicRemoteWorkflow{
		name:   strings.TrimSpace(name),
		client: client,
	}
}

func (r *publicRemoteWorkflow) ApplyDefinition(ctx context.Context, req *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setWorkflowProviderName(cloneWorkflowRequest(req, &proto.ApplyWorkflowProviderDefinitionRequest{}).(*proto.ApplyWorkflowProviderDefinitionRequest), r.name)
	return r.client.ApplyDefinition(ctx, providerReq)
}

func (r *publicRemoteWorkflow) GetDefinition(ctx context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneWorkflowRequest(req, &proto.GetWorkflowProviderDefinitionRequest{}).(*proto.GetWorkflowProviderDefinitionRequest)
	return r.client.GetDefinition(ctx, providerReq)
}

func (r *publicRemoteWorkflow) ListDefinitions(ctx context.Context, req *proto.ListWorkflowProviderDefinitionsRequest) (*proto.ListWorkflowProviderDefinitionsResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneWorkflowRequest(req, &proto.ListWorkflowProviderDefinitionsRequest{}).(*proto.ListWorkflowProviderDefinitionsRequest)
	return r.client.ListDefinitions(ctx, providerReq)
}

func (r *publicRemoteWorkflow) SetDefinitionPaused(ctx context.Context, req *proto.SetWorkflowProviderDefinitionPausedRequest) (*proto.WorkflowDefinition, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneWorkflowRequest(req, &proto.SetWorkflowProviderDefinitionPausedRequest{}).(*proto.SetWorkflowProviderDefinitionPausedRequest)
	return r.client.SetDefinitionPaused(ctx, providerReq)
}

func (r *publicRemoteWorkflow) SetActivationPaused(ctx context.Context, req *proto.SetWorkflowProviderActivationPausedRequest) (*proto.WorkflowDefinition, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneWorkflowRequest(req, &proto.SetWorkflowProviderActivationPausedRequest{}).(*proto.SetWorkflowProviderActivationPausedRequest)
	return r.client.SetActivationPaused(ctx, providerReq)
}

func (r *publicRemoteWorkflow) DeleteDefinition(ctx context.Context, req *proto.DeleteWorkflowProviderDefinitionRequest) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneWorkflowRequest(req, &proto.DeleteWorkflowProviderDefinitionRequest{}).(*proto.DeleteWorkflowProviderDefinitionRequest)
	_, err := r.client.DeleteDefinition(ctx, providerReq)
	return err
}

func (r *publicRemoteWorkflow) StartRun(ctx context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setWorkflowProviderName(cloneWorkflowRequest(req, &proto.StartWorkflowProviderRunRequest{}).(*proto.StartWorkflowProviderRunRequest), r.name)
	return r.client.StartRun(ctx, providerReq)
}

func (r *publicRemoteWorkflow) GetRun(ctx context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneWorkflowRequest(req, &proto.GetWorkflowProviderRunRequest{}).(*proto.GetWorkflowProviderRunRequest)
	return r.client.GetRun(ctx, providerReq)
}

func (r *publicRemoteWorkflow) ListRuns(ctx context.Context, req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneWorkflowRequest(req, &proto.ListWorkflowProviderRunsRequest{}).(*proto.ListWorkflowProviderRunsRequest)
	return r.client.ListRuns(ctx, providerReq)
}

func (r *publicRemoteWorkflow) GetRunEvents(ctx context.Context, req *proto.GetWorkflowProviderRunEventsRequest) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneWorkflowRequest(req, &proto.GetWorkflowProviderRunEventsRequest{}).(*proto.GetWorkflowProviderRunEventsRequest)
	return r.client.GetRunEvents(ctx, providerReq)
}

func (r *publicRemoteWorkflow) GetRunOutput(ctx context.Context, req *proto.GetWorkflowProviderRunOutputRequest) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneWorkflowRequest(req, &proto.GetWorkflowProviderRunOutputRequest{}).(*proto.GetWorkflowProviderRunOutputRequest)
	return r.client.GetRunOutput(ctx, providerReq)
}

func (r *publicRemoteWorkflow) CancelRun(ctx context.Context, req *proto.CancelWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneWorkflowRequest(req, &proto.CancelWorkflowProviderRunRequest{}).(*proto.CancelWorkflowProviderRunRequest)
	return r.client.CancelRun(ctx, providerReq)
}

func (r *publicRemoteWorkflow) SignalRun(ctx context.Context, req *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneWorkflowRequest(req, &proto.SignalWorkflowProviderRunRequest{}).(*proto.SignalWorkflowProviderRunRequest)
	return r.client.SignalRun(ctx, providerReq)
}

func (r *publicRemoteWorkflow) SignalOrStartRun(ctx context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setWorkflowProviderName(cloneWorkflowRequest(req, &proto.SignalOrStartWorkflowProviderRunRequest{}).(*proto.SignalOrStartWorkflowProviderRunRequest), r.name)
	return r.client.SignalOrStartRun(ctx, providerReq)
}

func (r *publicRemoteWorkflow) DeliverEvent(ctx context.Context, req *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setWorkflowProviderName(cloneWorkflowRequest(req, &proto.DeliverWorkflowProviderEventRequest{}).(*proto.DeliverWorkflowProviderEventRequest), r.name)
	return r.client.DeliverEvent(ctx, providerReq)
}

func (r *publicRemoteWorkflow) Ping(context.Context) error { return nil }

func (r *publicRemoteWorkflow) Close() error { return nil }

var _ coreworkflow.Provider = (*publicRemoteWorkflow)(nil)

func setWorkflowProviderName[T gproto.Message](req T, name string) T {
	name = strings.TrimSpace(name)
	if name == "" {
		return req
	}
	msg := req.ProtoReflect()
	field := msg.Descriptor().Fields().ByName("provider_name")
	if field == nil || field.Kind() != protoreflect.StringKind {
		return req
	}
	if strings.TrimSpace(msg.Get(field).String()) == "" {
		msg.Set(field, protoreflect.ValueOfString(name))
	}
	return req
}
