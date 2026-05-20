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
	for _, step := range steps {
		encoded, err := workflowStepToProto(step)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded)
	}
	return out, nil
}

func workflowStepsFromProto(steps []*proto.WorkflowStep) []coreworkflow.Step {
	if len(steps) == 0 {
		return nil
	}
	out := make([]coreworkflow.Step, 0, len(steps))
	for _, step := range steps {
		out = append(out, workflowStepFromProto(step))
	}
	return out
}

func workflowStepToProto(step coreworkflow.Step) (*proto.WorkflowStep, error) {
	inputs, err := workflowValueMapToProto(step.Inputs)
	if err != nil {
		return nil, err
	}
	when, err := workflowStepWhenToProto(step.When)
	if err != nil {
		return nil, err
	}
	delivery, err := workflowStepDeliveryToProto(step.OutputDelivery)
	if err != nil {
		return nil, err
	}
	metadata, err := structpb.NewStruct(step.Metadata)
	if err != nil {
		return nil, fmt.Errorf("workflow step %s.metadata: %w", step.ID, err)
	}
	out := &proto.WorkflowStep{
		Id:             step.ID,
		Inputs:         inputs,
		When:           when,
		TimeoutSeconds: int32(step.TimeoutSeconds),
		OutputDelivery: delivery,
		Metadata:       metadata,
	}
	switch {
	case step.Plugin != nil && step.Agent != nil:
		return nil, fmt.Errorf("workflow step %s must set exactly one action", step.ID)
	case step.Plugin != nil:
		plugin, err := workflowStepPluginCallToProto(step.Plugin)
		if err != nil {
			return nil, err
		}
		out.Action = &proto.WorkflowStep_Plugin{Plugin: plugin}
	case step.Agent != nil:
		agent, err := workflowStepAgentTurnToProto(step.Agent)
		if err != nil {
			return nil, err
		}
		out.Action = &proto.WorkflowStep_Agent{Agent: agent}
	}
	return out, nil
}

func workflowStepFromProto(step *proto.WorkflowStep) coreworkflow.Step {
	if step == nil {
		return coreworkflow.Step{}
	}
	out := coreworkflow.Step{
		ID:             step.GetId(),
		Inputs:         workflowValueMapFromProto(step.GetInputs()),
		When:           workflowStepWhenFromProto(step.GetWhen()),
		TimeoutSeconds: int(step.GetTimeoutSeconds()),
		OutputDelivery: workflowStepDeliveryFromProto(step.GetOutputDelivery()),
		Metadata:       structMap(step.GetMetadata()),
	}
	switch action := step.GetAction().(type) {
	case *proto.WorkflowStep_Plugin:
		out.Plugin = workflowStepPluginCallFromProto(action.Plugin)
	case *proto.WorkflowStep_Agent:
		out.Agent = workflowStepAgentTurnFromProto(action.Agent)
	}
	return out
}

func workflowStepPluginCallToProto(target *coreworkflow.PluginCall) (*proto.WorkflowStepPluginCall, error) {
	if target == nil {
		return nil, nil
	}
	input, err := workflowValueToProto(target.Input)
	if err != nil {
		return nil, err
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
		Name:           target.GetName(),
		Operation:      target.GetOperation(),
		Input:          workflowValueFromProto(target.GetInput()),
		Connection:     target.GetConnection(),
		Instance:       target.GetInstance(),
		CredentialMode: core.ConnectionMode(target.GetCredentialMode()),
	}
}

func workflowStepDeliveryToProto(delivery *coreworkflow.StepDelivery) (*proto.WorkflowStepDelivery, error) {
	if delivery == nil {
		return nil, nil
	}
	plugin, err := workflowStepPluginCallToProto(delivery.Plugin)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowStepDelivery{Plugin: plugin}, nil
}

func workflowStepDeliveryFromProto(delivery *proto.WorkflowStepDelivery) *coreworkflow.StepDelivery {
	if delivery == nil {
		return nil
	}
	return &coreworkflow.StepDelivery{Plugin: workflowStepPluginCallFromProto(delivery.GetPlugin())}
}

func workflowStepAgentTurnToProto(target *coreworkflow.AgentTurn) (*proto.WorkflowStepAgentTurn, error) {
	if target == nil {
		return nil, nil
	}
	messages, err := workflowAgentMessagesToProto(target.Messages)
	if err != nil {
		return nil, err
	}
	responseSchema, err := structpb.NewStruct(target.ResponseSchema)
	if err != nil {
		return nil, fmt.Errorf("workflow agent response_schema: %w", err)
	}
	modelOptions, err := structpb.NewStruct(target.ModelOptions)
	if err != nil {
		return nil, fmt.Errorf("workflow agent model_options: %w", err)
	}
	return &proto.WorkflowStepAgentTurn{
		Provider:       target.ProviderName,
		Model:          target.Model,
		SessionKey:     workflowTextToProto(coreworkflow.Text{Template: target.SessionKey}),
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
		ProviderName:   target.GetProvider(),
		Model:          target.GetModel(),
		SessionKey:     workflowTextFromProto(target.GetSessionKey()).Template,
		Prompt:         workflowTextFromProto(target.GetPrompt()),
		Messages:       workflowAgentMessagesFromProto(target.GetMessages()),
		ToolRefs:       agentwire.ToolRefsFromProto(target.GetTools()),
		ResponseSchema: structMap(target.GetResponseSchema()),
		ModelOptions:   structMap(target.GetModelOptions()),
	}
}

func workflowAgentMessagesToProto(messages []coreworkflow.AgentMessage) ([]*proto.WorkflowAgentMessage, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowAgentMessage, 0, len(messages))
	for _, message := range messages {
		metadata, err := structpb.NewStruct(message.Metadata)
		if err != nil {
			return nil, err
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
		out = append(out, coreworkflow.AgentMessage{
			Role:     message.GetRole(),
			Text:     workflowTextFromProto(message.GetText()),
			Metadata: structMap(message.GetMetadata()),
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
	value, err := workflowValueToProto(when.Value)
	if err != nil {
		return nil, err
	}
	equals, err := valueToProto(when.Equals, when.EqualsSet)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowStepWhen{Value: value, Equals: equals}, nil
}

func workflowStepWhenFromProto(when *proto.WorkflowStepWhen) *coreworkflow.StepWhen {
	if when == nil {
		return nil
	}
	equals := valueFromProto(when.GetEquals())
	return &coreworkflow.StepWhen{
		Value:     workflowValueFromProto(when.GetValue()),
		Equals:    equals,
		EqualsSet: when.GetEquals() != nil,
	}
}

func workflowValueMapToProto(values map[string]coreworkflow.Value) (map[string]*proto.WorkflowValue, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]*proto.WorkflowValue, len(values))
	for key := range values {
		encoded, err := workflowValueToProto(values[key])
		if err != nil {
			return nil, err
		}
		out[key] = encoded
	}
	return out, nil
}

func workflowValueMapFromProto(values map[string]*proto.WorkflowValue) map[string]coreworkflow.Value {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]coreworkflow.Value, len(values))
	for key, value := range values {
		out[key] = workflowValueFromProto(value)
	}
	return out
}

