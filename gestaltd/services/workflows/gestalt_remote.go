package workflows

import (
	"context"
	"fmt"
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type gestaltRemoteWorkflow struct {
	name   string
	client proto.WorkflowClient
}

// NewGestaltRemoteProvider routes workflow operations through a remote gestaltd public Workflow API.
func NewGestaltRemoteProvider(name string, client proto.WorkflowClient) (coreworkflow.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("workflow provider name is required")
	}
	if client == nil {
		return nil, fmt.Errorf("workflow provider client is required")
	}
	return &gestaltRemoteWorkflow{name: name, client: client}, nil
}

func (p *gestaltRemoteWorkflow) withProviderName(req gproto.Message) gproto.Message {
	if req == nil {
		return nil
	}
	cloned := gproto.Clone(req)
	msg := cloned.ProtoReflect()
	field := msg.Descriptor().Fields().ByName("provider_name")
	if field != nil && field.Kind() == protoreflect.StringKind {
		msg.Set(field, protoreflect.ValueOfString(p.name))
	}
	return cloned
}

func (p *gestaltRemoteWorkflow) call(ctx context.Context) (context.Context, context.CancelFunc) {
	return runtimehost.ProviderCallContext(ctx)
}

func (p *gestaltRemoteWorkflow) ApplyDefinition(ctx context.Context, req *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	ctx, cancel := p.call(ctx)
	defer cancel()
	return p.client.ApplyDefinition(ctx, p.withProviderName(req).(*proto.ApplyWorkflowProviderDefinitionRequest))
}

func (p *gestaltRemoteWorkflow) GetDefinition(ctx context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	ctx, cancel := p.call(ctx)
	defer cancel()
	return p.client.GetDefinition(ctx, req)
}

func (p *gestaltRemoteWorkflow) ListDefinitions(ctx context.Context, req *proto.ListWorkflowProviderDefinitionsRequest) (*proto.ListWorkflowProviderDefinitionsResponse, error) {
	ctx, cancel := p.call(ctx)
	defer cancel()
	return p.client.ListDefinitions(ctx, req)
}

func (p *gestaltRemoteWorkflow) SetDefinitionPaused(ctx context.Context, req *proto.SetWorkflowProviderDefinitionPausedRequest) (*proto.WorkflowDefinition, error) {
	ctx, cancel := p.call(ctx)
	defer cancel()
	return p.client.SetDefinitionPaused(ctx, req)
}

func (p *gestaltRemoteWorkflow) SetActivationPaused(ctx context.Context, req *proto.SetWorkflowProviderActivationPausedRequest) (*proto.WorkflowDefinition, error) {
	ctx, cancel := p.call(ctx)
	defer cancel()
	return p.client.SetActivationPaused(ctx, req)
}

func (p *gestaltRemoteWorkflow) DeleteDefinition(ctx context.Context, req *proto.DeleteWorkflowProviderDefinitionRequest) error {
	ctx, cancel := p.call(ctx)
	defer cancel()
	_, err := p.client.DeleteDefinition(ctx, req)
	return err
}

func (p *gestaltRemoteWorkflow) StartRun(ctx context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	ctx, cancel := p.call(ctx)
	defer cancel()
	return p.client.StartRun(ctx, p.withProviderName(req).(*proto.StartWorkflowProviderRunRequest))
}

func (p *gestaltRemoteWorkflow) GetRun(ctx context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	ctx, cancel := p.call(ctx)
	defer cancel()
	return p.client.GetRun(ctx, req)
}

func (p *gestaltRemoteWorkflow) ListRuns(ctx context.Context, req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	ctx, cancel := p.call(ctx)
	defer cancel()
	return p.client.ListRuns(ctx, req)
}

func (p *gestaltRemoteWorkflow) GetRunEvents(ctx context.Context, req *proto.GetWorkflowProviderRunEventsRequest) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	ctx, cancel := p.call(ctx)
	defer cancel()
	return p.client.GetRunEvents(ctx, req)
}

func (p *gestaltRemoteWorkflow) GetRunOutput(ctx context.Context, req *proto.GetWorkflowProviderRunOutputRequest) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	ctx, cancel := p.call(ctx)
	defer cancel()
	return p.client.GetRunOutput(ctx, req)
}

func (p *gestaltRemoteWorkflow) CancelRun(ctx context.Context, req *proto.CancelWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	ctx, cancel := p.call(ctx)
	defer cancel()
	return p.client.CancelRun(ctx, req)
}

func (p *gestaltRemoteWorkflow) SignalRun(ctx context.Context, req *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	ctx, cancel := p.call(ctx)
	defer cancel()
	return p.client.SignalRun(ctx, req)
}

func (p *gestaltRemoteWorkflow) SignalOrStartRun(ctx context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	ctx, cancel := p.call(ctx)
	defer cancel()
	return p.client.SignalOrStartRun(ctx, p.withProviderName(req).(*proto.SignalOrStartWorkflowProviderRunRequest))
}

func (p *gestaltRemoteWorkflow) DeliverEvent(ctx context.Context, req *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	ctx, cancel := p.call(ctx)
	defer cancel()
	return p.client.DeliverEvent(ctx, p.withProviderName(req).(*proto.DeliverWorkflowProviderEventRequest))
}

func (p *gestaltRemoteWorkflow) Ping(ctx context.Context) error {
	ctx, cancel := p.call(ctx)
	defer cancel()
	_, err := p.client.ListDefinitions(ctx, &proto.ListWorkflowProviderDefinitionsRequest{})
	if err != nil {
		return err
	}
	return nil
}

func (p *gestaltRemoteWorkflow) Close() error { return nil }

var _ coreworkflow.Provider = (*gestaltRemoteWorkflow)(nil)
