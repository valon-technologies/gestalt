// Package workflows exposes workflow provider transport primitives.
package workflows

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/workflowmanager"
)

const DefaultHostSocketEnv = providerhost.DefaultWorkflowHostSocketEnv
const DefaultManagerSocketEnv = providerhost.DefaultWorkflowManagerSocketEnv

type ExecConfig = providerhost.WorkflowExecConfig
type InvocationTokenManager = providerhost.InvocationTokenManager
type ManagerService = workflowmanager.Service

func HostSocketTokenEnv() string {
	return DefaultHostSocketEnv + "_TOKEN"
}

func ManagerSocketTokenEnv() string {
	return providerhost.WorkflowManagerSocketTokenEnv()
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (coreworkflow.Provider, error) {
	return providerhost.NewExecutableWorkflow(ctx, cfg)
}

func NewHostServer(
	providerName string,
	invoke func(context.Context, coreworkflow.InvokeOperationRequest) (*coreworkflow.InvokeOperationResponse, error),
) proto.WorkflowHostServer {
	return providerhost.NewWorkflowHostServer(providerName, invoke)
}

func NewManagerServer(pluginName string, manager ManagerService, tokens *InvocationTokenManager) proto.WorkflowManagerHostServer {
	return providerhost.NewWorkflowManagerServer(pluginName, manager, tokens)
}
