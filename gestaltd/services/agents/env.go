package agents

const (
	DefaultHostSocketEnv    = "GESTALT_AGENT_HOST_SOCKET"
	DefaultManagerSocketEnv = "GESTALT_AGENT_MANAGER_SOCKET"
)

func HostSocketTokenEnv() string {
	return DefaultHostSocketEnv + "_TOKEN"
}

func ManagerSocketTokenEnv() string {
	return DefaultManagerSocketEnv + "_TOKEN"
}
