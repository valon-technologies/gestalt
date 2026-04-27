package providerhost

import (
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/workflowmanager"
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

func workflowTargetToProto(target coreworkflow.Target) (*proto.BoundWorkflowTarget, error) {
	var pluginTarget coreworkflow.PluginTarget
	if target.Plugin != nil {
		pluginTarget = *target.Plugin
	}
	plugin, err := workflowPluginTargetToProto(pluginTarget)
	if err != nil {
		return nil, err
	}
	if target.Agent != nil && plugin != nil {
		return nil, fmt.Errorf("workflow target cannot include both agent and plugin fields")
	}
	agent, err := workflowAgentTargetToProto(target.Agent)
	if err != nil {
		return nil, err
	}
	value := &proto.BoundWorkflowTarget{
		Plugin: plugin,
		Agent:  agent,
	}
	return value, nil
}

func workflowTargetFromProto(target *proto.BoundWorkflowTarget) coreworkflow.Target {
	if target == nil {
		return coreworkflow.Target{}
	}
	plugin := workflowPluginTargetFromProto(target.GetPlugin())
	out := coreworkflow.Target{Agent: workflowAgentTargetFromProto(target.GetAgent())}
	if coreworkflow.PluginTargetSet(plugin) {
		out.Plugin = &plugin
	}
	return out
}

func workflowTargetFromProtoStrict(target *proto.BoundWorkflowTarget) (coreworkflow.Target, error) {
	if err := validateWorkflowTargetProtoKinds(target); err != nil {
		return coreworkflow.Target{}, err
	}
	return workflowTargetFromProto(target), nil
}

func validateWorkflowTargetProtoKinds(target *proto.BoundWorkflowTarget) error {
	if target == nil || target.GetAgent() == nil || target.GetPlugin() == nil {
		return nil
	}
	return fmt.Errorf("target cannot include both agent and plugin fields")
}

func workflowPluginTargetToProto(target coreworkflow.PluginTarget) (*proto.BoundWorkflowPluginTarget, error) {
	if coreworkflow.PluginTargetEmpty(target) {
		return nil, nil
	}
	input, err := structFromMap(target.Input)
	if err != nil {
		return nil, fmt.Errorf("workflow target input: %w", err)
	}
	return &proto.BoundWorkflowPluginTarget{
		PluginName: target.PluginName,
		Operation:  target.Operation,
		Input:      input,
		Connection: target.Connection,
		Instance:   target.Instance,
	}, nil
}

func workflowPluginTargetFromProto(target *proto.BoundWorkflowPluginTarget) coreworkflow.PluginTarget {
	if target == nil {
		return coreworkflow.PluginTarget{}
	}
	return coreworkflow.PluginTarget{
		PluginName: strings.TrimSpace(target.GetPluginName()),
		Operation:  strings.TrimSpace(target.GetOperation()),
		Connection: strings.TrimSpace(target.GetConnection()),
		Instance:   strings.TrimSpace(target.GetInstance()),
		Input:      mapFromStruct(target.GetInput()),
	}
}

func workflowAgentTargetToProto(target *coreworkflow.AgentTarget) (*proto.BoundWorkflowAgentTarget, error) {
	if target == nil {
		return nil, nil
	}
	messages, err := agentMessagesToProto(target.Messages)
	if err != nil {
		return nil, err
	}
	responseSchema, err := structFromMap(target.ResponseSchema)
	if err != nil {
		return nil, fmt.Errorf("workflow agent response_schema: %w", err)
	}
	metadata, err := structFromMap(target.Metadata)
	if err != nil {
		return nil, fmt.Errorf("workflow agent metadata: %w", err)
	}
	providerOptions, err := structFromMap(target.ProviderOptions)
	if err != nil {
		return nil, fmt.Errorf("workflow agent provider_options: %w", err)
	}
	return &proto.BoundWorkflowAgentTarget{
		ProviderName:    target.ProviderName,
		Model:           target.Model,
		Prompt:          target.Prompt,
		Messages:        messages,
		ToolRefs:        agentToolRefsToProto(target.ToolRefs),
		ToolSource:      agentToolSourceModeToProto(target.ToolSource),
		ResponseSchema:  responseSchema,
		Metadata:        metadata,
		ProviderOptions: providerOptions,
		TimeoutSeconds:  int32(target.TimeoutSeconds),
	}, nil
}

func workflowAgentTargetFromProto(target *proto.BoundWorkflowAgentTarget) *coreworkflow.AgentTarget {
	if target == nil {
		return nil
	}
	return &coreworkflow.AgentTarget{
		ProviderName:    strings.TrimSpace(target.GetProviderName()),
		Model:           strings.TrimSpace(target.GetModel()),
		Prompt:          target.GetPrompt(),
		Messages:        agentMessagesFromProto(target.GetMessages()),
		ToolRefs:        agentToolRefsFromProto(target.GetToolRefs()),
		ToolSource:      agentToolSourceModeFromProto(target.GetToolSource()),
		ResponseSchema:  mapFromStruct(target.GetResponseSchema()),
		Metadata:        mapFromStruct(target.GetMetadata()),
		ProviderOptions: mapFromStruct(target.GetProviderOptions()),
		TimeoutSeconds:  int(target.GetTimeoutSeconds()),
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

func workflowExecutionReferenceToProto(ref *coreworkflow.ExecutionReference) (*proto.WorkflowExecutionReference, error) {
	if ref == nil {
		return nil, nil
	}
	target, err := workflowTargetToProto(ref.Target)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowExecutionReference{
		Id:                  ref.ID,
		ProviderName:        ref.ProviderName,
		Target:              target,
		TargetFingerprint:   ref.TargetFingerprint,
		SubjectId:           ref.SubjectID,
		CredentialSubjectId: ref.CredentialSubjectID,
		Permissions:         workflowAccessPermissionsToProto(ref.Permissions),
		CreatedAt:           timeToProto(ref.CreatedAt),
		RevokedAt:           timeToProto(ref.RevokedAt),
	}, nil
}

func workflowExecutionReferenceFromProto(ref *proto.WorkflowExecutionReference) (*coreworkflow.ExecutionReference, error) {
	if ref == nil {
		return nil, nil
	}
	target, err := workflowTargetFromProtoStrict(ref.GetTarget())
	if err != nil {
		return nil, err
	}
	return &coreworkflow.ExecutionReference{
		ID:                  strings.TrimSpace(ref.GetId()),
		ProviderName:        strings.TrimSpace(ref.GetProviderName()),
		Target:              target,
		TargetFingerprint:   strings.TrimSpace(ref.GetTargetFingerprint()),
		SubjectID:           strings.TrimSpace(ref.GetSubjectId()),
		CredentialSubjectID: strings.TrimSpace(ref.GetCredentialSubjectId()),
		Permissions:         workflowAccessPermissionsFromProto(ref.GetPermissions()),
		CreatedAt:           timeFromProto(ref.GetCreatedAt()),
		RevokedAt:           timeFromProto(ref.GetRevokedAt()),
	}, nil
}

func workflowAccessPermissionsToProto(values []core.AccessPermission) []*proto.WorkflowAccessPermission {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.WorkflowAccessPermission, 0, len(values))
	for _, value := range values {
		pluginName := strings.TrimSpace(value.Plugin)
		if pluginName == "" {
			continue
		}
		out = append(out, &proto.WorkflowAccessPermission{
			Plugin:     pluginName,
			Operations: append([]string(nil), value.Operations...),
		})
	}
	return out
}

func workflowAccessPermissionsFromProto(values []*proto.WorkflowAccessPermission) []core.AccessPermission {
	if len(values) == 0 {
		return nil
	}
	out := make([]core.AccessPermission, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		pluginName := strings.TrimSpace(value.GetPlugin())
		if pluginName == "" {
			continue
		}
		out = append(out, core.AccessPermission{
			Plugin:     pluginName,
			Operations: append([]string(nil), value.GetOperations()...),
		})
	}
	return out
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
		ExecutionRef:  run.GetExecutionRef(),
		CreatedBy:     workflowActorFromProto(run.GetCreatedBy()),
		CreatedAt:     timeFromProto(run.GetCreatedAt()),
		StartedAt:     timeFromProto(run.GetStartedAt()),
		CompletedAt:   timeFromProto(run.GetCompletedAt()),
		StatusMessage: run.GetStatusMessage(),
		ResultBody:    run.GetResultBody(),
	}, nil
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
		Paused:       schedule.GetPaused(),
		ExecutionRef: schedule.GetExecutionRef(),
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
		ExecutionRef: schedule.ExecutionRef,
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
		Paused:       trigger.GetPaused(),
		ExecutionRef: trigger.GetExecutionRef(),
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
		ExecutionRef: trigger.ExecutionRef,
	}, nil
}

