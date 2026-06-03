package workflowwire

import (
	"fmt"
	"strings"
	"time"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/agentwire"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func EventToProto(event coreworkflow.Event) (*proto.WorkflowEvent, error) {
	data, err := protoutil.StructFromMap(event.Data)
	if err != nil {
		return nil, fmt.Errorf("workflow event data: %w", err)
	}
	extensions := make(map[string]*structpb.Value, len(event.Extensions))
	for key, value := range event.Extensions {
		if value == nil {
			extensions[key] = structpb.NewNullValue()
			continue
		}
		pbValue, err := protoutil.ValueFromAny(value)
		if err != nil {
			return nil, fmt.Errorf("workflow event extensions: %s: %w", key, err)
		}
		extensions[key] = pbValue
	}
	if len(extensions) == 0 {
		extensions = nil
	}
	return &proto.WorkflowEvent{
		Id:              event.ID,
		Source:          event.Source,
		SpecVersion:     event.SpecVersion,
		Type:            event.Type,
		Subject:         event.Subject,
		Time:            TimeToProto(event.Time),
		Datacontenttype: event.DataContentType,
		Data:            data,
		Extensions:      extensions,
	}, nil
}

func EventFromProto(event *proto.WorkflowEvent) (coreworkflow.Event, error) {
	if event == nil {
		return coreworkflow.Event{}, nil
	}
	return coreworkflow.Event{
		ID:              event.GetId(),
		Source:          event.GetSource(),
		SpecVersion:     event.GetSpecVersion(),
		Type:            event.GetType(),
		Subject:         event.GetSubject(),
		Time:            TimeFromProto(event.GetTime()),
		DataContentType: event.GetDatacontenttype(),
		Data:            protoutil.MapFromStruct(event.GetData()),
		Extensions:      workflowExtensionsFromProto(event.GetExtensions()),
	}, nil
}

func EventMatchToProto(match coreworkflow.EventMatch) *proto.WorkflowEventMatch {
	return &proto.WorkflowEventMatch{
		Type:    match.Type,
		Source:  match.Source,
		Subject: match.Subject,
	}
}

func EventMatchFromProto(match *proto.WorkflowEventMatch) coreworkflow.EventMatch {
	if match == nil {
		return coreworkflow.EventMatch{}
	}
	return coreworkflow.EventMatch{
		Type:    match.GetType(),
		Source:  match.GetSource(),
		Subject: match.GetSubject(),
	}
}

func RunStatusToProto(status coreworkflow.RunStatus) proto.WorkflowRunStatus {
	switch status {
	case coreworkflow.RunStatusPending:
		return proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING
	case coreworkflow.RunStatusRunning:
		return proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_RUNNING
	case coreworkflow.RunStatusSucceeded:
		return proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_SUCCEEDED
	case coreworkflow.RunStatusFailed:
		return proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_FAILED
	case coreworkflow.RunStatusCanceled:
		return proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_CANCELED
	default:
		return proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_UNSPECIFIED
	}
}

func RunStatusFromProto(status proto.WorkflowRunStatus) (coreworkflow.RunStatus, error) {
	switch status {
	case proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_UNSPECIFIED:
		return "", nil
	case proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING:
		return coreworkflow.RunStatusPending, nil
	case proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_RUNNING:
		return coreworkflow.RunStatusRunning, nil
	case proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_SUCCEEDED:
		return coreworkflow.RunStatusSucceeded, nil
	case proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_FAILED:
		return coreworkflow.RunStatusFailed, nil
	case proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_CANCELED:
		return coreworkflow.RunStatusCanceled, nil
	default:
		return "", fmt.Errorf("unknown workflow run status %v", status)
	}
}

