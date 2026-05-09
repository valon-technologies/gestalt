package gestalt

import (
	"time"

	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// BoundWorkflowPluginTargetInput contains native Go values for constructing a
// BoundWorkflowPluginTarget.
type BoundWorkflowPluginTargetInput struct {
	PluginName     string
	Operation      string
	Input          any
	Connection     string
	Instance       string
	CredentialMode string
}

// NewBoundWorkflowPluginTarget creates a plugin workflow target from native Go
// values.
func NewBoundWorkflowPluginTarget(input BoundWorkflowPluginTargetInput) (*BoundWorkflowPluginTarget, error) {
	value, err := StructFromAny(input.Input)
	if err != nil {
		return nil, err
	}
	return &BoundWorkflowPluginTarget{
		PluginName:     input.PluginName,
		Operation:      input.Operation,
		Input:          value,
		Connection:     input.Connection,
		Instance:       input.Instance,
		CredentialMode: input.CredentialMode,
	}, nil
}

// BoundWorkflowPluginTargetInputFromTarget converts an existing protocol target
// into native builder input.
func BoundWorkflowPluginTargetInputFromTarget(value *BoundWorkflowPluginTarget) BoundWorkflowPluginTargetInput {
	if value == nil {
		return BoundWorkflowPluginTargetInput{}
	}
	return BoundWorkflowPluginTargetInput{
		PluginName:     value.GetPluginName(),
		Operation:      value.GetOperation(),
		Input:          MapFromStruct(value.GetInput()),
		Connection:     value.GetConnection(),
		Instance:       value.GetInstance(),
		CredentialMode: value.GetCredentialMode(),
	}
}

// WorkflowOutputDeliveryInput contains native Go values for constructing a
// WorkflowOutputDelivery.
type WorkflowOutputDeliveryInput struct {
	Target         *BoundWorkflowPluginTargetInput
	InputBindings  []*WorkflowOutputBinding
	CredentialMode string
}

// NewWorkflowOutputDelivery creates a workflow output delivery from native Go
// values.
func NewWorkflowOutputDelivery(input WorkflowOutputDeliveryInput) (*WorkflowOutputDelivery, error) {
	var target *BoundWorkflowPluginTarget
	if input.Target != nil {
		value, err := NewBoundWorkflowPluginTarget(*input.Target)
		if err != nil {
			return nil, err
		}
		target = value
	}
	bindings, err := copyWorkflowOutputBindings(input.InputBindings)
	if err != nil {
		return nil, err
	}
	return &WorkflowOutputDelivery{
		Target:         target,
		InputBindings:  bindings,
		CredentialMode: input.CredentialMode,
	}, nil
}

// WorkflowOutputDeliveryInputFromDelivery converts an existing protocol delivery
// into native builder input.
func WorkflowOutputDeliveryInputFromDelivery(value *WorkflowOutputDelivery) *WorkflowOutputDeliveryInput {
	if value == nil {
		return nil
	}
	var target *BoundWorkflowPluginTargetInput
	if value.GetTarget() != nil {
		input := BoundWorkflowPluginTargetInputFromTarget(value.GetTarget())
		target = &input
	}
	return &WorkflowOutputDeliveryInput{
		Target:         target,
		InputBindings:  value.GetInputBindings(),
		CredentialMode: value.GetCredentialMode(),
	}
}

// BoundWorkflowAgentTargetInput contains native Go values for constructing a
// BoundWorkflowAgentTarget.
type BoundWorkflowAgentTargetInput struct {
	ProviderName         string
	Model                string
	Prompt               string
	Messages             []*AgentMessage
	ToolRefs             []*AgentToolRef
	ResponseSchema       any
	Metadata             any
	TimeoutSeconds       int32
	OutputDelivery       *WorkflowOutputDeliveryInput
	ModelOptions         any
	SessionReadyDelivery *WorkflowOutputDeliveryInput
}

// NewBoundWorkflowAgentTarget creates an agent workflow target from native Go
// values.
func NewBoundWorkflowAgentTarget(input BoundWorkflowAgentTargetInput) (*BoundWorkflowAgentTarget, error) {
	responseSchema, err := StructFromAny(input.ResponseSchema)
	if err != nil {
		return nil, err
	}
	metadata, err := StructFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	modelOptions, err := StructFromAny(input.ModelOptions)
	if err != nil {
		return nil, err
	}
	outputDelivery, err := newOptionalWorkflowOutputDelivery(input.OutputDelivery)
	if err != nil {
		return nil, err
	}
	sessionReadyDelivery, err := newOptionalWorkflowOutputDelivery(input.SessionReadyDelivery)
	if err != nil {
		return nil, err
	}
	return &BoundWorkflowAgentTarget{
		ProviderName:         input.ProviderName,
		Model:                input.Model,
		Prompt:               input.Prompt,
		Messages:             copyAgentMessages(input.Messages),
		ToolRefs:             copyAgentToolRefs(input.ToolRefs),
		ResponseSchema:       responseSchema,
		Metadata:             metadata,
		TimeoutSeconds:       input.TimeoutSeconds,
		OutputDelivery:       outputDelivery,
		ModelOptions:         modelOptions,
		SessionReadyDelivery: sessionReadyDelivery,
	}, nil
}

// BoundWorkflowAgentTargetInputFromTarget converts an existing protocol target
// into native builder input.
func BoundWorkflowAgentTargetInputFromTarget(value *BoundWorkflowAgentTarget) BoundWorkflowAgentTargetInput {
	if value == nil {
		return BoundWorkflowAgentTargetInput{}
	}
	return BoundWorkflowAgentTargetInput{
		ProviderName:         value.GetProviderName(),
		Model:                value.GetModel(),
		Prompt:               value.GetPrompt(),
		Messages:             value.GetMessages(),
		ToolRefs:             value.GetToolRefs(),
		ResponseSchema:       MapFromStruct(value.GetResponseSchema()),
		Metadata:             MapFromStruct(value.GetMetadata()),
		TimeoutSeconds:       value.GetTimeoutSeconds(),
		OutputDelivery:       WorkflowOutputDeliveryInputFromDelivery(value.GetOutputDelivery()),
		ModelOptions:         MapFromStruct(value.GetModelOptions()),
		SessionReadyDelivery: WorkflowOutputDeliveryInputFromDelivery(value.GetSessionReadyDelivery()),
	}
}

// BoundWorkflowTargetInput contains native Go values for constructing a
// BoundWorkflowTarget. Exactly one of Plugin or Agent should be set.
type BoundWorkflowTargetInput struct {
	Plugin *BoundWorkflowPluginTargetInput
	Agent  *BoundWorkflowAgentTargetInput
}

// NewBoundWorkflowTarget creates a workflow target from native Go values.
func NewBoundWorkflowTarget(input BoundWorkflowTargetInput) (*BoundWorkflowTarget, error) {
	switch {
	case input.Plugin != nil:
		plugin, err := NewBoundWorkflowPluginTarget(*input.Plugin)
		if err != nil {
			return nil, err
		}
		return &BoundWorkflowTarget{Kind: &BoundWorkflowTargetPlugin{Plugin: plugin}}, nil
	case input.Agent != nil:
		agent, err := NewBoundWorkflowAgentTarget(*input.Agent)
		if err != nil {
			return nil, err
		}
		return &BoundWorkflowTarget{Kind: &BoundWorkflowTargetAgent{Agent: agent}}, nil
	default:
		return &BoundWorkflowTarget{}, nil
	}
}

// BoundWorkflowTargetInputFromTarget converts an existing protocol target into
// native builder input.
func BoundWorkflowTargetInputFromTarget(value *BoundWorkflowTarget) BoundWorkflowTargetInput {
	if value == nil {
		return BoundWorkflowTargetInput{}
	}
	if plugin := value.GetPlugin(); plugin != nil {
		input := BoundWorkflowPluginTargetInputFromTarget(plugin)
		return BoundWorkflowTargetInput{Plugin: &input}
	}
	if agent := value.GetAgent(); agent != nil {
		input := BoundWorkflowAgentTargetInputFromTarget(agent)
		return BoundWorkflowTargetInput{Agent: &input}
	}
	return BoundWorkflowTargetInput{}
}

// NewBoundWorkflowTargetFromTarget creates a copy of an existing workflow target
// through the native target input builder.
func NewBoundWorkflowTargetFromTarget(value *BoundWorkflowTarget) (*BoundWorkflowTarget, error) {
	if value == nil {
		return nil, nil
	}
	return NewBoundWorkflowTarget(BoundWorkflowTargetInputFromTarget(value))
}

// WorkflowEventInput contains native Go values for constructing a WorkflowEvent.
type WorkflowEventInput struct {
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

// NewWorkflowEvent creates a workflow event from native Go values.
func NewWorkflowEvent(input WorkflowEventInput) (*WorkflowEvent, error) {
	data, err := StructFromAny(input.Data)
	if err != nil {
		return nil, err
	}
	extensions, err := ValuesFromMap(input.Extensions)
	if err != nil {
		return nil, err
	}
	return &WorkflowEvent{
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

// WorkflowEventInputFromEvent converts an existing protocol event into native
// builder input.
func WorkflowEventInputFromEvent(value *WorkflowEvent) WorkflowEventInput {
	if value == nil {
		return WorkflowEventInput{}
	}
	return WorkflowEventInput{
		ID:              value.GetId(),
		Source:          value.GetSource(),
		SpecVersion:     value.GetSpecVersion(),
		Type:            value.GetType(),
		Subject:         value.GetSubject(),
		Time:            TimeFromTimestamp(value.GetTime()),
		DataContentType: value.GetDatacontenttype(),
		Data:            MapFromStruct(value.GetData()),
		Extensions:      MapFromValues(value.GetExtensions()),
	}
}

// NewWorkflowEventFromEvent creates a copy of an existing workflow event through
// the native event input builder.
func NewWorkflowEventFromEvent(value *WorkflowEvent) (*WorkflowEvent, error) {
	if value == nil {
		return nil, nil
	}
	return NewWorkflowEvent(WorkflowEventInputFromEvent(value))
}

// WorkflowSignalInput contains native Go values for constructing a
// WorkflowSignal.
type WorkflowSignalInput struct {
	ID             string
	Name           string
	Payload        any
	Metadata       any
	CreatedBy      *WorkflowActor
	CreatedAt      time.Time
	IdempotencyKey string
	Sequence       int64
}

// NewWorkflowSignal creates a workflow signal from native Go values.
func NewWorkflowSignal(input WorkflowSignalInput) (*WorkflowSignal, error) {
	payload, err := StructFromAny(input.Payload)
	if err != nil {
		return nil, err
	}
	metadata, err := StructFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	return &WorkflowSignal{
		Id:             input.ID,
		Name:           input.Name,
		Payload:        payload,
		Metadata:       metadata,
		CreatedBy:      input.CreatedBy,
		CreatedAt:      timestampFromNonZeroTime(input.CreatedAt),
		IdempotencyKey: input.IdempotencyKey,
		Sequence:       input.Sequence,
	}, nil
}

// WorkflowSignalInputFromSignal converts an existing protocol signal into native
// builder input.
func WorkflowSignalInputFromSignal(value *WorkflowSignal) WorkflowSignalInput {
	if value == nil {
		return WorkflowSignalInput{}
	}
	return WorkflowSignalInput{
		ID:             value.GetId(),
		Name:           value.GetName(),
		Payload:        MapFromStruct(value.GetPayload()),
		Metadata:       MapFromStruct(value.GetMetadata()),
		CreatedBy:      copyWorkflowActor(value.GetCreatedBy()),
		CreatedAt:      TimeFromTimestamp(value.GetCreatedAt()),
		IdempotencyKey: value.GetIdempotencyKey(),
		Sequence:       value.GetSequence(),
	}
}

// NewWorkflowSignalFromSignal creates a copy of an existing workflow signal
// through the native signal input builder.
func NewWorkflowSignalFromSignal(value *WorkflowSignal) (*WorkflowSignal, error) {
	if value == nil {
		return nil, nil
	}
	return NewWorkflowSignal(WorkflowSignalInputFromSignal(value))
}

// NewWorkflowScheduleTrigger creates a schedule-trigger run trigger from native
// Go values.
func NewWorkflowScheduleTrigger(scheduleID string, scheduledFor time.Time) *WorkflowRunTrigger {
	return &WorkflowRunTrigger{Kind: &WorkflowRunTriggerSchedule{Schedule: &WorkflowScheduleTrigger{
		ScheduleId:   scheduleID,
		ScheduledFor: timestampFromNonZeroTime(scheduledFor),
	}}}
}

// NewWorkflowRunTriggerFromTrigger creates a copy of an existing workflow run
// trigger.
func NewWorkflowRunTriggerFromTrigger(value *WorkflowRunTrigger) (*WorkflowRunTrigger, error) {
	if value == nil {
		return nil, nil
	}
	switch kind := value.GetKind().(type) {
	case *WorkflowRunTriggerManual:
		return &WorkflowRunTrigger{Kind: &WorkflowRunTriggerManual{Manual: &WorkflowManualTrigger{}}}, nil
	case *WorkflowRunTriggerSchedule:
		if kind.Schedule == nil {
			return &WorkflowRunTrigger{}, nil
		}
		return NewWorkflowScheduleTrigger(kind.Schedule.GetScheduleId(), TimeFromTimestamp(kind.Schedule.GetScheduledFor())), nil
	case *WorkflowRunTriggerEvent:
		if kind.Event == nil {
			return &WorkflowRunTrigger{}, nil
		}
		event, err := NewWorkflowEventFromEvent(kind.Event.GetEvent())
		if err != nil {
			return nil, err
		}
		return &WorkflowRunTrigger{Kind: &WorkflowRunTriggerEvent{Event: &WorkflowEventTriggerInvocation{
			TriggerId: kind.Event.GetTriggerId(),
			Event:     event,
		}}}, nil
	default:
		return &WorkflowRunTrigger{}, nil
	}
}

// BoundWorkflowRunInput contains native Go values for constructing a
// BoundWorkflowRun.
type BoundWorkflowRunInput struct {
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
}

// NewBoundWorkflowRun creates a bound workflow run from native Go values.
func NewBoundWorkflowRun(input BoundWorkflowRunInput) *BoundWorkflowRun {
	return &BoundWorkflowRun{
		Id:            input.ID,
		Status:        input.Status,
		Target:        input.Target,
		Trigger:       input.Trigger,
		CreatedAt:     timestampFromNonZeroTime(input.CreatedAt),
		StartedAt:     timestampFromOptionalTime(input.StartedAt),
		CompletedAt:   timestampFromOptionalTime(input.CompletedAt),
		StatusMessage: input.StatusMessage,
		ResultBody:    input.ResultBody,
		CreatedBy:     input.CreatedBy,
		ExecutionRef:  input.ExecutionRef,
		WorkflowKey:   input.WorkflowKey,
	}
}

// BoundWorkflowRunInputFromRun converts an existing protocol run into native
// builder input.
func BoundWorkflowRunInputFromRun(value *BoundWorkflowRun) (BoundWorkflowRunInput, error) {
	if value == nil {
		return BoundWorkflowRunInput{}, nil
	}
	target, err := NewBoundWorkflowTargetFromTarget(value.GetTarget())
	if err != nil {
		return BoundWorkflowRunInput{}, err
	}
	startedAt, err := TimePtrFromTimestamp(value.GetStartedAt())
	if err != nil {
		return BoundWorkflowRunInput{}, err
	}
	completedAt, err := TimePtrFromTimestamp(value.GetCompletedAt())
	if err != nil {
		return BoundWorkflowRunInput{}, err
	}
	trigger, err := NewWorkflowRunTriggerFromTrigger(value.GetTrigger())
	if err != nil {
		return BoundWorkflowRunInput{}, err
	}
	return BoundWorkflowRunInput{
		ID:            value.GetId(),
		Status:        value.GetStatus(),
		Target:        target,
		Trigger:       trigger,
		CreatedAt:     TimeFromTimestamp(value.GetCreatedAt()),
		StartedAt:     startedAt,
		CompletedAt:   completedAt,
		StatusMessage: value.GetStatusMessage(),
		ResultBody:    value.GetResultBody(),
		CreatedBy:     copyWorkflowActor(value.GetCreatedBy()),
		ExecutionRef:  value.GetExecutionRef(),
		WorkflowKey:   value.GetWorkflowKey(),
	}, nil
}

// NewBoundWorkflowRunFromRun creates a copy of an existing bound workflow run
// through the native run input builder.
func NewBoundWorkflowRunFromRun(value *BoundWorkflowRun) (*BoundWorkflowRun, error) {
	input, err := BoundWorkflowRunInputFromRun(value)
	if err != nil || value == nil {
		return nil, err
	}
	return NewBoundWorkflowRun(input), nil
}

// BoundWorkflowScheduleInput contains native Go values for constructing a
// BoundWorkflowSchedule.
type BoundWorkflowScheduleInput struct {
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
}

// NewBoundWorkflowSchedule creates a bound workflow schedule from native Go
// values.
func NewBoundWorkflowSchedule(input BoundWorkflowScheduleInput) *BoundWorkflowSchedule {
	return &BoundWorkflowSchedule{
		Id:           input.ID,
		Cron:         input.Cron,
		Timezone:     input.Timezone,
		Target:       input.Target,
		Paused:       input.Paused,
		CreatedAt:    timestampFromNonZeroTime(input.CreatedAt),
		UpdatedAt:    timestampFromNonZeroTime(input.UpdatedAt),
		NextRunAt:    timestampFromOptionalTime(input.NextRunAt),
		CreatedBy:    input.CreatedBy,
		ExecutionRef: input.ExecutionRef,
	}
}

// BoundWorkflowScheduleInputFromSchedule converts an existing protocol schedule
// into native builder input.
func BoundWorkflowScheduleInputFromSchedule(value *BoundWorkflowSchedule) (BoundWorkflowScheduleInput, error) {
	if value == nil {
		return BoundWorkflowScheduleInput{}, nil
	}
	target, err := NewBoundWorkflowTargetFromTarget(value.GetTarget())
	if err != nil {
		return BoundWorkflowScheduleInput{}, err
	}
	nextRunAt, err := TimePtrFromTimestamp(value.GetNextRunAt())
	if err != nil {
		return BoundWorkflowScheduleInput{}, err
	}
	return BoundWorkflowScheduleInput{
		ID:           value.GetId(),
		Cron:         value.GetCron(),
		Timezone:     value.GetTimezone(),
		Target:       target,
		Paused:       value.GetPaused(),
		CreatedAt:    TimeFromTimestamp(value.GetCreatedAt()),
		UpdatedAt:    TimeFromTimestamp(value.GetUpdatedAt()),
		NextRunAt:    nextRunAt,
		CreatedBy:    copyWorkflowActor(value.GetCreatedBy()),
		ExecutionRef: value.GetExecutionRef(),
	}, nil
}

// NewBoundWorkflowScheduleFromSchedule creates a copy of an existing schedule
// through the native schedule input builder.
func NewBoundWorkflowScheduleFromSchedule(value *BoundWorkflowSchedule) (*BoundWorkflowSchedule, error) {
	input, err := BoundWorkflowScheduleInputFromSchedule(value)
	if err != nil || value == nil {
		return nil, err
	}
	return NewBoundWorkflowSchedule(input), nil
}

// BoundWorkflowEventTriggerInput contains native Go values for constructing a
// BoundWorkflowEventTrigger.
type BoundWorkflowEventTriggerInput struct {
	ID           string
	Match        *WorkflowEventMatch
	Target       *BoundWorkflowTarget
	Paused       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CreatedBy    *WorkflowActor
	ExecutionRef string
}

// NewBoundWorkflowEventTrigger creates a bound workflow event trigger from
// native Go values.
func NewBoundWorkflowEventTrigger(input BoundWorkflowEventTriggerInput) *BoundWorkflowEventTrigger {
	return &BoundWorkflowEventTrigger{
		Id:           input.ID,
		Match:        input.Match,
		Target:       input.Target,
		Paused:       input.Paused,
		CreatedAt:    timestampFromNonZeroTime(input.CreatedAt),
		UpdatedAt:    timestampFromNonZeroTime(input.UpdatedAt),
		CreatedBy:    input.CreatedBy,
		ExecutionRef: input.ExecutionRef,
	}
}

// BoundWorkflowEventTriggerInputFromTrigger converts an existing protocol event
// trigger into native builder input.
func BoundWorkflowEventTriggerInputFromTrigger(value *BoundWorkflowEventTrigger) (BoundWorkflowEventTriggerInput, error) {
	if value == nil {
		return BoundWorkflowEventTriggerInput{}, nil
	}
	target, err := NewBoundWorkflowTargetFromTarget(value.GetTarget())
	if err != nil {
		return BoundWorkflowEventTriggerInput{}, err
	}
	return BoundWorkflowEventTriggerInput{
		ID:           value.GetId(),
		Match:        copyWorkflowEventMatch(value.GetMatch()),
		Target:       target,
		Paused:       value.GetPaused(),
		CreatedAt:    TimeFromTimestamp(value.GetCreatedAt()),
		UpdatedAt:    TimeFromTimestamp(value.GetUpdatedAt()),
		CreatedBy:    copyWorkflowActor(value.GetCreatedBy()),
		ExecutionRef: value.GetExecutionRef(),
	}, nil
}

// NewBoundWorkflowEventTriggerFromTrigger creates a copy of an existing event
// trigger through the native event trigger input builder.
func NewBoundWorkflowEventTriggerFromTrigger(value *BoundWorkflowEventTrigger) (*BoundWorkflowEventTrigger, error) {
	input, err := BoundWorkflowEventTriggerInputFromTrigger(value)
	if err != nil || value == nil {
		return nil, err
	}
	return NewBoundWorkflowEventTrigger(input), nil
}

// WorkflowExecutionReferenceInput contains native Go values for constructing a
// WorkflowExecutionReference.
type WorkflowExecutionReferenceInput struct {
	ID                  string
	ProviderName        string
	Target              *BoundWorkflowTarget
	SubjectID           string
	CredentialSubjectID string
	Permissions         []*WorkflowAccessPermission
	CreatedAt           time.Time
	RevokedAt           *time.Time
	SubjectKind         string
	DisplayName         string
	AuthSource          string
	CallerPluginName    string
	RunAs               *WorkflowRunAsSubject
	SourceDefinitionID  string
}

// NewWorkflowExecutionReference creates a workflow execution reference from
// native Go values.
func NewWorkflowExecutionReference(input WorkflowExecutionReferenceInput) *WorkflowExecutionReference {
	return &WorkflowExecutionReference{
		Id:                  input.ID,
		ProviderName:        input.ProviderName,
		Target:              input.Target,
		SubjectId:           input.SubjectID,
		CredentialSubjectId: input.CredentialSubjectID,
		Permissions:         input.Permissions,
		CreatedAt:           timestampFromNonZeroTime(input.CreatedAt),
		RevokedAt:           timestampFromOptionalTime(input.RevokedAt),
		SubjectKind:         input.SubjectKind,
		DisplayName:         input.DisplayName,
		AuthSource:          input.AuthSource,
		CallerPluginName:    input.CallerPluginName,
		RunAs:               input.RunAs,
		SourceDefinitionId:  input.SourceDefinitionID,
	}
}

// WorkflowExecutionReferenceInputFromReference converts an existing protocol
// execution reference into native builder input.
func WorkflowExecutionReferenceInputFromReference(value *WorkflowExecutionReference) (WorkflowExecutionReferenceInput, error) {
	if value == nil {
		return WorkflowExecutionReferenceInput{}, nil
	}
	target, err := NewBoundWorkflowTargetFromTarget(value.GetTarget())
	if err != nil {
		return WorkflowExecutionReferenceInput{}, err
	}
	revokedAt, err := TimePtrFromTimestamp(value.GetRevokedAt())
	if err != nil {
		return WorkflowExecutionReferenceInput{}, err
	}
	return WorkflowExecutionReferenceInput{
		ID:                  value.GetId(),
		ProviderName:        value.GetProviderName(),
		Target:              target,
		SubjectID:           value.GetSubjectId(),
		CredentialSubjectID: value.GetCredentialSubjectId(),
		Permissions:         copyWorkflowAccessPermissions(value.GetPermissions()),
		CreatedAt:           TimeFromTimestamp(value.GetCreatedAt()),
		RevokedAt:           revokedAt,
		SubjectKind:         value.GetSubjectKind(),
		DisplayName:         value.GetDisplayName(),
		AuthSource:          value.GetAuthSource(),
		CallerPluginName:    value.GetCallerPluginName(),
		RunAs:               copyWorkflowRunAsSubject(value.GetRunAs()),
		SourceDefinitionID:  value.GetSourceDefinitionId(),
	}, nil
}

// NewWorkflowExecutionReferenceFromReference creates a copy of an existing
// execution reference through the native execution reference input builder.
func NewWorkflowExecutionReferenceFromReference(value *WorkflowExecutionReference) (*WorkflowExecutionReference, error) {
	input, err := WorkflowExecutionReferenceInputFromReference(value)
	if err != nil || value == nil {
		return nil, err
	}
	return NewWorkflowExecutionReference(input), nil
}

func timestampFromNonZeroTime(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return TimestampFromTime(value)
}

func timestampFromOptionalTime(value *time.Time) *timestamppb.Timestamp {
	if value == nil || value.IsZero() {
		return nil
	}
	return TimestampFromTime(*value)
}

func newOptionalWorkflowOutputDelivery(input *WorkflowOutputDeliveryInput) (*WorkflowOutputDelivery, error) {
	if input == nil {
		return nil, nil
	}
	return NewWorkflowOutputDelivery(*input)
}

func copyAgentMessages(values []*AgentMessage) []*AgentMessage {
	if len(values) == 0 {
		return nil
	}
	out := make([]*AgentMessage, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, gproto.Clone(value).(*AgentMessage))
	}
	return out
}

func copyAgentToolRefs(values []*AgentToolRef) []*AgentToolRef {
	if len(values) == 0 {
		return nil
	}
	out := make([]*AgentToolRef, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, gproto.Clone(value).(*AgentToolRef))
	}
	return out
}

func copyWorkflowActor(value *WorkflowActor) *WorkflowActor {
	if value == nil {
		return nil
	}
	return &WorkflowActor{
		SubjectId:   value.GetSubjectId(),
		SubjectKind: value.GetSubjectKind(),
		DisplayName: value.GetDisplayName(),
		AuthSource:  value.GetAuthSource(),
	}
}

func copyWorkflowEventMatch(value *WorkflowEventMatch) *WorkflowEventMatch {
	if value == nil {
		return nil
	}
	return &WorkflowEventMatch{
		Type:    value.GetType(),
		Source:  value.GetSource(),
		Subject: value.GetSubject(),
	}
}

func copyWorkflowRunAsSubject(value *WorkflowRunAsSubject) *WorkflowRunAsSubject {
	if value == nil {
		return nil
	}
	return &WorkflowRunAsSubject{
		SubjectId:   value.GetSubjectId(),
		SubjectKind: value.GetSubjectKind(),
		DisplayName: value.GetDisplayName(),
		AuthSource:  value.GetAuthSource(),
	}
}

func copyWorkflowAccessPermissions(values []*WorkflowAccessPermission) []*WorkflowAccessPermission {
	if len(values) == 0 {
		return nil
	}
	out := make([]*WorkflowAccessPermission, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, &WorkflowAccessPermission{
			Plugin:     value.GetPlugin(),
			Operations: append([]string(nil), value.GetOperations()...),
		})
	}
	return out
}

func copyWorkflowOutputBindings(values []*WorkflowOutputBinding) ([]*WorkflowOutputBinding, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*WorkflowOutputBinding, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		source, err := copyWorkflowOutputValueSource(value.GetValue())
		if err != nil {
			return nil, err
		}
		out = append(out, &WorkflowOutputBinding{
			InputField: value.GetInputField(),
			Value:      source,
		})
	}
	return out, nil
}

