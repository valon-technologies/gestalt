package gestalt

import (
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// BoundWorkflowTarget contains fields for constructing a
// BoundWorkflowTarget. The only target shape is an ordered list of steps.
type BoundWorkflowTarget struct {
	Steps []WorkflowStep
}

// WorkflowStep contains fields for constructing one workflow step. Exactly
// one of App or Agent should be set.
type WorkflowStep struct {
	ID             string
	Inputs         map[string]WorkflowValue
	App            *WorkflowStepAppCall
	Agent          *WorkflowStepAgentTurn
	When           *WorkflowStepWhen
	TimeoutSeconds int32
	Metadata       any
}

// WorkflowStepAppCall contains fields for a app action. Input is a
// WorkflowValue that must resolve to a JSON object at runtime.
type WorkflowStepAppCall struct {
	Name           string
	Operation      string
	Input          WorkflowValue
	Connection     string
	Instance       string
	CredentialMode string
}

// WorkflowStepAgentTurn contains fields for an agent action inside a workflow
// step.
type WorkflowStepAgentTurn struct {
	Provider     string
	Model        string
	SessionKey   string
	Prompt       WorkflowText
	Messages     []WorkflowAgentMessage
	Tools        []AgentToolRef
	Output       *AgentOutput
	ModelOptions any
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
	Literal    any
	LiteralSet bool
	Object     map[string]WorkflowValue
	Array      []WorkflowValue
	Template   *WorkflowText
	Input      string
	Signal     string
	StepOutput *WorkflowStepOutputSource
	StepInput  *WorkflowStepInputSource
}

// WorkflowStepOutputSource references a previous step output envelope.
type WorkflowStepOutputSource struct {
	StepID string
	Path   string
}

// WorkflowStepInputSource references a previous step input envelope.
type WorkflowStepInputSource struct {
	StepID string
	Path   string
}

// boundWorkflowTargetToProto creates a workflow target.
func boundWorkflowTargetToProto(input BoundWorkflowTarget) (*proto.BoundWorkflowTarget, error) {
	steps, err := workflowStepsToProto(input.Steps)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowTarget{Steps: steps}, nil
}

// boundWorkflowTargetFromProto converts an existing protocol target into
// builder input.
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
	out := &proto.WorkflowStep{
		Id:             step.ID,
		Inputs:         inputs,
		When:           when,
		TimeoutSeconds: step.TimeoutSeconds,
		Metadata:       metadata,
	}
	switch {
	case step.App != nil && step.Agent != nil:
		return nil, fmt.Errorf("cannot set both app and agent")
	case step.App != nil:
		app, err := workflowStepAppCallToProto(step.App)
		if err != nil {
			return nil, fmt.Errorf("app: %w", err)
		}
		out.Action = &proto.WorkflowStep_App{App: app}
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
		Metadata:       mapFromStruct(step.GetMetadata()),
	}
	if step.GetApp() != nil {
		out.App = workflowStepAppCallFromProto(step.GetApp())
	}
	if step.GetAgent() != nil {
		out.Agent = workflowStepAgentTurnFromProto(step.GetAgent())
	}
	return out
}

