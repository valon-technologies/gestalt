package providerhost

import workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"

type workflowInvokeFunc = workflowservice.InvokeFunc
type WorkflowHostServer = workflowservice.HostServer

func NewWorkflowHostServer(providerName string, invoke workflowInvokeFunc) *WorkflowHostServer {
	return workflowservice.NewHostServer(providerName, invoke)
}
