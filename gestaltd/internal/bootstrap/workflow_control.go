package bootstrap

import (
	"context"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

// WorkflowControl exposes the small subset of workflow runtime behavior that
// gestaltd's user-facing HTTP routes need for workflow runs and events.
type WorkflowControl interface {
	ResolveProvider(ctx context.Context, name string) (providerName string, provider coreworkflow.Provider, err error)
	DefaultProviderName() string
}

var _ WorkflowControl = (*workflowRuntime)(nil)
