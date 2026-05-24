package gestalt

import (
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	Provider       string
	Model          string
	SessionKey     string
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
	Literal       any
	LiteralSet    bool
	Object        map[string]WorkflowValue
	Array         []WorkflowValue
	Template      *WorkflowText
	RunInput      string
	SignalPayload string
	StepOutput    *WorkflowStepOutputSource
}

// WorkflowStepOutputSource references a previous step output envelope.
type WorkflowStepOutputSource struct {
	StepID string
	Path   string
}

// WorkflowActor contains fields for constructing workflow actor
// metadata.
type WorkflowActor struct {
	SubjectID   string
	SubjectKind string
	DisplayName string
	AuthSource  string
}

// workflowActorToProto creates workflow actor metadata.
func workflowActorToProto(input WorkflowActor) *proto.WorkflowActor {
	return &proto.WorkflowActor{
		SubjectId:   input.SubjectID,
		SubjectKind: input.SubjectKind,
		DisplayName: input.DisplayName,
		AuthSource:  input.AuthSource,
	}
}

// workflowActorFromProto converts existing workflow actor metadata into
// builder input.
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

// cloneBoundWorkflowTargetProto creates a copy of an existing workflow target
// through the target input builder.
func cloneBoundWorkflowTargetProto(value *proto.BoundWorkflowTarget) (*proto.BoundWorkflowTarget, error) {
	if value == nil {
		return nil, nil
	}
	return boundWorkflowTargetToProto(boundWorkflowTargetFromProto(value))
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
		SessionKey:     input.SessionKey,
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
		SessionKey:     value.GetSessionKey(),
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
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_RunInput{RunInput: &proto.WorkflowPathSource{Path: input.RunInput}}}, nil
	case strings.TrimSpace(input.SignalPayload) != "":
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_SignalPayload{SignalPayload: &proto.WorkflowPathSource{Path: input.SignalPayload}}}, nil
	case input.StepOutput != nil:
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_StepOutput{StepOutput: &proto.WorkflowStepOutputSource{
			StepId: input.StepOutput.StepID,
			Path:   input.StepOutput.Path,
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

// cloneWorkflowEventProto creates a copy of an existing workflow event through
// the event input builder.
func cloneWorkflowEventProto(value *proto.WorkflowEvent) (*proto.WorkflowEvent, error) {
	if value == nil {
		return nil, nil
	}
	return workflowEventToProto(workflowEventFromProto(value))
}

// WorkflowSignal contains fields for constructing a
// WorkflowSignal.
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
		CreatedBy:      workflowActorFromInput(input.CreatedBy),
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
		CreatedBy:      workflowActorInputPtrFromActor(value.GetCreatedBy()),
		CreatedAt:      timeFromTimestamp(value.GetCreatedAt()),
		IdempotencyKey: value.GetIdempotencyKey(),
		Sequence:       value.GetSequence(),
	}
}

// cloneWorkflowSignalProto creates a copy of an existing workflow signal
// through the signal input builder.
func cloneWorkflowSignalProto(value *proto.WorkflowSignal) (*proto.WorkflowSignal, error) {
	if value == nil {
		return nil, nil
	}
	return workflowSignalToProto(workflowSignalFromProto(value))
}

// WorkflowScheduleTrigger contains fields for constructing a
// schedule-triggered workflow run trigger.
type WorkflowScheduleTrigger struct {
	ScheduleID   string
	ScheduledFor *time.Time
}

// WorkflowEventTriggerInvocation contains fields for
// constructing an event-triggered workflow run trigger.
type WorkflowEventTriggerInvocation struct {
	TriggerID string
	Event     *WorkflowEvent
}

// WorkflowRunTrigger contains fields for constructing a
// workflow run trigger. Exactly one trigger kind should be set.
type WorkflowRunTrigger struct {
	Manual   bool
	Schedule *WorkflowScheduleTrigger
	Event    *WorkflowEventTriggerInvocation
}