func workflowValueToProto(value coreworkflow.Value) (*proto.WorkflowValue, error) {
	switch {
	case value.LiteralSet:
		literal, err := valueToProto(value.Literal, true)
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
			encoded, err := workflowValueToProto(value.Array[i])
			if err != nil {
				return nil, err
			}
			items = append(items, encoded)
		}
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Array{Array: &proto.WorkflowArray{Values: items}}}, nil
	case value.Template != nil:
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Template{Template: workflowTextToProto(*value.Template)}}, nil
	case value.RunInput != "":
		return pathValue(&proto.WorkflowValue_RunInput{}, value.RunInput), nil
	case value.SignalPayload != "":
		return pathValue(&proto.WorkflowValue_SignalPayload{}, value.SignalPayload), nil
	case value.StepOutput != nil:
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_StepOutput{StepOutput: &proto.WorkflowStepOutputSource{
			StepId: value.StepOutput.StepID,
			Path:   value.StepOutput.Path,
		}}}, nil
	default:
		return &proto.WorkflowValue{}, nil
	}
}

func pathValue(kind any, path string) *proto.WorkflowValue {
	source := &proto.WorkflowPathSource{Path: path}
	switch kind.(type) {
	case *proto.WorkflowValue_RunInput:
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_RunInput{RunInput: source}}
	case *proto.WorkflowValue_SignalPayload:
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_SignalPayload{SignalPayload: source}}
	default:
		return &proto.WorkflowValue{}
	}
}

