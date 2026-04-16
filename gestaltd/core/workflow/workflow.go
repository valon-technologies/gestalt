package workflow

import (
	"context"
	"time"
)

const ConfigManagedSchedulePrefix = "cfg_"

type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCanceled  RunStatus = "canceled"
)

type Actor struct {
	SubjectID   string
	SubjectKind string
	DisplayName string
	AuthSource  string
}

type Target struct {
	PluginName string
	Operation  string
	Input      map[string]any
}

type Event struct {
	ID              string
	Source          string
	SpecVersion     string
	Type            string
	Subject         string
	Time            *time.Time
	DataContentType string
	Data            map[string]any
	Extensions      map[string]any
}

type EventMatch struct {
	Type    string
	Source  string
	Subject string
}

type ScheduleTrigger struct {
	ScheduleID   string
	ScheduledFor *time.Time
}

type EventTriggerInvocation struct {
	TriggerID string
	Event     Event
}

type RunTrigger struct {
	Manual   bool
	Schedule *ScheduleTrigger
	Event    *EventTriggerInvocation
}

type Run struct {
	ID            string
	Status        RunStatus
	Target        Target
	Trigger       RunTrigger
	CreatedBy     Actor
	CreatedAt     *time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
	StatusMessage string
	ResultBody    string
}

type Schedule struct {
	ID        string
	Cron      string
	Timezone  string
	Target    Target
	Paused    bool
	CreatedBy Actor
	CreatedAt *time.Time
	UpdatedAt *time.Time
	NextRunAt *time.Time
}

type EventTrigger struct {
	ID        string
	Match     EventMatch
	Target    Target
	Paused    bool
	CreatedBy Actor
	CreatedAt *time.Time
	UpdatedAt *time.Time
}

type StartRunRequest struct {
	Target         Target
	IdempotencyKey string
	CreatedBy      Actor
}

type GetRunRequest struct {
	PluginName string
	RunID      string
}

type ListRunsRequest struct {
	PluginName string
}

type CancelRunRequest struct {
	PluginName string
	RunID      string
	Reason     string
}

type UpsertScheduleRequest struct {
	ScheduleID  string
	Cron        string
	Timezone    string
	Target      Target
	Paused      bool
	RequestedBy Actor
}

type ListSchedulesRequest struct {
	PluginName string
}

type GetScheduleRequest struct {
	PluginName string
	ScheduleID string
}

type DeleteScheduleRequest struct {
	PluginName string
	ScheduleID string
}

type PauseScheduleRequest struct {
	PluginName string
	ScheduleID string
}

type ResumeScheduleRequest struct {
	PluginName string
	ScheduleID string
}

type UpsertEventTriggerRequest struct {
	TriggerID   string
	Match       EventMatch
	Target      Target
	Paused      bool
	RequestedBy Actor
}

type ListEventTriggersRequest struct {
	PluginName string
}

type GetEventTriggerRequest struct {
	PluginName string
	TriggerID  string
}

type DeleteEventTriggerRequest struct {
	PluginName string
	TriggerID  string
}

type PauseEventTriggerRequest struct {
	PluginName string
	TriggerID  string
}

type ResumeEventTriggerRequest struct {
	PluginName string
	TriggerID  string
}

type PublishEventRequest struct {
	PluginName string
	Event      Event
}

type InvokeOperationRequest struct {
	ProviderName string
	PluginName   string
	RunID        string
	Trigger      RunTrigger
	Target       Target
	Input        map[string]any
	Metadata     map[string]any
	CreatedBy    Actor
}

type InvokeOperationResponse struct {
	Status int
	Body   string
}

type Provider interface {
	StartRun(ctx context.Context, req StartRunRequest) (*Run, error)
	GetRun(ctx context.Context, req GetRunRequest) (*Run, error)
	ListRuns(ctx context.Context, req ListRunsRequest) ([]*Run, error)
	CancelRun(ctx context.Context, req CancelRunRequest) (*Run, error)
	UpsertSchedule(ctx context.Context, req UpsertScheduleRequest) (*Schedule, error)
	GetSchedule(ctx context.Context, req GetScheduleRequest) (*Schedule, error)
	ListSchedules(ctx context.Context, req ListSchedulesRequest) ([]*Schedule, error)
	DeleteSchedule(ctx context.Context, req DeleteScheduleRequest) error
	PauseSchedule(ctx context.Context, req PauseScheduleRequest) (*Schedule, error)
	ResumeSchedule(ctx context.Context, req ResumeScheduleRequest) (*Schedule, error)
	UpsertEventTrigger(ctx context.Context, req UpsertEventTriggerRequest) (*EventTrigger, error)
	GetEventTrigger(ctx context.Context, req GetEventTriggerRequest) (*EventTrigger, error)
	ListEventTriggers(ctx context.Context, req ListEventTriggersRequest) ([]*EventTrigger, error)
	DeleteEventTrigger(ctx context.Context, req DeleteEventTriggerRequest) error
	PauseEventTrigger(ctx context.Context, req PauseEventTriggerRequest) (*EventTrigger, error)
	ResumeEventTrigger(ctx context.Context, req ResumeEventTriggerRequest) (*EventTrigger, error)
	PublishEvent(ctx context.Context, req PublishEventRequest) error
	Ping(ctx context.Context) error
	Close() error
}

type Host interface {
	InvokeOperation(ctx context.Context, req InvokeOperationRequest) (*InvokeOperationResponse, error)
}
