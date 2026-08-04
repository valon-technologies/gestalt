package server_test

import (
	"context"
	"maps"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	return maps.Clone(src)
}

type stubWorkflowControl struct {
	defaultProviderName string
	provider            coreworkflow.Provider
	providers           map[string]coreworkflow.Provider
}

func (c *stubWorkflowControl) ResolveProvider(_ context.Context, name string) (string, coreworkflow.Provider, error) {
	providerName := strings.TrimSpace(name)
	if providerName == "" {
		providerName = strings.TrimSpace(c.defaultProviderName)
	}
	if providerName == "" {
		providerName = "default"
	}
	if c.providers != nil {
		if provider := c.providers[providerName]; provider != nil {
			return providerName, provider, nil
		}
	}
	if c.provider != nil {
		return providerName, c.provider, nil
	}
	return "", nil, core.ErrNotFound
}

func (c *stubWorkflowControl) DefaultProviderName() string {
	return strings.TrimSpace(c.defaultProviderName)
}

func (c *stubWorkflowControl) ProviderNames() []string {
	if c.providers != nil {
		out := make([]string, 0, len(c.providers))
		for name := range c.providers {
			out = append(out, name)
		}
		return out
	}
	if c.provider == nil {
		return nil
	}
	name := strings.TrimSpace(c.defaultProviderName)
	if name == "" {
		name = "default"
	}
	return []string{name}
}

type memoryWorkflowProvider struct {
	mu               sync.Mutex
	runs             map[string]*coreworkflow.Run
	listRunReqs      []coreworkflow.ListRunsRequest
	listRunsNextPage string
	cancelReqs       []*proto.CancelWorkflowProviderRunRequest
	deliveredEvents  []*proto.DeliverWorkflowProviderEventRequest
}

func newMemoryWorkflowProvider() *memoryWorkflowProvider {
	return &memoryWorkflowProvider{
		runs: map[string]*coreworkflow.Run{},
	}
}

func (p *memoryWorkflowProvider) ApplyDefinition(context.Context, *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	return nil, core.ErrNotFound
}

func (p *memoryWorkflowProvider) GetDefinition(context.Context, *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	return nil, core.ErrNotFound
}

func (p *memoryWorkflowProvider) ListDefinitions(context.Context, *proto.ListWorkflowProviderDefinitionsRequest) (*proto.ListWorkflowProviderDefinitionsResponse, error) {
	return &proto.ListWorkflowProviderDefinitionsResponse{}, nil
}

func (p *memoryWorkflowProvider) SetDefinitionPaused(context.Context, *proto.SetWorkflowProviderDefinitionPausedRequest) (*proto.WorkflowDefinition, error) {
	return nil, core.ErrNotFound
}

func (p *memoryWorkflowProvider) SetActivationPaused(context.Context, *proto.SetWorkflowProviderActivationPausedRequest) (*proto.WorkflowDefinition, error) {
	return nil, core.ErrNotFound
}

func (p *memoryWorkflowProvider) DeleteDefinition(context.Context, *proto.DeleteWorkflowProviderDefinitionRequest) error {
	return core.ErrNotFound
}

func (p *memoryWorkflowProvider) StartRun(context.Context, *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	return nil, core.ErrNotFound
}

func (p *memoryWorkflowProvider) GetRun(_ context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	run := p.runs[strings.TrimSpace(req.GetRunId())]
	if run == nil {
		return nil, core.ErrNotFound
	}
	return workflowwire.RunToProto(cloneWorkflowRun(run))
}

func (p *memoryWorkflowProvider) ListRuns(_ context.Context, req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	status, err := workflowwire.RunStatusFromProto(req.GetStatus())
	if err != nil {
		return nil, err
	}
	p.listRunReqs = append(p.listRunReqs, coreworkflow.ListRunsRequest{
		PageSize:  int(req.GetPageSize()),
		PageToken: strings.TrimSpace(req.GetPageToken()),
		TargetApp: strings.TrimSpace(req.GetTargetApp()),
		Status:    status,
	})
	resp := &proto.ListWorkflowProviderRunsResponse{NextPageToken: p.listRunsNextPage}
	for _, run := range p.runs {
		pb, err := workflowwire.RunToProto(cloneWorkflowRun(run))
		if err != nil {
			return nil, err
		}
		resp.Runs = append(resp.Runs, pb)
	}
	return resp, nil
}

func (p *memoryWorkflowProvider) GetRunEvents(context.Context, *proto.GetWorkflowProviderRunEventsRequest) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	return &proto.GetWorkflowProviderRunEventsResponse{}, nil
}

func (p *memoryWorkflowProvider) GetRunOutput(context.Context, *proto.GetWorkflowProviderRunOutputRequest) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	return &proto.GetWorkflowProviderRunOutputResponse{}, nil
}

func (p *memoryWorkflowProvider) CancelRun(_ context.Context, req *proto.CancelWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	run := p.runs[strings.TrimSpace(req.GetRunId())]
	if run == nil {
		return nil, core.ErrNotFound
	}
	p.cancelReqs = append(p.cancelReqs, &proto.CancelWorkflowProviderRunRequest{
		RunId:  strings.TrimSpace(req.GetRunId()),
		Reason: strings.TrimSpace(req.GetReason()),
	})
	run.Status = coreworkflow.RunStatusCanceled
	run.StatusMessage = strings.TrimSpace(req.GetReason())
	return workflowwire.RunToProto(cloneWorkflowRun(run))
}

func (p *memoryWorkflowProvider) SignalRun(context.Context, *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	return nil, core.ErrNotFound
}

func (p *memoryWorkflowProvider) SignalOrStartRun(context.Context, *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	return nil, core.ErrNotFound
}

func (p *memoryWorkflowProvider) DeliverEvent(_ context.Context, req *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deliveredEvents = append(p.deliveredEvents, &proto.DeliverWorkflowProviderEventRequest{
		Event:   req.GetEvent(),
		Context: req.GetContext(),
	})
	return req.GetEvent(), nil
}

func (p *memoryWorkflowProvider) Ping(context.Context) error { return nil }
func (p *memoryWorkflowProvider) Close() error               { return nil }

func cloneWorkflowRun(src *coreworkflow.Run) *coreworkflow.Run {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Target = cloneWorkflowTarget(src.Target)
	dst.Input = cloneMap(src.Input)
	return &dst
}

func cloneWorkflowTarget(src coreworkflow.Target) coreworkflow.Target {
	dst := coreworkflow.Target{Steps: make([]coreworkflow.Step, len(src.Steps))}
	for i := range src.Steps {
		step := src.Steps[i]
		if step.Inputs != nil {
			step.Inputs = make(map[string]coreworkflow.Value, len(step.Inputs))
			for key, value := range step.Inputs {
				step.Inputs[key] = coreworkflow.CloneValue(value)
			}
		}
		if step.App != nil {
			app := *step.App
			app.Input = coreworkflow.CloneValue(app.Input)
			step.App = &app
		}
		if step.Metadata != nil {
			step.Metadata = cloneMap(step.Metadata)
		}
		dst.Steps[i] = step
	}
	return dst
}