func RunFromProto(run *proto.BoundWorkflowRun) (*coreworkflow.Run, error) {
	if run == nil {
		return nil, nil
	}
	status, err := RunStatusFromProto(run.GetStatus())
	if err != nil {
		return nil, err
	}
	trigger, err := RunTriggerFromProto(run.GetTrigger())
	if err != nil {
		return nil, err
	}
	return &coreworkflow.Run{
		ID:                 run.GetId(),
		Status:             status,
		WorkflowKey:        run.GetWorkflowKey(),
		Target:             TargetFromProto(run.GetTarget()),
		DefinitionID:       run.GetDefinitionId(),
		Trigger:            trigger,
		CreatedBySubjectID: strings.TrimSpace(run.GetCreatedBySubjectId()),
		RunAs:              agentwire.RunAsSubjectFromProto(run.GetRunAs()),
		CreatedAt:          TimeFromProto(run.GetCreatedAt()),
		StartedAt:          TimeFromProto(run.GetStartedAt()),
		CompletedAt:        TimeFromProto(run.GetCompletedAt()),
		StatusMessage:      run.GetStatusMessage(),
		ResultBody:         run.GetResultBody(),
	}, nil
}

func RunToProto(run *coreworkflow.Run) (*proto.BoundWorkflowRun, error) {
	if run == nil {
		return nil, nil
	}
	target, err := TargetToProto(run.Target)
	if err != nil {
		return nil, err
	}
	trigger, err := RunTriggerToProto(run.Trigger)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowRun{
		Id:                 run.ID,
		Status:             RunStatusToProto(run.Status),
		Target:             target,
		Trigger:            trigger,
		CreatedAt:          TimeToProto(run.CreatedAt),
		StartedAt:          TimeToProto(run.StartedAt),
		CompletedAt:        TimeToProto(run.CompletedAt),
		StatusMessage:      run.StatusMessage,
		ResultBody:         run.ResultBody,
		CreatedBySubjectId: strings.TrimSpace(run.CreatedBySubjectID),
		WorkflowKey:        run.WorkflowKey,
		DefinitionId:       run.DefinitionID,
		RunAs:              agentwire.RunAsSubjectToProto(run.RunAs),
	}, nil
}

func ScheduleFromProto(schedule *proto.BoundWorkflowSchedule) (*coreworkflow.Schedule, error) {
	if schedule == nil {
		return nil, nil
	}
	return &coreworkflow.Schedule{
		ID:                 schedule.GetId(),
		Cron:               schedule.GetCron(),
		Timezone:           schedule.GetTimezone(),
		Target:             TargetFromProto(schedule.GetTarget()),
		DefinitionID:       schedule.GetDefinitionId(),
		Paused:             schedule.GetPaused(),
		CreatedBySubjectID: strings.TrimSpace(schedule.GetCreatedBySubjectId()),
		RunAs:              agentwire.RunAsSubjectFromProto(schedule.GetRunAs()),
		CreatedAt:          TimeFromProto(schedule.GetCreatedAt()),
		UpdatedAt:          TimeFromProto(schedule.GetUpdatedAt()),
		NextRunAt:          TimeFromProto(schedule.GetNextRunAt()),
	}, nil
}

func ScheduleToProto(schedule *coreworkflow.Schedule) (*proto.BoundWorkflowSchedule, error) {
	if schedule == nil {
		return nil, nil
	}
	target, err := TargetToProto(schedule.Target)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowSchedule{
		Id:                 schedule.ID,
		Cron:               schedule.Cron,
		Timezone:           schedule.Timezone,
		Target:             target,
		Paused:             schedule.Paused,
		CreatedAt:          TimeToProto(schedule.CreatedAt),
		UpdatedAt:          TimeToProto(schedule.UpdatedAt),
		NextRunAt:          TimeToProto(schedule.NextRunAt),
		CreatedBySubjectId: strings.TrimSpace(schedule.CreatedBySubjectID),
		DefinitionId:       schedule.DefinitionID,
		RunAs:              agentwire.RunAsSubjectToProto(schedule.RunAs),
	}, nil
}