func workflowValueFromProto(value *proto.WorkflowValue) coreworkflow.Value {
	if value == nil {
		return coreworkflow.Value{}
	}
	switch typed := value.GetKind().(type) {
	case *proto.WorkflowValue_Literal:
		return coreworkflow.Value{Literal: valueFromProto(typed.Literal), LiteralSet: true}
	case *proto.WorkflowValue_Object:
		return coreworkflow.Value{Object: workflowValueMapFromProto(typed.Object.GetFields())}
	case *proto.WorkflowValue_Array:
		out := make([]coreworkflow.Value, 0, len(typed.Array.GetValues()))
		for _, item := range typed.Array.GetValues() {
			out = append(out, workflowValueFromProto(item))
		}
		return coreworkflow.Value{Array: out}
	case *proto.WorkflowValue_Template:
		text := workflowTextFromProto(typed.Template)
		return coreworkflow.Value{Template: &text}
	case *proto.WorkflowValue_RunInput:
		return coreworkflow.Value{RunInput: typed.RunInput.GetPath()}
	case *proto.WorkflowValue_SignalPayload:
		return coreworkflow.Value{SignalPayload: typed.SignalPayload.GetPath()}
	case *proto.WorkflowValue_StepOutput:
		return coreworkflow.Value{StepOutput: &coreworkflow.StepOutputSource{
			StepID: typed.StepOutput.GetStepId(),
			Path:   typed.StepOutput.GetPath(),
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
	if subject == nil {
		return nil
	}
	return &proto.WorkflowRunAsSubject{
		SubjectId:           subject.SubjectID,
		SubjectKind:         subject.SubjectKind,
		DisplayName:         subject.DisplayName,
		AuthSource:          subject.AuthSource,
		CredentialSubjectId: subject.CredentialSubjectID,
	}
}

func workflowRunAsSubjectFromProto(subject *proto.WorkflowRunAsSubject) *core.RunAsSubject {
	if subject == nil {
		return nil
	}
	return core.NormalizeRunAsSubject(&core.RunAsSubject{
		SubjectID:           subject.GetSubjectId(),
		SubjectKind:         subject.GetSubjectKind(),
		DisplayName:         subject.GetDisplayName(),
		AuthSource:          subject.GetAuthSource(),
		CredentialSubjectID: subject.GetCredentialSubjectId(),
	})
}

func workflowAccessPermissionsToProto(values []core.AccessPermission) []*proto.WorkflowAccessPermission {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.WorkflowAccessPermission, 0, len(values))
	for _, value := range values {
		out = append(out, &proto.WorkflowAccessPermission{
			Plugin:     value.Plugin,
			Operations: append([]string(nil), value.Operations...),
			Actions:    append([]string(nil), value.Actions...),
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
		out = append(out, core.AccessPermission{
			Plugin:     value.GetPlugin(),
			Operations: append([]string(nil), value.GetOperations()...),
			Actions:    append([]string(nil), value.GetActions()...),
		})
	}
	return out
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
		TargetDigest:        ref.TargetDigest,
		ProviderPlanDigest:  ref.ProviderPlanDigest,
		PermissionsDigest:   ref.PermissionsDigest,
		SemanticsVersion:    ref.SemanticsVersion,
		Generation:          ref.Generation,
		Seal:                ref.Seal,
	}, nil
}

func workflowExecutionReferenceFromProto(ref *proto.WorkflowExecutionReference) *coreworkflow.ExecutionReference {
	if ref == nil {
		return nil
	}
	return &coreworkflow.ExecutionReference{
		ID:                  ref.GetId(),
		ProviderName:        ref.GetProviderName(),
		Target:              workflowTargetFromProto(ref.GetTarget()),
		CallerPluginName:    ref.GetCallerPluginName(),
		SourceDefinitionID:  ref.GetSourceDefinitionId(),
		SubjectID:           ref.GetSubjectId(),
		SubjectKind:         ref.GetSubjectKind(),
		DisplayName:         ref.GetDisplayName(),
		AuthSource:          ref.GetAuthSource(),
		CredentialSubjectID: ref.GetCredentialSubjectId(),
		RunAs:               workflowRunAsSubjectFromProto(ref.GetRunAs()),
		Permissions:         workflowAccessPermissionsFromProto(ref.GetPermissions()),
		CreatedAt:           timeFromProto(ref.GetCreatedAt()),
		RevokedAt:           timeFromProto(ref.GetRevokedAt()),
		TargetDigest:        ref.GetTargetDigest(),
		ProviderPlanDigest:  ref.GetProviderPlanDigest(),
		PermissionsDigest:   ref.GetPermissionsDigest(),
		SemanticsVersion:    ref.GetSemanticsVersion(),
		Generation:          ref.GetGeneration(),
		Seal:                ref.GetSeal(),
	}
}

func workflowUnsupportedFeaturesFromProto(values []*proto.WorkflowUnsupportedFeature) []coreworkflow.UnsupportedFeature {
	if len(values) == 0 {
		return nil
	}
	out := make([]coreworkflow.UnsupportedFeature, 0, len(values))
	for _, value := range values {
		out = append(out, coreworkflow.UnsupportedFeature{Feature: value.GetFeature(), Reason: value.GetReason()})
	}
	return out
}

func workflowUnsupportedFeaturesToProto(values []coreworkflow.UnsupportedFeature) []*proto.WorkflowUnsupportedFeature {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.WorkflowUnsupportedFeature, 0, len(values))
	for _, value := range values {
		out = append(out, &proto.WorkflowUnsupportedFeature{Feature: value.Feature, Reason: value.Reason})
	}
	return out
}

func workflowPlanRequestToProto(req coreworkflow.PlanWorkflowRequest) (*proto.PlanWorkflowRequest, error) {
	spec, err := workflowDeploymentSpecToProto(req.Spec)
	if err != nil {
		return nil, err
	}
	return &proto.PlanWorkflowRequest{
		Spec:                          spec,
		SpecDigest:                    req.SpecDigest,
		TargetDigest:                  req.TargetDigest,
		ActionTableDigest:             req.ActionTableDigest,
		TargetCanonicalizationVersion: req.TargetCanonicalizationVersion,
		WorkflowSemanticsVersion:      req.WorkflowSemanticsVersion,
	}, nil
}

func workflowPlanResponseFromProto(resp *proto.PlanWorkflowResponse) *coreworkflow.CompileTargetResponse {
	if resp == nil {
		return nil
	}
	return &coreworkflow.CompileTargetResponse{
		AcceptedSpecDigest:        strings.TrimSpace(resp.GetAcceptedSpecDigest()),
		ProviderPlanID:            strings.TrimSpace(resp.GetProviderPlanId()),
		ProviderPlanDigest:        strings.TrimSpace(resp.GetProviderPlanDigest()),
		ProviderPlanFormatVersion: strings.TrimSpace(resp.GetProviderPlanFormatVersion()),
		Unsupported:               workflowUnsupportedFeaturesFromProto(resp.GetUnsupported()),
		SupportedFeatureFlags:     append([]string(nil), resp.GetSupportedFeatureFlags()...),
	}
}

func workflowPlanResponseToProto(resp *coreworkflow.CompileTargetResponse) *proto.PlanWorkflowResponse {
	if resp == nil {
		return nil
	}
	return &proto.PlanWorkflowResponse{
		AcceptedSpecDigest:        resp.AcceptedSpecDigest,
		ProviderPlanId:            resp.ProviderPlanID,
		ProviderPlanDigest:        resp.ProviderPlanDigest,
		ProviderPlanFormatVersion: resp.ProviderPlanFormatVersion,
		Unsupported:               workflowUnsupportedFeaturesToProto(resp.Unsupported),
		SupportedFeatureFlags:     append([]string(nil), resp.SupportedFeatureFlags...),
	}
}

func workflowEventToProto(event coreworkflow.Event) (*proto.WorkflowEvent, error) {
	data, err := structpb.NewStruct(event.Data)
	if err != nil {
		return nil, fmt.Errorf("workflow event.data: %w", err)
	}
	extensions, err := workflowExtensionsToProto(event.Extensions)
	if err != nil {
		return nil, err
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
		Data:            structMap(event.GetData()),
		Extensions:      extensions,
	}, nil
}

func workflowEventMatchToProto(match coreworkflow.EventMatch) *proto.WorkflowEventMatch {
	if match == (coreworkflow.EventMatch{}) {
		return nil
	}
	return &proto.WorkflowEventMatch{Type: match.Type, Source: match.Source, Subject: match.Subject}
}

func workflowEventMatchFromProto(match *proto.WorkflowEventMatch) coreworkflow.EventMatch {
	if match == nil {
		return coreworkflow.EventMatch{}
	}
	return coreworkflow.EventMatch{Type: match.GetType(), Source: match.GetSource(), Subject: match.GetSubject()}
}

func workflowRunTriggerToProto(trigger coreworkflow.RunTrigger) (*proto.WorkflowRunTrigger, error) {
	out := &proto.WorkflowRunTrigger{
		DeploymentId:         trigger.DeploymentID,
		DeploymentGeneration: trigger.DeploymentGeneration,
		ActivationId:         trigger.ActivationID,
	}
	switch {
	case trigger.Schedule != nil:
		out.Kind = &proto.WorkflowRunTrigger_Schedule{Schedule: &proto.WorkflowScheduleTrigger{
			ActivationId: firstNonEmpty(trigger.Schedule.ActivationID, trigger.ActivationID),
			ScheduledFor: timeToProto(trigger.Schedule.ScheduledFor),
		}}
	case trigger.Event != nil:
		event, err := workflowEventToProto(trigger.Event.Event)
		if err != nil {
			return nil, err
		}
		out.Kind = &proto.WorkflowRunTrigger_Event{Event: &proto.WorkflowEventTrigger{
			ActivationId: firstNonEmpty(trigger.Event.ActivationID, trigger.ActivationID),
			Event:        event,
		}}
	case trigger.Manual:
		out.Kind = &proto.WorkflowRunTrigger_Manual{Manual: &proto.WorkflowManualTrigger{}}
	}
	return out, nil
}

func workflowRunTriggerFromProto(trigger *proto.WorkflowRunTrigger) (coreworkflow.RunTrigger, error) {
	if trigger == nil {
		return coreworkflow.RunTrigger{}, nil
	}
	out := coreworkflow.RunTrigger{
		DeploymentID:         trigger.GetDeploymentId(),
		DeploymentGeneration: trigger.GetDeploymentGeneration(),
		ActivationID:         trigger.GetActivationId(),
	}
	switch typed := trigger.GetKind().(type) {
	case *proto.WorkflowRunTrigger_Manual:
		out.Manual = true
	case *proto.WorkflowRunTrigger_Schedule:
		out.Schedule = &coreworkflow.ScheduleTrigger{
			ActivationID: firstNonEmpty(typed.Schedule.GetActivationId(), trigger.GetActivationId()),
			ScheduledFor: timeFromProto(typed.Schedule.GetScheduledFor()),
		}
	case *proto.WorkflowRunTrigger_Event:
		event, err := workflowEventFromProto(typed.Event.GetEvent())
		if err != nil {
			return coreworkflow.RunTrigger{}, err
		}
		out.Event = &coreworkflow.EventTriggerInvocation{
			ActivationID: firstNonEmpty(typed.Event.GetActivationId(), trigger.GetActivationId()),
			Event:        event,
		}
	}
	return out, nil
}

func workflowRunFromProto(run *proto.WorkflowRun) (*coreworkflow.Run, error) {
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
		ID:                     run.GetId(),
		DeploymentID:           run.GetDeploymentId(),
		DeploymentGeneration:   run.GetDeploymentGeneration(),
		Status:                 status,
		WorkflowKey:            run.GetWorkflowKey(),
		Trigger:                trigger,
		Input:                  structMap(run.GetInput()),
		ExecutionRef:           run.GetExecutionRef(),
		ExecutionRefGeneration: run.GetExecutionRefGeneration(),
		CreatedBy:              workflowActorFromProto(run.GetCreatedBy()),
		CreatedAt:              timeFromProto(run.GetCreatedAt()),
		StartedAt:              timeFromProto(run.GetStartedAt()),
		CompletedAt:            timeFromProto(run.GetCompletedAt()),
		StatusMessage:          run.GetStatusMessage(),
		TargetDigest:           run.GetTargetDigest(),
		SpecDigest:             run.GetSpecDigest(),
		ActionTableDigest:      run.GetActionTableDigest(),
		PlanDigest:             run.GetProviderPlanDigest(),
		Steps:                  workflowStepStatesFromProto(run.GetSteps()),
		Error:                  workflowRunErrorFromProto(run.GetError()),
	}, nil
}

func workflowRunToProto(run *coreworkflow.Run) (*proto.WorkflowRun, error) {
	if run == nil {
		return nil, nil
	}
	trigger, err := workflowRunTriggerToProto(run.Trigger)
	if err != nil {
		return nil, err
	}
	input, err := structpb.NewStruct(run.Input)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowRun{
		Id:                     run.ID,
		DeploymentId:           run.DeploymentID,
		DeploymentGeneration:   run.DeploymentGeneration,
		WorkflowKey:            run.WorkflowKey,
		Status:                 workflowRunStatusToProto(run.Status),
		Trigger:                trigger,
		Input:                  input,
		CreatedBy:              workflowActorToProto(run.CreatedBy),
		CreatedAt:              timeToProto(run.CreatedAt),
		StartedAt:              timeToProto(run.StartedAt),
		CompletedAt:            timeToProto(run.CompletedAt),
		StatusMessage:          run.StatusMessage,
		ExecutionRef:           run.ExecutionRef,
		ExecutionRefGeneration: run.ExecutionRefGeneration,
		TargetDigest:           run.TargetDigest,
		SpecDigest:             run.SpecDigest,
		ActionTableDigest:      run.ActionTableDigest,
		ProviderPlanDigest:     run.PlanDigest,
		Steps:                  workflowStepStatesToProto(run.Steps),
		Error:                  workflowRunErrorToProto(run.Error),
	}, nil
}

func workflowStepStatusFromProto(status proto.WorkflowStepStatus) coreworkflow.StepStatus {
	switch status {
	case proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_PENDING:
		return coreworkflow.StepStatusPending
	case proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_RUNNING:
		return coreworkflow.StepStatusRunning
	case proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_SUCCEEDED:
		return coreworkflow.StepStatusSucceeded
	case proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_FAILED:
		return coreworkflow.StepStatusFailed
	case proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_SKIPPED:
		return coreworkflow.StepStatusSkipped
	case proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_CANCELED:
		return coreworkflow.StepStatusCanceled
	default:
		return ""
	}
}

func workflowStepStatusToProto(status coreworkflow.StepStatus) proto.WorkflowStepStatus {
	switch status {
	case coreworkflow.StepStatusPending:
		return proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_PENDING
	case coreworkflow.StepStatusRunning:
		return proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_RUNNING
	case coreworkflow.StepStatusSucceeded:
		return proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_SUCCEEDED
	case coreworkflow.StepStatusFailed:
		return proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_FAILED
	case coreworkflow.StepStatusSkipped:
		return proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_SKIPPED
	case coreworkflow.StepStatusCanceled:
		return proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_CANCELED
	default:
		return proto.WorkflowStepStatus_WORKFLOW_STEP_STATUS_UNSPECIFIED
	}
}

func workflowStepStatesFromProto(values []*proto.WorkflowStepState) []coreworkflow.StepState {
	if len(values) == 0 {
		return nil
	}
	out := make([]coreworkflow.StepState, 0, len(values))
	for _, value := range values {
		out = append(out, coreworkflow.StepState{
			StepID:        value.GetStepId(),
			StepIndex:     int(value.GetStepIndex()),
			Status:        workflowStepStatusFromProto(value.GetStatus()),
			SkippedReason: value.GetSkippedReason(),
			AttemptNumber: int(value.GetAttemptNumber()),
			OutputSummary: workflowOutputSummaryFromProto(value.GetOutputSummary()),
			OutputRef:     value.GetOutputRef(),
			Error:         workflowRunErrorFromProto(value.GetError()),
			UpdatedAt:     timeFromProto(value.GetUpdatedAt()),
		})
	}
	return out
}

func workflowStepStatesToProto(values []coreworkflow.StepState) []*proto.WorkflowStepState {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.WorkflowStepState, 0, len(values))
	for _, value := range values {
		out = append(out, &proto.WorkflowStepState{
			StepId:        value.StepID,
			StepIndex:     int32(value.StepIndex),
			Status:        workflowStepStatusToProto(value.Status),
			SkippedReason: value.SkippedReason,
			AttemptNumber: int32(value.AttemptNumber),
			OutputSummary: workflowOutputSummaryToProto(value.OutputSummary),
			OutputRef:     value.OutputRef,
			Error:         workflowRunErrorToProto(value.Error),
			UpdatedAt:     timeToProto(value.UpdatedAt),
		})
	}
	return out
}