func workflowStepAppCallToProto(input *WorkflowStepAppCall) (*proto.WorkflowStepAppCall, error) {
	if input == nil {
		return nil, nil
	}
	value, err := workflowValueToProto(input.Input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	return &proto.WorkflowStepAppCall{
		Name:           input.Name,
		Operation:      input.Operation,
		Input:          value,
		Connection:     input.Connection,
		Instance:       input.Instance,
		CredentialMode: input.CredentialMode,
	}, nil
}

func workflowStepAppCallFromProto(value *proto.WorkflowStepAppCall) *WorkflowStepAppCall {
	if value == nil {
		return nil
	}
	return &WorkflowStepAppCall{
		Name:           value.GetName(),
		Operation:      value.GetOperation(),
		Input:          workflowValueFromProto(value.GetInput()),
		Connection:     value.GetConnection(),
		Instance:       value.GetInstance(),
		CredentialMode: value.GetCredentialMode(),
	}
}

func workflowStepAgentTurnToProto(input *WorkflowStepAgentTurn) (*proto.WorkflowStepAgentTurn, error) {
	if input == nil {
		return nil, nil
	}
	messages, err := workflowAgentMessagesToProto(input.Messages)
	if err != nil {
		return nil, err
	}
	output, err := agentOutputToProto(input.Output)
	if err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	modelOptions, err := structFromAny(input.ModelOptions)
	if err != nil {
		return nil, fmt.Errorf("model_options: %w", err)
	}
	return &proto.WorkflowStepAgentTurn{
		Provider:     input.Provider,
		Model:        input.Model,
		SessionKey:   input.SessionKey,
		Prompt:       workflowTextToProto(input.Prompt),
		Messages:     messages,
		Tools:        agentToolRefsToProto(input.Tools),
		Output:       output,
		ModelOptions: modelOptions,
	}, nil
}

func workflowStepAgentTurnFromProto(value *proto.WorkflowStepAgentTurn) *WorkflowStepAgentTurn {
	if value == nil {
		return nil
	}
	return &WorkflowStepAgentTurn{
		Provider:     value.GetProvider(),
		Model:        value.GetModel(),
		SessionKey:   value.GetSessionKey(),
		Prompt:       workflowTextFromProto(value.GetPrompt()),
		Messages:     workflowAgentMessagesFromProto(value.GetMessages()),
		Tools:        agentToolRefsFromProto(value.GetTools()),
		Output:       agentOutputFromProto(value.GetOutput()),
		ModelOptions: mapFromStruct(value.GetModelOptions()),
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
	if strings.TrimSpace(input.Input) != "" {
		set++
	}
	if strings.TrimSpace(input.Signal) != "" {
		set++
	}
	if input.StepOutput != nil {
		set++
	}
	if input.StepInput != nil {
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
	case strings.TrimSpace(input.Input) != "":
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Input{Input: &proto.WorkflowPathSource{Path: input.Input}}}, nil
	case strings.TrimSpace(input.Signal) != "":
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Signal{Signal: &proto.WorkflowPathSource{Path: input.Signal}}}, nil
	case input.StepOutput != nil:
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_StepOutput{StepOutput: &proto.WorkflowStepOutputSource{
			StepId: input.StepOutput.StepID,
			Path:   input.StepOutput.Path,
		}}}, nil
	case input.StepInput != nil:
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_StepInput{StepInput: &proto.WorkflowStepInputSource{
			StepId: input.StepInput.StepID,
			Path:   input.StepInput.Path,
		}}}, nil
	default:
		return nil, nil
	}
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
	case *proto.WorkflowValue_Input:
		return WorkflowValue{Input: workflowPathSourcePath(typed.Input)}
	case *proto.WorkflowValue_Signal:
		return WorkflowValue{Signal: workflowPathSourcePath(typed.Signal)}
	case *proto.WorkflowValue_StepOutput:
		if typed.StepOutput == nil {
			return WorkflowValue{StepOutput: &WorkflowStepOutputSource{}}
		}
		return WorkflowValue{StepOutput: &WorkflowStepOutputSource{
			StepID: typed.StepOutput.GetStepId(),
			Path:   typed.StepOutput.GetPath(),
		}}
	case *proto.WorkflowValue_StepInput:
		if typed.StepInput == nil {
			return WorkflowValue{StepInput: &WorkflowStepInputSource{}}
		}
		return WorkflowValue{StepInput: &WorkflowStepInputSource{
			StepID: typed.StepInput.GetStepId(),
			Path:   typed.StepInput.GetPath(),
		}}
	default:
		return WorkflowValue{}
	}
}

func workflowPathSourcePath(value *proto.WorkflowPathSource) string {
	if value == nil {
		return ""
	}
	return value.GetPath()
}

// WorkflowEvent contains fields for constructing a WorkflowEvent.
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

// workflowEventToProto creates a workflow event.
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

// workflowEventFromProto converts an existing protocol event into builder input.
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

// WorkflowSignal contains fields for constructing a
// WorkflowSignal.
type WorkflowSignal struct {
	ID             string
	Name           string
	Payload        any
	Metadata       any
	CreatedBy      string
	CreatedAt      time.Time
	IdempotencyKey string
	Sequence       int64
}

// workflowSignalToProto creates a workflow signal.
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
		CreatedAt:      timestampFromNonZeroTime(input.CreatedAt),
		IdempotencyKey: input.IdempotencyKey,
		Sequence:       input.Sequence,
	}, nil
}

// workflowSignalFromProto converts an existing protocol signal into builder input.
func workflowSignalFromProto(value *proto.WorkflowSignal) WorkflowSignal {
	if value == nil {
		return WorkflowSignal{}
	}
	return WorkflowSignal{
		ID:             value.GetId(),
		Name:           value.GetName(),
		Payload:        mapFromStruct(value.GetPayload()),
		Metadata:       mapFromStruct(value.GetMetadata()),
		CreatedAt:      timeFromTimestamp(value.GetCreatedAt()),
		IdempotencyKey: value.GetIdempotencyKey(),
		Sequence:       value.GetSequence(),
	}
}

