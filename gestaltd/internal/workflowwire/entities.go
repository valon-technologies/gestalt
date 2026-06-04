package workflowwire

import (
	"fmt"
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

func StepStatusToProto(status coreworkflow.StepStatus) proto.WorkflowStepStatus {
	switch status {
	case coreworkflow.StepStatusPending:
		return proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_PENDING
	case coreworkflow.StepStatusRunning:
		return proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_RUNNING
	case coreworkflow.StepStatusSkipped:
		return proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_SKIPPED
	case coreworkflow.StepStatusSucceeded:
		return proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_SUCCEEDED
	case coreworkflow.StepStatusFailed:
		return proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_FAILED
	case coreworkflow.StepStatusUnknown:
		return proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_UNKNOWN
	default:
		return proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_UNSPECIFIED
	}
}

func StepStatusFromProto(status proto.WorkflowStepStatus) (coreworkflow.StepStatus, error) {
	switch status {
	case proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_UNSPECIFIED:
		return "", nil
	case proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_PENDING:
		return coreworkflow.StepStatusPending, nil
	case proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_RUNNING:
		return coreworkflow.StepStatusRunning, nil
	case proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_SKIPPED:
		return coreworkflow.StepStatusSkipped, nil
	case proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_SUCCEEDED:
		return coreworkflow.StepStatusSucceeded, nil
	case proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_FAILED:
		return coreworkflow.StepStatusFailed, nil
	case proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_UNKNOWN:
		return coreworkflow.StepStatusUnknown, nil
	default:
		return "", fmt.Errorf("unknown workflow step status %v", status)
	}
}

func RunFromProto(run *proto.WorkflowRun) (*coreworkflow.Run, error) {
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
	steps, err := StepExecutionsFromProto(run.GetSteps())
	if err != nil {
		return nil, err
	}
	return &coreworkflow.Run{
		ID:                   run.GetId(),
		Status:               status,
		WorkflowKey:          run.GetWorkflowKey(),
		Target:               TargetFromProto(run.GetTarget()),
		DefinitionID:         run.GetDefinitionId(),
		DefinitionGeneration: run.GetDefinitionGeneration(),
		Input:                protoutil.MapFromStruct(run.GetInput()),
		CurrentStepID:        run.GetCurrentStepId(),
		Steps:                steps,
		Trigger:              trigger,
		CreatedBySubjectID:   run.GetCreatedBySubjectId(),
		RunAs:                agentwire.RunAsSubjectFromProto(run.GetRunAs()),
		CreatedAt:            TimeFromProto(run.GetCreatedAt()),
		StartedAt:            TimeFromProto(run.GetStartedAt()),
		CompletedAt:          TimeFromProto(run.GetCompletedAt()),
		StatusMessage:        run.GetStatusMessage(),
		Output:               protoutil.ValueToAny(run.GetOutput()),
	}, nil
}

func RunToProto(run *coreworkflow.Run) (*proto.WorkflowRun, error) {
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
	output, err := protoutil.ValueFromAny(run.Output)
	if err != nil {
		return nil, fmt.Errorf("workflow run output: %w", err)
	}
	inputStruct, err := protoutil.StructFromMap(run.Input)
	if err != nil {
		return nil, fmt.Errorf("workflow run input: %w", err)
	}
	steps, err := StepExecutionsToProto(run.Steps)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowRun{
		Id:                   run.ID,
		Status:               RunStatusToProto(run.Status),
		Target:               target,
		Trigger:              trigger,
		CreatedAt:            TimeToProto(run.CreatedAt),
		StartedAt:            TimeToProto(run.StartedAt),
		CompletedAt:          TimeToProto(run.CompletedAt),
		StatusMessage:        run.StatusMessage,
		Output:               output,
		CreatedBySubjectId:   run.CreatedBySubjectID,
		WorkflowKey:          run.WorkflowKey,
		DefinitionId:         run.DefinitionID,
		DefinitionGeneration: run.DefinitionGeneration,
		Input:                inputStruct,
		CurrentStepId:        run.CurrentStepID,
		Steps:                steps,
		RunAs:                agentwire.RunAsSubjectToProto(run.RunAs),
	}, nil
}

func DefinitionSpecFromProto(definition *proto.WorkflowDefinitionSpec) (*coreworkflow.DefinitionSpec, error) {
	if definition == nil {
		return nil, nil
	}
	activations, err := ActivationsFromProto(definition.GetActivations())
	if err != nil {
		return nil, err
	}
	return &coreworkflow.DefinitionSpec{
		ID:          definition.GetId(),
		Target:      TargetFromProto(definition.GetTarget()),
		Activations: activations,
		Paused:      definition.GetPaused(),
		RunAs:       agentwire.RunAsSubjectFromProto(definition.GetRunAs()),
	}, nil
}

func DefinitionSpecToProto(definition *coreworkflow.DefinitionSpec) (*proto.WorkflowDefinitionSpec, error) {
	if definition == nil {
		return nil, nil
	}
	target, err := TargetToProto(definition.Target)
	if err != nil {
		return nil, err
	}
	activations, err := ActivationsToProto(definition.Activations)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowDefinitionSpec{
		Id:          definition.ID,
		Target:      target,
		Activations: activations,
		Paused:      definition.Paused,
		RunAs:       agentwire.RunAsSubjectToProto(definition.RunAs),
	}, nil
}

func DefinitionFromProto(definition *proto.WorkflowDefinition) (*coreworkflow.Definition, error) {
	if definition == nil {
		return nil, nil
	}
	activations, err := ActivationsFromProto(definition.GetActivations())
	if err != nil {
		return nil, err
	}
	return &coreworkflow.Definition{
		ID:                 definition.GetId(),
		Generation:         definition.GetGeneration(),
		Target:             TargetFromProto(definition.GetTarget()),
		Activations:        activations,
		Paused:             definition.GetPaused(),
		CreatedBySubjectID: definition.GetCreatedBySubjectId(),
		CreatedAt:          TimeFromProto(definition.GetCreatedAt()),
		UpdatedAt:          TimeFromProto(definition.GetUpdatedAt()),
		ProviderName:       definition.GetProviderName(),
		RunAs:              agentwire.RunAsSubjectFromProto(definition.GetRunAs()),
	}, nil
}

func DefinitionToProto(definition *coreworkflow.Definition) (*proto.WorkflowDefinition, error) {
	if definition == nil {
		return nil, nil
	}
	target, err := TargetToProto(definition.Target)
	if err != nil {
		return nil, err
	}
	activations, err := ActivationsToProto(definition.Activations)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowDefinition{
		Id:                 definition.ID,
		Generation:         definition.Generation,
		Target:             target,
		Activations:        activations,
		Paused:             definition.Paused,
		CreatedBySubjectId: definition.CreatedBySubjectID,
		CreatedAt:          TimeToProto(definition.CreatedAt),
		UpdatedAt:          TimeToProto(definition.UpdatedAt),
		ProviderName:       definition.ProviderName,
		RunAs:              agentwire.RunAsSubjectToProto(definition.RunAs),
	}, nil
}

func ActivationsToProto(values []coreworkflow.Activation) ([]*proto.WorkflowActivation, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowActivation, 0, len(values))
	for i := range values {
		activation, err := ActivationToProto(values[i])
		if err != nil {
			return nil, fmt.Errorf("workflow activations[%d]: %w", i, err)
		}
		out = append(out, activation)
	}
	return out, nil
}