func workflowOutputSummaryFromProto(value *proto.WorkflowOutputSummary) *coreworkflow.OutputSummary {
	if value == nil {
		return nil
	}
	return &coreworkflow.OutputSummary{
		EnvelopeVersion: value.GetEnvelopeVersion(),
		Kind:            value.GetKind(),
		SizeBytes:       value.GetSizeBytes(),
		SHA256:          value.GetSha256(),
		Truncated:       value.GetTruncated(),
		Redacted:        value.GetRedacted(),
		MediaType:       value.GetMediaType(),
	}
}

func workflowOutputSummaryToProto(value *coreworkflow.OutputSummary) *proto.WorkflowOutputSummary {
	if value == nil {
		return nil
	}
	return &proto.WorkflowOutputSummary{
		EnvelopeVersion: value.EnvelopeVersion,
		Kind:            value.Kind,
		SizeBytes:       value.SizeBytes,
		Sha256:          value.SHA256,
		Truncated:       value.Truncated,
		Redacted:        value.Redacted,
		MediaType:       value.MediaType,
	}
}

func workflowRunErrorFromProto(value *proto.WorkflowRunError) *coreworkflow.RunError {
	if value == nil {
		return nil
	}
	return &coreworkflow.RunError{
		Code:     value.GetCode(),
		Message:  value.GetMessage(),
		StepID:   value.GetStepId(),
		ActionID: value.GetActionId(),
	}
}

