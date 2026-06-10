package gestalt

type WorkflowApplyDefinition struct {
	ProviderName   string
	Spec           *WorkflowDefinitionSpec
	IdempotencyKey string
}

type WorkflowGetDefinition struct {
	DefinitionID string
}

type WorkflowListDefinitions struct{}

type WorkflowSetDefinitionPaused struct {
	DefinitionID string
	Paused       bool
}

type WorkflowSetActivationPaused struct {
	DefinitionID string
	ActivationID string
	Paused       bool
}

type WorkflowDeleteDefinition struct {
	DefinitionID string
}

type WorkflowStartRun struct {
	ProviderName                 string
	DefinitionID                 string
	ExpectedDefinitionGeneration int64
	Input                        map[string]any
	IdempotencyKey               string
	WorkflowKey                  string
	RunAs                        *Subject
}

type WorkflowSignalRun struct {
	RunID  string
	Signal *WorkflowSignal
}

type WorkflowSignalOrStartRun struct {
	ProviderName                 string
	WorkflowKey                  string
	DefinitionID                 string
	ExpectedDefinitionGeneration int64
	Input                        map[string]any
	IdempotencyKey               string
	Signal                       *WorkflowSignal
	RunAs                        *Subject
}

type WorkflowGetRun struct {
	RunID string
}

type WorkflowGetRunEvents struct {
	RunID string
}

type WorkflowGetRunOutput struct {
	RunID string
}

type WorkflowDeliverEvent struct {
	ProviderName string
	Event        *WorkflowEvent
}

type WorkflowRunSignal struct {
	Run         *WorkflowRun
	Signal      *WorkflowSignal
	StartedRun  bool
	WorkflowKey string
}