func ActivationToProto(activation coreworkflow.Activation) (*proto.WorkflowActivation, error) {
	input, err := workflowValueToProto(activation.Input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	out := &proto.WorkflowActivation{
		Id:     activation.ID,
		Input:  input,
		Paused: activation.Paused,
	}
	switch {
	case activation.Schedule != nil && activation.Event != nil:
		return nil, fmt.Errorf("activation must set exactly one of schedule or event")
	case activation.Schedule != nil:
		out.Trigger = &proto.WorkflowActivation_Schedule{Schedule: &proto.WorkflowScheduleActivation{
			Cron:     activation.Schedule.Cron,
			Timezone: activation.Schedule.Timezone,
		}}
	case activation.Event != nil:
		out.Trigger = &proto.WorkflowActivation_Event{Event: &proto.WorkflowEventActivation{
			Match: EventMatchToProto(activation.Event.Match),
		}}
	default:
		return nil, fmt.Errorf("activation must set schedule or event")
	}
	return out, nil
}

func ActivationsFromProto(values []*proto.WorkflowActivation) ([]coreworkflow.Activation, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]coreworkflow.Activation, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, ActivationFromProto(value))
	}
	return out, nil
}

func ActivationFromProto(value *proto.WorkflowActivation) coreworkflow.Activation {
	out := coreworkflow.Activation{
		ID:     value.GetId(),
		Input:  workflowValueFromProto(value.GetInput()),
		Paused: value.GetPaused(),
	}
	if schedule := value.GetSchedule(); schedule != nil {
		out.Schedule = &coreworkflow.ScheduleActivation{
			Cron:     schedule.GetCron(),
			Timezone: schedule.GetTimezone(),
		}
	}
	if event := value.GetEvent(); event != nil {
		out.Event = &coreworkflow.EventActivation{Match: EventMatchFromProto(event.GetMatch())}
	}
	return out
}

func StepExecutionsToProto(values []coreworkflow.StepExecution) ([]*proto.WorkflowStepExecution, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowStepExecution, 0, len(values))
	for i := range values {
		step, err := StepExecutionToProto(values[i])
		if err != nil {
			return nil, fmt.Errorf("workflow step executions[%d]: %w", i, err)
		}
		out = append(out, step)
	}
	return out, nil
}

