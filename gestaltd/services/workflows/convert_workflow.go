package workflows

import (
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/internal/agentwire"
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

func workflowTargetToProto(target coreworkflow.Target) (*proto.BoundWorkflowTarget, error) {
	steps, err := workflowStepsToProto(target.Steps)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowTarget{Steps: steps}, nil
}

func workflowTargetFromProto(target *proto.BoundWorkflowTarget) coreworkflow.Target {
	if target == nil {
		return coreworkflow.Target{}
	}
	return coreworkflow.Target{Steps: workflowStepsFromProto(target.GetSteps())}
}

func workflowStepsToProto(steps []coreworkflow.Step) ([]*proto.WorkflowStep, error) {
	if len(steps) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowStep, 0, len(steps))
	for i := range steps {
		step, err := workflowStepToProto(steps[i])
		if err != nil {
			return nil, fmt.Errorf("workflow steps[%d]: %w", i, err)
		}
		out = append(out, step)
	}
	return out, nil
}

func workflowStepsFromProto(steps []*proto.WorkflowStep) []coreworkflow.Step {
	if len(steps) == 0 {
		return nil
	}
	out := make([]coreworkflow.Step, 0, len(steps))
	for _, step := range steps {
		if step == nil {
			continue
		}
		out = append(out, workflowStepFromProto(step))
	}
	return out
}

