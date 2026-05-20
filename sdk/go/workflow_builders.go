package gestalt

import (
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// WorkflowActivationMode identifies how an activation starts or signals runs.
type WorkflowActivationMode int32

const (
	WorkflowActivationModeValueUnspecified   WorkflowActivationMode = 0
	WorkflowActivationModeValueStart         WorkflowActivationMode = 1
	WorkflowActivationModeValueSignal        WorkflowActivationMode = 2
	WorkflowActivationModeValueSignalOrStart WorkflowActivationMode = 3
)

// WorkflowActionKind identifies one action in a provider workflow plan.
type WorkflowActionKind int32

const (
	WorkflowActionKindValueUnspecified WorkflowActionKind = 0
	WorkflowActionKindValuePlugin      WorkflowActionKind = 1
	WorkflowActionKindValueAgentTurn   WorkflowActionKind = 2
	WorkflowActionKindValueDelivery    WorkflowActionKind = 3
)

// WorkflowDeploymentStatus identifies the lifecycle state of a deployment.
type WorkflowDeploymentStatus int32

const (
	WorkflowDeploymentStatusValueUnspecified WorkflowDeploymentStatus = 0
	WorkflowDeploymentStatusValuePending     WorkflowDeploymentStatus = 1
	WorkflowDeploymentStatusValueActive      WorkflowDeploymentStatus = 2
	WorkflowDeploymentStatusValuePaused      WorkflowDeploymentStatus = 3
	WorkflowDeploymentStatusValueDeleted     WorkflowDeploymentStatus = 4
	WorkflowDeploymentStatusValueFailed      WorkflowDeploymentStatus = 5
)

// WorkflowRunStatus identifies the lifecycle state of a workflow run.
type WorkflowRunStatus int32

// Workflow run status value constants provide stable SDK names for common
// generated enum values without colliding with workflow telemetry dimensions.
const (
	WorkflowRunStatusValueUnspecified WorkflowRunStatus = 0
	WorkflowRunStatusValuePending     WorkflowRunStatus = 1
	WorkflowRunStatusValueRunning     WorkflowRunStatus = 2
	WorkflowRunStatusValueSucceeded   WorkflowRunStatus = 3
	WorkflowRunStatusValueFailed      WorkflowRunStatus = 4
	WorkflowRunStatusValueCanceled    WorkflowRunStatus = 5
)

// WorkflowStepStatus identifies the lifecycle state of one workflow step.
type WorkflowStepStatus int32

// Workflow step status value constants provide stable SDK names for provider
// step projections.
const (
	WorkflowStepStatusValueUnspecified WorkflowStepStatus = 0
	WorkflowStepStatusValuePending     WorkflowStepStatus = 1
	WorkflowStepStatusValueRunning     WorkflowStepStatus = 2
	WorkflowStepStatusValueSucceeded   WorkflowStepStatus = 3
	WorkflowStepStatusValueFailed      WorkflowStepStatus = 4
	WorkflowStepStatusValueSkipped     WorkflowStepStatus = 5
	WorkflowStepStatusValueCanceled    WorkflowStepStatus = 6
)

// WorkflowRunEventType identifies a provider run event.
type WorkflowRunEventType int32

const (
	WorkflowRunEventTypeValueUnspecified     WorkflowRunEventType = 0
	WorkflowRunEventTypeValueRunStarted      WorkflowRunEventType = 1
	WorkflowRunEventTypeValueRunCompleted    WorkflowRunEventType = 2
	WorkflowRunEventTypeValueRunFailed       WorkflowRunEventType = 3
	WorkflowRunEventTypeValueRunCanceled     WorkflowRunEventType = 4
	WorkflowRunEventTypeValueSignalReceived  WorkflowRunEventType = 5
	WorkflowRunEventTypeValueStepStarted     WorkflowRunEventType = 6
	WorkflowRunEventTypeValueStepSucceeded   WorkflowRunEventType = 7
	WorkflowRunEventTypeValueStepFailed      WorkflowRunEventType = 8
	WorkflowRunEventTypeValueStepSkipped     WorkflowRunEventType = 9
	WorkflowRunEventTypeValueActionInvoked   WorkflowRunEventType = 10
	WorkflowRunEventTypeValueActionCompleted WorkflowRunEventType = 11
	WorkflowRunEventTypeValueActionFailed    WorkflowRunEventType = 12
)

// BoundWorkflowTarget contains the provider-owned workflow step plan.
type BoundWorkflowTarget struct {
	Steps []WorkflowStep
}

// WorkflowStep contains fields for constructing one workflow step.
// Exactly one of Plugin or Agent should be set.
type WorkflowStep struct {
	ID             string
	Inputs         map[string]WorkflowValue
	Plugin         *WorkflowStepPluginCall
	Agent          *WorkflowStepAgentTurn
	When           *WorkflowStepWhen
	TimeoutSeconds int32
	OutputDelivery *WorkflowStepDelivery
	Metadata       any
}

// WorkflowStepPluginCall contains fields for a plugin step action or output
// delivery. Input is a WorkflowValue that must resolve to a JSON object.
type WorkflowStepPluginCall struct {
	Name           string
	Operation      string
	Input          WorkflowValue
	Connection     string
	Instance       string
	CredentialMode string
}

// WorkflowStepDelivery contains fields for step output delivery.
type WorkflowStepDelivery struct {
	Plugin *WorkflowStepPluginCall
}

// WorkflowStepAgentTurn contains fields for an agent action inside a workflow
// step.
type WorkflowStepAgentTurn struct {
	Provider       string
	Model          string
	SessionKey     WorkflowText
	Prompt         WorkflowText
	Messages       []WorkflowAgentMessage
	Tools          []AgentToolRef
	ResponseSchema any
	ModelOptions   any
}

// WorkflowAgentMessage contains one rendered agent message.
type WorkflowAgentMessage struct {
	Role     string
	Text     WorkflowText
	Metadata any
}

// WorkflowText is text rendered by the workflow template engine.
type WorkflowText struct {
	Template string
}

// WorkflowStepWhen contains a scalar equality guard for a step.
type WorkflowStepWhen struct {
	Value  WorkflowValue
	Equals any
}

// WorkflowValue contains one workflow value expression. LiteralSet
// distinguishes an explicit literal null from an unset value.
type WorkflowValue struct {
	Literal         any
	LiteralSet      bool
	Object          map[string]WorkflowValue
	Array           []WorkflowValue
	Template        *WorkflowText
	RunInput      string
	SignalPayload string
	StepOutput    *WorkflowStepOutputSource
}

// WorkflowStepOutputSource references a previous step output envelope.
type WorkflowStepOutputSource struct {
	StepID string
	Path   string
}

// WorkflowActor contains workflow actor metadata.
type WorkflowActor struct {
	SubjectID   string
	SubjectKind string
	DisplayName string
	AuthSource  string
}

// WorkflowRunAsSubject contains workflow run-as metadata.
type WorkflowRunAsSubject struct {
	SubjectID           string
	SubjectKind         string
	DisplayName         string
	AuthSource          string
	CredentialSubjectID string
}

// WorkflowEvent contains a CloudEvents-like workflow event.
type WorkflowEvent struct {
	ID              string
	Source          string
	SpecVersion     string
	Type            string
	Subject         string
	Time            time.Time
	DataContentType string
	Data            any
	Extensions      map[string]any
}

// WorkflowEventMatch contains fields for matching workflow events.
type WorkflowEventMatch struct {
	Type    string
	Source  string
	Subject string
}

// WorkflowScheduleActivation contains cron activation settings.
type WorkflowScheduleActivation struct {
	Cron     string
	Timezone string
}

// WorkflowEventActivation contains event activation settings.
type WorkflowEventActivation struct {
	Match *WorkflowEventMatch
}

// WorkflowActivation contains one deployment activation.
// Exactly one of Manual, Schedule, or Event should be set.
type WorkflowActivation struct {
	ID             string
	Paused         bool
	Mode           WorkflowActivationMode
	Input          *WorkflowValue
	RunKey         *WorkflowValue
	IdempotencyKey *WorkflowValue
	Manual         bool
	Schedule       *WorkflowScheduleActivation
	Event          *WorkflowEventActivation
}

// WorkflowAccessPermission contains fields for a workflow access permission.
type WorkflowAccessPermission struct {
	Plugin     string
	Operations []string
	Actions    []string
}

// WorkflowExecutionReference is provider-owned authority data used by workflow
// host callbacks.
type WorkflowExecutionReference struct {
	ID                  string
	ProviderName        string
	Target              *BoundWorkflowTarget
	CallerPluginName    string
	SourceDefinitionID  string
	SubjectID           string
	SubjectKind         string
	DisplayName         string
	AuthSource          string
	CredentialSubjectID string
	RunAs               *WorkflowRunAsSubject
	Permissions         []WorkflowAccessPermission
	CreatedAt           *time.Time
	RevokedAt           *time.Time
	TargetDigest        string
	ProviderPlanDigest  string
	PermissionsDigest   string
	SemanticsVersion    string
	Generation          int64
	Seal                string
}

// WorkflowDeploymentSpec contains a workflow deployment declaration.
type WorkflowDeploymentSpec struct {
	ID                       string
	Generation               int64
	Target                   *BoundWorkflowTarget
	Activations              []WorkflowActivation
	Paused                   bool
	RunAs                    *WorkflowRunAsSubject
	Permissions              []WorkflowAccessPermission
	Labels                   map[string]string
	WorkflowSemanticsVersion string
}

// WorkflowActionDescriptor describes one action in a provider workflow plan.
type WorkflowActionDescriptor struct {
	ActionID string
	StepID   string
	Kind     WorkflowActionKind
	Plugin   *WorkflowStepPluginCall
	Agent    *WorkflowStepAgentTurn
}

// WorkflowActionTable contains provider action descriptors and a digest.
type WorkflowActionTable struct {
	Actions []WorkflowActionDescriptor
	Digest  string
}

// WorkflowUnsupportedFeature reports a plan-time provider capability gap.
type WorkflowUnsupportedFeature struct {
	Feature string
	Reason  string
}

// PlanWorkflowResponse returns provider-local plan identity for a deployment.
type PlanWorkflowResponse struct {
	AcceptedSpecDigest        string
	ProviderPlanID            string
	ProviderPlanDigest        string
	ProviderPlanFormatVersion string
	Unsupported               []WorkflowUnsupportedFeature
	SupportedFeatureFlags     []string
}

// WorkflowDeploymentBinding binds host deployment identity to a provider plan.
type WorkflowDeploymentBinding struct {
	ID                       string
	ExecutionRef             string
	ExecutionRefGeneration   int64
	ExecutionRefSeal         string
	DeploymentID             string
	DeploymentGeneration     int64
	SpecDigest               string
	TargetDigest             string
	ActionTableDigest        string
	ProviderPlanID           string
	ProviderPlanDigest       string
	WorkflowSemanticsVersion string
	RequestID                string
}

// WorkflowDeployment contains provider deployment state.
type WorkflowDeployment struct {
	Spec               *WorkflowDeploymentSpec
	Status             WorkflowDeploymentStatus
	CreatedAt          time.Time
	UpdatedAt          time.Time
	AppliedGeneration  int64
	SpecDigest         string
	TargetDigest       string
	ActionTableDigest  string
	ProviderPlanID     string
	ProviderPlanDigest string
	Binding            *WorkflowDeploymentBinding
	Error              *WorkflowRunError
}

// WorkflowScheduleTrigger contains fields for constructing a
// schedule-triggered workflow run trigger.
type WorkflowScheduleTrigger struct {
	ActivationID string
	ScheduledFor *time.Time
}

// WorkflowEventTrigger contains fields for constructing an event-triggered
// workflow run trigger.
type WorkflowEventTrigger struct {
	ActivationID string
	Event        *WorkflowEvent
}

// WorkflowRunTrigger contains fields for constructing a workflow run trigger.
// Exactly one trigger kind should be set.
type WorkflowRunTrigger struct {
	DeploymentID         string
	DeploymentGeneration int64
	ActivationID         string
	Manual               bool
	Schedule             *WorkflowScheduleTrigger
	Event                *WorkflowEventTrigger
}

// WorkflowSignal contains fields for constructing a WorkflowSignal.
type WorkflowSignal struct {
	ID             string
	Name           string
	Payload        any
	Metadata       any
	CreatedBy      *WorkflowActor
	CreatedAt      time.Time
	IdempotencyKey string
	Sequence       int64
}

// WorkflowOutputSummary summarizes a host-returned workflow output envelope.
type WorkflowOutputSummary struct {
	EnvelopeVersion string
	Kind            string
	SizeBytes       int64
	SHA256          string
	Truncated       bool
	Redacted        bool
	MediaType       string
}

// WorkflowRunError contains a workflow run, step, or action failure.
type WorkflowRunError struct {
	Code     string
	Message  string
	StepID   string
	ActionID string
}

// WorkflowStepState contains a provider-owned step projection for a run.
type WorkflowStepState struct {
	StepID        string
	StepIndex     int32
	Status        WorkflowStepStatus
	SkippedReason string
	AttemptNumber int32
	OutputSummary *WorkflowOutputSummary
	OutputRef     string
	Error         *WorkflowRunError
	UpdatedAt     *time.Time
}

// WorkflowRun contains provider run state.
type WorkflowRun struct {
	ID                     string
	DeploymentID           string
	DeploymentGeneration   int64
	WorkflowKey            string
	Status                 WorkflowRunStatus
	Trigger                *WorkflowRunTrigger
	Input                  any
	CreatedBy              *WorkflowActor
	CreatedAt              time.Time
	StartedAt              *time.Time
	CompletedAt            *time.Time
	StatusMessage          string
	ExecutionRef           string
	ExecutionRefGeneration int64
	TargetDigest           string
	SpecDigest             string
	ActionTableDigest      string
	ProviderPlanDigest     string
	Steps                  []WorkflowStepState
	Error                  *WorkflowRunError
}

// WorkflowRunSignal contains a run and signal affected by a signal operation.
type WorkflowRunSignal struct {
	Run         *WorkflowRun
	Signal      *WorkflowSignal
	StartedRun  bool
	WorkflowKey string
}

// WorkflowEventDeliveryResult contains one event delivery result.
type WorkflowEventDeliveryResult struct {
	DeploymentID string
	ActivationID string
	Run          *WorkflowRun
	Signal       *WorkflowSignal
	StartedRun   bool
}

// WorkflowRunEvent contains one event in a workflow run event log.
type WorkflowRunEvent struct {
	ID            string
	RunID         string
	Sequence      int64
	Type          WorkflowRunEventType
	StepID        string
	ActionID      string
	AttemptNumber int32
	Message       string
	OutputSummary *WorkflowOutputSummary
	OutputRef     string
	Error         *WorkflowRunError
	ObservedAt    time.Time
}

// WorkflowRunOutput contains a fetched run or step output body.
type WorkflowRunOutput struct {
	OutputRef string
	Summary   *WorkflowOutputSummary
	Body      any
	BodySet   bool
}

// WorkflowActionResult is returned by WorkflowHostClient.InvokeWorkflowAction.
type WorkflowActionResult struct {
	ActionEventID string
	Status        int32
	Body          string
	OutputSummary *WorkflowOutputSummary
	OutputRef     string
	Error         *WorkflowRunError
}

// GetStatus returns the HTTP-style action status code.
func (r *WorkflowActionResult) GetStatus() int32 {
	if r == nil {
		return 0
	}
	return r.Status
}

// GetBody returns the action response body.
func (r *WorkflowActionResult) GetBody() string {
	if r == nil {
		return ""
	}
	return r.Body
}

func boundWorkflowTargetToProto(input BoundWorkflowTarget) (*proto.BoundWorkflowTarget, error) {
	steps, err := workflowStepsToProto(input.Steps)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowTarget{Steps: steps}, nil
}

func boundWorkflowTargetFromProto(value *proto.BoundWorkflowTarget) BoundWorkflowTarget {
	if value == nil {
		return BoundWorkflowTarget{}
	}
	return BoundWorkflowTarget{Steps: workflowStepsFromProto(value.GetSteps())}
}

func workflowStepsToProto(steps []WorkflowStep) ([]*proto.WorkflowStep, error) {
	if len(steps) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowStep, 0, len(steps))
	for i := range steps {
		step, err := workflowStepToProto(steps[i])
		if err != nil {
			return nil, fmt.Errorf("steps[%d]: %w", i, err)
		}
		out = append(out, step)
	}
	return out, nil
}