func workflowRunErrorToProto(value *coreworkflow.RunError) *proto.WorkflowRunError {
	if value == nil {
		return nil
	}
	return &proto.WorkflowRunError{
		Code:     value.Code,
		Message:  value.Message,
		StepId:   value.StepID,
		ActionId: value.ActionID,
	}
}

func workflowSignalToProto(signal coreworkflow.Signal) (*proto.WorkflowSignal, error) {
	payload, err := structpb.NewStruct(signal.Payload)
	if err != nil {
		return nil, err
	}
	metadata, err := structpb.NewStruct(signal.Metadata)
	if err != nil {
		return nil, err
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
		ID:             signal.GetId(),
		Name:           signal.GetName(),
		Payload:        structMap(signal.GetPayload()),
		Metadata:       structMap(signal.GetMetadata()),
		CreatedBy:      workflowActorFromProto(signal.GetCreatedBy()),
		CreatedAt:      timeFromProto(signal.GetCreatedAt()),
		IdempotencyKey: signal.GetIdempotencyKey(),
		Sequence:       signal.GetSequence(),
	}
}

func workflowSignalsFromProto(signals []*proto.WorkflowSignal) []coreworkflow.Signal {
	if len(signals) == 0 {
		return nil
	}
	out := make([]coreworkflow.Signal, 0, len(signals))
	for i := range signals {
		out = append(out, workflowSignalFromProto(signals[i]))
	}
	return out
}

func workflowSignalRunResponseFromProto(resp *proto.WorkflowRunSignal) (*coreworkflow.SignalRunResponse, error) {
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

func workflowEventDeliveryResultsFromProto(values []*proto.WorkflowEventDeliveryResult) ([]coreworkflow.EventDeliveryResult, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]coreworkflow.EventDeliveryResult, 0, len(values))
	for _, value := range values {
		run, err := workflowRunFromProto(value.GetRun())
		if err != nil {
			return nil, err
		}
		out = append(out, coreworkflow.EventDeliveryResult{
			DeploymentID: value.GetDeploymentId(),
			ActivationID: value.GetActivationId(),
			Run:          run,
			Signal:       workflowSignalFromProto(value.GetSignal()),
			StartedRun:   value.GetStartedRun(),
		})
	}
	return out, nil
}

func workflowEventDeliveryResultsToProto(values []coreworkflow.EventDeliveryResult) ([]*proto.WorkflowEventDeliveryResult, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowEventDeliveryResult, 0, len(values))
	for i := range values {
		value := values[i]
		run, err := workflowRunToProto(value.Run)
		if err != nil {
			return nil, err
		}
		signal, err := workflowSignalToProto(value.Signal)
		if err != nil {
			return nil, err
		}
		out = append(out, &proto.WorkflowEventDeliveryResult{
			DeploymentId: value.DeploymentID,
			ActivationId: value.ActivationID,
			Run:          run,
			Signal:       signal,
			StartedRun:   value.StartedRun,
		})
	}
	return out, nil
}

func workflowRunEventTypeFromProto(value proto.WorkflowRunEventType) coreworkflow.RunEventType {
	switch value {
	case proto.WorkflowRunEventType_WORKFLOW_RUN_EVENT_TYPE_RUN_STARTED:
		return coreworkflow.RunEventTypeRunStarted
	case proto.WorkflowRunEventType_WORKFLOW_RUN_EVENT_TYPE_RUN_COMPLETED:
		return coreworkflow.RunEventTypeRunCompleted
	case proto.WorkflowRunEventType_WORKFLOW_RUN_EVENT_TYPE_RUN_FAILED:
		return coreworkflow.RunEventTypeRunFailed
	case proto.WorkflowRunEventType_WORKFLOW_RUN_EVENT_TYPE_RUN_CANCELED:
		return coreworkflow.RunEventTypeRunCanceled
	case proto.WorkflowRunEventType_WORKFLOW_RUN_EVENT_TYPE_SIGNAL_RECEIVED:
		return coreworkflow.RunEventTypeSignalReceived
	case proto.WorkflowRunEventType_WORKFLOW_RUN_EVENT_TYPE_STEP_STARTED:
		return coreworkflow.RunEventTypeStepStarted
	case proto.WorkflowRunEventType_WORKFLOW_RUN_EVENT_TYPE_STEP_SUCCEEDED:
		return coreworkflow.RunEventTypeStepSucceeded
	case proto.WorkflowRunEventType_WORKFLOW_RUN_EVENT_TYPE_STEP_FAILED:
		return coreworkflow.RunEventTypeStepFailed
	case proto.WorkflowRunEventType_WORKFLOW_RUN_EVENT_TYPE_STEP_SKIPPED:
		return coreworkflow.RunEventTypeStepSkipped
	case proto.WorkflowRunEventType_WORKFLOW_RUN_EVENT_TYPE_ACTION_INVOKED:
		return coreworkflow.RunEventTypeActionInvoked
	case proto.WorkflowRunEventType_WORKFLOW_RUN_EVENT_TYPE_ACTION_COMPLETED:
		return coreworkflow.RunEventTypeActionCompleted
	case proto.WorkflowRunEventType_WORKFLOW_RUN_EVENT_TYPE_ACTION_FAILED:
		return coreworkflow.RunEventTypeActionFailed
	default:
		return ""
	}
}