func EventTriggerFromProto(trigger *proto.BoundWorkflowEventTrigger) (*coreworkflow.EventTrigger, error) {
	if trigger == nil {
		return nil, nil
	}
	return &coreworkflow.EventTrigger{
		ID:                 trigger.GetId(),
		Match:              EventMatchFromProto(trigger.GetMatch()),
		Target:             TargetFromProto(trigger.GetTarget()),
		DefinitionID:       trigger.GetDefinitionId(),
		Paused:             trigger.GetPaused(),
		CreatedBySubjectID: strings.TrimSpace(trigger.GetCreatedBySubjectId()),
		RunAs:              agentwire.RunAsSubjectFromProto(trigger.GetRunAs()),
		CreatedAt:          TimeFromProto(trigger.GetCreatedAt()),
		UpdatedAt:          TimeFromProto(trigger.GetUpdatedAt()),
	}, nil
}

func EventTriggerToProto(trigger *coreworkflow.EventTrigger) (*proto.BoundWorkflowEventTrigger, error) {
	if trigger == nil {
		return nil, nil
	}
	target, err := TargetToProto(trigger.Target)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowEventTrigger{
		Id:                 trigger.ID,
		Match:              EventMatchToProto(trigger.Match),
		Target:             target,
		Paused:             trigger.Paused,
		CreatedAt:          TimeToProto(trigger.CreatedAt),
		UpdatedAt:          TimeToProto(trigger.UpdatedAt),
		CreatedBySubjectId: strings.TrimSpace(trigger.CreatedBySubjectID),
		DefinitionId:       trigger.DefinitionID,
		RunAs:              agentwire.RunAsSubjectToProto(trigger.RunAs),
	}, nil
}

func DefinitionFromProto(definition *proto.BoundWorkflowDefinition) (*coreworkflow.Definition, error) {
	if definition == nil {
		return nil, nil
	}
	return &coreworkflow.Definition{
		ID:                 definition.GetId(),
		Target:             TargetFromProto(definition.GetTarget()),
		CreatedBySubjectID: strings.TrimSpace(definition.GetCreatedBySubjectId()),
		CreatedAt:          TimeFromProto(definition.GetCreatedAt()),
	}, nil
}

func DefinitionToProto(definition *coreworkflow.Definition) (*proto.BoundWorkflowDefinition, error) {
	if definition == nil {
		return nil, nil
	}
	target, err := TargetToProto(definition.Target)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowDefinition{
		Id:                 definition.ID,
		Target:             target,
		CreatedBySubjectId: strings.TrimSpace(definition.CreatedBySubjectID),
		CreatedAt:          TimeToProto(definition.CreatedAt),
	}, nil
}

func SignalToProto(signal coreworkflow.Signal) (*proto.WorkflowSignal, error) {
	payload, err := protoutil.StructFromMap(signal.Payload)
	if err != nil {
		return nil, fmt.Errorf("workflow signal payload: %w", err)
	}
	metadata, err := protoutil.StructFromMap(signal.Metadata)
	if err != nil {
		return nil, fmt.Errorf("workflow signal metadata: %w", err)
	}
	return &proto.WorkflowSignal{
		Id:                 signal.ID,
		Name:               signal.Name,
		Payload:            payload,
		Metadata:           metadata,
		CreatedBySubjectId: strings.TrimSpace(signal.CreatedBySubjectID),
		CreatedAt:          TimeToProto(signal.CreatedAt),
		IdempotencyKey:     signal.IdempotencyKey,
		Sequence:           signal.Sequence,
	}, nil
}

func SignalFromProto(signal *proto.WorkflowSignal) coreworkflow.Signal {
	if signal == nil {
		return coreworkflow.Signal{}
	}
	return coreworkflow.Signal{
		ID:                 signal.GetId(),
		Name:               signal.GetName(),
		Payload:            protoutil.MapFromStruct(signal.GetPayload()),
		Metadata:           protoutil.MapFromStruct(signal.GetMetadata()),
		CreatedBySubjectID: strings.TrimSpace(signal.GetCreatedBySubjectId()),
		CreatedAt:          TimeFromProto(signal.GetCreatedAt()),
		IdempotencyKey:     signal.GetIdempotencyKey(),
		Sequence:           signal.GetSequence(),
	}
}

