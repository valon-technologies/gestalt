package gestalt

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

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

// NewWorkflowScheduleTrigger creates a schedule-trigger run trigger from native
// Go values.
func NewWorkflowScheduleTrigger(scheduleID string, scheduledFor time.Time) *WorkflowRunTrigger {
	return &WorkflowRunTrigger{Kind: &WorkflowRunTriggerSchedule{Schedule: &WorkflowScheduleTrigger{
		ScheduleId:   scheduleID,
		ScheduledFor: timestampFromNonZeroTime(scheduledFor),
	}}}
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
