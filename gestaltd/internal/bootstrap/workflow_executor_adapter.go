package bootstrap

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	gestaltsdk "github.com/valon-technologies/gestalt/sdk/go"
	sdkworkflow "github.com/valon-technologies/gestalt/sdk/go/workflow"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

func (r *workflowRuntime) invokeWorkflowStepsWithExecutor(ctx context.Context, req coreworkflow.InvokeOperationRequest, target coreworkflow.Target, agentManager agentmanager.Service, invoker invocation.Invoker, p *principal.Principal, callerAppName string) (*coreworkflow.InvokeOperationResponse, error) {
	executor := sdkworkflow.New(sdkworkflow.Config{
		AppInvoker: workflowExecutorAppInvoker{invoker: invoker, principal: p},
		NewAgent: func(string) (sdkworkflow.AgentClient, error) {
			return workflowExecutorAgentClient{manager: agentManager, principal: p, callerAppName: callerAppName}, nil
		},
	})
	resp, err := executor.Execute(ctx, workflowExecRequestFromCore(req, target))
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("workflow executor returned nil response")
	}
	return &coreworkflow.InvokeOperationResponse{Status: resp.Status, Body: resp.Body}, nil
}

type workflowExecutorAppInvoker struct {
	invoker   invocation.Invoker
	principal *principal.Principal
}

func (i workflowExecutorAppInvoker) InvokeWorkflowApp(ctx context.Context, call sdkworkflow.AppInvocation) (*sdkworkflow.AppResult, error) {
	if i.invoker == nil {
		return nil, fmt.Errorf("%w: workflow step requires an invoker", invocation.ErrInternal)
	}
	if contextValue := call.WorkflowContext; len(contextValue) > 0 {
		ctx = invocation.WithWorkflowContext(ctx, contextValue)
	}
	if connection := strings.TrimSpace(call.Connection); connection != "" {
		ctx = invocation.WithConnection(ctx, connection)
	}
	if mode := core.NormalizeOptionalConnectionMode(core.ConnectionMode(call.CredentialMode)); mode != "" {
		ctx = invocation.WithCredentialModeOverride(ctx, mode)
	}
	ctx = invocation.WithIdempotencyKey(ctx, call.IdempotencyKey)
	result, err := i.invoker.Invoke(ctx, i.principal, call.App, strings.TrimSpace(call.Instance), call.Operation, call.Params)
	if err != nil {
		return nil, err
	}
	out := &sdkworkflow.AppResult{Headers: map[string][]string{}}
	if result != nil {
		out.Status = result.Status
		out.Body = result.Body
		for key, values := range result.Headers {
			out.Headers[key] = append([]string(nil), values...)
		}
	}
	return out, nil
}

type workflowExecutorAgentClient struct {
	manager       agentmanager.Service
	principal     *principal.Principal
	callerAppName string
}