// WorkflowScheduleTrigger contains fields for constructing a
// schedule-triggered workflow run trigger.
type WorkflowScheduleTrigger struct {
	ActivationID string
	ScheduledFor *time.Time
}

// WorkflowEventTriggerInvocation contains fields for
// constructing an event-triggered workflow run trigger.
type WorkflowEventTriggerInvocation struct {
	ActivationID string
	Event        *WorkflowEvent
}

// WorkflowRunTrigger contains fields for constructing a
// workflow run trigger. Exactly one trigger kind should be set.
type WorkflowRunTrigger struct {
	Manual   bool
	Schedule *WorkflowScheduleTrigger
	Event    *WorkflowEventTriggerInvocation
}

// workflowRunTriggerToProto creates a workflow run trigger.
func workflowRunTriggerToProto(input WorkflowRunTrigger) (*proto.WorkflowRunTrigger, error) {
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
		return &proto.WorkflowRunTrigger{}, nil
	}
	if selected > 1 {
		return nil, fmt.Errorf("workflow run trigger must set exactly one trigger kind")
	}
	if input.Manual {
		return &proto.WorkflowRunTrigger{Kind: &proto.WorkflowRunTrigger_Manual{Manual: &proto.WorkflowManualTrigger{}}}, nil
	}
	if input.Schedule != nil {
		return &proto.WorkflowRunTrigger{Kind: &proto.WorkflowRunTrigger_Schedule{Schedule: &proto.WorkflowScheduleTrigger{
			ActivationId: input.Schedule.ActivationID,
			ScheduledFor: timestampFromOptionalTime(input.Schedule.ScheduledFor),
		}}}, nil
	}

	var event *proto.WorkflowEvent
	if input.Event.Event != nil {
		value, err := workflowEventToProto(*input.Event.Event)
		if err != nil {
			return nil, err
		}
		event = value
	}
	return &proto.WorkflowRunTrigger{Kind: &proto.WorkflowRunTrigger_Event{Event: &proto.WorkflowEventTriggerInvocation{
		ActivationId: input.Event.ActivationID,
		Event:        event,
	}}}, nil
}

// workflowRunTriggerFromProto converts an existing protocol trigger into
// builder input.
func workflowRunTriggerFromProto(value *proto.WorkflowRunTrigger) (WorkflowRunTrigger, error) {
	if value == nil {
		return WorkflowRunTrigger{}, nil
	}
	switch kind := value.GetKind().(type) {
	case *proto.WorkflowRunTrigger_Manual:
		return WorkflowRunTrigger{Manual: true}, nil
	case *proto.WorkflowRunTrigger_Schedule:
		if kind.Schedule == nil {
			return WorkflowRunTrigger{}, nil
		}
		scheduledFor, err := timePtrFromTimestamp(kind.Schedule.GetScheduledFor())
		if err != nil {
			return WorkflowRunTrigger{}, err
		}
		return WorkflowRunTrigger{Schedule: &WorkflowScheduleTrigger{
			ActivationID: kind.Schedule.GetActivationId(),
			ScheduledFor: scheduledFor,
		}}, nil
	case *proto.WorkflowRunTrigger_Event:
		if kind.Event == nil {
			return WorkflowRunTrigger{}, nil
		}
		var event *WorkflowEvent
		if kind.Event.GetEvent() != nil {
			input := workflowEventFromProto(kind.Event.GetEvent())
			event = &input
		}
		return WorkflowRunTrigger{Event: &WorkflowEventTriggerInvocation{
			ActivationID: kind.Event.GetActivationId(),
			Event:        event,
		}}, nil
	default:
		return WorkflowRunTrigger{}, nil
	}
}

// cloneWorkflowRunTriggerProto creates a copy of an existing workflow run
// trigger.
func cloneWorkflowRunTriggerProto(value *proto.WorkflowRunTrigger) (*proto.WorkflowRunTrigger, error) {
	input, err := workflowRunTriggerFromProto(value)
	if err != nil || value == nil {
		return nil, err
	}
	return workflowRunTriggerToProto(input)
}

// WorkflowScheduleActivation contains a cron activation on a workflow
// definition.
type WorkflowScheduleActivation struct {
	Cron     string
	Timezone string
}

// WorkflowEventActivation contains an event activation on a workflow
// definition.
type WorkflowEventActivation struct {
	Match *WorkflowEventMatch
}