func workflowStepsFromProto(steps []*proto.WorkflowStep) []WorkflowStep {
	if len(steps) == 0 {
		return nil
	}
	out := make([]WorkflowStep, 0, len(steps))
	for _, step := range steps {
		if step == nil {
			continue
		}
		out = append(out, workflowStepFromProto(step))
	}
	return out
}

func workflowStepToProto(step WorkflowStep) (*proto.WorkflowStep, error) {
	inputs, err := workflowValueMapToProto(step.Inputs)
	if err != nil {
		return nil, fmt.Errorf("inputs: %w", err)
	}
	when, err := workflowStepWhenToProto(step.When)
	if err != nil {
		return nil, fmt.Errorf("when: %w", err)
	}
	metadata, err := structFromAny(step.Metadata)
	if err != nil {
		return nil, fmt.Errorf("metadata: %w", err)
	}
	delivery, err := workflowStepDeliveryToProto(step.OutputDelivery)
	if err != nil {
		return nil, fmt.Errorf("output_delivery: %w", err)
	}
	out := &proto.WorkflowStep{
		Id:             step.ID,
		Inputs:         inputs,
		When:           when,
		TimeoutSeconds: step.TimeoutSeconds,
		OutputDelivery: delivery,
		Metadata:       metadata,
	}
	switch {
	case step.Plugin != nil && step.Agent != nil:
		return nil, fmt.Errorf("cannot set both plugin and agent")
	case step.Plugin != nil:
		plugin, err := workflowStepPluginCallToProto(step.Plugin)
		if err != nil {
			return nil, fmt.Errorf("plugin: %w", err)
		}
		out.Action = &proto.WorkflowStep_Plugin{Plugin: plugin}
	case step.Agent != nil:
		agent, err := workflowStepAgentTurnToProto(step.Agent)
		if err != nil {
			return nil, fmt.Errorf("agent: %w", err)
		}
		out.Action = &proto.WorkflowStep_Agent{Agent: agent}
	}
	return out, nil
}