func StepExecutionToProto(step coreworkflow.StepExecution) (*proto.WorkflowStepExecution, error) {
	attempts, err := StepAttemptsToProto(step.Attempts)
	if err != nil {
		return nil, err
	}
	input, err := protoutil.ValueFromAny(step.Input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	output, err := protoutil.ValueFromAny(step.Output)
	if err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	return &proto.WorkflowStepExecution{
		StepId:        step.StepID,
		Status:        StepStatusToProto(step.Status),
		Attempts:      attempts,
		Input:         input,
		Output:        output,
		StatusMessage: step.StatusMessage,
		SkipReason:    step.SkipReason,
		StartedAt:     TimeToProto(step.StartedAt),
		CompletedAt:   TimeToProto(step.CompletedAt),
	}, nil
}

func StepExecutionsFromProto(values []*proto.WorkflowStepExecution) ([]coreworkflow.StepExecution, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]coreworkflow.StepExecution, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		step, err := StepExecutionFromProto(value)
		if err != nil {
			return nil, err
		}
		out = append(out, step)
	}
	return out, nil
}

func StepExecutionFromProto(value *proto.WorkflowStepExecution) (coreworkflow.StepExecution, error) {
	status, err := StepStatusFromProto(value.GetStatus())
	if err != nil {
		return coreworkflow.StepExecution{}, err
	}
	attempts, err := StepAttemptsFromProto(value.GetAttempts())
	if err != nil {
		return coreworkflow.StepExecution{}, err
	}
	return coreworkflow.StepExecution{
		StepID:        value.GetStepId(),
		Status:        status,
		Attempts:      attempts,
		Input:         protoutil.ValueToAny(value.GetInput()),
		Output:        protoutil.ValueToAny(value.GetOutput()),
		StatusMessage: value.GetStatusMessage(),
		SkipReason:    value.GetSkipReason(),
		StartedAt:     TimeFromProto(value.GetStartedAt()),
		CompletedAt:   TimeFromProto(value.GetCompletedAt()),
	}, nil
}

func StepAttemptsToProto(values []coreworkflow.StepAttempt) ([]*proto.WorkflowStepAttempt, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowStepAttempt, 0, len(values))
	for i := range values {
		attempt, err := StepAttemptToProto(values[i])
		if err != nil {
			return nil, fmt.Errorf("workflow step attempts[%d]: %w", i, err)
		}
		out = append(out, attempt)
	}
	return out, nil
}

func StepAttemptToProto(attempt coreworkflow.StepAttempt) (*proto.WorkflowStepAttempt, error) {
	input, err := protoutil.ValueFromAny(attempt.Input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	output, err := protoutil.ValueFromAny(attempt.Output)
	if err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	return &proto.WorkflowStepAttempt{
		Id:             attempt.ID,
		Status:         StepStatusToProto(attempt.Status),
		IdempotencyKey: attempt.IdempotencyKey,
		Input:          input,
		Output:         output,
		StatusMessage:  attempt.StatusMessage,
		StartedAt:      TimeToProto(attempt.StartedAt),
		CompletedAt:    TimeToProto(attempt.CompletedAt),
	}, nil
}

func StepAttemptsFromProto(values []*proto.WorkflowStepAttempt) ([]coreworkflow.StepAttempt, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]coreworkflow.StepAttempt, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		attempt, err := StepAttemptFromProto(value)
		if err != nil {
			return nil, err
		}
		out = append(out, attempt)
	}
	return out, nil
}

func StepAttemptFromProto(value *proto.WorkflowStepAttempt) (coreworkflow.StepAttempt, error) {
	status, err := StepStatusFromProto(value.GetStatus())
	if err != nil {
		return coreworkflow.StepAttempt{}, err
	}
	return coreworkflow.StepAttempt{
		ID:             value.GetId(),
		Status:         status,
		IdempotencyKey: value.GetIdempotencyKey(),
		Input:          protoutil.ValueToAny(value.GetInput()),
		Output:         protoutil.ValueToAny(value.GetOutput()),
		StatusMessage:  value.GetStatusMessage(),
		StartedAt:      TimeFromProto(value.GetStartedAt()),
		CompletedAt:    TimeFromProto(value.GetCompletedAt()),
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
		CreatedBySubjectId: signal.CreatedBySubjectID,
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
		CreatedBySubjectID: signal.GetCreatedBySubjectId(),
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
				ActivationID: kind.Schedule.GetActivationId(),
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
				ActivationID: kind.Event.GetActivationId(),
				Event:        event,
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
					ActivationId: trigger.Schedule.ActivationID,
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
					ActivationId: trigger.Event.ActivationID,
					Event:        event,
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