func workflowRunEventsFromProto(values []*proto.WorkflowRunEvent) []coreworkflow.RunEvent {
	if len(values) == 0 {
		return nil
	}
	out := make([]coreworkflow.RunEvent, 0, len(values))
	for _, value := range values {
		out = append(out, coreworkflow.RunEvent{
			ID:            value.GetId(),
			RunID:         value.GetRunId(),
			Sequence:      value.GetSequence(),
			Type:          workflowRunEventTypeFromProto(value.GetType()),
			StepID:        value.GetStepId(),
			ActionID:      value.GetActionId(),
			AttemptNumber: int(value.GetAttemptNumber()),
			Message:       value.GetMessage(),
			OutputSummary: workflowOutputSummaryFromProto(value.GetOutputSummary()),
			OutputRef:     value.GetOutputRef(),
			Error:         workflowRunErrorFromProto(value.GetError()),
			ObservedAt:    timeFromProto(value.GetObservedAt()),
		})
	}
	return out
}

func workflowRunOutputFromProto(value *proto.WorkflowRunOutput) coreworkflow.RunOutput {
	if value == nil {
		return coreworkflow.RunOutput{}
	}
	return coreworkflow.RunOutput{
		OutputRef: value.GetOutputRef(),
		Summary:   workflowOutputSummaryFromProto(value.GetSummary()),
		Body:      valueFromProto(value.GetBody()),
	}
}

func workflowHostActionSelectorFromProto(selector *proto.WorkflowHostActionSelector) coreworkflow.HostActionSelector {
	if selector == nil {
		return coreworkflow.HostActionSelector{}
	}
	return coreworkflow.HostActionSelector{
		ExecutionRef:           selector.GetExecutionRef(),
		ExecutionRefGeneration: selector.GetExecutionRefGeneration(),
		ExecutionRefSeal:       selector.GetExecutionRefSeal(),
		RunID:                  selector.GetRunId(),
		StepID:                 selector.GetStepId(),
		ActionID:               selector.GetActionId(),
		AttemptNumber:          int(selector.GetAttemptNumber()),
		IdempotencyKey:         selector.GetIdempotencyKey(),
		TargetDigest:           selector.GetTargetDigest(),
		ActionTableDigest:      selector.GetActionTableDigest(),
		ProviderPlanDigest:     selector.GetProviderPlanDigest(),
	}
}

func workflowActionRequestFromProto(req *proto.InvokeWorkflowActionRequest) (coreworkflow.InvokeActionRequest, error) {
	if req == nil {
		return coreworkflow.InvokeActionRequest{}, nil
	}
	out := coreworkflow.InvokeActionRequest{
		Selector: workflowHostActionSelectorFromProto(req.GetSelector()),
		Metadata: structMap(req.GetMetadata()),
		Signals:  workflowSignalsFromProto(req.GetSignals()),
	}
	trigger, err := workflowRunTriggerFromProto(req.GetTrigger())
	if err != nil {
		return coreworkflow.InvokeActionRequest{}, err
	}
	out.Trigger = trigger
	switch typed := req.GetAction().(type) {
	case *proto.InvokeWorkflowActionRequest_Plugin:
		out.Plugin = &coreworkflow.PluginActionPayload{Input: structMap(typed.Plugin.GetInput())}
	case *proto.InvokeWorkflowActionRequest_AgentTurn:
		out.AgentTurn = &coreworkflow.AgentTurnPayload{
			Prompt:   workflowTextFromProto(typed.AgentTurn.GetPrompt()),
			Messages: workflowAgentMessagesFromProto(typed.AgentTurn.GetMessages()),
		}
	}
	return out, nil
}

func workflowHostActionResponseToProto(resp *coreworkflow.HostActionResponse) *proto.WorkflowActionResult {
	if resp == nil {
		return nil
	}
	return &proto.WorkflowActionResult{
		ActionEventId: resp.ActionEventID,
		Status:        int32(resp.Status),
		Body:          resp.Body,
		OutputSummary: workflowOutputSummaryToProto(resp.OutputSummary),
		OutputRef:     resp.OutputRef,
		Error:         workflowRunErrorToProto(resp.Error),
	}
}

func workflowPlanBindingToProto(binding *coreworkflow.PlanBinding) *proto.WorkflowDeploymentBinding {
	if binding == nil {
		return nil
	}
	return &proto.WorkflowDeploymentBinding{
		Id:                       binding.ID,
		ExecutionRef:             binding.ExecutionRef,
		ExecutionRefGeneration:   binding.ExecutionRefGeneration,
		ExecutionRefSeal:         binding.ExecutionRefSeal,
		DeploymentId:             binding.DeploymentID,
		DeploymentGeneration:     binding.DeploymentGeneration,
		SpecDigest:               binding.SpecDigest,
		TargetDigest:             binding.TargetDigest,
		ActionTableDigest:        binding.ActionTableDigest,
		ProviderPlanId:           binding.ProviderPlanID,
		ProviderPlanDigest:       binding.ProviderPlanDigest,
		WorkflowSemanticsVersion: binding.SemanticsVersion,
		RequestId:                binding.RequestID,
	}
}

func workflowPlanBindingFromProto(binding *proto.WorkflowDeploymentBinding) *coreworkflow.PlanBinding {
	if binding == nil {
		return nil
	}
	return &coreworkflow.PlanBinding{
		ID:                     binding.GetId(),
		ExecutionRef:           binding.GetExecutionRef(),
		ExecutionRefGeneration: binding.GetExecutionRefGeneration(),
		ExecutionRefSeal:       binding.GetExecutionRefSeal(),
		DeploymentID:           binding.GetDeploymentId(),
		DeploymentGeneration:   binding.GetDeploymentGeneration(),
		SpecDigest:             binding.GetSpecDigest(),
		TargetDigest:           binding.GetTargetDigest(),
		ActionTableDigest:      binding.GetActionTableDigest(),
		ProviderPlanID:         binding.GetProviderPlanId(),
		ProviderPlanDigest:     binding.GetProviderPlanDigest(),
		SemanticsVersion:       binding.GetWorkflowSemanticsVersion(),
		RequestID:              binding.GetRequestId(),
	}
}