func workflowStepFromProto(step *proto.WorkflowStep) WorkflowStep {
	if step == nil {
		return WorkflowStep{}
	}
	out := WorkflowStep{
		ID:             step.GetId(),
		Inputs:         workflowValueMapFromProto(step.GetInputs()),
		When:           workflowStepWhenFromProto(step.GetWhen()),
		TimeoutSeconds: step.GetTimeoutSeconds(),
		OutputDelivery: workflowStepDeliveryFromProto(step.GetOutputDelivery()),
		Metadata:       mapFromStruct(step.GetMetadata()),
	}
	if step.GetPlugin() != nil {
		out.Plugin = workflowStepPluginCallFromProto(step.GetPlugin())
	}
	if step.GetAgent() != nil {
		out.Agent = workflowStepAgentTurnFromProto(step.GetAgent())
	}
	return out
}

func workflowStepPluginCallToProto(input *WorkflowStepPluginCall) (*proto.WorkflowStepPluginCall, error) {
	if input == nil {
		return nil, nil
	}
	value, err := workflowValueToProto(input.Input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	return &proto.WorkflowStepPluginCall{
		Name:           input.Name,
		Operation:      input.Operation,
		Input:          value,
		Connection:     input.Connection,
		Instance:       input.Instance,
		CredentialMode: input.CredentialMode,
	}, nil
}

func workflowStepPluginCallFromProto(value *proto.WorkflowStepPluginCall) *WorkflowStepPluginCall {
	if value == nil {
		return nil
	}
	return &WorkflowStepPluginCall{
		Name:           value.GetName(),
		Operation:      value.GetOperation(),
		Input:          workflowValueFromProto(value.GetInput()),
		Connection:     value.GetConnection(),
		Instance:       value.GetInstance(),
		CredentialMode: value.GetCredentialMode(),
	}
}

func workflowStepDeliveryToProto(input *WorkflowStepDelivery) (*proto.WorkflowStepDelivery, error) {
	if input == nil {
		return nil, nil
	}
	plugin, err := workflowStepPluginCallToProto(input.Plugin)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowStepDelivery{Plugin: plugin}, nil
}

func workflowStepDeliveryFromProto(value *proto.WorkflowStepDelivery) *WorkflowStepDelivery {
	if value == nil {
		return nil
	}
	return &WorkflowStepDelivery{Plugin: workflowStepPluginCallFromProto(value.GetPlugin())}
}

func workflowStepAgentTurnToProto(input *WorkflowStepAgentTurn) (*proto.WorkflowStepAgentTurn, error) {
	if input == nil {
		return nil, nil
	}
	messages, err := workflowAgentMessagesToProto(input.Messages)
	if err != nil {
		return nil, err
	}
	responseSchema, err := structFromAny(input.ResponseSchema)
	if err != nil {
		return nil, fmt.Errorf("response_schema: %w", err)
	}
	modelOptions, err := structFromAny(input.ModelOptions)
	if err != nil {
		return nil, fmt.Errorf("model_options: %w", err)
	}
	return &proto.WorkflowStepAgentTurn{
		Provider:       input.Provider,
		Model:          input.Model,
		SessionKey:     workflowTextToProto(input.SessionKey),
		Prompt:         workflowTextToProto(input.Prompt),
		Messages:       messages,
		Tools:          agentToolRefsToProto(input.Tools),
		ResponseSchema: responseSchema,
		ModelOptions:   modelOptions,
	}, nil
}

func workflowStepAgentTurnFromProto(value *proto.WorkflowStepAgentTurn) *WorkflowStepAgentTurn {
	if value == nil {
		return nil
	}
	return &WorkflowStepAgentTurn{
		Provider:       value.GetProvider(),
		Model:          value.GetModel(),
		SessionKey:     workflowTextFromProto(value.GetSessionKey()),
		Prompt:         workflowTextFromProto(value.GetPrompt()),
		Messages:       workflowAgentMessagesFromProto(value.GetMessages()),
		Tools:          agentToolRefsFromProto(value.GetTools()),
		ResponseSchema: mapFromStruct(value.GetResponseSchema()),
		ModelOptions:   mapFromStruct(value.GetModelOptions()),
	}
}

func workflowAgentMessagesToProto(values []WorkflowAgentMessage) ([]*proto.WorkflowAgentMessage, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowAgentMessage, 0, len(values))
	for i, value := range values {
		metadata, err := structFromAny(value.Metadata)
		if err != nil {
			return nil, fmt.Errorf("messages[%d].metadata: %w", i, err)
		}
		out = append(out, &proto.WorkflowAgentMessage{
			Role:     value.Role,
			Text:     workflowTextToProto(value.Text),
			Metadata: metadata,
		})
	}
	return out, nil
}

func workflowAgentMessagesFromProto(values []*proto.WorkflowAgentMessage) []WorkflowAgentMessage {
	if len(values) == 0 {
		return nil
	}
	out := make([]WorkflowAgentMessage, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, WorkflowAgentMessage{
			Role:     value.GetRole(),
			Text:     workflowTextFromProto(value.GetText()),
			Metadata: mapFromStruct(value.GetMetadata()),
		})
	}
	return out
}

func workflowTextToProto(input WorkflowText) *proto.WorkflowText {
	if input.Template == "" {
		return nil
	}
	return &proto.WorkflowText{Template: input.Template}
}

func workflowTextFromProto(value *proto.WorkflowText) WorkflowText {
	if value == nil {
		return WorkflowText{}
	}
	return WorkflowText{Template: value.GetTemplate()}
}

func workflowStepWhenToProto(input *WorkflowStepWhen) (*proto.WorkflowStepWhen, error) {
	if input == nil {
		return nil, nil
	}
	value, err := workflowValueToProto(input.Value)
	if err != nil {
		return nil, fmt.Errorf("value: %w", err)
	}
	equals, err := valueFromAny(input.Equals)
	if err != nil {
		return nil, fmt.Errorf("equals: %w", err)
	}
	return &proto.WorkflowStepWhen{Value: value, Equals: equals}, nil
}

func workflowStepWhenFromProto(value *proto.WorkflowStepWhen) *WorkflowStepWhen {
	if value == nil {
		return nil
	}
	return &WorkflowStepWhen{
		Value:  workflowValueFromProto(value.GetValue()),
		Equals: anyFromValue(value.GetEquals()),
	}
}

func workflowValueMapToProto(values map[string]WorkflowValue) (map[string]*proto.WorkflowValue, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]*proto.WorkflowValue, len(values))
	for key, value := range values {
		converted, err := workflowValueToProto(value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out[key] = converted
	}
	return out, nil
}

func workflowValueMapFromProto(values map[string]*proto.WorkflowValue) map[string]WorkflowValue {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]WorkflowValue, len(values))
	for key, value := range values {
		out[key] = workflowValueFromProto(value)
	}
	return out
}

func workflowValueToProto(input WorkflowValue) (*proto.WorkflowValue, error) {
	set := 0
	if input.LiteralSet {
		set++
	}
	if input.Object != nil {
		set++
	}
	if input.Array != nil {
		set++
	}
	if input.Template != nil {
		set++
	}
	if strings.TrimSpace(input.RunInput) != "" {
		set++
	}
	if strings.TrimSpace(input.SignalPayload) != "" {
		set++
	}
	if input.StepOutput != nil {
		set++
	}
	if set == 0 {
		return nil, nil
	}
	if set != 1 {
		return nil, fmt.Errorf("must set exactly one value kind")
	}
	switch {
	case input.LiteralSet:
		literal, err := valueFromAny(input.Literal)
		if err != nil {
			return nil, err
		}
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Literal{Literal: literal}}, nil
	case input.Object != nil:
		fields, err := workflowValueMapToProto(input.Object)
		if err != nil {
			return nil, err
		}
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Object{Object: &proto.WorkflowObject{Fields: fields}}}, nil
	case input.Array != nil:
		values := make([]*proto.WorkflowValue, 0, len(input.Array))
		for i := range input.Array {
			value, err := workflowValueToProto(input.Array[i])
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			values = append(values, value)
		}
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Array{Array: &proto.WorkflowArray{Values: values}}}, nil
	case input.Template != nil:
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Template{Template: workflowTextToProto(*input.Template)}}, nil
	case strings.TrimSpace(input.RunInput) != "":
		return workflowPathValue(input.RunInput, func(path *proto.WorkflowPathSource) *proto.WorkflowValue {
			return &proto.WorkflowValue{Kind: &proto.WorkflowValue_RunInput{RunInput: path}}
		}), nil
	case strings.TrimSpace(input.SignalPayload) != "":
		return workflowPathValue(input.SignalPayload, func(path *proto.WorkflowPathSource) *proto.WorkflowValue {
			return &proto.WorkflowValue{Kind: &proto.WorkflowValue_SignalPayload{SignalPayload: path}}
		}), nil
	case input.StepOutput != nil:
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_StepOutput{StepOutput: &proto.WorkflowStepOutputSource{
			StepId: input.StepOutput.StepID,
			Path:   input.StepOutput.Path,
		}}}, nil
	default:
		return nil, nil
	}
}

