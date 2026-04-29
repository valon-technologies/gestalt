package bootstrap

import coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"

// WorkflowControl exposes the small subset of workflow runtime behavior that
// gestaltd's workflow event and run routes need.
type WorkflowControl interface {
	ResolveProvider(name string) (coreworkflow.Provider, error)
	ResolveProviderSelection(name string) (providerName string, provider coreworkflow.Provider, err error)
	ProviderNames() []string
}

var _ WorkflowControl = (*workflowRuntime)(nil)
