package workflowmanager

import (
	"context"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/featureflags"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type featureGate struct {
	enabled bool
	next    Service
}

func NewFeatureGate(enabled bool, next Service) Service {
	return &featureGate{enabled: enabled, next: next}
}

func (g *featureGate) disabled() error {
	if g != nil && g.enabled {
		return nil
	}
	return featureflags.NewDisabledError(featureflags.Workflow)
}

func (g *featureGate) ApplyDefinition(ctx context.Context, p *principal.Principal, req DefinitionApply) (*ManagedDefinition, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.ApplyDefinition(ctx, p, req)
}

func (g *featureGate) GetDefinition(ctx context.Context, p *principal.Principal, providerName, definitionID string) (*ManagedDefinition, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.GetDefinition(ctx, p, providerName, definitionID)
}

func (g *featureGate) ListDefinitions(ctx context.Context, p *principal.Principal, providerName string) (*ListDefinitionsResponse, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.ListDefinitions(ctx, p, providerName)
}

func (g *featureGate) SetDefinitionPaused(ctx context.Context, p *principal.Principal, providerName, definitionID string, paused bool) (*ManagedDefinition, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.SetDefinitionPaused(ctx, p, providerName, definitionID, paused)
}

func (g *featureGate) SetActivationPaused(ctx context.Context, p *principal.Principal, providerName, definitionID, activationID string, paused bool) (*ManagedDefinition, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.SetActivationPaused(ctx, p, providerName, definitionID, activationID, paused)
}

func (g *featureGate) DeleteDefinition(ctx context.Context, p *principal.Principal, providerName, definitionID string) error {
	if err := g.disabled(); err != nil {
		return err
	}
	return g.next.DeleteDefinition(ctx, p, providerName, definitionID)
}

func (g *featureGate) ListRuns(ctx context.Context, p *principal.Principal, providerName string, req coreworkflow.ListRunsRequest) (*ListRunsResponse, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.ListRuns(ctx, p, providerName, req)
}

func (g *featureGate) StartRun(ctx context.Context, p *principal.Principal, req RunStart) (*ManagedRun, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.StartRun(ctx, p, req)
}

func (g *featureGate) GetRun(ctx context.Context, p *principal.Principal, providerName, runID string) (*ManagedRun, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.GetRun(ctx, p, providerName, runID)
}

func (g *featureGate) GetRunEvents(ctx context.Context, p *principal.Principal, providerName, runID string) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.GetRunEvents(ctx, p, providerName, runID)
}

func (g *featureGate) GetRunOutput(ctx context.Context, p *principal.Principal, providerName, runID string) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.GetRunOutput(ctx, p, providerName, runID)
}

func (g *featureGate) CancelRun(ctx context.Context, p *principal.Principal, providerName, runID, reason string) (*ManagedRun, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.CancelRun(ctx, p, providerName, runID, reason)
}

func (g *featureGate) SignalRun(ctx context.Context, p *principal.Principal, req RunSignal) (*ManagedRunSignal, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.SignalRun(ctx, p, req)
}

func (g *featureGate) SignalOrStartRun(ctx context.Context, p *principal.Principal, req RunSignalOrStart) (*ManagedRunSignal, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.SignalOrStartRun(ctx, p, req)
}

func (g *featureGate) DeliverEvent(ctx context.Context, p *principal.Principal, req EventDeliver) (coreworkflow.Event, error) {
	if err := g.disabled(); err != nil {
		return coreworkflow.Event{}, err
	}
	return g.next.DeliverEvent(ctx, p, req)
}

var _ Service = (*featureGate)(nil)