func workflowPathValue(path string, build func(*proto.WorkflowPathSource) *proto.WorkflowValue) *proto.WorkflowValue {
	return build(&proto.WorkflowPathSource{Path: path})
}

func workflowValueFromProto(value *proto.WorkflowValue) WorkflowValue {
	if value == nil {
		return WorkflowValue{}
	}
	switch typed := value.GetKind().(type) {
	case *proto.WorkflowValue_Literal:
		return WorkflowValue{Literal: anyFromValue(typed.Literal), LiteralSet: true}
	case *proto.WorkflowValue_Object:
		var fields map[string]*proto.WorkflowValue
		if typed.Object != nil {
			fields = typed.Object.GetFields()
		}
		return WorkflowValue{Object: workflowValueMapFromProto(fields)}
	case *proto.WorkflowValue_Array:
		var values []*proto.WorkflowValue
		if typed.Array != nil {
			values = typed.Array.GetValues()
		}
		out := make([]WorkflowValue, 0, len(values))
		for _, value := range values {
			out = append(out, workflowValueFromProto(value))
		}
		return WorkflowValue{Array: out}
	case *proto.WorkflowValue_Template:
		text := workflowTextFromProto(typed.Template)
		return WorkflowValue{Template: &text}
	case *proto.WorkflowValue_RunInput:
		return WorkflowValue{RunInput: workflowPathSourcePath(typed.RunInput)}
	case *proto.WorkflowValue_SignalPayload:
		return WorkflowValue{SignalPayload: workflowPathSourcePath(typed.SignalPayload)}
	case *proto.WorkflowValue_StepOutput:
		if typed.StepOutput == nil {
			return WorkflowValue{StepOutput: &WorkflowStepOutputSource{}}
		}
		return WorkflowValue{StepOutput: &WorkflowStepOutputSource{
			StepID: typed.StepOutput.GetStepId(),
			Path:   typed.StepOutput.GetPath(),
		}}
	default:
		return WorkflowValue{}
	}
}

func workflowValuePtrToProto(input *WorkflowValue) (*proto.WorkflowValue, error) {
	if input == nil {
		return nil, nil
	}
	return workflowValueToProto(*input)
}

func workflowValueInputPtrFromValue(value *proto.WorkflowValue) *WorkflowValue {
	if value == nil {
		return nil
	}
	input := workflowValueFromProto(value)
	return &input
}

func workflowPathSourcePath(value *proto.WorkflowPathSource) string {
	if value == nil {
		return ""
	}
	return value.GetPath()
}

// cloneBoundWorkflowTargetProto creates a copy of an existing workflow target
// through the target input builder.
func cloneBoundWorkflowTargetProto(value *proto.BoundWorkflowTarget) (*proto.BoundWorkflowTarget, error) {
	if value == nil {
		return nil, nil
	}
	return boundWorkflowTargetToProto(boundWorkflowTargetFromProto(value))
}

func workflowActorToProto(input WorkflowActor) *proto.WorkflowActor {
	return &proto.WorkflowActor{
		SubjectId:   input.SubjectID,
		SubjectKind: input.SubjectKind,
		DisplayName: input.DisplayName,
		AuthSource:  input.AuthSource,
	}
}

func workflowActorFromProto(value *proto.WorkflowActor) WorkflowActor {
	if value == nil {
		return WorkflowActor{}
	}
	return WorkflowActor{
		SubjectID:   value.GetSubjectId(),
		SubjectKind: value.GetSubjectKind(),
		DisplayName: value.GetDisplayName(),
		AuthSource:  value.GetAuthSource(),
	}
}

func workflowActorFromInput(input *WorkflowActor) *proto.WorkflowActor {
	if input == nil {
		return nil
	}
	return workflowActorToProto(*input)
}

func workflowActorInputPtrFromActor(value *proto.WorkflowActor) *WorkflowActor {
	if value == nil {
		return nil
	}
	input := workflowActorFromProto(value)
	return &input
}

func workflowRunAsSubjectFromInput(input *WorkflowRunAsSubject) *proto.WorkflowRunAsSubject {
	if input == nil {
		return nil
	}
	return &proto.WorkflowRunAsSubject{
		SubjectId:           input.SubjectID,
		SubjectKind:         input.SubjectKind,
		DisplayName:         input.DisplayName,
		AuthSource:          input.AuthSource,
		CredentialSubjectId: input.CredentialSubjectID,
	}
}

func workflowRunAsSubjectInputPtrFromSubject(value *proto.WorkflowRunAsSubject) *WorkflowRunAsSubject {
	if value == nil {
		return nil
	}
	return &WorkflowRunAsSubject{
		SubjectID:           value.GetSubjectId(),
		SubjectKind:         value.GetSubjectKind(),
		DisplayName:         value.GetDisplayName(),
		AuthSource:          value.GetAuthSource(),
		CredentialSubjectID: value.GetCredentialSubjectId(),
	}
}

func workflowEventToProto(input WorkflowEvent) (*proto.WorkflowEvent, error) {
	data, err := structFromAny(input.Data)
	if err != nil {
		return nil, err
	}
	extensions, err := valuesFromMap(input.Extensions)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowEvent{
		Id:              input.ID,
		Source:          input.Source,
		SpecVersion:     input.SpecVersion,
		Type:            input.Type,
		Subject:         input.Subject,
		Time:            timestampFromNonZeroTime(input.Time),
		Datacontenttype: input.DataContentType,
		Data:            data,
		Extensions:      extensions,
	}, nil
}

func workflowEventFromProto(value *proto.WorkflowEvent) WorkflowEvent {
	if value == nil {
		return WorkflowEvent{}
	}
	return WorkflowEvent{
		ID:              value.GetId(),
		Source:          value.GetSource(),
		SpecVersion:     value.GetSpecVersion(),
		Type:            value.GetType(),
		Subject:         value.GetSubject(),
		Time:            timeFromTimestamp(value.GetTime()),
		DataContentType: value.GetDatacontenttype(),
		Data:            mapFromStruct(value.GetData()),
		Extensions:      mapFromValues(value.GetExtensions()),
	}
}

func cloneWorkflowEventProto(value *proto.WorkflowEvent) (*proto.WorkflowEvent, error) {
	if value == nil {
		return nil, nil
	}
	return workflowEventToProto(workflowEventFromProto(value))
}

func workflowEventPtrToProto(input *WorkflowEvent) (*proto.WorkflowEvent, error) {
	if input == nil {
		return nil, nil
	}
	return workflowEventToProto(*input)
}

func workflowEventInputPtrFromEvent(value *proto.WorkflowEvent) *WorkflowEvent {
	if value == nil {
		return nil
	}
	input := workflowEventFromProto(value)
	return &input
}

func workflowEventMatchFromInput(input *WorkflowEventMatch) *proto.WorkflowEventMatch {
	if input == nil {
		return nil
	}
	return &proto.WorkflowEventMatch{
		Type:    input.Type,
		Source:  input.Source,
		Subject: input.Subject,
	}
}

func workflowEventMatchInputPtrFromMatch(value *proto.WorkflowEventMatch) *WorkflowEventMatch {
	if value == nil {
		return nil
	}
	return &WorkflowEventMatch{
		Type:    value.GetType(),
		Source:  value.GetSource(),
		Subject: value.GetSubject(),
	}
}

func workflowActivationToProto(input WorkflowActivation) (*proto.WorkflowActivation, error) {
	value, err := workflowValuePtrToProto(input.Input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	runKey, err := workflowValuePtrToProto(input.RunKey)
	if err != nil {
		return nil, fmt.Errorf("run_key: %w", err)
	}
	idempotencyKey, err := workflowValuePtrToProto(input.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("idempotency_key: %w", err)
	}
	out := &proto.WorkflowActivation{
		Id:             input.ID,
		Paused:         input.Paused,
		Mode:           proto.WorkflowActivationMode(input.Mode),
		Input:          value,
		RunKey:         runKey,
		IdempotencyKey: idempotencyKey,
	}
	selected := 0
	if input.Manual {
		selected++
	}
	if input.Schedule != nil {
		selected++
	}
	if input.Event != nil {
		selected++
	}
	if selected != 1 {
		return nil, fmt.Errorf("workflow activation must set exactly one activation kind")
	}
	switch {
	case input.Manual:
		out.Kind = &proto.WorkflowActivation_Manual{Manual: &proto.WorkflowManualActivation{}}
	case input.Schedule != nil:
		out.Kind = &proto.WorkflowActivation_Schedule{Schedule: &proto.WorkflowScheduleActivation{
			Cron:     input.Schedule.Cron,
			Timezone: input.Schedule.Timezone,
		}}
	case input.Event != nil:
		out.Kind = &proto.WorkflowActivation_Event{Event: &proto.WorkflowEventActivation{
			Match: workflowEventMatchFromInput(input.Event.Match),
		}}
	}
	return out, nil
}

func workflowActivationFromProto(value *proto.WorkflowActivation) WorkflowActivation {
	if value == nil {
		return WorkflowActivation{}
	}
	out := WorkflowActivation{
		ID:             value.GetId(),
		Paused:         value.GetPaused(),
		Mode:           WorkflowActivationMode(value.GetMode()),
		Input:          workflowValueInputPtrFromValue(value.GetInput()),
		RunKey:         workflowValueInputPtrFromValue(value.GetRunKey()),
		IdempotencyKey: workflowValueInputPtrFromValue(value.GetIdempotencyKey()),
	}
	switch kind := value.GetKind().(type) {
	case *proto.WorkflowActivation_Manual:
		out.Manual = true
	case *proto.WorkflowActivation_Schedule:
		if kind.Schedule != nil {
			out.Schedule = &WorkflowScheduleActivation{
				Cron:     kind.Schedule.GetCron(),
				Timezone: kind.Schedule.GetTimezone(),
			}
		}
	case *proto.WorkflowActivation_Event:
		if kind.Event != nil {
			out.Event = &WorkflowEventActivation{Match: workflowEventMatchInputPtrFromMatch(kind.Event.GetMatch())}
		}
	}
	return out
}

func workflowActivationsToProto(values []WorkflowActivation) ([]*proto.WorkflowActivation, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowActivation, 0, len(values))
	for i, value := range values {
		activation, err := workflowActivationToProto(value)
		if err != nil {
			return nil, fmt.Errorf("activations[%d]: %w", i, err)
		}
		out = append(out, activation)
	}
	return out, nil
}