func (c workflowExecutorAgentClient) CreateSession(ctx context.Context, req gestaltsdk.AgentCreateSession) (*gestaltsdk.AgentSession, error) {
	if c.manager == nil {
		return nil, fmt.Errorf("workflow runtime agent manager is not configured")
	}
	session, err := c.manager.CreateSession(runtimehost.WithWorkflowAgentProviderDeadline(ctx), c.principal, coreagent.ManagerCreateSessionRequest{
		ProviderName:   req.ProviderName,
		Model:          req.Model,
		Metadata:       workflowExecMapValue(req.Metadata),
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	return workflowExecAgentSessionFromCore(session), nil
}

func (c workflowExecutorAgentClient) CreateTurn(ctx context.Context, req gestaltsdk.AgentCreateTurn) (*gestaltsdk.AgentTurn, error) {
	if c.manager == nil {
		return nil, fmt.Errorf("workflow runtime agent manager is not configured")
	}
	responseSchema := workflowExecMapValue(req.ResponseSchema)
	turn, err := c.manager.CreateTurn(runtimehost.WithWorkflowAgentProviderDeadline(ctx), c.principal, coreagent.ManagerCreateTurnRequest{
		CallerAppName:     c.callerAppName,
		SessionID:         req.SessionID,
		Model:             req.Model,
		Messages:          workflowExecMessagesToCore(req.Messages),
		ToolRefs:          workflowExecToolRefsToCore(req.ToolRefs),
		ToolRefsSet:       req.ToolRefsSet,
		ResponseSchema:    responseSchema,
		ResponseSchemaSet: len(responseSchema) > 0,
		Metadata:          workflowExecMapValue(req.Metadata),
		ModelOptions:      workflowExecMapValue(req.ModelOptions),
		TimeoutSeconds:    int(req.TimeoutSeconds),
		IdempotencyKey:    req.IdempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	return workflowExecAgentTurnFromCore(turn), nil
}

func (c workflowExecutorAgentClient) GetTurn(ctx context.Context, req gestaltsdk.AgentGetTurn) (*gestaltsdk.AgentTurn, error) {
	if c.manager == nil {
		return nil, fmt.Errorf("workflow runtime agent manager is not configured")
	}
	turn, err := c.manager.GetTurn(runtimehost.WithWorkflowAgentProviderDeadline(ctx), c.principal, req.TurnID)
	if err != nil {
		return nil, err
	}
	return workflowExecAgentTurnFromCore(turn), nil
}

func (c workflowExecutorAgentClient) CancelTurn(ctx context.Context, req gestaltsdk.AgentCancelTurn) (*gestaltsdk.AgentTurn, error) {
	if c.manager == nil {
		return nil, fmt.Errorf("workflow runtime agent manager is not configured")
	}
	turn, err := c.manager.CancelTurn(runtimehost.WithWorkflowAgentProviderDeadline(ctx), c.principal, req.TurnID, req.Reason)
	if err != nil {
		return nil, err
	}
	return workflowExecAgentTurnFromCore(turn), nil
}

func workflowExecRequestFromCore(req coreworkflow.InvokeOperationRequest, target coreworkflow.Target) sdkworkflow.Request {
	return sdkworkflow.Request{
		ProviderName:    req.ProviderName,
		RunID:           req.RunID,
		Target:          workflowExecTargetFromCore(target),
		Trigger:         workflowExecTriggerFromCore(req.Trigger),
		Input:           maps.Clone(req.Input),
		Metadata:        maps.Clone(req.Metadata),
		CreatedBy:       workflowExecActorPtrFromCore(req.CreatedBy),
		ExecutionRef:    req.ExecutionRef,
		InvocationToken: "gestaltd-internal",
		Signals:         workflowExecSignalsFromCore(req.Signals),
	}
}

func workflowExecTargetFromCore(target coreworkflow.Target) *gestaltsdk.BoundWorkflowTarget {
	if len(target.Steps) == 0 {
		return &gestaltsdk.BoundWorkflowTarget{}
	}
	steps := make([]gestaltsdk.WorkflowStep, 0, len(target.Steps))
	for i := range target.Steps {
		step := target.Steps[i]
		out := gestaltsdk.WorkflowStep{
			ID:             step.ID,
			Inputs:         workflowExecValuesFromCore(step.Inputs),
			When:           workflowExecWhenFromCore(step.When),
			TimeoutSeconds: int32(step.TimeoutSeconds),
			Metadata:       maps.Clone(step.Metadata),
		}
		if step.App != nil {
			out.App = &gestaltsdk.WorkflowStepAppCall{
				Name:           step.App.Name,
				Operation:      step.App.Operation,
				Input:          workflowExecValueFromCore(step.App.Input),
				Connection:     step.App.Connection,
				Instance:       step.App.Instance,
				CredentialMode: string(step.App.CredentialMode),
			}
		}
		if step.Agent != nil {
			out.Agent = &gestaltsdk.WorkflowStepAgentTurn{
				Provider:       step.Agent.ProviderName,
				Model:          step.Agent.Model,
				SessionKey:     step.Agent.SessionKey,
				Prompt:         gestaltsdk.WorkflowText{Template: step.Agent.Prompt.Template},
				Messages:       workflowExecAgentMessagesFromCore(step.Agent.Messages),
				Tools:          workflowExecToolRefsFromCore(step.Agent.ToolRefs),
				ResponseSchema: maps.Clone(step.Agent.ResponseSchema),
				ModelOptions:   maps.Clone(step.Agent.ModelOptions),
			}
		}
		steps = append(steps, out)
	}
	return &gestaltsdk.BoundWorkflowTarget{Steps: steps}
}

func workflowExecValuesFromCore(values map[string]coreworkflow.Value) map[string]gestaltsdk.WorkflowValue {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]gestaltsdk.WorkflowValue, len(values))
	for key, value := range values {
		out[key] = workflowExecValueFromCore(value)
	}
	return out
}

func workflowExecValueFromCore(value coreworkflow.Value) gestaltsdk.WorkflowValue {
	out := gestaltsdk.WorkflowValue{
		Literal:       value.Literal,
		LiteralSet:    value.LiteralSet,
		RunInput:      value.RunInput,
		SignalPayload: value.SignalPayload,
	}
	if value.Object != nil {
		out.Object = workflowExecValuesFromCore(value.Object)
	}
	if value.Array != nil {
		out.Array = make([]gestaltsdk.WorkflowValue, 0, len(value.Array))
		for i := range value.Array {
			out.Array = append(out.Array, workflowExecValueFromCore(value.Array[i]))
		}
	}
	if value.Template != nil {
		out.Template = &gestaltsdk.WorkflowText{Template: value.Template.Template}
	}
	if value.StepOutput != nil {
		out.StepOutput = &gestaltsdk.WorkflowStepOutputSource{StepID: value.StepOutput.StepID, Path: value.StepOutput.Path}
	}
	return out
}

func workflowExecWhenFromCore(when *coreworkflow.StepWhen) *gestaltsdk.WorkflowStepWhen {
	if when == nil {
		return nil
	}
	return &gestaltsdk.WorkflowStepWhen{Value: workflowExecValueFromCore(when.Value), Equals: when.Equals}
}

func workflowExecAgentMessagesFromCore(messages []coreworkflow.AgentMessage) []gestaltsdk.WorkflowAgentMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]gestaltsdk.WorkflowAgentMessage, 0, len(messages))
	for i := range messages {
		message := messages[i]
		out = append(out, gestaltsdk.WorkflowAgentMessage{
			Role:     message.Role,
			Text:     gestaltsdk.WorkflowText{Template: message.Text.Template},
			Metadata: maps.Clone(message.Metadata),
		})
	}
	return out
}