// workflowScheduleTriggerToProto creates a schedule-trigger run trigger.
func workflowScheduleTriggerToProto(scheduleID string, scheduledFor time.Time) *proto.WorkflowRunTrigger {
	return &proto.WorkflowRunTrigger{Kind: &proto.WorkflowRunTrigger_Schedule{Schedule: &proto.WorkflowScheduleTrigger{
		ScheduleId:   scheduleID,
		ScheduledFor: timestampFromNonZeroTime(scheduledFor),
	}}}
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
			ScheduleId:   input.Schedule.ScheduleID,
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
		TriggerId: input.Event.TriggerID,
		Event:     event,
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
			ScheduleID:   kind.Schedule.GetScheduleId(),
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
			TriggerID: kind.Event.GetTriggerId(),
			Event:     event,
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

// BoundWorkflowRun contains fields for constructing a
// BoundWorkflowRun.
type BoundWorkflowRun struct {
	ID            string
	Status        WorkflowRunStatus
	Target        *BoundWorkflowTarget
	Trigger       *WorkflowRunTrigger
	CreatedAt     time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
	StatusMessage string
	ResultBody    string
	CreatedBy     *WorkflowActor
	ExecutionRef  string
	WorkflowKey   string
	ProviderName  string
}

// boundWorkflowRunToProto creates a bound workflow run.
func boundWorkflowRunToProto(input BoundWorkflowRun) (*proto.BoundWorkflowRun, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	trigger, err := newOptionalWorkflowRunTrigger(input.Trigger)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowRun{
		Id:            input.ID,
		Status:        proto.WorkflowRunStatus(input.Status),
		Target:        target,
		Trigger:       trigger,
		CreatedAt:     timestampFromNonZeroTime(input.CreatedAt),
		StartedAt:     timestampFromOptionalTime(input.StartedAt),
		CompletedAt:   timestampFromOptionalTime(input.CompletedAt),
		StatusMessage: input.StatusMessage,
		ResultBody:    input.ResultBody,
		CreatedBy:     workflowActorFromInput(input.CreatedBy),
		ExecutionRef:  input.ExecutionRef,
		WorkflowKey:   input.WorkflowKey,
		ProviderName:  input.ProviderName,
	}, nil
}

// boundWorkflowRunFromProto converts an existing protocol run into builder input.
func boundWorkflowRunFromProto(value *proto.BoundWorkflowRun) (BoundWorkflowRun, error) {
	if value == nil {
		return BoundWorkflowRun{}, nil
	}
	startedAt, err := timePtrFromTimestamp(value.GetStartedAt())
	if err != nil {
		return BoundWorkflowRun{}, err
	}
	completedAt, err := timePtrFromTimestamp(value.GetCompletedAt())
	if err != nil {
		return BoundWorkflowRun{}, err
	}
	trigger, err := workflowRunTriggerFromProto(value.GetTrigger())
	if err != nil {
		return BoundWorkflowRun{}, err
	}
	return BoundWorkflowRun{
		ID:            value.GetId(),
		Status:        WorkflowRunStatus(value.GetStatus()),
		Target:        workflowTargetInputPtrFromTarget(value.GetTarget()),
		Trigger:       &trigger,
		CreatedAt:     timeFromTimestamp(value.GetCreatedAt()),
		StartedAt:     startedAt,
		CompletedAt:   completedAt,
		StatusMessage: value.GetStatusMessage(),
		ResultBody:    value.GetResultBody(),
		CreatedBy:     workflowActorInputPtrFromActor(value.GetCreatedBy()),
		ExecutionRef:  value.GetExecutionRef(),
		WorkflowKey:   value.GetWorkflowKey(),
		ProviderName:  value.GetProviderName(),
	}, nil
}

// BoundWorkflowDefinition contains fields for constructing a
// BoundWorkflowDefinition.
type BoundWorkflowDefinition struct {
	ID           string
	Target       *BoundWorkflowTarget
	CreatedBy    *WorkflowActor
	CreatedAt    time.Time
	ProviderName string
}

// boundWorkflowDefinitionToProto creates a bound workflow definition.
func boundWorkflowDefinitionToProto(input BoundWorkflowDefinition) (*proto.BoundWorkflowDefinition, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowDefinition{
		Id:           input.ID,
		Target:       target,
		CreatedBy:    workflowActorFromInput(input.CreatedBy),
		CreatedAt:    timestampFromNonZeroTime(input.CreatedAt),
		ProviderName: input.ProviderName,
	}, nil
}

// boundWorkflowDefinitionFromProto converts an existing definition into
// builder input.
func boundWorkflowDefinitionFromProto(value *proto.BoundWorkflowDefinition) (BoundWorkflowDefinition, error) {
	if value == nil {
		return BoundWorkflowDefinition{}, nil
	}
	return BoundWorkflowDefinition{
		ID:           value.GetId(),
		Target:       workflowTargetInputPtrFromTarget(value.GetTarget()),
		CreatedBy:    workflowActorInputPtrFromActor(value.GetCreatedBy()),
		CreatedAt:    timeFromTimestamp(value.GetCreatedAt()),
		ProviderName: value.GetProviderName(),
	}, nil
}

// cloneBoundWorkflowDefinitionProto creates a copy of an existing definition
// through the definition input builder.
func cloneBoundWorkflowDefinitionProto(value *proto.BoundWorkflowDefinition) (*proto.BoundWorkflowDefinition, error) {
	input, err := boundWorkflowDefinitionFromProto(value)
	if err != nil || value == nil {
		return nil, err
	}
	return boundWorkflowDefinitionToProto(input)
}

// cloneBoundWorkflowRunProto creates a copy of an existing bound workflow run
// through the run input builder.
func cloneBoundWorkflowRunProto(value *proto.BoundWorkflowRun) (*proto.BoundWorkflowRun, error) {
	input, err := boundWorkflowRunFromProto(value)
	if err != nil || value == nil {
		return nil, err
	}
	return boundWorkflowRunToProto(input)
}

// BoundWorkflowSchedule contains fields for constructing a
// BoundWorkflowSchedule.
type BoundWorkflowSchedule struct {
	ID           string
	Cron         string
	Timezone     string
	Target       *BoundWorkflowTarget
	Paused       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	NextRunAt    *time.Time
	CreatedBy    *WorkflowActor
	ExecutionRef string
	ProviderName string
}

// boundWorkflowScheduleToProto creates a bound workflow schedule.
func boundWorkflowScheduleToProto(input BoundWorkflowSchedule) (*proto.BoundWorkflowSchedule, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowSchedule{
		Id:           input.ID,
		Cron:         input.Cron,
		Timezone:     input.Timezone,
		Target:       target,
		Paused:       input.Paused,
		CreatedAt:    timestampFromNonZeroTime(input.CreatedAt),
		UpdatedAt:    timestampFromNonZeroTime(input.UpdatedAt),
		NextRunAt:    timestampFromOptionalTime(input.NextRunAt),
		CreatedBy:    workflowActorFromInput(input.CreatedBy),
		ExecutionRef: input.ExecutionRef,
		ProviderName: input.ProviderName,
	}, nil
}

// boundWorkflowScheduleFromProto converts an existing protocol schedule
// into builder input.
func boundWorkflowScheduleFromProto(value *proto.BoundWorkflowSchedule) (BoundWorkflowSchedule, error) {
	if value == nil {
		return BoundWorkflowSchedule{}, nil
	}
	nextRunAt, err := timePtrFromTimestamp(value.GetNextRunAt())
	if err != nil {
		return BoundWorkflowSchedule{}, err
	}
	return BoundWorkflowSchedule{
		ID:           value.GetId(),
		Cron:         value.GetCron(),
		Timezone:     value.GetTimezone(),
		Target:       workflowTargetInputPtrFromTarget(value.GetTarget()),
		Paused:       value.GetPaused(),
		CreatedAt:    timeFromTimestamp(value.GetCreatedAt()),
		UpdatedAt:    timeFromTimestamp(value.GetUpdatedAt()),
		NextRunAt:    nextRunAt,
		CreatedBy:    workflowActorInputPtrFromActor(value.GetCreatedBy()),
		ExecutionRef: value.GetExecutionRef(),
		ProviderName: value.GetProviderName(),
	}, nil
}

// cloneBoundWorkflowScheduleProto creates a copy of an existing schedule
// through the schedule input builder.
func cloneBoundWorkflowScheduleProto(value *proto.BoundWorkflowSchedule) (*proto.BoundWorkflowSchedule, error) {
	input, err := boundWorkflowScheduleFromProto(value)
	if err != nil || value == nil {
		return nil, err
	}
	return boundWorkflowScheduleToProto(input)
}

// BoundWorkflowEventTrigger contains fields for constructing a
// BoundWorkflowEventTrigger.
type BoundWorkflowEventTrigger struct {
	ID           string
	Match        *WorkflowEventMatch
	Target       *BoundWorkflowTarget
	Paused       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CreatedBy    *WorkflowActor
	ExecutionRef string
	ProviderName string
}

// WorkflowEventMatch contains fields for matching workflow
// events.
type WorkflowEventMatch struct {
	Type    string
	Source  string
	Subject string
}

// boundWorkflowEventTriggerToProto creates a bound workflow event trigger.
func boundWorkflowEventTriggerToProto(input BoundWorkflowEventTrigger) (*proto.BoundWorkflowEventTrigger, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowEventTrigger{
		Id:           input.ID,
		Match:        workflowEventMatchFromInput(input.Match),
		Target:       target,
		Paused:       input.Paused,
		CreatedAt:    timestampFromNonZeroTime(input.CreatedAt),
		UpdatedAt:    timestampFromNonZeroTime(input.UpdatedAt),
		CreatedBy:    workflowActorFromInput(input.CreatedBy),
		ExecutionRef: input.ExecutionRef,
		ProviderName: input.ProviderName,
	}, nil
}

// boundWorkflowEventTriggerFromProto converts an existing protocol event
// trigger into builder input.
func boundWorkflowEventTriggerFromProto(value *proto.BoundWorkflowEventTrigger) (BoundWorkflowEventTrigger, error) {
	if value == nil {
		return BoundWorkflowEventTrigger{}, nil
	}
	return BoundWorkflowEventTrigger{
		ID:           value.GetId(),
		Match:        workflowEventMatchInputPtrFromMatch(value.GetMatch()),
		Target:       workflowTargetInputPtrFromTarget(value.GetTarget()),
		Paused:       value.GetPaused(),
		CreatedAt:    timeFromTimestamp(value.GetCreatedAt()),
		UpdatedAt:    timeFromTimestamp(value.GetUpdatedAt()),
		CreatedBy:    workflowActorInputPtrFromActor(value.GetCreatedBy()),
		ExecutionRef: value.GetExecutionRef(),
		ProviderName: value.GetProviderName(),
	}, nil
}

// cloneBoundWorkflowEventTriggerProto creates a copy of an existing event
// trigger through the event trigger input builder.
func cloneBoundWorkflowEventTriggerProto(value *proto.BoundWorkflowEventTrigger) (*proto.BoundWorkflowEventTrigger, error) {
	input, err := boundWorkflowEventTriggerFromProto(value)
	if err != nil || value == nil {
		return nil, err
	}
	return boundWorkflowEventTriggerToProto(input)
}

// WorkflowExecutionReference contains fields for constructing a
// WorkflowExecutionReference.
type WorkflowExecutionReference struct {
	ID                  string
	ProviderName        string
	Target              *BoundWorkflowTarget
	SubjectID           string
	CredentialSubjectID string
	Permissions         []WorkflowAccessPermission
	CreatedAt           time.Time
	RevokedAt           *time.Time
	SubjectKind         string
	DisplayName         string
	AuthSource          string
	CallerAppName       string
	RunAs               *WorkflowRunAsSubject
	SourceDefinitionID  string
}

// WorkflowAccessPermission contains fields for an execution
// reference permission.
type WorkflowAccessPermission struct {
	App        string
	Operations []string
}

// WorkflowRunAsSubject contains fields for workflow run-as
// metadata.
type WorkflowRunAsSubject struct {
	SubjectID   string
	SubjectKind string
	DisplayName string
	AuthSource  string
}

// workflowExecutionReferenceToProto creates a workflow execution reference.
func workflowExecutionReferenceToProto(input WorkflowExecutionReference) (*proto.WorkflowExecutionReference, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowExecutionReference{
		Id:                  input.ID,
		ProviderName:        input.ProviderName,
		Target:              target,
		SubjectId:           input.SubjectID,
		CredentialSubjectId: input.CredentialSubjectID,
		Permissions:         workflowAccessPermissionsFromInputs(input.Permissions),
		CreatedAt:           timestampFromNonZeroTime(input.CreatedAt),
		RevokedAt:           timestampFromOptionalTime(input.RevokedAt),
		SubjectKind:         input.SubjectKind,
		DisplayName:         input.DisplayName,
		AuthSource:          input.AuthSource,
		CallerAppName:       input.CallerAppName,
		RunAs:               workflowRunAsSubjectFromInput(input.RunAs),
		SourceDefinitionId:  input.SourceDefinitionID,
	}, nil
}

// workflowExecutionReferenceFromProto converts an existing protocol
// execution reference into builder input.
func workflowExecutionReferenceFromProto(value *proto.WorkflowExecutionReference) (WorkflowExecutionReference, error) {
	if value == nil {
		return WorkflowExecutionReference{}, nil
	}
	revokedAt, err := timePtrFromTimestamp(value.GetRevokedAt())
	if err != nil {
		return WorkflowExecutionReference{}, err
	}
	return WorkflowExecutionReference{
		ID:                  value.GetId(),
		ProviderName:        value.GetProviderName(),
		Target:              workflowTargetInputPtrFromTarget(value.GetTarget()),
		SubjectID:           value.GetSubjectId(),
		CredentialSubjectID: value.GetCredentialSubjectId(),
		Permissions:         workflowAccessPermissionInputsFromPermissions(value.GetPermissions()),
		CreatedAt:           timeFromTimestamp(value.GetCreatedAt()),
		RevokedAt:           revokedAt,
		SubjectKind:         value.GetSubjectKind(),
		DisplayName:         value.GetDisplayName(),
		AuthSource:          value.GetAuthSource(),
		CallerAppName:       value.GetCallerAppName(),
		RunAs:               workflowRunAsSubjectInputPtrFromSubject(value.GetRunAs()),
		SourceDefinitionID:  value.GetSourceDefinitionId(),
	}, nil
}

// cloneWorkflowExecutionReferenceProto creates a copy of an existing
// execution reference through the execution reference input builder.
func cloneWorkflowExecutionReferenceProto(value *proto.WorkflowExecutionReference) (*proto.WorkflowExecutionReference, error) {
	input, err := workflowExecutionReferenceFromProto(value)
	if err != nil || value == nil {
		return nil, err
	}
	return workflowExecutionReferenceToProto(input)
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

func workflowTargetInputPtrFromTarget(value *proto.BoundWorkflowTarget) *BoundWorkflowTarget {
	if value == nil {
		return nil
	}
	input := boundWorkflowTargetFromProto(value)
	return &input
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

func workflowRunAsSubjectFromInput(input *WorkflowRunAsSubject) *proto.WorkflowRunAsSubject {
	if input == nil {
		return nil
	}
	return &proto.WorkflowRunAsSubject{
		SubjectId:   input.SubjectID,
		SubjectKind: input.SubjectKind,
		DisplayName: input.DisplayName,
		AuthSource:  input.AuthSource,
	}
}

func workflowRunAsSubjectInputPtrFromSubject(value *proto.WorkflowRunAsSubject) *WorkflowRunAsSubject {
	if value == nil {
		return nil
	}
	return &WorkflowRunAsSubject{
		SubjectID:   value.GetSubjectId(),
		SubjectKind: value.GetSubjectKind(),
		DisplayName: value.GetDisplayName(),
		AuthSource:  value.GetAuthSource(),
	}
}

func workflowAccessPermissionsFromInputs(values []WorkflowAccessPermission) []*proto.WorkflowAccessPermission {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.WorkflowAccessPermission, 0, len(values))
	for _, value := range values {
		out = append(out, &proto.WorkflowAccessPermission{
			App:        value.App,
			Operations: append([]string(nil), value.Operations...),
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
			App:        value.GetApp(),
			Operations: append([]string(nil), value.GetOperations()...),
		})
	}
	return out
}