func workflowActivationsFromProto(values []*proto.WorkflowActivation) []WorkflowActivation {
	if len(values) == 0 {
		return nil
	}
	out := make([]WorkflowActivation, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, workflowActivationFromProto(value))
	}
	return out
}

func workflowAccessPermissionsFromInputs(values []WorkflowAccessPermission) []*proto.WorkflowAccessPermission {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.WorkflowAccessPermission, 0, len(values))
	for _, value := range values {
		out = append(out, &proto.WorkflowAccessPermission{
			Plugin:     value.Plugin,
			Operations: append([]string(nil), value.Operations...),
			Actions:    append([]string(nil), value.Actions...),
		})
	}
	return out
}

func workflowAccessPermissionInputsFromPermissions(values []*proto.WorkflowAccessPermission) []WorkflowAccessPermission {
	if len(values) == 0 {
		return nil
	}
	out := make([]WorkflowAccessPermission, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, WorkflowAccessPermission{
			Plugin:     value.GetPlugin(),
			Operations: append([]string(nil), value.GetOperations()...),
			Actions:    append([]string(nil), value.GetActions()...),
		})
	}
	return out
}

func workflowExecutionReferenceToProto(input *WorkflowExecutionReference) (*proto.WorkflowExecutionReference, error) {
	if input == nil {
		return nil, nil
	}
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowExecutionReference{
		Id:                  input.ID,
		ProviderName:        input.ProviderName,
		Target:              target,
		CallerPluginName:    input.CallerPluginName,
		SourceDefinitionId:  input.SourceDefinitionID,
		SubjectId:           input.SubjectID,
		SubjectKind:         input.SubjectKind,
		DisplayName:         input.DisplayName,
		AuthSource:          input.AuthSource,
		CredentialSubjectId: input.CredentialSubjectID,
		RunAs:               workflowRunAsSubjectFromInput(input.RunAs),
		Permissions:         workflowAccessPermissionsFromInputs(input.Permissions),
		CreatedAt:           timestampFromOptionalTime(input.CreatedAt),
		RevokedAt:           timestampFromOptionalTime(input.RevokedAt),
		TargetDigest:        input.TargetDigest,
		ProviderPlanDigest:  input.ProviderPlanDigest,
		PermissionsDigest:   input.PermissionsDigest,
		SemanticsVersion:    input.SemanticsVersion,
		Generation:          input.Generation,
		Seal:                input.Seal,
	}, nil
}

func workflowExecutionReferenceFromProto(value *proto.WorkflowExecutionReference) (*WorkflowExecutionReference, error) {
	if value == nil {
		return nil, nil
	}
	createdAt, err := timePtrFromTimestamp(value.GetCreatedAt())
	if err != nil {
		return nil, err
	}
	revokedAt, err := timePtrFromTimestamp(value.GetRevokedAt())
	if err != nil {
		return nil, err
	}
	return &WorkflowExecutionReference{
		ID:                  value.GetId(),
		ProviderName:        value.GetProviderName(),
		Target:              workflowTargetInputPtrFromTarget(value.GetTarget()),
		CallerPluginName:    value.GetCallerPluginName(),
		SourceDefinitionID:  value.GetSourceDefinitionId(),
		SubjectID:           value.GetSubjectId(),
		SubjectKind:         value.GetSubjectKind(),
		DisplayName:         value.GetDisplayName(),
		AuthSource:          value.GetAuthSource(),
		CredentialSubjectID: value.GetCredentialSubjectId(),
		RunAs:               workflowRunAsSubjectInputPtrFromSubject(value.GetRunAs()),
		Permissions:         workflowAccessPermissionInputsFromPermissions(value.GetPermissions()),
		CreatedAt:           createdAt,
		RevokedAt:           revokedAt,
		TargetDigest:        value.GetTargetDigest(),
		ProviderPlanDigest:  value.GetProviderPlanDigest(),
		PermissionsDigest:   value.GetPermissionsDigest(),
		SemanticsVersion:    value.GetSemanticsVersion(),
		Generation:          value.GetGeneration(),
		Seal:                value.GetSeal(),
	}, nil
}

func workflowExecutionReferencesToProto(values []WorkflowExecutionReference) ([]*proto.WorkflowExecutionReference, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowExecutionReference, 0, len(values))
	for i := range values {
		value, err := workflowExecutionReferenceToProto(&values[i])
		if err != nil {
			return nil, fmt.Errorf("execution_refs[%d]: %w", i, err)
		}
		out = append(out, value)
	}
	return out, nil
}

func workflowExecutionReferencesFromProto(values []*proto.WorkflowExecutionReference) ([]WorkflowExecutionReference, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]WorkflowExecutionReference, 0, len(values))
	for i, value := range values {
		converted, err := workflowExecutionReferenceFromProto(value)
		if err != nil {
			return nil, fmt.Errorf("execution_refs[%d]: %w", i, err)
		}
		if converted != nil {
			out = append(out, *converted)
		}
	}
	return out, nil
}

func workflowDeploymentSpecToProto(input WorkflowDeploymentSpec) (*proto.WorkflowDeploymentSpec, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	activations, err := workflowActivationsToProto(input.Activations)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowDeploymentSpec{
		Id:                       input.ID,
		Generation:               input.Generation,
		Target:                   target,
		Activations:              activations,
		Paused:                   input.Paused,
		RunAs:                    workflowRunAsSubjectFromInput(input.RunAs),
		Permissions:              workflowAccessPermissionsFromInputs(input.Permissions),
		Labels:                   cloneStringMap(input.Labels),
		WorkflowSemanticsVersion: input.WorkflowSemanticsVersion,
	}, nil
}

func workflowDeploymentSpecFromProto(value *proto.WorkflowDeploymentSpec) WorkflowDeploymentSpec {
	if value == nil {
		return WorkflowDeploymentSpec{}
	}
	return WorkflowDeploymentSpec{
		ID:                       value.GetId(),
		Generation:               value.GetGeneration(),
		Target:                   workflowTargetInputPtrFromTarget(value.GetTarget()),
		Activations:              workflowActivationsFromProto(value.GetActivations()),
		Paused:                   value.GetPaused(),
		RunAs:                    workflowRunAsSubjectInputPtrFromSubject(value.GetRunAs()),
		Permissions:              workflowAccessPermissionInputsFromPermissions(value.GetPermissions()),
		Labels:                   cloneStringMap(value.GetLabels()),
		WorkflowSemanticsVersion: value.GetWorkflowSemanticsVersion(),
	}
}

func newOptionalWorkflowDeploymentSpec(input *WorkflowDeploymentSpec) (*proto.WorkflowDeploymentSpec, error) {
	if input == nil {
		return nil, nil
	}
	return workflowDeploymentSpecToProto(*input)
}

func workflowDeploymentSpecInputPtrFromSpec(value *proto.WorkflowDeploymentSpec) *WorkflowDeploymentSpec {
	if value == nil {
		return nil
	}
	input := workflowDeploymentSpecFromProto(value)
	return &input
}

func cloneWorkflowDeploymentSpecProto(value *proto.WorkflowDeploymentSpec) (*proto.WorkflowDeploymentSpec, error) {
	if value == nil {
		return nil, nil
	}
	return workflowDeploymentSpecToProto(workflowDeploymentSpecFromProto(value))
}

func workflowUnsupportedFeaturesToProto(values []WorkflowUnsupportedFeature) []*proto.WorkflowUnsupportedFeature {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.WorkflowUnsupportedFeature, 0, len(values))
	for _, value := range values {
		out = append(out, &proto.WorkflowUnsupportedFeature{Feature: value.Feature, Reason: value.Reason})
	}
	return out
}

