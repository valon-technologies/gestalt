package providerhost

import workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"

type WorkflowManagerServer = workflowservice.ManagerServer

func NewWorkflowManagerServer(pluginName string, manager workflowservice.ManagerService, tokens *InvocationTokenManager) *WorkflowManagerServer {
	return workflowservice.NewManagerServer(pluginName, manager, tokens)
}