func workflowExecToolRefsFromCore(refs []coreagent.ToolRef) []gestaltsdk.AgentToolRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]gestaltsdk.AgentToolRef, 0, len(refs))
	for i := range refs {
		ref := refs[i]
		out = append(out, gestaltsdk.AgentToolRef{
			App:                   ref.App,
			Operation:             ref.Operation,
			Connection:            ref.Connection,
			Instance:              ref.Instance,
			Title:                 ref.Title,
			Description:           ref.Description,
			System:                ref.System,
			RunAs:                 workflowExecSubjectFromRunAs(ref.RunAs),
			RunAsExternalIdentity: workflowExecExternalIdentityFromCore(ref.RunAsExternalIdentity),
		})
	}
	return out
}

func workflowExecToolRefsToCore(refs []gestaltsdk.AgentToolRef) []coreagent.ToolRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]coreagent.ToolRef, 0, len(refs))
	for i := range refs {
		ref := refs[i]
		out = append(out, coreagent.ToolRef{
			App:                   ref.App,
			Operation:             ref.Operation,
			Connection:            ref.Connection,
			Instance:              ref.Instance,
			Title:                 ref.Title,
			Description:           ref.Description,
			System:                ref.System,
			RunAs:                 workflowExecRunAsFromSubject(ref.RunAs),
			RunAsExternalIdentity: workflowExecExternalIdentityToCore(ref.RunAsExternalIdentity),
		})
	}
	return out
}