func workflowUnsupportedFeaturesFromProto(values []*proto.WorkflowUnsupportedFeature) []WorkflowUnsupportedFeature {
	if len(values) == 0 {
		return nil
	}
	out := make([]WorkflowUnsupportedFeature, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, WorkflowUnsupportedFeature{Feature: value.GetFeature(), Reason: value.GetReason()})
	}
	return out
}

func planWorkflowResponseToProto(resp *PlanWorkflowResponse) *proto.PlanWorkflowResponse {
	if resp == nil {
		return nil
	}
	return &proto.PlanWorkflowResponse{
		AcceptedSpecDigest:        resp.AcceptedSpecDigest,
		ProviderPlanId:            resp.ProviderPlanID,
		ProviderPlanDigest:        resp.ProviderPlanDigest,
		ProviderPlanFormatVersion: resp.ProviderPlanFormatVersion,
		Unsupported:               workflowUnsupportedFeaturesToProto(resp.Unsupported),
		SupportedFeatureFlags:     append([]string(nil), resp.SupportedFeatureFlags...),
	}
}

func planWorkflowResponseFromProto(value *proto.PlanWorkflowResponse) *PlanWorkflowResponse {
	if value == nil {
		return nil
	}
	return &PlanWorkflowResponse{
		AcceptedSpecDigest:        value.GetAcceptedSpecDigest(),
		ProviderPlanID:            value.GetProviderPlanId(),
		ProviderPlanDigest:        value.GetProviderPlanDigest(),
		ProviderPlanFormatVersion: value.GetProviderPlanFormatVersion(),
		Unsupported:               workflowUnsupportedFeaturesFromProto(value.GetUnsupported()),
		SupportedFeatureFlags:     append([]string(nil), value.GetSupportedFeatureFlags()...),
	}
}

func workflowDeploymentBindingToProto(value *WorkflowDeploymentBinding) *proto.WorkflowDeploymentBinding {
	if value == nil {
		return nil
	}
	return &proto.WorkflowDeploymentBinding{
		Id:                       value.ID,
		ExecutionRef:             value.ExecutionRef,
		ExecutionRefGeneration:   value.ExecutionRefGeneration,
		ExecutionRefSeal:         value.ExecutionRefSeal,
		DeploymentId:             value.DeploymentID,
		DeploymentGeneration:     value.DeploymentGeneration,
		SpecDigest:               value.SpecDigest,
		TargetDigest:             value.TargetDigest,
		ActionTableDigest:        value.ActionTableDigest,
		ProviderPlanId:           value.ProviderPlanID,
		ProviderPlanDigest:       value.ProviderPlanDigest,
		WorkflowSemanticsVersion: value.WorkflowSemanticsVersion,
		RequestId:                value.RequestID,
	}
}

func workflowDeploymentBindingFromProto(value *proto.WorkflowDeploymentBinding) *WorkflowDeploymentBinding {
	if value == nil {
		return nil
	}
	return &WorkflowDeploymentBinding{
		ID:                       value.GetId(),
		ExecutionRef:             value.GetExecutionRef(),
		ExecutionRefGeneration:   value.GetExecutionRefGeneration(),
		ExecutionRefSeal:         value.GetExecutionRefSeal(),
		DeploymentID:             value.GetDeploymentId(),
		DeploymentGeneration:     value.GetDeploymentGeneration(),
		SpecDigest:               value.GetSpecDigest(),
		TargetDigest:             value.GetTargetDigest(),
		ActionTableDigest:        value.GetActionTableDigest(),
		ProviderPlanID:           value.GetProviderPlanId(),
		ProviderPlanDigest:       value.GetProviderPlanDigest(),
		WorkflowSemanticsVersion: value.GetWorkflowSemanticsVersion(),
		RequestID:                value.GetRequestId(),
	}
}

func workflowDeploymentToProto(input *WorkflowDeployment) (*proto.WorkflowDeployment, error) {
	if input == nil {
		return nil, nil
	}
	spec, err := newOptionalWorkflowDeploymentSpec(input.Spec)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowDeployment{
		Spec:               spec,
		Status:             proto.WorkflowDeploymentStatus(input.Status),
		CreatedAt:          timestampFromNonZeroTime(input.CreatedAt),
		UpdatedAt:          timestampFromNonZeroTime(input.UpdatedAt),
		AppliedGeneration:  input.AppliedGeneration,
		SpecDigest:         input.SpecDigest,
		TargetDigest:       input.TargetDigest,
		ActionTableDigest:  input.ActionTableDigest,
		ProviderPlanId:     input.ProviderPlanID,
		ProviderPlanDigest: input.ProviderPlanDigest,
		Binding:            workflowDeploymentBindingToProto(input.Binding),
		Error:              workflowRunErrorToProto(input.Error),
	}, nil
}

func workflowDeploymentFromProto(value *proto.WorkflowDeployment) (*WorkflowDeployment, error) {
	if value == nil {
		return nil, nil
	}
	return &WorkflowDeployment{
		Spec:               workflowDeploymentSpecInputPtrFromSpec(value.GetSpec()),
		Status:             WorkflowDeploymentStatus(value.GetStatus()),
		CreatedAt:          timeFromTimestamp(value.GetCreatedAt()),
		UpdatedAt:          timeFromTimestamp(value.GetUpdatedAt()),
		AppliedGeneration:  value.GetAppliedGeneration(),
		SpecDigest:         value.GetSpecDigest(),
		TargetDigest:       value.GetTargetDigest(),
		ActionTableDigest:  value.GetActionTableDigest(),
		ProviderPlanID:     value.GetProviderPlanId(),
		ProviderPlanDigest: value.GetProviderPlanDigest(),
		Binding:            workflowDeploymentBindingFromProto(value.GetBinding()),
		Error:              workflowRunErrorFromProto(value.GetError()),
	}, nil
}

func workflowDeploymentsToProto(values []WorkflowDeployment) ([]*proto.WorkflowDeployment, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowDeployment, 0, len(values))
	for i := range values {
		value, err := workflowDeploymentToProto(&values[i])
		if err != nil {
			return nil, fmt.Errorf("deployments[%d]: %w", i, err)
		}
		out = append(out, value)
	}
	return out, nil
}

func workflowDeploymentsFromProto(values []*proto.WorkflowDeployment) ([]WorkflowDeployment, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]WorkflowDeployment, 0, len(values))
	for i, value := range values {
		converted, err := workflowDeploymentFromProto(value)
		if err != nil {
			return nil, fmt.Errorf("deployments[%d]: %w", i, err)
		}
		if converted != nil {
			out = append(out, *converted)
		}
	}
	return out, nil
}

func workflowRunTriggerToProto(input WorkflowRunTrigger) (*proto.WorkflowRunTrigger, error) {
	out := &proto.WorkflowRunTrigger{
		DeploymentId:         input.DeploymentID,
		DeploymentGeneration: input.DeploymentGeneration,
		ActivationId:         input.ActivationID,
	}
	selected := 0
	if input.Manual {
		selected++
	}
	if input.Schedule != nil {
		selected++
	}
	if input.Event != nil {
		selected++
	}
	if selected == 0 {
		return out, nil
	}
	if selected > 1 {
		return nil, fmt.Errorf("workflow run trigger must set exactly one trigger kind")
	}
	if input.Manual {
		out.Kind = &proto.WorkflowRunTrigger_Manual{Manual: &proto.WorkflowManualTrigger{}}
		return out, nil
	}
	if input.Schedule != nil {
		activationID := input.Schedule.ActivationID
		if activationID == "" {
			activationID = input.ActivationID
		}
		out.Kind = &proto.WorkflowRunTrigger_Schedule{Schedule: &proto.WorkflowScheduleTrigger{
			ActivationId: activationID,
			ScheduledFor: timestampFromOptionalTime(input.Schedule.ScheduledFor),
		}}
		return out, nil
	}
	event, err := workflowEventPtrToProto(input.Event.Event)
	if err != nil {
		return nil, err
	}
	activationID := input.Event.ActivationID
	if activationID == "" {
		activationID = input.ActivationID
	}
	out.Kind = &proto.WorkflowRunTrigger_Event{Event: &proto.WorkflowEventTrigger{
		ActivationId: activationID,
		Event:        event,
	}}
	return out, nil
}

func workflowRunTriggerFromProto(value *proto.WorkflowRunTrigger) (WorkflowRunTrigger, error) {
	if value == nil {
		return WorkflowRunTrigger{}, nil
	}
	out := WorkflowRunTrigger{
		DeploymentID:         value.GetDeploymentId(),
		DeploymentGeneration: value.GetDeploymentGeneration(),
		ActivationID:         value.GetActivationId(),
	}
	switch kind := value.GetKind().(type) {
	case *proto.WorkflowRunTrigger_Manual:
		out.Manual = true
	case *proto.WorkflowRunTrigger_Schedule:
		if kind.Schedule == nil {
			return out, nil
		}
		scheduledFor, err := timePtrFromTimestamp(kind.Schedule.GetScheduledFor())
		if err != nil {
			return WorkflowRunTrigger{}, err
		}
		out.Schedule = &WorkflowScheduleTrigger{
			ActivationID: kind.Schedule.GetActivationId(),
			ScheduledFor: scheduledFor,
		}
	case *proto.WorkflowRunTrigger_Event:
		if kind.Event == nil {
			return out, nil
		}
		out.Event = &WorkflowEventTrigger{
			ActivationID: kind.Event.GetActivationId(),
			Event:        workflowEventInputPtrFromEvent(kind.Event.GetEvent()),
		}
	}
	return out, nil
}