func workflowStepToProto(step coreworkflow.Step) (*proto.WorkflowStep, error) {
	inputs, err := workflowValueMapToProto(step.Inputs)
	if err != nil {
		return nil, fmt.Errorf("inputs: %w", err)
	}
	when, err := workflowStepWhenToProto(step.When)
	if err != nil {
		return nil, fmt.Errorf("when: %w", err)
	}
	metadata, err := structFromMap(step.Metadata)
	if err != nil {
		return nil, fmt.Errorf("metadata: %w", err)
	}
	out := &proto.WorkflowStep{
		Id:             step.ID,
		Inputs:         inputs,
		When:           when,
		TimeoutSeconds: int32(step.TimeoutSeconds),
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

func workflowStepFromProto(step *proto.WorkflowStep) coreworkflow.Step {
	out := coreworkflow.Step{
		ID:             strings.TrimSpace(step.GetId()),
		Inputs:         workflowValueMapFromProto(step.GetInputs()),
		When:           workflowStepWhenFromProto(step.GetWhen()),
		TimeoutSeconds: int(step.GetTimeoutSeconds()),
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

func workflowStepPluginCallToProto(target *coreworkflow.PluginCall) (*proto.WorkflowStepPluginCall, error) {
	if target == nil {
		return nil, nil
	}
	input, err := workflowValueToProto(target.Input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	return &proto.WorkflowStepPluginCall{
		Name:           target.Name,
		Operation:      target.Operation,
		Input:          input,
		Connection:     target.Connection,
		Instance:       target.Instance,
		CredentialMode: string(target.CredentialMode),
	}, nil
}

func workflowStepPluginCallFromProto(target *proto.WorkflowStepPluginCall) *coreworkflow.PluginCall {
	if target == nil {
		return nil
	}
	return &coreworkflow.PluginCall{
		Name:           strings.TrimSpace(target.GetName()),
		Operation:      strings.TrimSpace(target.GetOperation()),
		Connection:     strings.TrimSpace(target.GetConnection()),
		Instance:       strings.TrimSpace(target.GetInstance()),
		CredentialMode: core.NormalizeOptionalConnectionMode(core.ConnectionMode(target.GetCredentialMode())),
		Input:          workflowValueFromProto(target.GetInput()),
	}
}

func workflowStepAgentTurnToProto(target *coreworkflow.AgentTurn) (*proto.WorkflowStepAgentTurn, error) {
	if target == nil {
		return nil, nil
	}
	messages, err := workflowAgentMessagesToProto(target.Messages)
	if err != nil {
		return nil, err
	}
	responseSchema, err := structFromMap(target.ResponseSchema)
	if err != nil {
		return nil, fmt.Errorf("response_schema: %w", err)
	}
	modelOptions, err := structFromMap(target.ModelOptions)
	if err != nil {
		return nil, fmt.Errorf("model_options: %w", err)
	}
	return &proto.WorkflowStepAgentTurn{
		Provider:       target.ProviderName,
		Model:          target.Model,
		SessionKey:     target.SessionKey,
		Prompt:         workflowTextToProto(target.Prompt),
		Messages:       messages,
		Tools:          agentwire.ToolRefsToProto(target.ToolRefs),
		ResponseSchema: responseSchema,
		ModelOptions:   modelOptions,
	}, nil
}

func workflowStepAgentTurnFromProto(target *proto.WorkflowStepAgentTurn) *coreworkflow.AgentTurn {
	if target == nil {
		return nil
	}
	return &coreworkflow.AgentTurn{
		ProviderName:   strings.TrimSpace(target.GetProvider()),
		Model:          strings.TrimSpace(target.GetModel()),
		SessionKey:     strings.TrimSpace(target.GetSessionKey()),
		Prompt:         workflowTextFromProto(target.GetPrompt()),
		Messages:       workflowAgentMessagesFromProto(target.GetMessages()),
		ToolRefs:       agentwire.ToolRefsFromProto(target.GetTools()),
		ResponseSchema: mapFromStruct(target.GetResponseSchema()),
		ModelOptions:   mapFromStruct(target.GetModelOptions()),
	}
}

func workflowAgentMessagesToProto(messages []coreworkflow.AgentMessage) ([]*proto.WorkflowAgentMessage, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowAgentMessage, 0, len(messages))
	for i := range messages {
		message := messages[i]
		metadata, err := structFromMap(message.Metadata)
		if err != nil {
			return nil, fmt.Errorf("messages[%d].metadata: %w", i, err)
		}
		out = append(out, &proto.WorkflowAgentMessage{
			Role:     message.Role,
			Text:     workflowTextToProto(message.Text),
			Metadata: metadata,
		})
	}
	return out, nil
}

func workflowAgentMessagesFromProto(messages []*proto.WorkflowAgentMessage) []coreworkflow.AgentMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]coreworkflow.AgentMessage, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		out = append(out, coreworkflow.AgentMessage{
			Role:     strings.TrimSpace(message.GetRole()),
			Text:     workflowTextFromProto(message.GetText()),
			Metadata: mapFromStruct(message.GetMetadata()),
		})
	}
	return out
}

func workflowTextToProto(text coreworkflow.Text) *proto.WorkflowText {
	if text.Template == "" {
		return nil
	}
	return &proto.WorkflowText{Template: text.Template}
}

func workflowTextFromProto(text *proto.WorkflowText) coreworkflow.Text {
	if text == nil {
		return coreworkflow.Text{}
	}
	return coreworkflow.Text{Template: text.GetTemplate()}
}

func workflowStepWhenToProto(when *coreworkflow.StepWhen) (*proto.WorkflowStepWhen, error) {
	if when == nil {
		return nil, nil
	}
	if !when.EqualsSet {
		return nil, fmt.Errorf("equals is required")
	}
	value, err := workflowValueToProto(when.Value)
	if err != nil {
		return nil, fmt.Errorf("value: %w", err)
	}
	equals, err := protoValueFromAny(when.Equals)
	if err != nil {
		return nil, fmt.Errorf("equals: %w", err)
	}
	return &proto.WorkflowStepWhen{Value: value, Equals: equals}, nil
}

func workflowStepWhenFromProto(when *proto.WorkflowStepWhen) *coreworkflow.StepWhen {
	if when == nil {
		return nil
	}
	return &coreworkflow.StepWhen{
		Value:     workflowValueFromProto(when.GetValue()),
		Equals:    protoValueToAny(when.GetEquals()),
		EqualsSet: when.Equals != nil,
	}
}

func workflowValueMapToProto(values map[string]coreworkflow.Value) (map[string]*proto.WorkflowValue, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]*proto.WorkflowValue, len(values))
	for key := range values {
		converted, err := workflowValueToProto(values[key])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out[key] = converted
	}
	return out, nil
}

func workflowValueMapFromProto(values map[string]*proto.WorkflowValue) map[string]coreworkflow.Value {
	if len(values) == 0 {
		return nil
	}
	return workflowValueObjectMapFromProto(values)
}

