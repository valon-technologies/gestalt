package gestalt

// AgentCreateSession is the native input for creating an agent session
// through the host agent service. The workflow executor uses it as the
// fakeable agent-step contract.
type AgentCreateSession struct {
	ProviderName   string
	Model          string
	ClientRef      string
	Metadata       any
	IdempotencyKey string
	Workspace      *AgentWorkspace
	Tools          AgentToolConfig
}

// AgentCreateTurn is the native input for creating an agent turn through the
// host agent service.
type AgentCreateTurn struct {
	ProviderName   string
	SessionID      string
	Model          string
	Messages       []AgentMessage
	Output         *AgentOutput
	Metadata       any
	IdempotencyKey string
	ModelOptions   any
	TimeoutSeconds int32
}

// AgentGetTurn is the native input for fetching one agent turn through the
// host agent service.
type AgentGetTurn struct {
	ProviderName string
	TurnID       string
}

// AgentCancelTurn is the native input for canceling an agent turn through the
// host agent service.
type AgentCancelTurn struct {
	ProviderName string
	TurnID       string
	Reason       string
}