func workflowDeploymentBindingToProto(binding *coreworkflow.DeploymentBinding) *proto.WorkflowDeploymentBinding {
	return workflowPlanBindingToProto(binding)
}

func workflowDeploymentBindingFromProto(binding *proto.WorkflowDeploymentBinding) *coreworkflow.DeploymentBinding {
	plan := workflowPlanBindingFromProto(binding)
	return plan
}

func workflowDeploymentSpecToProto(spec coreworkflow.DeploymentSpec) (*proto.WorkflowDeploymentSpec, error) {
	target, err := workflowTargetToProto(spec.Target)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowDeploymentSpec{
		Id:                       spec.ID,
		Generation:               spec.Generation,
		Target:                   target,
		Activations:              workflowActivationsToProto(spec.Activations),
		Paused:                   spec.Paused,
		RunAs:                    workflowRunAsSubjectToProto(spec.RunAs),
		Permissions:              workflowAccessPermissionsToProto(spec.Permissions),
		Labels:                   spec.Labels,
		WorkflowSemanticsVersion: spec.WorkflowSemanticsVersion,
	}, nil
}

func workflowDeploymentSpecFromProto(spec *proto.WorkflowDeploymentSpec) coreworkflow.DeploymentSpec {
	if spec == nil {
		return coreworkflow.DeploymentSpec{}
	}
	return coreworkflow.DeploymentSpec{
		ID:                       spec.GetId(),
		Generation:               spec.GetGeneration(),
		Target:                   workflowTargetFromProto(spec.GetTarget()),
		Activations:              workflowActivationsFromProto(spec.GetActivations()),
		Paused:                   spec.GetPaused(),
		RunAs:                    workflowRunAsSubjectFromProto(spec.GetRunAs()),
		Permissions:              workflowAccessPermissionsFromProto(spec.GetPermissions()),
		Labels:                   spec.GetLabels(),
		WorkflowSemanticsVersion: spec.GetWorkflowSemanticsVersion(),
	}
}

func workflowDeploymentStatusToProto(status coreworkflow.DeploymentStatus) proto.WorkflowDeploymentStatus {
	switch status {
	case coreworkflow.DeploymentStatusPending:
		return proto.WorkflowDeploymentStatus_WORKFLOW_DEPLOYMENT_STATUS_PENDING
	case coreworkflow.DeploymentStatusActive:
		return proto.WorkflowDeploymentStatus_WORKFLOW_DEPLOYMENT_STATUS_ACTIVE
	case coreworkflow.DeploymentStatusPaused:
		return proto.WorkflowDeploymentStatus_WORKFLOW_DEPLOYMENT_STATUS_PAUSED
	case coreworkflow.DeploymentStatusDeleted:
		return proto.WorkflowDeploymentStatus_WORKFLOW_DEPLOYMENT_STATUS_DELETED
	case coreworkflow.DeploymentStatusFailed:
		return proto.WorkflowDeploymentStatus_WORKFLOW_DEPLOYMENT_STATUS_FAILED
	default:
		return proto.WorkflowDeploymentStatus_WORKFLOW_DEPLOYMENT_STATUS_UNSPECIFIED
	}
}

func workflowDeploymentStatusFromProto(status proto.WorkflowDeploymentStatus) coreworkflow.DeploymentStatus {
	switch status {
	case proto.WorkflowDeploymentStatus_WORKFLOW_DEPLOYMENT_STATUS_PENDING:
		return coreworkflow.DeploymentStatusPending
	case proto.WorkflowDeploymentStatus_WORKFLOW_DEPLOYMENT_STATUS_ACTIVE:
		return coreworkflow.DeploymentStatusActive
	case proto.WorkflowDeploymentStatus_WORKFLOW_DEPLOYMENT_STATUS_PAUSED:
		return coreworkflow.DeploymentStatusPaused
	case proto.WorkflowDeploymentStatus_WORKFLOW_DEPLOYMENT_STATUS_DELETED:
		return coreworkflow.DeploymentStatusDeleted
	case proto.WorkflowDeploymentStatus_WORKFLOW_DEPLOYMENT_STATUS_FAILED:
		return coreworkflow.DeploymentStatusFailed
	default:
		return ""
	}
}

func workflowDeploymentToProto(deployment *coreworkflow.Deployment) (*proto.WorkflowDeployment, error) {
	if deployment == nil {
		return nil, nil
	}
	spec, err := workflowDeploymentSpecToProto(deployment.Spec)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowDeployment{
		Spec:               spec,
		Status:             workflowDeploymentStatusToProto(deployment.Status),
		CreatedAt:          timeToProto(deployment.CreatedAt),
		UpdatedAt:          timeToProto(deployment.UpdatedAt),
		AppliedGeneration:  deployment.AppliedGeneration,
		SpecDigest:         deployment.SpecDigest,
		TargetDigest:       deployment.TargetDigest,
		ActionTableDigest:  deployment.ActionTableDigest,
		ProviderPlanId:     deployment.ProviderPlanID,
		ProviderPlanDigest: deployment.ProviderPlanDigest,
		Binding:            workflowDeploymentBindingToProto(deployment.Binding),
		Error:              workflowRunErrorToProto(deployment.Error),
	}, nil
}

func workflowDeploymentFromProto(deployment *proto.WorkflowDeployment) (*coreworkflow.Deployment, error) {
	if deployment == nil {
		return nil, nil
	}
	return &coreworkflow.Deployment{
		Spec:               workflowDeploymentSpecFromProto(deployment.GetSpec()),
		Status:             workflowDeploymentStatusFromProto(deployment.GetStatus()),
		CreatedAt:          timeFromProto(deployment.GetCreatedAt()),
		UpdatedAt:          timeFromProto(deployment.GetUpdatedAt()),
		AppliedGeneration:  deployment.GetAppliedGeneration(),
		SpecDigest:         deployment.GetSpecDigest(),
		TargetDigest:       deployment.GetTargetDigest(),
		ActionTableDigest:  deployment.GetActionTableDigest(),
		ProviderPlanID:     deployment.GetProviderPlanId(),
		ProviderPlanDigest: deployment.GetProviderPlanDigest(),
		Binding:            workflowDeploymentBindingFromProto(deployment.GetBinding()),
		Error:              workflowRunErrorFromProto(deployment.GetError()),
	}, nil
}

func workflowActivationsToProto(values []coreworkflow.Activation) []*proto.WorkflowActivation {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.WorkflowActivation, 0, len(values))
	for i := range values {
		out = append(out, workflowActivationToProto(values[i]))
	}
	return out
}