func copyWorkflowOutputValueSource(value *WorkflowOutputValueSource) (*WorkflowOutputValueSource, error) {
	if value == nil {
		return nil, nil
	}
	switch kind := value.GetKind().(type) {
	case *WorkflowOutputValueSourceAgentOutput:
		return &WorkflowOutputValueSource{Kind: &WorkflowOutputValueSourceAgentOutput{AgentOutput: kind.AgentOutput}}, nil
	case *WorkflowOutputValueSourceSignalPayload:
		return &WorkflowOutputValueSource{Kind: &WorkflowOutputValueSourceSignalPayload{SignalPayload: kind.SignalPayload}}, nil
	case *WorkflowOutputValueSourceSignalMetadata:
		return &WorkflowOutputValueSource{Kind: &WorkflowOutputValueSourceSignalMetadata{SignalMetadata: kind.SignalMetadata}}, nil
	case *WorkflowOutputValueSourceAgentSession:
		return &WorkflowOutputValueSource{Kind: &WorkflowOutputValueSourceAgentSession{AgentSession: kind.AgentSession}}, nil
	case *WorkflowOutputValueSourceLiteral:
		literal, err := ValueFromAny(AnyFromValue(kind.Literal))
		if err != nil {
			return nil, err
		}
		return &WorkflowOutputValueSource{Kind: &WorkflowOutputValueSourceLiteral{Literal: literal}}, nil
	default:
		return &WorkflowOutputValueSource{}, nil
	}
}