func SignalRunResponseFromProto(resp *proto.SignalWorkflowRunResponse) (*coreworkflow.SignalRunResponse, error) {
	if resp == nil {
		return nil, nil
	}
	run, err := RunFromProto(resp.GetRun())
	if err != nil {
		return nil, err
	}
	return &coreworkflow.SignalRunResponse{
		Run:         run,
		Signal:      SignalFromProto(resp.GetSignal()),
		StartedRun:  resp.GetStartedRun(),
		WorkflowKey: resp.GetWorkflowKey(),
	}, nil
}

func RunTriggerFromProto(trigger *proto.WorkflowRunTrigger) (coreworkflow.RunTrigger, error) {
	if trigger == nil {
		return coreworkflow.RunTrigger{}, nil
	}
	switch kind := trigger.GetKind().(type) {
	case *proto.WorkflowRunTrigger_Manual:
		return coreworkflow.RunTrigger{Manual: kind.Manual != nil}, nil
	case *proto.WorkflowRunTrigger_Schedule:
		if kind.Schedule == nil {
			return coreworkflow.RunTrigger{}, nil
		}
		return coreworkflow.RunTrigger{
			Schedule: &coreworkflow.ScheduleTrigger{
				ScheduleID:   kind.Schedule.GetScheduleId(),
				ScheduledFor: TimeFromProto(kind.Schedule.GetScheduledFor()),
			},
		}, nil
	case *proto.WorkflowRunTrigger_Event:
		if kind.Event == nil {
			return coreworkflow.RunTrigger{}, nil
		}
		event, err := EventFromProto(kind.Event.GetEvent())
		if err != nil {
			return coreworkflow.RunTrigger{}, err
		}
		return coreworkflow.RunTrigger{
			Event: &coreworkflow.EventTriggerInvocation{
				TriggerID: kind.Event.GetTriggerId(),
				Event:     event,
			},
		}, nil
	default:
		return coreworkflow.RunTrigger{}, nil
	}
}

func RunTriggerToProto(trigger coreworkflow.RunTrigger) (*proto.WorkflowRunTrigger, error) {
	switch {
	case trigger.Schedule != nil:
		return &proto.WorkflowRunTrigger{
			Kind: &proto.WorkflowRunTrigger_Schedule{
				Schedule: &proto.WorkflowScheduleTrigger{
					ScheduleId:   trigger.Schedule.ScheduleID,
					ScheduledFor: TimeToProto(trigger.Schedule.ScheduledFor),
				},
			},
		}, nil
	case trigger.Event != nil:
		event, err := EventToProto(trigger.Event.Event)
		if err != nil {
			return nil, err
		}
		return &proto.WorkflowRunTrigger{
			Kind: &proto.WorkflowRunTrigger_Event{
				Event: &proto.WorkflowEventTriggerInvocation{
					TriggerId: trigger.Event.TriggerID,
					Event:     event,
				},
			},
		}, nil
	case trigger.Manual:
		return &proto.WorkflowRunTrigger{
			Kind: &proto.WorkflowRunTrigger_Manual{
				Manual: &proto.WorkflowManualTrigger{},
			},
		}, nil
	default:
		return nil, nil
	}
}

func SignalRunResponseToProto(resp *coreworkflow.SignalRunResponse) (*proto.SignalWorkflowRunResponse, error) {
	if resp == nil {
		return nil, nil
	}
	run, err := RunToProto(resp.Run)
	if err != nil {
		return nil, err
	}
	signal, err := SignalToProto(resp.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.SignalWorkflowRunResponse{
		Run:         run,
		Signal:      signal,
		StartedRun:  resp.StartedRun,
		WorkflowKey: resp.WorkflowKey,
	}, nil
}

func workflowExtensionsFromProto(values map[string]*structpb.Value) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		if value == nil {
			out[key] = nil
			continue
		}
		out[key] = value.AsInterface()
	}
	return out
}

func TimeToProto(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func TimeFromProto(t *timestamppb.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	value := t.AsTime()
	return &value
}