func cloneWorkflowRunTriggerProto(value *proto.WorkflowRunTrigger) (*proto.WorkflowRunTrigger, error) {
	input, err := workflowRunTriggerFromProto(value)
	if err != nil || value == nil {
		return nil, err
	}
	return workflowRunTriggerToProto(input)
}

func workflowSignalToProto(input WorkflowSignal) (*proto.WorkflowSignal, error) {
	payload, err := structFromAny(input.Payload)
	if err != nil {
		return nil, err
	}
	metadata, err := structFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowSignal{
		Id:             input.ID,
		Name:           input.Name,
		Payload:        payload,
		Metadata:       metadata,
		CreatedBy:      workflowActorFromInput(input.CreatedBy),
		CreatedAt:      timestampFromNonZeroTime(input.CreatedAt),
		IdempotencyKey: input.IdempotencyKey,
		Sequence:       input.Sequence,
	}, nil
}

func workflowSignalFromProto(value *proto.WorkflowSignal) WorkflowSignal {
	if value == nil {
		return WorkflowSignal{}
	}
	return WorkflowSignal{
		ID:             value.GetId(),
		Name:           value.GetName(),
		Payload:        mapFromStruct(value.GetPayload()),
		Metadata:       mapFromStruct(value.GetMetadata()),
		CreatedBy:      workflowActorInputPtrFromActor(value.GetCreatedBy()),
		CreatedAt:      timeFromTimestamp(value.GetCreatedAt()),
		IdempotencyKey: value.GetIdempotencyKey(),
		Sequence:       value.GetSequence(),
	}
}

func cloneWorkflowSignalProto(value *proto.WorkflowSignal) (*proto.WorkflowSignal, error) {
	if value == nil {
		return nil, nil
	}
	return workflowSignalToProto(workflowSignalFromProto(value))
}

func newOptionalWorkflowSignal(input *WorkflowSignal) (*proto.WorkflowSignal, error) {
	if input == nil {
		return nil, nil
	}
	return workflowSignalToProto(*input)
}

func workflowSignalsToProto(values []WorkflowSignal) ([]*proto.WorkflowSignal, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowSignal, 0, len(values))
	for i, input := range values {
		signal, err := workflowSignalToProto(input)
		if err != nil {
			return nil, fmt.Errorf("signals[%d]: %w", i, err)
		}
		out = append(out, signal)
	}
	return out, nil
}

func workflowSignalsFromProto(values []*proto.WorkflowSignal) []WorkflowSignal {
	if len(values) == 0 {
		return nil
	}
	out := make([]WorkflowSignal, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, workflowSignalFromProto(value))
	}
	return out
}

func workflowOutputSummaryToProto(value *WorkflowOutputSummary) *proto.WorkflowOutputSummary {
	if value == nil {
		return nil
	}
	return &proto.WorkflowOutputSummary{
		EnvelopeVersion: value.EnvelopeVersion,
		Kind:            value.Kind,
		SizeBytes:       value.SizeBytes,
		Sha256:          value.SHA256,
		Truncated:       value.Truncated,
		Redacted:        value.Redacted,
		MediaType:       value.MediaType,
	}
}

func workflowOutputSummaryFromProto(value *proto.WorkflowOutputSummary) *WorkflowOutputSummary {
	if value == nil {
		return nil
	}
	return &WorkflowOutputSummary{
		EnvelopeVersion: value.GetEnvelopeVersion(),
		Kind:            value.GetKind(),
		SizeBytes:       value.GetSizeBytes(),
		SHA256:          value.GetSha256(),
		Truncated:       value.GetTruncated(),
		Redacted:        value.GetRedacted(),
		MediaType:       value.GetMediaType(),
	}
}

func workflowRunErrorToProto(value *WorkflowRunError) *proto.WorkflowRunError {
	if value == nil {
		return nil
	}
	return &proto.WorkflowRunError{
		Code:     value.Code,
		Message:  value.Message,
		StepId:   value.StepID,
		ActionId: value.ActionID,
	}
}

func workflowRunErrorFromProto(value *proto.WorkflowRunError) *WorkflowRunError {
	if value == nil {
		return nil
	}
	return &WorkflowRunError{
		Code:     value.GetCode(),
		Message:  value.GetMessage(),
		StepID:   value.GetStepId(),
		ActionID: value.GetActionId(),
	}
}

func workflowStepStatesToProto(values []WorkflowStepState) []*proto.WorkflowStepState {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.WorkflowStepState, 0, len(values))
	for _, value := range values {
		out = append(out, &proto.WorkflowStepState{
			StepId:        value.StepID,
			StepIndex:     value.StepIndex,
			Status:        proto.WorkflowStepStatus(value.Status),
			SkippedReason: value.SkippedReason,
			AttemptNumber: value.AttemptNumber,
			OutputSummary: workflowOutputSummaryToProto(value.OutputSummary),
			OutputRef:     value.OutputRef,
			Error:         workflowRunErrorToProto(value.Error),
			UpdatedAt:     timestampFromOptionalTime(value.UpdatedAt),
		})
	}
	return out
}

func workflowStepStatesFromProto(values []*proto.WorkflowStepState) []WorkflowStepState {
	if len(values) == 0 {
		return nil
	}
	out := make([]WorkflowStepState, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		updatedAt, _ := timePtrFromTimestamp(value.GetUpdatedAt())
		out = append(out, WorkflowStepState{
			StepID:        value.GetStepId(),
			StepIndex:     value.GetStepIndex(),
			Status:        WorkflowStepStatus(value.GetStatus()),
			SkippedReason: value.GetSkippedReason(),
			AttemptNumber: value.GetAttemptNumber(),
			OutputSummary: workflowOutputSummaryFromProto(value.GetOutputSummary()),
			OutputRef:     value.GetOutputRef(),
			Error:         workflowRunErrorFromProto(value.GetError()),
			UpdatedAt:     updatedAt,
		})
	}
	return out
}

func workflowRunToProto(input *WorkflowRun) (*proto.WorkflowRun, error) {
	if input == nil {
		return nil, nil
	}
	trigger, err := newOptionalWorkflowRunTrigger(input.Trigger)
	if err != nil {
		return nil, err
	}
	body, err := structFromAny(input.Input)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowRun{
		Id:                     input.ID,
		DeploymentId:           input.DeploymentID,
		DeploymentGeneration:   input.DeploymentGeneration,
		WorkflowKey:            input.WorkflowKey,
		Status:                 proto.WorkflowRunStatus(input.Status),
		Trigger:                trigger,
		Input:                  body,
		CreatedBy:              workflowActorFromInput(input.CreatedBy),
		CreatedAt:              timestampFromNonZeroTime(input.CreatedAt),
		StartedAt:              timestampFromOptionalTime(input.StartedAt),
		CompletedAt:            timestampFromOptionalTime(input.CompletedAt),
		StatusMessage:          input.StatusMessage,
		ExecutionRef:           input.ExecutionRef,
		ExecutionRefGeneration: input.ExecutionRefGeneration,
		TargetDigest:           input.TargetDigest,
		SpecDigest:             input.SpecDigest,
		ActionTableDigest:      input.ActionTableDigest,
		ProviderPlanDigest:     input.ProviderPlanDigest,
		Steps:                  workflowStepStatesToProto(input.Steps),
		Error:                  workflowRunErrorToProto(input.Error),
	}, nil
}

func workflowRunFromProto(value *proto.WorkflowRun) (*WorkflowRun, error) {
	if value == nil {
		return nil, nil
	}
	startedAt, err := timePtrFromTimestamp(value.GetStartedAt())
	if err != nil {
		return nil, err
	}
	completedAt, err := timePtrFromTimestamp(value.GetCompletedAt())
	if err != nil {
		return nil, err
	}
	var trigger *WorkflowRunTrigger
	if value.GetTrigger() != nil {
		input, err := workflowRunTriggerFromProto(value.GetTrigger())
		if err != nil {
			return nil, err
		}
		trigger = &input
	}
	return &WorkflowRun{
		ID:                     value.GetId(),
		DeploymentID:           value.GetDeploymentId(),
		DeploymentGeneration:   value.GetDeploymentGeneration(),
		WorkflowKey:            value.GetWorkflowKey(),
		Status:                 WorkflowRunStatus(value.GetStatus()),
		Trigger:                trigger,
		Input:                  mapFromStruct(value.GetInput()),
		CreatedBy:              workflowActorInputPtrFromActor(value.GetCreatedBy()),
		CreatedAt:              timeFromTimestamp(value.GetCreatedAt()),
		StartedAt:              startedAt,
		CompletedAt:            completedAt,
		StatusMessage:          value.GetStatusMessage(),
		ExecutionRef:           value.GetExecutionRef(),
		ExecutionRefGeneration: value.GetExecutionRefGeneration(),
		TargetDigest:           value.GetTargetDigest(),
		SpecDigest:             value.GetSpecDigest(),
		ActionTableDigest:      value.GetActionTableDigest(),
		ProviderPlanDigest:     value.GetProviderPlanDigest(),
		Steps:                  workflowStepStatesFromProto(value.GetSteps()),
		Error:                  workflowRunErrorFromProto(value.GetError()),
	}, nil
}

