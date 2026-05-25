package workflows

import (
	"fmt"
	"strings"
	"time"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func workflowRunStatusFromProto(status proto.WorkflowRunStatus) (coreworkflow.RunStatus, error) {
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

func workflowActorToProto(actor coreworkflow.Actor) *proto.WorkflowActor {
	if actor == (coreworkflow.Actor{}) {
		return nil
	}
	return &proto.WorkflowActor{
		SubjectId:   actor.SubjectID,
		SubjectKind: actor.SubjectKind,
		DisplayName: actor.DisplayName,
		AuthSource:  actor.AuthSource,
	}
}

func workflowActorFromProto(actor *proto.WorkflowActor) coreworkflow.Actor {
	if actor == nil {
		return coreworkflow.Actor{}
	}
	return coreworkflow.Actor{
		SubjectID:   actor.GetSubjectId(),
		SubjectKind: actor.GetSubjectKind(),
		DisplayName: actor.GetDisplayName(),
		AuthSource:  actor.GetAuthSource(),
	}
}

func workflowEventToProto(event coreworkflow.Event) (*proto.WorkflowEvent, error) {
	data, err := structFromMap(event.Data)
	if err != nil {
		return nil, fmt.Errorf("workflow event data: %w", err)
	}
	extensions, err := workflowExtensionsToProto(event.Extensions)
	if err != nil {
		return nil, fmt.Errorf("workflow event extensions: %w", err)
	}
	return &proto.WorkflowEvent{
		Id:              event.ID,
		Source:          event.Source,
		SpecVersion:     event.SpecVersion,
		Type:            event.Type,
		Subject:         event.Subject,
		Time:            timeToProto(event.Time),
		Datacontenttype: event.DataContentType,
		Data:            data,
		Extensions:      extensions,
	}, nil
}

func workflowEventFromProto(event *proto.WorkflowEvent) (coreworkflow.Event, error) {
	if event == nil {
		return coreworkflow.Event{}, nil
	}
	extensions, err := workflowExtensionsFromProto(event.GetExtensions())
	if err != nil {
		return coreworkflow.Event{}, err
	}
	return coreworkflow.Event{
		ID:              event.GetId(),
		Source:          event.GetSource(),
		SpecVersion:     event.GetSpecVersion(),
		Type:            event.GetType(),
		Subject:         event.GetSubject(),
		Time:            timeFromProto(event.GetTime()),
		DataContentType: event.GetDatacontenttype(),
		Data:            mapFromStruct(event.GetData()),
		Extensions:      extensions,
	}, nil
}

func workflowEventMatchToProto(match coreworkflow.EventMatch) *proto.WorkflowEventMatch {
	return &proto.WorkflowEventMatch{
		Type:    match.Type,
		Source:  match.Source,
		Subject: match.Subject,
	}
}

func workflowEventMatchFromProto(match *proto.WorkflowEventMatch) coreworkflow.EventMatch {
	if match == nil {
		return coreworkflow.EventMatch{}
	}
	return coreworkflow.EventMatch{
		Type:    match.GetType(),
		Source:  match.GetSource(),
		Subject: match.GetSubject(),
	}
}

func workflowRunTriggerToProto(trigger coreworkflow.RunTrigger) (*proto.WorkflowRunTrigger, error) {
	switch {
	case trigger.Schedule != nil:
		return &proto.WorkflowRunTrigger{
			Kind: &proto.WorkflowRunTrigger_Schedule{
				Schedule: &proto.WorkflowScheduleTrigger{
					ScheduleId:   trigger.Schedule.ScheduleID,
					ScheduledFor: timeToProto(trigger.Schedule.ScheduledFor),
				},
			},
		}, nil
	case trigger.Event != nil:
		event, err := workflowEventToProto(trigger.Event.Event)
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

func workflowRunTriggerFromProto(trigger *proto.WorkflowRunTrigger) (coreworkflow.RunTrigger, error) {
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
				ScheduledFor: timeFromProto(kind.Schedule.GetScheduledFor()),
			},
		}, nil
	case *proto.WorkflowRunTrigger_Event:
		if kind.Event == nil {
			return coreworkflow.RunTrigger{}, nil
		}
		event, err := workflowEventFromProto(kind.Event.GetEvent())
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

func workflowRunFromProto(run *proto.BoundWorkflowRun) (*coreworkflow.Run, error) {
	if run == nil {
		return nil, nil
	}
	status, err := workflowRunStatusFromProto(run.GetStatus())
	if err != nil {
		return nil, err
	}
	trigger, err := workflowRunTriggerFromProto(run.GetTrigger())
	if err != nil {
		return nil, err
	}
	return &coreworkflow.Run{
		ID:            run.GetId(),
		Status:        status,
		WorkflowKey:   run.GetWorkflowKey(),
		Target:        workflowTargetFromProto(run.GetTarget()),
		DefinitionID:  run.GetDefinitionId(),
		Trigger:       trigger,
		CreatedBy:     workflowActorFromProto(run.GetCreatedBy()),
		CreatedAt:     timeFromProto(run.GetCreatedAt()),
		StartedAt:     timeFromProto(run.GetStartedAt()),
		CompletedAt:   timeFromProto(run.GetCompletedAt()),
		StatusMessage: run.GetStatusMessage(),
		ResultBody:    run.GetResultBody(),
	}, nil
}

func workflowRunToProto(run *coreworkflow.Run) (*proto.BoundWorkflowRun, error) {
	if run == nil {
		return nil, nil
	}
	target, err := workflowTargetToProto(run.Target)
	if err != nil {
		return nil, err
	}
	trigger, err := workflowRunTriggerToProto(run.Trigger)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowRun{
		Id:            run.ID,
		Status:        workflowRunStatusToProto(run.Status),
		Target:        target,
		Trigger:       trigger,
		CreatedAt:     timeToProto(run.CreatedAt),
		StartedAt:     timeToProto(run.StartedAt),
		CompletedAt:   timeToProto(run.CompletedAt),
		StatusMessage: run.StatusMessage,
		ResultBody:    run.ResultBody,
		CreatedBy:     workflowActorToProto(run.CreatedBy),
		WorkflowKey:   run.WorkflowKey,
		DefinitionId:  run.DefinitionID,
	}, nil
}

func workflowRunStatusToProto(status coreworkflow.RunStatus) proto.WorkflowRunStatus {
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

func workflowScheduleFromProto(schedule *proto.BoundWorkflowSchedule) (*coreworkflow.Schedule, error) {
	if schedule == nil {
		return nil, nil
	}
	return &coreworkflow.Schedule{
		ID:           schedule.GetId(),
		Cron:         schedule.GetCron(),
		Timezone:     schedule.GetTimezone(),
		Target:       workflowTargetFromProto(schedule.GetTarget()),
		DefinitionID: schedule.GetDefinitionId(),
		Paused:       schedule.GetPaused(),
		CreatedBy:    workflowActorFromProto(schedule.GetCreatedBy()),
		CreatedAt:    timeFromProto(schedule.GetCreatedAt()),
		UpdatedAt:    timeFromProto(schedule.GetUpdatedAt()),
		NextRunAt:    timeFromProto(schedule.GetNextRunAt()),
	}, nil
}

func workflowScheduleToProto(schedule *coreworkflow.Schedule) (*proto.BoundWorkflowSchedule, error) {
	if schedule == nil {
		return nil, nil
	}
	target, err := workflowTargetToProto(schedule.Target)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowSchedule{
		Id:           schedule.ID,
		Cron:         schedule.Cron,
		Timezone:     schedule.Timezone,
		Target:       target,
		Paused:       schedule.Paused,
		CreatedAt:    timeToProto(schedule.CreatedAt),
		UpdatedAt:    timeToProto(schedule.UpdatedAt),
		NextRunAt:    timeToProto(schedule.NextRunAt),
		CreatedBy:    workflowActorToProto(schedule.CreatedBy),
		DefinitionId: schedule.DefinitionID,
	}, nil
}

func workflowEventTriggerFromProto(trigger *proto.BoundWorkflowEventTrigger) (*coreworkflow.EventTrigger, error) {
	if trigger == nil {
		return nil, nil
	}
	return &coreworkflow.EventTrigger{
		ID:           trigger.GetId(),
		Match:        workflowEventMatchFromProto(trigger.GetMatch()),
		Target:       workflowTargetFromProto(trigger.GetTarget()),
		DefinitionID: trigger.GetDefinitionId(),
		Paused:       trigger.GetPaused(),
		CreatedBy:    workflowActorFromProto(trigger.GetCreatedBy()),
		CreatedAt:    timeFromProto(trigger.GetCreatedAt()),
		UpdatedAt:    timeFromProto(trigger.GetUpdatedAt()),
	}, nil
}

func workflowEventTriggerToProto(trigger *coreworkflow.EventTrigger) (*proto.BoundWorkflowEventTrigger, error) {
	if trigger == nil {
		return nil, nil
	}
	target, err := workflowTargetToProto(trigger.Target)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowEventTrigger{
		Id:           trigger.ID,
		Match:        workflowEventMatchToProto(trigger.Match),
		Target:       target,
		Paused:       trigger.Paused,
		CreatedAt:    timeToProto(trigger.CreatedAt),
		UpdatedAt:    timeToProto(trigger.UpdatedAt),
		CreatedBy:    workflowActorToProto(trigger.CreatedBy),
		DefinitionId: trigger.DefinitionID,
	}, nil
}

func workflowDefinitionFromProto(definition *proto.BoundWorkflowDefinition) (*coreworkflow.Definition, error) {
	if definition == nil {
		return nil, nil
	}
	return &coreworkflow.Definition{
		ID:        definition.GetId(),
		Target:    workflowTargetFromProto(definition.GetTarget()),
		CreatedBy: workflowActorFromProto(definition.GetCreatedBy()),
		CreatedAt: timeFromProto(definition.GetCreatedAt()),
	}, nil
}

func workflowDefinitionToProto(definition *coreworkflow.Definition) (*proto.BoundWorkflowDefinition, error) {
	if definition == nil {
		return nil, nil
	}
	target, err := workflowTargetToProto(definition.Target)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowDefinition{
		Id:        definition.ID,
		Target:    target,
		CreatedBy: workflowActorToProto(definition.CreatedBy),
		CreatedAt: timeToProto(definition.CreatedAt),
	}, nil
}

func workflowSignalToProto(signal coreworkflow.Signal) (*proto.WorkflowSignal, error) {
	payload, err := structFromMap(signal.Payload)
	if err != nil {
		return nil, fmt.Errorf("workflow signal payload: %w", err)
	}
	metadata, err := structFromMap(signal.Metadata)
	if err != nil {
		return nil, fmt.Errorf("workflow signal metadata: %w", err)
	}
	return &proto.WorkflowSignal{
		Id:             signal.ID,
		Name:           signal.Name,
		Payload:        payload,
		Metadata:       metadata,
		CreatedBy:      workflowActorToProto(signal.CreatedBy),
		CreatedAt:      timeToProto(signal.CreatedAt),
		IdempotencyKey: signal.IdempotencyKey,
		Sequence:       signal.Sequence,
	}, nil
}

func workflowSignalFromProto(signal *proto.WorkflowSignal) coreworkflow.Signal {
	if signal == nil {
		return coreworkflow.Signal{}
	}
	return coreworkflow.Signal{
		ID:             strings.TrimSpace(signal.GetId()),
		Name:           strings.TrimSpace(signal.GetName()),
		Payload:        mapFromStruct(signal.GetPayload()),
		Metadata:       mapFromStruct(signal.GetMetadata()),
		CreatedBy:      workflowActorFromProto(signal.GetCreatedBy()),
		CreatedAt:      timeFromProto(signal.GetCreatedAt()),
		IdempotencyKey: strings.TrimSpace(signal.GetIdempotencyKey()),
		Sequence:       signal.GetSequence(),
	}
}

func workflowSignalRunResponseFromProto(resp *proto.SignalWorkflowRunResponse) (*coreworkflow.SignalRunResponse, error) {
	if resp == nil {
		return nil, nil
	}
	run, err := workflowRunFromProto(resp.GetRun())
	if err != nil {
		return nil, err
	}
	return &coreworkflow.SignalRunResponse{
		Run:         run,
		Signal:      workflowSignalFromProto(resp.GetSignal()),
		StartedRun:  resp.GetStartedRun(),
		WorkflowKey: resp.GetWorkflowKey(),
	}, nil
}

func managedWorkflowScheduleToProto(managed *workflowmanager.ManagedSchedule) (*proto.BoundWorkflowSchedule, error) {
	if managed == nil {
		return nil, nil
	}
	schedule, err := workflowScheduleToProto(managed.Schedule)
	if err != nil {
		return nil, err
	}
	schedule.ProviderName = managed.ProviderName
	return schedule, nil
}

func managedWorkflowEventTriggerToProto(managed *workflowmanager.ManagedEventTrigger) (*proto.BoundWorkflowEventTrigger, error) {
	if managed == nil {
		return nil, nil
	}
	trigger, err := workflowEventTriggerToProto(managed.Trigger)
	if err != nil {
		return nil, err
	}
	trigger.ProviderName = managed.ProviderName
	return trigger, nil
}

func managedWorkflowDefinitionToProto(managed *workflowmanager.ManagedDefinition) (*proto.BoundWorkflowDefinition, error) {
	if managed == nil {
		return nil, nil
	}
	definition, err := workflowDefinitionToProto(managed.Definition)
	if err != nil {
		return nil, err
	}
	definition.ProviderName = managed.ProviderName
	return definition, nil
}

func managedWorkflowRunToProto(managed *workflowmanager.ManagedRun) (*proto.BoundWorkflowRun, error) {
	if managed == nil {
		return nil, nil
	}
	run, err := workflowRunToProto(managed.Run)
	if err != nil {
		return nil, err
	}
	run.ProviderName = managed.ProviderName
	return run, nil
}

func managedWorkflowRunSignalToProto(managed *workflowmanager.ManagedRunSignal) (*proto.SignalWorkflowRunResponse, error) {
	if managed == nil {
		return nil, nil
	}
	run, err := workflowRunToProto(managed.Run)
	if err != nil {
		return nil, err
	}
	run.ProviderName = managed.ProviderName
	signal, err := workflowSignalToProto(managed.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.SignalWorkflowRunResponse{
		Run:         run,
		Signal:      signal,
		StartedRun:  managed.StartedRun,
		WorkflowKey: managed.WorkflowKey,
	}, nil
}

func workflowExtensionsToProto(values map[string]any) (map[string]*structpb.Value, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]*structpb.Value, len(values))
	for key, value := range values {
		pbValue, err := workflowExtensionValueToProto(value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out[key] = pbValue
	}
	return out, nil
}

func workflowExtensionValueToProto(value any) (*structpb.Value, error) {
	if value == nil {
		return structpb.NewNullValue(), nil
	}
	return protoValueFromAny(value)
}

func workflowExtensionsFromProto(values map[string]*structpb.Value) (map[string]any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		if value == nil {
			out[key] = nil
			continue
		}
		out[key] = value.AsInterface()
	}
	return out, nil
}

func timeToProto(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func timeFromProto(t *timestamppb.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	value := t.AsTime()
	return &value
}