func workflowExecTriggerFromCore(trigger coreworkflow.RunTrigger) *gestaltsdk.WorkflowRunTrigger {
	out := &gestaltsdk.WorkflowRunTrigger{Manual: trigger.Manual}
	if trigger.Schedule != nil {
		out.Schedule = &gestaltsdk.WorkflowScheduleTrigger{ScheduleID: trigger.Schedule.ScheduleID, ScheduledFor: workflowExecTimePtr(trigger.Schedule.ScheduledFor)}
	}
	if trigger.Event != nil {
		event := workflowExecEventFromCore(trigger.Event.Event)
		out.Event = &gestaltsdk.WorkflowEventTriggerInvocation{TriggerID: trigger.Event.TriggerID, Event: &event}
	}
	return out
}

func workflowExecEventFromCore(event coreworkflow.Event) gestaltsdk.WorkflowEvent {
	return gestaltsdk.WorkflowEvent{
		ID:              event.ID,
		Source:          event.Source,
		SpecVersion:     event.SpecVersion,
		Type:            event.Type,
		Subject:         event.Subject,
		Time:            workflowExecTime(event.Time),
		DataContentType: event.DataContentType,
		Data:            maps.Clone(event.Data),
		Extensions:      maps.Clone(event.Extensions),
	}
}

func workflowExecSignalsFromCore(signals []coreworkflow.Signal) []gestaltsdk.WorkflowSignal {
	if len(signals) == 0 {
		return nil
	}
	out := make([]gestaltsdk.WorkflowSignal, 0, len(signals))
	for i := range signals {
		signal := signals[i]
		out = append(out, gestaltsdk.WorkflowSignal{
			ID:             signal.ID,
			Name:           signal.Name,
			Payload:        maps.Clone(signal.Payload),
			Metadata:       maps.Clone(signal.Metadata),
			CreatedBy:      workflowExecActorPtrFromCore(signal.CreatedBy),
			CreatedAt:      workflowExecTime(signal.CreatedAt),
			IdempotencyKey: signal.IdempotencyKey,
			Sequence:       signal.Sequence,
		})
	}
	return out
}

func workflowExecActorPtrFromCore(actor core.Actor) *gestaltsdk.WorkflowActor {
	if actor.SubjectID == "" && actor.SubjectKind == "" && actor.DisplayName == "" && actor.AuthSource == "" {
		return nil
	}
	return &gestaltsdk.WorkflowActor{
		SubjectID:   actor.SubjectID,
		SubjectKind: actor.SubjectKind,
		DisplayName: actor.DisplayName,
		AuthSource:  actor.AuthSource,
	}
}

func workflowExecSubjectFromRunAs(subject *core.RunAsSubject) *gestaltsdk.Subject {
	if subject == nil {
		return nil
	}
	return &gestaltsdk.Subject{
		ID:                  subject.SubjectID,
		Kind:                subject.SubjectKind,
		CredentialSubjectID: subject.CredentialSubjectID,
		DisplayName:         subject.DisplayName,
		AuthSource:          subject.AuthSource,
	}
}

func workflowExecRunAsFromSubject(subject *gestaltsdk.Subject) *core.RunAsSubject {
	if subject == nil {
		return nil
	}
	return &core.RunAsSubject{
		SubjectID:           subject.ID,
		SubjectKind:         subject.Kind,
		CredentialSubjectID: subject.CredentialSubjectID,
		DisplayName:         subject.DisplayName,
		AuthSource:          subject.AuthSource,
	}
}

func workflowExecExternalIdentityFromCore(ref *core.ExternalIdentityRef) *gestaltsdk.ExternalIdentity {
	if ref == nil {
		return nil
	}
	return &gestaltsdk.ExternalIdentity{Type: ref.Type, ID: ref.ID}
}