func cloneWorkflowRunProto(value *proto.WorkflowRun) (*proto.WorkflowRun, error) {
	input, err := workflowRunFromProto(value)
	if err != nil || value == nil {
		return nil, err
	}
	return workflowRunToProto(input)
}

func workflowRunsToProto(values []WorkflowRun) ([]*proto.WorkflowRun, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowRun, 0, len(values))
	for i := range values {
		run, err := workflowRunToProto(&values[i])
		if err != nil {
			return nil, fmt.Errorf("runs[%d]: %w", i, err)
		}
		out = append(out, run)
	}
	return out, nil
}

func workflowRunsFromProto(values []*proto.WorkflowRun) ([]WorkflowRun, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]WorkflowRun, 0, len(values))
	for i, value := range values {
		run, err := workflowRunFromProto(value)
		if err != nil {
			return nil, fmt.Errorf("runs[%d]: %w", i, err)
		}
		if run != nil {
			out = append(out, *run)
		}
	}
	return out, nil
}

func workflowRunSignalToProto(input *WorkflowRunSignal) (*proto.WorkflowRunSignal, error) {
	if input == nil {
		return nil, nil
	}
	run, err := workflowRunToProto(input.Run)
	if err != nil {
		return nil, err
	}
	signal, err := newOptionalWorkflowSignal(input.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowRunSignal{
		Run:         run,
		Signal:      signal,
		StartedRun:  input.StartedRun,
		WorkflowKey: input.WorkflowKey,
	}, nil
}

func workflowRunSignalFromProto(value *proto.WorkflowRunSignal) (*WorkflowRunSignal, error) {
	if value == nil {
		return nil, nil
	}
	run, err := workflowRunFromProto(value.GetRun())
	if err != nil {
		return nil, err
	}
	var signal *WorkflowSignal
	if value.GetSignal() != nil {
		input := workflowSignalFromProto(value.GetSignal())
		signal = &input
	}
	return &WorkflowRunSignal{
		Run:         run,
		Signal:      signal,
		StartedRun:  value.GetStartedRun(),
		WorkflowKey: value.GetWorkflowKey(),
	}, nil
}

func workflowEventDeliveryResultsToProto(values []WorkflowEventDeliveryResult) ([]*proto.WorkflowEventDeliveryResult, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowEventDeliveryResult, 0, len(values))
	for i := range values {
		run, err := workflowRunToProto(values[i].Run)
		if err != nil {
			return nil, fmt.Errorf("results[%d].run: %w", i, err)
		}
		signal, err := newOptionalWorkflowSignal(values[i].Signal)
		if err != nil {
			return nil, fmt.Errorf("results[%d].signal: %w", i, err)
		}
		out = append(out, &proto.WorkflowEventDeliveryResult{
			DeploymentId: values[i].DeploymentID,
			ActivationId: values[i].ActivationID,
			Run:          run,
			Signal:       signal,
			StartedRun:   values[i].StartedRun,
		})
	}
	return out, nil
}

func workflowEventDeliveryResultsFromProto(values []*proto.WorkflowEventDeliveryResult) ([]WorkflowEventDeliveryResult, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]WorkflowEventDeliveryResult, 0, len(values))
	for i, value := range values {
		if value == nil {
			continue
		}
		run, err := workflowRunFromProto(value.GetRun())
		if err != nil {
			return nil, fmt.Errorf("results[%d].run: %w", i, err)
		}
		var signal *WorkflowSignal
		if value.GetSignal() != nil {
			input := workflowSignalFromProto(value.GetSignal())
			signal = &input
		}
		out = append(out, WorkflowEventDeliveryResult{
			DeploymentID: value.GetDeploymentId(),
			ActivationID: value.GetActivationId(),
			Run:          run,
			Signal:       signal,
			StartedRun:   value.GetStartedRun(),
		})
	}
	return out, nil
}

func workflowRunEventToProto(input WorkflowRunEvent) *proto.WorkflowRunEvent {
	return &proto.WorkflowRunEvent{
		Id:            input.ID,
		RunId:         input.RunID,
		Sequence:      input.Sequence,
		Type:          proto.WorkflowRunEventType(input.Type),
		StepId:        input.StepID,
		ActionId:      input.ActionID,
		AttemptNumber: input.AttemptNumber,
		Message:       input.Message,
		OutputSummary: workflowOutputSummaryToProto(input.OutputSummary),
		OutputRef:     input.OutputRef,
		Error:         workflowRunErrorToProto(input.Error),
		ObservedAt:    timestampFromNonZeroTime(input.ObservedAt),
	}
}

func workflowRunEventFromProto(value *proto.WorkflowRunEvent) WorkflowRunEvent {
	if value == nil {
		return WorkflowRunEvent{}
	}
	return WorkflowRunEvent{
		ID:            value.GetId(),
		RunID:         value.GetRunId(),
		Sequence:      value.GetSequence(),
		Type:          WorkflowRunEventType(value.GetType()),
		StepID:        value.GetStepId(),
		ActionID:      value.GetActionId(),
		AttemptNumber: value.GetAttemptNumber(),
		Message:       value.GetMessage(),
		OutputSummary: workflowOutputSummaryFromProto(value.GetOutputSummary()),
		OutputRef:     value.GetOutputRef(),
		Error:         workflowRunErrorFromProto(value.GetError()),
		ObservedAt:    timeFromTimestamp(value.GetObservedAt()),
	}
}

func workflowRunEventsToProto(values []WorkflowRunEvent) []*proto.WorkflowRunEvent {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.WorkflowRunEvent, 0, len(values))
	for _, value := range values {
		out = append(out, workflowRunEventToProto(value))
	}
	return out
}

func workflowRunEventsFromProto(values []*proto.WorkflowRunEvent) []WorkflowRunEvent {
	if len(values) == 0 {
		return nil
	}
	out := make([]WorkflowRunEvent, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, workflowRunEventFromProto(value))
	}
	return out
}

func workflowRunOutputToProto(input *WorkflowRunOutput) (*proto.WorkflowRunOutput, error) {
	if input == nil {
		return nil, nil
	}
	out := &proto.WorkflowRunOutput{
		OutputRef: input.OutputRef,
		Summary:   workflowOutputSummaryToProto(input.Summary),
	}
	if input.BodySet || input.Body != nil {
		body, err := valueFromAny(input.Body)
		if err != nil {
			return nil, err
		}
		out.Body = body
	}
	return out, nil
}

func workflowRunOutputFromProto(value *proto.WorkflowRunOutput) *WorkflowRunOutput {
	if value == nil {
		return nil
	}
	return &WorkflowRunOutput{
		OutputRef: value.GetOutputRef(),
		Summary:   workflowOutputSummaryFromProto(value.GetSummary()),
		Body:      anyFromValue(value.GetBody()),
		BodySet:   value.GetBody() != nil,
	}
}

func workflowActionResultToProto(input *WorkflowActionResult) *proto.WorkflowActionResult {
	if input == nil {
		return nil
	}
	return &proto.WorkflowActionResult{
		ActionEventId: input.ActionEventID,
		Status:        input.Status,
		Body:          input.Body,
		OutputSummary: workflowOutputSummaryToProto(input.OutputSummary),
		OutputRef:     input.OutputRef,
		Error:         workflowRunErrorToProto(input.Error),
	}
}

func workflowActionResultFromProto(resp *proto.WorkflowActionResult) *WorkflowActionResult {
	if resp == nil {
		return nil
	}
	return &WorkflowActionResult{
		ActionEventID: resp.GetActionEventId(),
		Status:        resp.GetStatus(),
		Body:          resp.GetBody(),
		OutputSummary: workflowOutputSummaryFromProto(resp.GetOutputSummary()),
		OutputRef:     resp.GetOutputRef(),
		Error:         workflowRunErrorFromProto(resp.GetError()),
	}
}

func timestampFromNonZeroTime(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func timestampFromOptionalTime(value *time.Time) *timestamppb.Timestamp {
	if value == nil || value.IsZero() {
		return nil
	}
	return timestamppb.New(*value)
}

func newOptionalBoundWorkflowTarget(input *BoundWorkflowTarget) (*proto.BoundWorkflowTarget, error) {
	if input == nil {
		return nil, nil
	}
	return boundWorkflowTargetToProto(*input)
}

func newOptionalWorkflowRunTrigger(input *WorkflowRunTrigger) (*proto.WorkflowRunTrigger, error) {
	if input == nil {
		return nil, nil
	}
	return workflowRunTriggerToProto(*input)
}

func newOptionalWorkflowEvent(input *WorkflowEvent) (*proto.WorkflowEvent, error) {
	if input == nil {
		return nil, nil
	}
	return workflowEventToProto(*input)
}

func workflowTargetInputPtrFromTarget(value *proto.BoundWorkflowTarget) *BoundWorkflowTarget {
	if value == nil {
		return nil
	}
	input := boundWorkflowTargetFromProto(value)
	return &input
}