func workflowInvokeRequestFromProto(req *proto.InvokeWorkflowOperationRequest) (coreworkflow.InvokeOperationRequest, error) {
	if req == nil {
		return coreworkflow.InvokeOperationRequest{}, nil
	}
	target, err := workflowTargetFromProtoStrict(req.GetTarget())
	if err != nil {
		return coreworkflow.InvokeOperationRequest{}, err
	}
	trigger, err := workflowRunTriggerFromProto(req.GetTrigger())
	if err != nil {
		return coreworkflow.InvokeOperationRequest{}, err
	}
	return coreworkflow.InvokeOperationRequest{
		RunID:        req.GetRunId(),
		Trigger:      trigger,
		Target:       target,
		Input:        mapFromStruct(req.GetInput()),
		Metadata:     mapFromStruct(req.GetMetadata()),
		CreatedBy:    workflowActorFromProto(req.GetCreatedBy()),
		ExecutionRef: req.GetExecutionRef(),
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

func managedWorkflowScheduleToProto(managed *workflowmanager.ManagedSchedule) (*proto.ManagedWorkflowSchedule, error) {
	if managed == nil {
		return nil, nil
	}
	schedule, err := workflowScheduleToProto(managed.Schedule)
	if err != nil {
		return nil, err
	}
	return &proto.ManagedWorkflowSchedule{
		ProviderName: managed.ProviderName,
		Schedule:     schedule,
	}, nil
}

func managedWorkflowEventTriggerToProto(managed *workflowmanager.ManagedEventTrigger) (*proto.ManagedWorkflowEventTrigger, error) {
	if managed == nil {
		return nil, nil
	}
	trigger, err := workflowEventTriggerToProto(managed.Trigger)
	if err != nil {
		return nil, err
	}
	return &proto.ManagedWorkflowEventTrigger{
		ProviderName: managed.ProviderName,
		Trigger:      trigger,
	}, nil
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