// WorkflowActivation contains one schedule or event activation for a
// definition. Input is evaluated when the activation fires and becomes
// run.input.
type WorkflowActivation struct {
	ID       string
	Input    WorkflowValue
	Paused   bool
	Schedule *WorkflowScheduleActivation
	Event    *WorkflowEventActivation
}

// WorkflowDefinitionSpec is the authored desired state applied atomically to
// provider-owned definition storage.
type WorkflowDefinitionSpec struct {
	ID          string
	Target      *BoundWorkflowTarget
	Activations []WorkflowActivation
	Paused      bool
	RunAs       string
}

// WorkflowDefinition is the provider-owned definition projection.
type WorkflowDefinition struct {
	ID           string
	Generation   int64
	Target       *BoundWorkflowTarget
	Activations  []WorkflowActivation
	Paused       bool
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ProviderName string
	RunAs        *Subject
}

func workflowDefinitionSpecToProto(input WorkflowDefinitionSpec) (*proto.WorkflowDefinitionSpec, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	activations, err := workflowActivationsToProto(input.Activations)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowDefinitionSpec{
		Id:          input.ID,
		Target:      target,
		Activations: activations,
		Paused:      input.Paused,
		RunAs:       strings.TrimSpace(input.RunAs),
	}, nil
}

func workflowDefinitionSpecFromProto(value *proto.WorkflowDefinitionSpec) (WorkflowDefinitionSpec, error) {
	if value == nil {
		return WorkflowDefinitionSpec{}, nil
	}
	activations, err := workflowActivationsFromProto(value.GetActivations())
	if err != nil {
		return WorkflowDefinitionSpec{}, err
	}
	return WorkflowDefinitionSpec{
		ID:          value.GetId(),
		Target:      workflowTargetInputPtrFromTarget(value.GetTarget()),
		Activations: activations,
		Paused:      value.GetPaused(),
		RunAs:       strings.TrimSpace(value.GetRunAs()),
	}, nil
}

func workflowDefinitionToProto(input WorkflowDefinition) (*proto.WorkflowDefinition, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	activations, err := workflowActivationsToProto(input.Activations)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowDefinition{
		Id:          input.ID,
		Generation:  input.Generation,
		Target:      target,
		Activations: activations,
		Paused:      input.Paused,
		CreatedAt:   timestampFromNonZeroTime(input.CreatedAt),
		UpdatedAt:   timestampFromNonZeroTime(input.UpdatedAt),
		Provider:    input.ProviderName,
		RunAs:       workflowSubjectID(input.RunAs),
	}, nil
}

func workflowActivationsToProto(values []WorkflowActivation) ([]*proto.WorkflowActivation, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowActivation, 0, len(values))
	for i := range values {
		activation, err := workflowActivationToProto(values[i])
		if err != nil {
			return nil, fmt.Errorf("activations[%d]: %w", i, err)
		}
		out = append(out, activation)
	}
	return out, nil
}

func workflowActivationToProto(input WorkflowActivation) (*proto.WorkflowActivation, error) {
	value, err := workflowValueToProto(input.Input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	out := &proto.WorkflowActivation{Id: input.ID, Input: value, Paused: input.Paused}
	switch {
	case input.Schedule != nil && input.Event != nil:
		return nil, fmt.Errorf("activation must set exactly one of schedule or event")
	case input.Schedule != nil:
		out.Trigger = &proto.WorkflowActivation_Schedule{Schedule: &proto.WorkflowScheduleActivation{
			Cron:     input.Schedule.Cron,
			Timezone: input.Schedule.Timezone,
		}}
	case input.Event != nil:
		out.Trigger = &proto.WorkflowActivation_Event{Event: &proto.WorkflowEventActivation{
			Match: workflowEventMatchFromInput(input.Event.Match),
		}}
	default:
		return nil, fmt.Errorf("activation must set schedule or event")
	}
	return out, nil
}

func workflowActivationsFromProto(values []*proto.WorkflowActivation) ([]WorkflowActivation, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]WorkflowActivation, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, workflowActivationFromProto(value))
	}
	return out, nil
}

func workflowActivationFromProto(value *proto.WorkflowActivation) WorkflowActivation {
	out := WorkflowActivation{
		ID:     value.GetId(),
		Input:  workflowValueFromProto(value.GetInput()),
		Paused: value.GetPaused(),
	}
	if schedule := value.GetSchedule(); schedule != nil {
		out.Schedule = &WorkflowScheduleActivation{Cron: schedule.GetCron(), Timezone: schedule.GetTimezone()}
	}
	if event := value.GetEvent(); event != nil {
		out.Event = &WorkflowEventActivation{Match: workflowEventMatchInputPtrFromMatch(event.GetMatch())}
	}
	return out
}