func workflowExecExternalIdentityToCore(ref *gestaltsdk.ExternalIdentity) *core.ExternalIdentityRef {
	if ref == nil {
		return nil
	}
	return &core.ExternalIdentityRef{Type: ref.Type, ID: ref.ID}
}

func workflowExecMessagesToCore(messages []gestaltsdk.AgentMessage) []coreagent.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]coreagent.Message, 0, len(messages))
	for i := range messages {
		message := messages[i]
		out = append(out, coreagent.Message{
			Role:     message.Role,
			Text:     message.Text,
			Parts:    workflowExecMessagePartsToCore(message.Parts),
			Metadata: maps.Clone(message.Metadata),
		})
	}
	return out
}

func workflowExecMessagePartsToCore(parts []gestaltsdk.AgentMessagePart) []coreagent.MessagePart {
	if len(parts) == 0 {
		return nil
	}
	out := make([]coreagent.MessagePart, 0, len(parts))
	for i := range parts {
		part := parts[i]
		out = append(out, coreagent.MessagePart{
			Type:       workflowExecMessagePartTypeToCore(part.Type),
			Text:       part.Text,
			JSON:       maps.Clone(part.JSON),
			ToolCall:   workflowExecToolCallPartToCore(part.ToolCall),
			ToolResult: workflowExecToolResultPartToCore(part.ToolResult),
			ImageRef:   workflowExecImageRefPartToCore(part.ImageRef),
		})
	}
	return out
}

func workflowExecToolCallPartToCore(part *gestaltsdk.AgentMessagePartToolCall) *coreagent.ToolCallPart {
	if part == nil {
		return nil
	}
	return &coreagent.ToolCallPart{ID: part.ID, ToolID: part.ToolID, Arguments: maps.Clone(part.Arguments)}
}

func workflowExecToolResultPartToCore(part *gestaltsdk.AgentMessagePartToolResult) *coreagent.ToolResultPart {
	if part == nil {
		return nil
	}
	return &coreagent.ToolResultPart{ToolCallID: part.ToolCallID, Status: int(part.Status), Content: part.Content, Output: maps.Clone(part.Output)}
}

func workflowExecImageRefPartToCore(part *gestaltsdk.AgentMessagePartImageRef) *coreagent.ImageRefPart {
	if part == nil {
		return nil
	}
	return &coreagent.ImageRefPart{URI: part.URI, MIMEType: part.MimeType}
}

func workflowExecMessagePartTypeToCore(value gestaltsdk.AgentMessagePartType) coreagent.MessagePartType {
	switch value {
	case gestaltsdk.AgentMessagePartTypeText:
		return coreagent.MessagePartTypeText
	case gestaltsdk.AgentMessagePartTypeJSON:
		return coreagent.MessagePartTypeJSON
	case gestaltsdk.AgentMessagePartTypeToolCall:
		return coreagent.MessagePartTypeToolCall
	case gestaltsdk.AgentMessagePartTypeToolResult:
		return coreagent.MessagePartTypeToolResult
	case gestaltsdk.AgentMessagePartTypeImageRef:
		return coreagent.MessagePartTypeImageRef
	default:
		return ""
	}
}

func workflowExecAgentSessionFromCore(session *coreagent.Session) *gestaltsdk.AgentSession {
	if session == nil {
		return nil
	}
	return &gestaltsdk.AgentSession{
		ID:           session.ID,
		ProviderName: session.ProviderName,
		Model:        session.Model,
		ClientRef:    session.ClientRef,
		State:        workflowExecSessionStateFromCore(session.State),
		Metadata:     maps.Clone(session.Metadata),
		CreatedBy:    workflowExecAgentActorFromCore(session.CreatedBy),
		CreatedAt:    workflowExecTime(session.CreatedAt),
		UpdatedAt:    workflowExecTime(session.UpdatedAt),
		LastTurnAt:   workflowExecTimePtr(session.LastTurnAt),
	}
}

