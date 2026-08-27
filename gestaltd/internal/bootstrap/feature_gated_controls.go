package bootstrap

import (
	"context"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/featureflags"
)

type featureGatedAgentControl struct {
	enabled bool
	next    AgentControl
}

func newFeatureGatedAgentControl(enabled bool, next AgentControl) AgentControl {
	return &featureGatedAgentControl{enabled: enabled, next: next}
}

func (c *featureGatedAgentControl) ResolveProvider(ctx context.Context, name string) (string, coreagent.Provider, error) {
	if c == nil || !c.enabled {
		return "", nil, featureflags.NewDisabledError(featureflags.Agent)
	}
	return c.next.ResolveProvider(ctx, name)
}

func (c *featureGatedAgentControl) ProviderNames() []string {
	if c == nil || !c.enabled {
		return nil
	}
	return c.next.ProviderNames()
}

func (c *featureGatedAgentControl) Ping(ctx context.Context) error {
	if c == nil || !c.enabled {
		return featureflags.NewDisabledError(featureflags.Agent)
	}
	return c.next.Ping(ctx)
}

type featureGatedWorkflowControl struct {
	enabled bool
	next    WorkflowControl
}

func newFeatureGatedWorkflowControl(enabled bool, next WorkflowControl) WorkflowControl {
	return &featureGatedWorkflowControl{enabled: enabled, next: next}
}

func (c *featureGatedWorkflowControl) ResolveProvider(ctx context.Context, name string) (string, coreworkflow.Provider, error) {
	if c == nil || !c.enabled {
		return "", nil, featureflags.NewDisabledError(featureflags.Workflow)
	}
	return c.next.ResolveProvider(ctx, name)
}

func (c *featureGatedWorkflowControl) DefaultProviderName() string {
	if c == nil || !c.enabled {
		return ""
	}
	return c.next.DefaultProviderName()
}

var _ AgentControl = (*featureGatedAgentControl)(nil)
var _ WorkflowControl = (*featureGatedWorkflowControl)(nil)