func workflowValueObjectMapFromProto(values map[string]*proto.WorkflowValue) map[string]coreworkflow.Value {
	out := make(map[string]coreworkflow.Value, len(values))
	for key, value := range values {
		out[key] = workflowValueFromProto(value)
	}
	return out
}

func workflowValueToProto(value coreworkflow.Value) (*proto.WorkflowValue, error) {
	set := 0
	if value.LiteralSet {
		set++
	}
	if value.Object != nil {
		set++
	}
	if value.Array != nil {
		set++
	}
	if value.Template != nil {
		set++
	}
	if strings.TrimSpace(value.RunInput) != "" {
		set++
	}
	if strings.TrimSpace(value.SignalPayload) != "" {
		set++
	}
	if value.StepOutput != nil {
		set++
	}
	if set == 0 {
		return nil, nil
	}
	if set != 1 {
		return nil, fmt.Errorf("must set exactly one value kind")
	}
	switch {
	case value.LiteralSet:
		literal, err := protoValueFromAny(value.Literal)
		if err != nil {
			return nil, err
		}
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Literal{Literal: literal}}, nil
	case value.Object != nil:
		fields, err := workflowValueMapToProto(value.Object)
		if err != nil {
			return nil, err
		}
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Object{Object: &proto.WorkflowObject{Fields: fields}}}, nil
	case value.Array != nil:
		items := make([]*proto.WorkflowValue, 0, len(value.Array))
		for i := range value.Array {
			item, err := workflowValueToProto(value.Array[i])
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			items = append(items, item)
		}
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Array{Array: &proto.WorkflowArray{Values: items}}}, nil
	case value.Template != nil:
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Template{Template: workflowTextToProto(*value.Template)}}, nil
	case strings.TrimSpace(value.RunInput) != "":
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_RunInput{RunInput: &proto.WorkflowPathSource{Path: value.RunInput}}}, nil
	case strings.TrimSpace(value.SignalPayload) != "":
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_SignalPayload{SignalPayload: &proto.WorkflowPathSource{Path: value.SignalPayload}}}, nil
	case value.StepOutput != nil:
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_StepOutput{StepOutput: &proto.WorkflowStepOutputSource{
			StepId: value.StepOutput.StepID,
			Path:   value.StepOutput.Path,
		}}}, nil
	default:
		return nil, nil
	}
}

