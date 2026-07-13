package gestalt

// WorkflowApplyDefinition carries the inputs of the call that applies a workflow definition for a provider.
type WorkflowApplyDefinition struct {
	Spec           *WorkflowDefinitionSpec
	IdempotencyKey string
}

// WorkflowGetDefinition carries the inputs of the call that fetches one workflow definition.
type WorkflowGetDefinition struct {
	DefinitionID string
}

// WorkflowListDefinitions carries the inputs of the call that lists workflow definitions.
type WorkflowListDefinitions struct{}

// WorkflowSetDefinitionPaused carries the inputs of the call that pauses or resumes a workflow definition.
type WorkflowSetDefinitionPaused struct {
	DefinitionID string
	Paused       bool
}

// WorkflowSetActivationPaused carries the inputs of the call that pauses or resumes one activation.
type WorkflowSetActivationPaused struct {
	DefinitionID string
	ActivationID string
	Paused       bool
}

// WorkflowDeleteDefinition carries the inputs of the call that deletes a workflow definition.
type WorkflowDeleteDefinition struct {
	DefinitionID string
}

// WorkflowStartRun carries the inputs of the call that starts a workflow run.
type WorkflowStartRun struct {
	DefinitionID                 string
	ExpectedDefinitionGeneration int64
	Input                        map[string]any
	IdempotencyKey               string
	WorkflowKey                  string
}

// WorkflowSignalRun carries the inputs of the call that signals an existing workflow run.
type WorkflowSignalRun struct {
	RunID  string
	Signal *WorkflowSignal
}

// WorkflowSignalOrStartRun carries the inputs of the call that signals a run, starting it first when absent.
type WorkflowSignalOrStartRun struct {
	WorkflowKey                  string
	DefinitionID                 string
	ExpectedDefinitionGeneration int64
	Input                        map[string]any
	IdempotencyKey               string
	Signal                       *WorkflowSignal
}

// WorkflowGetRun carries the inputs of the call that fetches one workflow run.
type WorkflowGetRun struct {
	RunID string
}

// WorkflowGetRunEvents carries the inputs of the call that fetches the events of a workflow run.
type WorkflowGetRunEvents struct {
	RunID string
}

// WorkflowGetRunOutput carries the inputs of the call that fetches the output value of a workflow run.
type WorkflowGetRunOutput struct {
	RunID string
}

// WorkflowDeliverEvent carries the inputs of the call that delivers an external event to the workflow engine.
type WorkflowDeliverEvent struct {
	Event *WorkflowEvent
}

// WorkflowRunSignal is one signal delivered to a workflow run.
type WorkflowRunSignal struct {
	Run         *WorkflowRun
	Signal      *WorkflowSignal
	StartedRun  bool
	WorkflowKey string
}
