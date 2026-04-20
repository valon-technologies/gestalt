package bootstrap

import coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"

// WorkflowControl exposes the small subset of workflow runtime behavior that
// gestaltd's user-facing HTTP routes need for user-owned schedules.
type WorkflowControl interface {
	ResolveBinding(pluginName string) (providerName string, operations map[string]struct{}, err error)
	ResolveProvider(name string) (coreworkflow.Provider, error)
}

var _ WorkflowControl = (*workflowRuntime)(nil)
