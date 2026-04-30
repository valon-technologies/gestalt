package providerhost

import (
	"context"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
)

type WorkflowExecConfig = workflowservice.ExecConfig

func NewExecutableWorkflow(ctx context.Context, cfg WorkflowExecConfig) (coreworkflow.Provider, error) {
	return workflowservice.NewExecutable(ctx, cfg)
}