func workflowExecAgentTurnFromCore(turn *coreagent.Turn) *gestaltsdk.AgentTurn {
	if turn == nil {
		return nil
	}
	return &gestaltsdk.AgentTurn{
		ID:               turn.ID,
		SessionID:        turn.SessionID,
		ProviderName:     turn.ProviderName,
		Model:            turn.Model,
		Status:           workflowExecExecutionStatusFromCore(turn.Status),
		Messages:         workflowExecMessagesFromCore(turn.Messages),
		OutputText:       turn.OutputText,
		StructuredOutput: maps.Clone(turn.StructuredOutput),
		StatusMessage:    turn.StatusMessage,
		CreatedBy:        workflowExecAgentActorFromCore(turn.CreatedBy),
		CreatedAt:        workflowExecTime(turn.CreatedAt),
		StartedAt:        workflowExecTimePtr(turn.StartedAt),
		CompletedAt:      workflowExecTimePtr(turn.CompletedAt),
		ExecutionRef:     turn.ExecutionRef,
	}
}

func workflowExecMessagesFromCore(messages []coreagent.Message) []gestaltsdk.AgentMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]gestaltsdk.AgentMessage, 0, len(messages))
	for i := range messages {
		message := messages[i]
		out = append(out, gestaltsdk.AgentMessage{Role: message.Role, Text: message.Text, Metadata: maps.Clone(message.Metadata)})
	}
	return out
}

func workflowExecAgentActorFromCore(actor coreagent.Actor) *gestaltsdk.AgentActor {
	if actor.SubjectID == "" && actor.SubjectKind == "" && actor.DisplayName == "" && actor.AuthSource == "" {
		return nil
	}
	return &gestaltsdk.AgentActor{SubjectID: actor.SubjectID, SubjectKind: actor.SubjectKind, DisplayName: actor.DisplayName, AuthSource: actor.AuthSource}
}

func workflowExecSessionStateFromCore(status coreagent.SessionState) gestaltsdk.AgentSessionState {
	switch status {
	case coreagent.SessionStateActive:
		return gestaltsdk.AgentSessionStateActive
	case coreagent.SessionStateArchived:
		return gestaltsdk.AgentSessionStateArchived
	default:
		return gestaltsdk.AgentSessionStateUnspecified
	}
}

func workflowExecExecutionStatusFromCore(status coreagent.ExecutionStatus) gestaltsdk.AgentExecutionStatus {
	switch status {
	case coreagent.ExecutionStatusPending:
		return gestaltsdk.AgentExecutionStatusPending
	case coreagent.ExecutionStatusRunning:
		return gestaltsdk.AgentExecutionStatusRunning
	case coreagent.ExecutionStatusSucceeded:
		return gestaltsdk.AgentExecutionStatusSucceeded
	case coreagent.ExecutionStatusFailed:
		return gestaltsdk.AgentExecutionStatusFailed
	case coreagent.ExecutionStatusCanceled:
		return gestaltsdk.AgentExecutionStatusCanceled
	case coreagent.ExecutionStatusWaitingForInput:
		return gestaltsdk.AgentExecutionStatusWaitingForInput
	default:
		return gestaltsdk.AgentExecutionStatusUnspecified
	}
}

func workflowExecTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func workflowExecTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func workflowExecMapValue(value any) map[string]any {
	if value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		return maps.Clone(typed)
	}
	return map[string]any{"value": value}
}

var _ sdkworkflow.AppInvoker = workflowExecutorAppInvoker{}
var _ sdkworkflow.AgentClient = workflowExecutorAgentClient{}

type workflowStepsResult = sdkworkflow.StepsResult

func workflowTargetContext(target coreworkflow.Target) map[string]any {
	return sdkworkflow.WorkflowTargetContext(workflowExecTargetFromCore(target))
}

func workflowTriggerContext(trigger coreworkflow.RunTrigger) map[string]any {
	return sdkworkflow.WorkflowTriggerContext(workflowExecTriggerFromCore(trigger))
}
