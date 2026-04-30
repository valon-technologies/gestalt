package providerhost

import workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"

const (
	DefaultWorkflowHostSocketEnv    = workflowservice.DefaultHostSocketEnv
	DefaultWorkflowManagerSocketEnv = workflowservice.DefaultManagerSocketEnv
)

func WorkflowManagerSocketTokenEnv() string {
	return workflowservice.ManagerSocketTokenEnv()
}