func workflowValueFromProto(value *proto.WorkflowValue) coreworkflow.Value {
	if value == nil {
		return coreworkflow.Value{}
	}
	switch typed := value.GetKind().(type) {
	case *proto.WorkflowValue_Literal:
		return coreworkflow.Value{Literal: protoValueToAny(typed.Literal), LiteralSet: true}
	case *proto.WorkflowValue_Object:
		return coreworkflow.Value{Object: workflowValueObjectMapFromProto(typed.Object.GetFields())}
	case *proto.WorkflowValue_Array:
		items := typed.Array.GetValues()
		out := make([]coreworkflow.Value, 0, len(items))
		for _, item := range items {
			out = append(out, workflowValueFromProto(item))
		}
		return coreworkflow.Value{Array: out}
	case *proto.WorkflowValue_Template:
		text := workflowTextFromProto(typed.Template)
		return coreworkflow.Value{Template: &text}
	case *proto.WorkflowValue_RunInput:
		return coreworkflow.Value{RunInput: strings.TrimSpace(typed.RunInput.GetPath())}
	case *proto.WorkflowValue_SignalPayload:
		return coreworkflow.Value{SignalPayload: strings.TrimSpace(typed.SignalPayload.GetPath())}
	case *proto.WorkflowValue_StepOutput:
		return coreworkflow.Value{StepOutput: &coreworkflow.StepOutputSource{
			StepID: strings.TrimSpace(typed.StepOutput.GetStepId()),
			Path:   strings.TrimSpace(typed.StepOutput.GetPath()),
		}}
	default:
		return coreworkflow.Value{}
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

func workflowRunAsSubjectToProto(subject *core.RunAsSubject) *proto.WorkflowRunAsSubject {
	subject = core.NormalizeRunAsSubject(subject)
	if subject == nil {
		return nil
	}
	return &proto.WorkflowRunAsSubject{
		SubjectId:   subject.SubjectID,
		SubjectKind: subject.SubjectKind,
		DisplayName: subject.DisplayName,
		AuthSource:  subject.AuthSource,
	}
}

func workflowRunAsSubjectFromProto(subject *proto.WorkflowRunAsSubject) *core.RunAsSubject {
	if subject == nil {
		return nil
	}
	return core.NormalizeRunAsSubject(&core.RunAsSubject{
		SubjectID:   subject.GetSubjectId(),
		SubjectKind: subject.GetSubjectKind(),
		DisplayName: subject.GetDisplayName(),
		AuthSource:  subject.GetAuthSource(),
	})
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
		CallerPluginName:    ref.CallerPluginName,
		SourceDefinitionId:  ref.SourceDefinitionID,
		SubjectId:           ref.SubjectID,
		SubjectKind:         ref.SubjectKind,
		DisplayName:         ref.DisplayName,
		AuthSource:          ref.AuthSource,
		CredentialSubjectId: ref.CredentialSubjectID,
		RunAs:               workflowRunAsSubjectToProto(ref.RunAs),
		Permissions:         workflowAccessPermissionsToProto(ref.Permissions),
		CreatedAt:           timeToProto(ref.CreatedAt),
		RevokedAt:           timeToProto(ref.RevokedAt),
	}, nil
}

func workflowExecutionReferenceFromProto(ref *proto.WorkflowExecutionReference) (*coreworkflow.ExecutionReference, error) {
	if ref == nil {
		return nil, nil
	}
	target := workflowTargetFromProto(ref.GetTarget())
	return &coreworkflow.ExecutionReference{
		ID:                  strings.TrimSpace(ref.GetId()),
		ProviderName:        strings.TrimSpace(ref.GetProviderName()),
		Target:              target,
		CallerPluginName:    strings.TrimSpace(ref.GetCallerPluginName()),
		SourceDefinitionID:  strings.TrimSpace(ref.GetSourceDefinitionId()),
		SubjectID:           strings.TrimSpace(ref.GetSubjectId()),
		SubjectKind:         strings.TrimSpace(ref.GetSubjectKind()),
		DisplayName:         strings.TrimSpace(ref.GetDisplayName()),
		AuthSource:          strings.TrimSpace(ref.GetAuthSource()),
		CredentialSubjectID: strings.TrimSpace(ref.GetCredentialSubjectId()),
		RunAs:               workflowRunAsSubjectFromProto(ref.GetRunAs()),
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
		WorkflowKey:   run.GetWorkflowKey(),
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
		ExecutionRef:  run.ExecutionRef,
		WorkflowKey:   run.WorkflowKey,
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

func workflowDefinitionToProto(ref *coreworkflow.ExecutionReference) (*proto.BoundWorkflowDefinition, error) {
	if ref == nil {
		return nil, nil
	}
	target, err := workflowTargetToProto(ref.Target)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowDefinition{
		Id:     ref.ID,
		Target: target,
		CreatedBy: workflowActorToProto(coreworkflow.Actor{
			SubjectID:   ref.SubjectID,
			SubjectKind: ref.SubjectKind,
			DisplayName: ref.DisplayName,
			AuthSource:  ref.AuthSource,
		}),
		CreatedAt: timeToProto(ref.CreatedAt),
	}, nil
}

func workflowInvokeRequestFromProto(req *proto.InvokeWorkflowOperationRequest) (coreworkflow.InvokeOperationRequest, error) {
	if req == nil {
		return coreworkflow.InvokeOperationRequest{}, nil
	}
	target := workflowTargetFromProto(req.GetTarget())
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
		Signals:      workflowSignalsFromProto(req.GetSignals()),
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

func workflowSignalsFromProto(signals []*proto.WorkflowSignal) []coreworkflow.Signal {
	if len(signals) == 0 {
		return nil
	}
	out := make([]coreworkflow.Signal, 0, len(signals))
	for _, signal := range signals {
		out = append(out, workflowSignalFromProto(signal))
	}
	return out
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

func workflowInvokeResponseToProto(resp *coreworkflow.InvokeOperationResponse) *proto.InvokeWorkflowOperationResponse {
	if resp == nil {
		return nil
	}
	return &proto.InvokeWorkflowOperationResponse{
		Status: int32(resp.Status),
		Body:   resp.Body,
	}
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