func workflowActivationToProto(value coreworkflow.Activation) *proto.WorkflowActivation {
	input, _ := workflowValueToProto(value.Input)
	runKey, _ := workflowValueToProto(value.RunKey)
	idempotencyKey, _ := workflowValueToProto(value.IdempotencyKey)
	out := &proto.WorkflowActivation{
		Id:             value.ID,
		Paused:         value.Paused,
		Mode:           workflowActivationModeToProto(value.Mode),
		Input:          input,
		RunKey:         runKey,
		IdempotencyKey: idempotencyKey,
	}
	switch {
	case value.Manual:
		out.Kind = &proto.WorkflowActivation_Manual{Manual: &proto.WorkflowManualActivation{}}
	case value.Schedule != nil:
		out.Kind = &proto.WorkflowActivation_Schedule{Schedule: &proto.WorkflowScheduleActivation{Cron: value.Schedule.Cron, Timezone: value.Schedule.Timezone}}
	case value.Event != nil:
		out.Kind = &proto.WorkflowActivation_Event{Event: &proto.WorkflowEventActivation{Match: workflowEventMatchToProto(value.Event.Match)}}
	}
	return out
}

func workflowActivationsFromProto(values []*proto.WorkflowActivation) []coreworkflow.Activation {
	if len(values) == 0 {
		return nil
	}
	out := make([]coreworkflow.Activation, 0, len(values))
	for _, value := range values {
		out = append(out, workflowActivationFromProto(value))
	}
	return out
}

func workflowActivationFromProto(value *proto.WorkflowActivation) coreworkflow.Activation {
	if value == nil {
		return coreworkflow.Activation{}
	}
	out := coreworkflow.Activation{
		ID:             value.GetId(),
		Paused:         value.GetPaused(),
		Mode:           workflowActivationModeFromProto(value.GetMode()),
		Input:          workflowValueFromProto(value.GetInput()),
		RunKey:         workflowValueFromProto(value.GetRunKey()),
		IdempotencyKey: workflowValueFromProto(value.GetIdempotencyKey()),
	}
	switch typed := value.GetKind().(type) {
	case *proto.WorkflowActivation_Schedule:
		out.Schedule = &coreworkflow.ScheduleActivation{Cron: typed.Schedule.GetCron(), Timezone: typed.Schedule.GetTimezone()}
	case *proto.WorkflowActivation_Event:
		out.Event = &coreworkflow.EventActivation{Match: workflowEventMatchFromProto(typed.Event.GetMatch())}
	case *proto.WorkflowActivation_Manual:
		out.Manual = true
	}
	return out
}

func workflowActivationModeToProto(mode coreworkflow.ActivationMode) proto.WorkflowActivationMode {
	switch mode {
	case coreworkflow.ActivationModeStart:
		return proto.WorkflowActivationMode_WORKFLOW_ACTIVATION_MODE_START
	case coreworkflow.ActivationModeSignal:
		return proto.WorkflowActivationMode_WORKFLOW_ACTIVATION_MODE_SIGNAL
	case coreworkflow.ActivationModeSignalOrStart:
		return proto.WorkflowActivationMode_WORKFLOW_ACTIVATION_MODE_SIGNAL_OR_START
	default:
		return proto.WorkflowActivationMode_WORKFLOW_ACTIVATION_MODE_UNSPECIFIED
	}
}

func workflowActivationModeFromProto(mode proto.WorkflowActivationMode) coreworkflow.ActivationMode {
	switch mode {
	case proto.WorkflowActivationMode_WORKFLOW_ACTIVATION_MODE_START:
		return coreworkflow.ActivationModeStart
	case proto.WorkflowActivationMode_WORKFLOW_ACTIVATION_MODE_SIGNAL:
		return coreworkflow.ActivationModeSignal
	case proto.WorkflowActivationMode_WORKFLOW_ACTIVATION_MODE_SIGNAL_OR_START:
		return coreworkflow.ActivationModeSignalOrStart
	default:
		return ""
	}
}

func managedWorkflowDeploymentToProto(providerName string, deployment *coreworkflow.Deployment) (*proto.ManagedWorkflowDeployment, error) {
	pb, err := workflowDeploymentToProto(deployment)
	if err != nil {
		return nil, err
	}
	return &proto.ManagedWorkflowDeployment{ProviderName: providerName, Deployment: pb}, nil
}

func managedWorkflowRunToProto(managed *workflowmanager.ManagedRun) (*proto.ManagedWorkflowRun, error) {
	if managed == nil {
		return nil, nil
	}
	run, err := workflowRunToProto(managed.Run)
	if err != nil {
		return nil, err
	}
	return &proto.ManagedWorkflowRun{ProviderName: managed.ProviderName, Run: run}, nil
}

func managedWorkflowRunSignalToProto(managed *workflowmanager.ManagedRunSignal) (*proto.ManagedWorkflowRunSignal, error) {
	if managed == nil {
		return nil, nil
	}
	run, err := workflowRunToProto(managed.Run)
	if err != nil {
		return nil, err
	}
	signal, err := workflowSignalToProto(managed.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.ManagedWorkflowRunSignal{
		ProviderName: managed.ProviderName,
		Run:          run,
		Signal:       signal,
		StartedRun:   managed.StartedRun,
		WorkflowKey:  managed.WorkflowKey,
	}, nil
}

func valueToProto(value any, set bool) (*structpb.Value, error) {
	if !set {
		return nil, nil
	}
	return structpb.NewValue(value)
}

func valueFromProto(value *structpb.Value) any {
	if value == nil {
		return nil
	}
	return value.AsInterface()
}

func workflowExtensionsToProto(values map[string]any) (map[string]*structpb.Value, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]*structpb.Value, len(values))
	for key, value := range values {
		encoded, err := structpb.NewValue(value)
		if err != nil {
			return nil, err
		}
		out[key] = encoded
	}
	return out, nil
}

func workflowExtensionsFromProto(values map[string]*structpb.Value) (map[string]any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = valueFromProto(value)
	}
	return out, nil
}

func structMap(value *structpb.Struct) map[string]any {
	if value == nil {
		return nil
	}
	return value.AsMap()
}

func structFromMap(value map[string]any) (*structpb.Struct, error) {
	if value == nil {
		return nil, nil
	}
	return structpb.NewStruct(value)
}

func timeToProto(t *time.Time) *timestamppb.Timestamp {
	if t == nil || t.IsZero() {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