// WorkflowStepAttempt contains one durable attempt for a workflow step.
type WorkflowStepAttempt struct {
	ID             string
	Status         WorkflowStepStatus
	IdempotencyKey string
	Input          any
	Output         any
	StatusMessage  string
	StartedAt      *time.Time
	CompletedAt    *time.Time
}

// WorkflowStepExecution contains persisted state for one workflow step.
type WorkflowStepExecution struct {
	StepID        string
	Status        WorkflowStepStatus
	Attempts      []WorkflowStepAttempt
	Input         any
	Output        any
	StatusMessage string
	SkipReason    string
	StartedAt     *time.Time
	CompletedAt   *time.Time
}

// WorkflowRun contains a provider-owned workflow run projection.
type WorkflowRun struct {
	ID                   string
	Status               WorkflowRunStatus
	Target               *BoundWorkflowTarget
	Trigger              *WorkflowRunTrigger
	CreatedAt            time.Time
	StartedAt            *time.Time
	CompletedAt          *time.Time
	StatusMessage        string
	Output               any
	CreatedBy            string
	RunAs                *Subject
	WorkflowKey          string
	ProviderName         string
	DefinitionID         string
	Input                map[string]any
	DefinitionGeneration int64
	CurrentStepID        string
	Steps                []WorkflowStepExecution
}

func workflowRunToProto(input WorkflowRun) (*proto.WorkflowRun, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	trigger, err := newOptionalWorkflowRunTrigger(input.Trigger)
	if err != nil {
		return nil, err
	}
	output, err := valueFromAny(input.Output)
	if err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	inputStruct, err := structFromAny(input.Input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	steps, err := workflowStepExecutionsToProto(input.Steps)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowRun{
		Id:                   input.ID,
		Status:               proto.WorkflowRunStatus(input.Status),
		Target:               target,
		Trigger:              trigger,
		CreatedAt:            timestampFromNonZeroTime(input.CreatedAt),
		StartedAt:            timestampFromOptionalTime(input.StartedAt),
		CompletedAt:          timestampFromOptionalTime(input.CompletedAt),
		StatusMessage:        input.StatusMessage,
		Output:               output,
		RunAs:                workflowSubjectID(input.RunAs),
		WorkflowKey:          input.WorkflowKey,
		Provider:             input.ProviderName,
		DefinitionId:         input.DefinitionID,
		Input:                inputStruct,
		DefinitionGeneration: input.DefinitionGeneration,
		CurrentStepId:        input.CurrentStepID,
		Steps:                steps,
	}, nil
}

func workflowStepExecutionsToProto(values []WorkflowStepExecution) ([]*proto.WorkflowStepExecution, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowStepExecution, 0, len(values))
	for i := range values {
		value, err := workflowStepExecutionToProto(values[i])
		if err != nil {
			return nil, fmt.Errorf("steps[%d]: %w", i, err)
		}
		out = append(out, value)
	}
	return out, nil
}

func workflowStepExecutionToProto(input WorkflowStepExecution) (*proto.WorkflowStepExecution, error) {
	attempts, err := workflowStepAttemptsToProto(input.Attempts)
	if err != nil {
		return nil, err
	}
	stepInput, err := valueFromAny(input.Input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	output, err := valueFromAny(input.Output)
	if err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	return &proto.WorkflowStepExecution{
		StepId:        input.StepID,
		Status:        proto.WorkflowStepStatus(input.Status),
		Attempts:      attempts,
		Input:         stepInput,
		Output:        output,
		StatusMessage: input.StatusMessage,
		SkipReason:    input.SkipReason,
		StartedAt:     timestampFromOptionalTime(input.StartedAt),
		CompletedAt:   timestampFromOptionalTime(input.CompletedAt),
	}, nil
}

func workflowStepAttemptsToProto(values []WorkflowStepAttempt) ([]*proto.WorkflowStepAttempt, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowStepAttempt, 0, len(values))
	for i := range values {
		value, err := workflowStepAttemptToProto(values[i])
		if err != nil {
			return nil, fmt.Errorf("attempts[%d]: %w", i, err)
		}
		out = append(out, value)
	}
	return out, nil
}

