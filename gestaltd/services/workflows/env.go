package workflows

const (
	DefaultHostSocketEnv    = "GESTALT_WORKFLOW_HOST_SOCKET"
	DefaultManagerSocketEnv = "GESTALT_WORKFLOW_MANAGER_SOCKET"
)

func HostSocketTokenEnv() string {
	return DefaultHostSocketEnv + "_TOKEN"
}

func ManagerSocketTokenEnv() string {
	return DefaultManagerSocketEnv + "_TOKEN"
}
