package providerhost

import (
	"fmt"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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

func workflowTargetToProto(target coreworkflow.Target) (*proto.BoundWorkflowTarget, error) {
	input, err := structFromMap(target.Input)
	if err != nil {
		return nil, fmt.Errorf("workflow target input: %w", err)
	}
	return &proto.BoundWorkflowTarget{
		PluginName: target.PluginName,
		Operation:  target.Operation,
		Input:      input,
	}, nil
}

func workflowTargetFromProto(target *proto.BoundWorkflowTarget) coreworkflow.Target {
	if target == nil {
		return coreworkflow.Target{}
	}
	return coreworkflow.Target{
		PluginName: target.GetPluginName(),
		Operation:  target.GetOperation(),
		Input:      mapFromStruct(target.GetInput()),
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

func pluginWorkflowTargetToProto(target coreworkflow.Target) (*proto.WorkflowTarget, error) {
	input, err := structFromMap(target.Input)
	if err != nil {
		return nil, fmt.Errorf("workflow target input: %w", err)
	}
	return &proto.WorkflowTarget{
		Operation: target.Operation,
		Input:     input,
	}, nil
}

func pluginWorkflowTargetFromProto(pluginName string, target *proto.WorkflowTarget) coreworkflow.Target {
	if target == nil {
		return coreworkflow.Target{PluginName: pluginName}
	}
	return coreworkflow.Target{
		PluginName: pluginName,
		Operation:  target.GetOperation(),
		Input:      mapFromStruct(target.GetInput()),
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
		Target:        workflowTargetFromProto(run.GetTarget()),
		Trigger:       trigger,
		CreatedBy:     workflowActorFromProto(run.GetCreatedBy()),
		CreatedAt:     timeFromProto(run.GetCreatedAt()),
		StartedAt:     timeFromProto(run.GetStartedAt()),
		CompletedAt:   timeFromProto(run.GetCompletedAt()),
		StatusMessage: run.GetStatusMessage(),
		ResultBody:    run.GetResultBody(),
	}, nil
}

func workflowRunToPluginProto(run *coreworkflow.Run) (*proto.WorkflowRun, error) {
	if run == nil {
		return nil, nil
	}
	target, err := pluginWorkflowTargetToProto(run.Target)
	if err != nil {
		return nil, err
	}
	trigger, err := workflowRunTriggerToProto(run.Trigger)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowRun{
		Id:            run.ID,
		Status:        workflowRunStatusToProto(run.Status),
		Target:        target,
		Trigger:       trigger,
		CreatedBy:     workflowActorToProto(run.CreatedBy),
		CreatedAt:     timeToProto(run.CreatedAt),
		StartedAt:     timeToProto(run.StartedAt),
		CompletedAt:   timeToProto(run.CompletedAt),
		StatusMessage: run.StatusMessage,
		ResultBody:    run.ResultBody,
	}, nil
}

func workflowScheduleFromProto(schedule *proto.BoundWorkflowSchedule) (*coreworkflow.Schedule, error) {
	if schedule == nil {
		return nil, nil
	}
	return &coreworkflow.Schedule{
		ID:        schedule.GetId(),
		Cron:      schedule.GetCron(),
		Timezone:  schedule.GetTimezone(),
		Target:    workflowTargetFromProto(schedule.GetTarget()),
		Paused:    schedule.GetPaused(),
		CreatedBy: workflowActorFromProto(schedule.GetCreatedBy()),
		CreatedAt: timeFromProto(schedule.GetCreatedAt()),
		UpdatedAt: timeFromProto(schedule.GetUpdatedAt()),
		NextRunAt: timeFromProto(schedule.GetNextRunAt()),
	}, nil
}

func workflowScheduleToPluginProto(schedule *coreworkflow.Schedule) (*proto.WorkflowSchedule, error) {
	if schedule == nil {
		return nil, nil
	}
	target, err := pluginWorkflowTargetToProto(schedule.Target)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowSchedule{
		Id:        schedule.ID,
		Cron:      schedule.Cron,
		Timezone:  schedule.Timezone,
		Target:    target,
		Paused:    schedule.Paused,
		CreatedBy: workflowActorToProto(schedule.CreatedBy),
		CreatedAt: timeToProto(schedule.CreatedAt),
		UpdatedAt: timeToProto(schedule.UpdatedAt),
		NextRunAt: timeToProto(schedule.NextRunAt),
	}, nil
}

func workflowEventTriggerFromProto(trigger *proto.BoundWorkflowEventTrigger) (*coreworkflow.EventTrigger, error) {
	if trigger == nil {
		return nil, nil
	}
	return &coreworkflow.EventTrigger{
		ID:        trigger.GetId(),
		Match:     workflowEventMatchFromProto(trigger.GetMatch()),
		Target:    workflowTargetFromProto(trigger.GetTarget()),
		Paused:    trigger.GetPaused(),
		CreatedBy: workflowActorFromProto(trigger.GetCreatedBy()),
		CreatedAt: timeFromProto(trigger.GetCreatedAt()),
		UpdatedAt: timeFromProto(trigger.GetUpdatedAt()),
	}, nil
}

func workflowEventTriggerToPluginProto(trigger *coreworkflow.EventTrigger) (*proto.WorkflowEventTrigger, error) {
	if trigger == nil {
		return nil, nil
	}
	target, err := pluginWorkflowTargetToProto(trigger.Target)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowEventTrigger{
		Id:        trigger.ID,
		Match:     workflowEventMatchToProto(trigger.Match),
		Target:    target,
		Paused:    trigger.Paused,
		CreatedBy: workflowActorToProto(trigger.CreatedBy),
		CreatedAt: timeToProto(trigger.CreatedAt),
		UpdatedAt: timeToProto(trigger.UpdatedAt),
	}, nil
}

func workflowInvokeRequestFromProto(req *proto.InvokeWorkflowOperationRequest) (coreworkflow.InvokeOperationRequest, error) {
	if req == nil {
		return coreworkflow.InvokeOperationRequest{}, nil
	}
	trigger, err := workflowRunTriggerFromProto(req.GetTrigger())
	if err != nil {
		return coreworkflow.InvokeOperationRequest{}, err
	}
	return coreworkflow.InvokeOperationRequest{
		PluginName: req.GetPluginName(),
		RunID:      req.GetRunId(),
		Trigger:    trigger,
		Target:     workflowTargetFromProto(req.GetTarget()),
		Input:      mapFromStruct(req.GetInput()),
		Metadata:   mapFromStruct(req.GetMetadata()),
		CreatedBy:  workflowActorFromProto(req.GetCreatedBy()),
	}, nil
}

func workflowInvokeResponseToProto(resp *coreworkflow.InvokeOperationResponse) *proto.InvokeWorkflowOperationResponse {
	if resp == nil {
		return nil
	}
	return &proto.InvokeWorkflowOperationResponse{
		Status: int32(resp.Status),
		Body:   resp.Body,
	}
}

func workflowExtensionsToProto(values map[string]any) (map[string]*structpb.Value, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]*structpb.Value, len(values))
	for key, value := range values {
		normalized, err := normalizeStructValue(value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		pbValue, err := structpb.NewValue(normalized)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out[key] = pbValue
	}
	return out, nil
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