func workflowStepAttemptToProto(input WorkflowStepAttempt) (*proto.WorkflowStepAttempt, error) {
	stepInput, err := valueFromAny(input.Input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	output, err := valueFromAny(input.Output)
	if err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	return &proto.WorkflowStepAttempt{
		Id:             input.ID,
		Status:         proto.WorkflowStepStatus(input.Status),
		IdempotencyKey: input.IdempotencyKey,
		Input:          stepInput,
		Output:         output,
		StatusMessage:  input.StatusMessage,
		StartedAt:      timestampFromOptionalTime(input.StartedAt),
		CompletedAt:    timestampFromOptionalTime(input.CompletedAt),
	}, nil
}

// WorkflowRunEvent contains one provider-persisted run event.
type WorkflowRunEvent struct {
	ID        string
	RunID     string
	StepID    string
	Type      string
	Data      any
	CreatedAt time.Time
}

func workflowRunEventToProto(input WorkflowRunEvent) (*proto.WorkflowRunEvent, error) {
	data, err := structFromAny(input.Data)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowRunEvent{
		Id:        input.ID,
		RunId:     input.RunID,
		StepId:    input.StepID,
		Type:      input.Type,
		Data:      data,
		CreatedAt: timestampFromNonZeroTime(input.CreatedAt),
	}, nil
}

// WorkflowEventMatch contains fields for matching workflow
// events.
type WorkflowEventMatch struct {
	Type    string
	Source  string
	Subject string
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

func workflowTargetInputPtrFromTarget(value *proto.BoundWorkflowTarget) *BoundWorkflowTarget {
	if value == nil {
		return nil
	}
	input := boundWorkflowTargetFromProto(value)
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

func workflowSubjectID(value *Subject) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(value.ID)
}

func workflowSubjectFromID(value string) *Subject {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &Subject{ID: value}
}

// WorkflowRefInput references a run-input path in a workflow definition.
func WorkflowRefInput(path string) WorkflowValue {
	return WorkflowValue{Input: strings.TrimSpace(path)}
}

// WorkflowRefSignal references an activation signal path in a workflow definition.
func WorkflowRefSignal(path string) WorkflowValue {
	return WorkflowValue{Signal: strings.TrimSpace(path)}
}

// WorkflowRefStepOutput references a prior step output path.
func WorkflowRefStepOutput(stepID, path string) WorkflowValue {
	return WorkflowValue{StepOutput: &WorkflowStepOutputSource{
		StepID: strings.TrimSpace(stepID),
		Path:   strings.TrimSpace(path),
	}}
}

// WorkflowRefStepInput references a prior step input path.
func WorkflowRefStepInput(stepID, path string) WorkflowValue {
	return WorkflowValue{StepInput: &WorkflowStepInputSource{
		StepID: strings.TrimSpace(stepID),
		Path:   strings.TrimSpace(path),
	}}
}

// WorkflowRefLiteral builds a literal workflow value.
func WorkflowRefLiteral(value any) WorkflowValue {
	return WorkflowValue{Literal: value, LiteralSet: true}
}

// WorkflowRefTemplate builds a template workflow value.
func WorkflowRefTemplate(template string) WorkflowValue {
	text := WorkflowText{Template: template}
	return WorkflowValue{Template: &text}
}

// WorkflowRefObject builds an object workflow value from field builders.
func WorkflowRefObject(fields map[string]WorkflowValue) WorkflowValue {
	return WorkflowValue{Object: fields}
}

// WorkflowRefArray builds an array workflow value.
func WorkflowRefArray(values ...WorkflowValue) WorkflowValue {
	return WorkflowValue{Array: values}
}

// WorkflowEventActivationOptions configures an event activation.
type WorkflowEventActivationOptions struct {
	ID      string
	Source  string
	Subject string
	Paused  bool
}

// WorkflowScheduleActivationOptions configures a schedule activation.
type WorkflowScheduleActivationOptions struct {
	ID       string
	Timezone string
	Paused   bool
}

// WorkflowEventActivationConfig is accepted by WorkflowBuilder.On.
type WorkflowEventActivationConfig struct {
	Type     string
	MapInput func() map[string]WorkflowValue
	Options  WorkflowEventActivationOptions
}

// WorkflowScheduleActivationConfig is accepted by WorkflowBuilder.On.
type WorkflowScheduleActivationConfig struct {
	Cron     string
	MapInput func() map[string]WorkflowValue
	Options  WorkflowScheduleActivationOptions
}

// WorkflowActivationConfig is an activation accepted by WorkflowBuilder.On.
type WorkflowActivationConfig interface {
	workflowActivationConfig()
}

func (WorkflowEventActivationConfig) workflowActivationConfig()    {}
func (WorkflowScheduleActivationConfig) workflowActivationConfig() {}

// WorkflowStepAppConfig configures an app step action.
type WorkflowStepAppConfig struct {
	Name           string
	Operation      string
	Input          func() map[string]WorkflowValue
	Connection     string
	Instance       string
	CredentialMode string
}

// WorkflowStepAgentMessageConfig configures one agent message.
type WorkflowStepAgentMessageConfig struct {
	Role string
	Text string
}

// WorkflowStepAgentConfig configures an agent step action.
type WorkflowStepAgentConfig struct {
	Provider     string
	Model        string
	SessionKey   string
	Prompt       string
	Messages     []WorkflowStepAgentMessageConfig
	Tools        []AgentToolRef
	Output       *AgentOutput
	ModelOptions any
}

// WorkflowStepWhenConfig configures a step guard.
type WorkflowStepWhenConfig struct {
	Value  WorkflowValue
	Equals any
}

// WorkflowStepConfig configures one workflow step.
type WorkflowStepConfig struct {
	Inputs         func() map[string]WorkflowValue
	App            *WorkflowStepAppConfig
	Agent          *WorkflowStepAgentConfig
	When           *WorkflowStepWhenConfig
	TimeoutSeconds int32
	Metadata       any
}

// WorkflowBuilder incrementally authors a workflow definition spec.
type WorkflowBuilder struct {
	id          string
	runAs       string
	activations []WorkflowActivation
	steps       []WorkflowStep
}

// Event creates an event activation configuration. The options argument is
// optional; its zero value is used when it is omitted.
func Event(typeName string, mapInput func() map[string]WorkflowValue, options ...WorkflowEventActivationOptions) WorkflowEventActivationConfig {
	var config WorkflowEventActivationOptions
	if len(options) > 0 {
		config = options[0]
	}
	return WorkflowEventActivationConfig{
		Type:     typeName,
		MapInput: mapInput,
		Options:  config,
	}
}

// Schedule creates a schedule activation configuration. The options argument
// is optional; its zero value is used when it is omitted.
func Schedule(cron string, mapInput func() map[string]WorkflowValue, options ...WorkflowScheduleActivationOptions) WorkflowScheduleActivationConfig {
	var config WorkflowScheduleActivationOptions
	if len(options) > 0 {
		config = options[0]
	}
	return WorkflowScheduleActivationConfig{
		Cron:     cron,
		MapInput: mapInput,
		Options:  config,
	}
}

// DefineWorkflow starts a fluent workflow definition builder. ID and runAs
// are checked when ToSpec or an apply bridge materializes the definition.
func DefineWorkflow(id, runAs string) *WorkflowBuilder {
	return &WorkflowBuilder{
		id:    strings.TrimSpace(id),
		runAs: strings.TrimSpace(runAs),
	}
}

// On appends one activation to the builder.
func (b *WorkflowBuilder) On(activation WorkflowActivationConfig) *WorkflowBuilder {
	if b == nil {
		return b
	}
	switch typed := activation.(type) {
	case WorkflowEventActivationConfig:
		activationID := strings.TrimSpace(typed.Options.ID)
		if activationID == "" {
			activationID = strings.TrimSpace(typed.Type)
		}
		var input WorkflowValue
		if typed.MapInput != nil {
			input = WorkflowRefObject(typed.MapInput())
		}
		b.activations = append(b.activations, WorkflowActivation{
			ID:     activationID,
			Paused: typed.Options.Paused,
			Event: &WorkflowEventActivation{Match: &WorkflowEventMatch{
				Type:    typed.Type,
				Source:  typed.Options.Source,
				Subject: typed.Options.Subject,
			}},
			Input: input,
		})
	case WorkflowScheduleActivationConfig:
		activationID := strings.TrimSpace(typed.Options.ID)
		if activationID == "" {
			activationID = strings.TrimSpace(typed.Cron)
		}
		var input WorkflowValue
		if typed.MapInput != nil {
			input = WorkflowRefObject(typed.MapInput())
		}
		b.activations = append(b.activations, WorkflowActivation{
			ID:     activationID,
			Paused: typed.Options.Paused,
			Schedule: &WorkflowScheduleActivation{
				Cron:     typed.Cron,
				Timezone: typed.Options.Timezone,
			},
			Input: input,
		})
	}
	return b
}

// Step appends one step to the builder.
func (b *WorkflowBuilder) Step(stepID string, config WorkflowStepConfig) *WorkflowBuilder {
	if b == nil {
		return b
	}
	if config.App != nil && config.Agent != nil {
		panic("workflow step cannot configure both app and agent actions")
	}
	step := WorkflowStep{ID: stepID}
	if config.Inputs != nil {
		step.Inputs = config.Inputs()
	}
	if config.App != nil {
		var input WorkflowValue
		if config.App.Input != nil {
			input = WorkflowRefObject(config.App.Input())
		}
		step.App = &WorkflowStepAppCall{
			Name:           config.App.Name,
			Operation:      config.App.Operation,
			Input:          input,
			Connection:     config.App.Connection,
			Instance:       config.App.Instance,
			CredentialMode: config.App.CredentialMode,
		}
	}
	if config.Agent != nil {
		messages := make([]WorkflowAgentMessage, 0, len(config.Agent.Messages))
		for _, message := range config.Agent.Messages {
			messages = append(messages, WorkflowAgentMessage{
				Role: message.Role,
				Text: WorkflowText{Template: message.Text},
			})
		}
		step.Agent = &WorkflowStepAgentTurn{
			Provider:     config.Agent.Provider,
			Model:        config.Agent.Model,
			SessionKey:   config.Agent.SessionKey,
			Prompt:       WorkflowText{Template: config.Agent.Prompt},
			Messages:     messages,
			Tools:        config.Agent.Tools,
			Output:       config.Agent.Output,
			ModelOptions: config.Agent.ModelOptions,
		}
	}
	if config.When != nil {
		step.When = &WorkflowStepWhen{
			Value:  config.When.Value,
			Equals: config.When.Equals,
		}
	}
	step.TimeoutSeconds = config.TimeoutSeconds
	step.Metadata = config.Metadata
	b.steps = append(b.steps, step)
	return b
}

// Validate checks the required identity fields of a workflow definition.
func (s WorkflowDefinitionSpec) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("workflow definition requires ID")
	}
	if strings.TrimSpace(s.RunAs) == "" {
		return fmt.Errorf("workflow definition requires RunAs")
	}
	return nil
}

// ToSpec returns the authored workflow definition spec after validating its
// required identity fields.
func (b *WorkflowBuilder) ToSpec() (WorkflowDefinitionSpec, error) {
	if b == nil {
		return WorkflowDefinitionSpec{}, fmt.Errorf("workflow builder is nil")
	}
	var target *BoundWorkflowTarget
	if len(b.steps) > 0 {
		target = &BoundWorkflowTarget{Steps: append([]WorkflowStep(nil), b.steps...)}
	}
	spec := WorkflowDefinitionSpec{
		ID:          strings.TrimSpace(b.id),
		RunAs:       strings.TrimSpace(b.runAs),
		Activations: append([]WorkflowActivation(nil), b.activations...),
		Target:      target,
	}
	if err := spec.Validate(); err != nil {
		return WorkflowDefinitionSpec{}, err
	}
	return spec, nil
}

// Text builds workflow template text from literals and workflow value
// references. Step output and input references intentionally lower to the
// plural outputs and inputs template paths.
func Text(parts ...any) string {
	var builder strings.Builder
	for _, part := range parts {
		switch typed := part.(type) {
		case string:
			builder.WriteString(typed)
		case WorkflowValue:
			builder.WriteString(workflowValueToTemplatePlaceholder(typed))
		default:
			panic(fmt.Sprintf("Text parts must be strings or WorkflowValue, got %T", part))
		}
	}
	return builder.String()
}

// WorkflowComposeText is kept as a compatibility alias for Text.
func WorkflowComposeText(parts ...any) string {
	return Text(parts...)
}

func workflowValueToTemplatePlaceholder(value WorkflowValue) string {
	if value.Input != "" {
		return fmt.Sprintf("${{ input.%s }}", strings.TrimSpace(value.Input))
	}
	if value.Signal != "" {
		return fmt.Sprintf("${{ signal.%s }}", strings.TrimSpace(value.Signal))
	}
	if value.StepOutput != nil {
		return fmt.Sprintf(
			"${{ steps.%s.outputs.%s }}",
			strings.TrimSpace(value.StepOutput.StepID),
			strings.TrimSpace(value.StepOutput.Path),
		)
	}
	if value.StepInput != nil {
		return fmt.Sprintf(
			"${{ steps.%s.inputs.%s }}",
			strings.TrimSpace(value.StepInput.StepID),
			strings.TrimSpace(value.StepInput.Path),
		)
	}
	panic("Text references must be input, signal, step output, or step input paths")
}
