package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type appClientInvoker struct {
	newApp func(string) (gestalt.App, error)
}

func (i appClientInvoker) InvokeWorkflowApp(ctx context.Context, call AppInvocation) (*AppResult, error) {
	client, err := i.newApp(call.Token)
	if err != nil {
		return nil, err
	}
	if closer, ok := client.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}
	result, err := client.Invoke(ctx, call.App, call.Operation, call.Params, &gestalt.InvokeOptions{
		Connection:      call.Connection,
		Instance:        call.Instance,
		CredentialMode:  call.CredentialMode,
		IdempotencyKey:  call.IdempotencyKey,
		WorkflowContext: call.WorkflowContext,
	})
	if err != nil {
		return nil, err
	}
	out := &AppResult{}
	if result != nil {
		out.Status = result.Status
		out.Body = result.Body
	}
	return out, nil
}

func (e *Executor) invokeAppStep(ctx context.Context, req Request, token string, app *gestalt.WorkflowStepAppCall, inputs map[string]any, outputs map[string]any, invocationScope, stepID string) (any, error) {
	appName := strings.TrimSpace(app.Name)
	operation := strings.TrimSpace(app.Operation)
	if appName == "" || operation == "" {
		return nil, fmt.Errorf("workflow step app name and operation are required")
	}
	evalCtx := EvalContext{Request: req, Outputs: outputs, Inputs: inputs, AllowInputs: true}
	paramsValue, ok, err := evalCtx.EvaluateValue(app.Input)
	if err != nil {
		return nil, WorkflowEvalError(err)
	}
	if !ok {
		return nil, fmt.Errorf("workflow step app input did not resolve")
	}
	params := map[string]any{}
	if paramsValue != nil {
		object, ok := paramsValue.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("workflow step app input must resolve to an object")
		}
		params = object
	}
	if req.Input != nil {
		params = maps.Clone(params)
		for key, value := range req.Input {
			params[key] = value
		}
	}
	result, err := e.appInvoker.InvokeWorkflowApp(ctx, AppInvocation{
		Token:           token,
		App:             appName,
		Operation:       operation,
		Params:          params,
		Connection:      strings.TrimSpace(app.Connection),
		Instance:        strings.TrimSpace(app.Instance),
		CredentialMode:  strings.TrimSpace(app.CredentialMode),
		IdempotencyKey:  WorkflowStepIdempotencyKey(req, invocationScope, stepID, "step"),
		WorkflowContext: WorkflowRunContext(req),
	})
	if err != nil {
		return nil, err
	}
	output := WorkflowAppOutputEnvelope(result)
	if result != nil && result.Status >= http.StatusBadRequest {
		return nil, fmt.Errorf("workflow step app %s.%s returned status %d", appName, operation, result.Status)
	}
	return output, nil
}

func (e *Executor) invokeAgentStep(ctx context.Context, req Request, token string, agent *gestalt.WorkflowStepAgentTurn, inputs map[string]any, sessions map[string]workflowAgentSessionState, invocationScope, stepID string, timeoutSeconds int32, stepMetadata any) (any, string, error) {
	if timeoutSeconds <= 0 {
		return nil, "", fmt.Errorf("workflow agent step timeout_seconds must be positive")
	}
	client, err := e.newAgent(token)
	if err != nil {
		return nil, "", err
	}
	if closer, ok := client.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}
	providerName := strings.TrimSpace(agent.Provider)
	model := strings.TrimSpace(agent.Model)
	if providerName == "" {
		return nil, "", fmt.Errorf("workflow agent provider is required")
	}
	workflowContext := WorkflowRunContext(req)
	ctx = gestalt.WithWorkflowContext(ctx, workflowContext)
	sessionKey := strings.TrimSpace(agent.SessionKey)
	if sessionKey == "" {
		sessionKey = stepID
	}
	optionsKey := stableJSON(agent.ModelOptions)
	state, ok := sessions[sessionKey]
	if ok {
		if state.providerName != providerName || state.model != model || state.options != optionsKey {
			return nil, "", fmt.Errorf("workflow agent session_key %q uses incompatible provider, model, or model_options", sessionKey)
		}
	} else {
		session, err := client.CreateSession(ctx, gestalt.AgentCreateSession{
			ProviderName: providerName,
			Model:        model,
			Metadata: map[string]any{
				"workflow":       workflowContext,
				"workflowStepId": stepID,
			},
			IdempotencyKey: WorkflowStepIdempotencyKey(req, invocationScope, stepID, "agent-session:"+sessionKey),
		})
		if err != nil {
			return nil, "", err
		}
		state = workflowAgentSessionState{session: session, providerName: providerName, model: model, options: optionsKey}
		sessions[sessionKey] = state
	}
	messages, err := workflowAgentTurnMessages(agent, inputs, req, nil)
	if err != nil {
		return nil, "", err
	}
	turn, err := client.CreateTurn(ctx, gestalt.AgentCreateTurn{
		SessionID:      state.session.ID,
		Model:          model,
		Messages:       messages,
		ToolRefs:       append([]gestalt.AgentToolRef(nil), agent.Tools...),
		ToolRefsSet:    true,
		Output:         agent.Output,
		Metadata:       stepMetadata,
		ModelOptions:   agent.ModelOptions,
		TimeoutSeconds: timeoutSeconds,
		IdempotencyKey: WorkflowStepIdempotencyKey(req, invocationScope, stepID, "agent-turn"),
	})
	if err != nil {
		return nil, "", err
	}
	turn, err = waitForWorkflowAgentTurn(ctx, client, turn, e.agentPollInterval)
	if err != nil {
		if turn != nil && strings.TrimSpace(turn.ID) != "" {
			_, _ = client.CancelTurn(context.WithoutCancel(ctx), gestalt.AgentCancelTurn{TurnID: turn.ID, Reason: err.Error()})
		}
		return nil, workflowAgentTurnID(turn), err
	}
	switch turn.Status {
	case gestalt.AgentExecutionStatusSucceeded:
		return WorkflowAgentOutputEnvelope(state.session, turn), turn.ID, nil
	case gestalt.AgentExecutionStatusCanceled:
		return nil, turn.ID, fmt.Errorf("workflow agent turn %q was canceled: %s", turn.ID, strings.TrimSpace(turn.StatusMessage))
	case gestalt.AgentExecutionStatusWaitingForInput:
		_, _ = client.CancelTurn(context.WithoutCancel(ctx), gestalt.AgentCancelTurn{TurnID: turn.ID, Reason: "workflow agent step turn cannot wait for input"})
		return nil, turn.ID, fmt.Errorf("workflow agent turn %q is waiting for input", turn.ID)
	default:
		return nil, turn.ID, fmt.Errorf("workflow agent turn %q finished with status %q: %s", turn.ID, turn.Status, strings.TrimSpace(turn.StatusMessage))
	}
}

func workflowAgentTurnMessages(agent *gestalt.WorkflowStepAgentTurn, inputs map[string]any, req Request, outputs map[string]any) ([]gestalt.AgentMessage, error) {
	evalCtx := EvalContext{Request: req, Outputs: outputs, Inputs: inputs, AllowInputs: true}
	messages := make([]gestalt.AgentMessage, 0, len(agent.Messages)+1)
	for i := range agent.Messages {
		message := agent.Messages[i]
		text, err := evalCtx.RenderTemplate(message.Text.Template)
		if err != nil {
			return nil, fmt.Errorf("messages[%d].text: %w", i, WorkflowEvalError(err))
		}
		messages = append(messages, gestalt.AgentMessage{
			Role:     message.Role,
			Text:     text,
			Metadata: workflowMapValue(message.Metadata),
		})
	}
	if agent.Prompt.Template != "" {
		text, err := evalCtx.RenderTemplate(agent.Prompt.Template)
		if err != nil {
			return nil, fmt.Errorf("prompt: %w", WorkflowEvalError(err))
		}
		messages = append(messages, gestalt.AgentMessage{Role: "user", Text: text})
	}
	return messages, nil
}

func waitForWorkflowAgentTurn(ctx context.Context, agent AgentClient, turn *gestalt.AgentTurn, interval time.Duration) (*gestalt.AgentTurn, error) {
	if turn == nil || strings.TrimSpace(turn.ID) == "" {
		return nil, fmt.Errorf("workflow agent turn is missing")
	}
	if interval <= 0 {
		interval = time.Second
	}
	current := turn
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		switch current.Status {
		case gestalt.AgentExecutionStatusSucceeded, gestalt.AgentExecutionStatusFailed, gestalt.AgentExecutionStatusCanceled, gestalt.AgentExecutionStatusWaitingForInput:
			return current, nil
		}
		select {
		case <-ctx.Done():
			return current, ctx.Err()
		case <-ticker.C:
			next, err := agent.GetTurn(ctx, gestalt.AgentGetTurn{TurnID: current.ID})
			if err != nil {
				return current, err
			}
			if next == nil || strings.TrimSpace(next.ID) == "" {
				return current, fmt.Errorf("workflow agent turn is missing")
			}
			current = next
		}
	}
}

func workflowAgentTurnID(turn *gestalt.AgentTurn) string {
	if turn == nil {
		return ""
	}
	return strings.TrimSpace(turn.ID)
}

type workflowAgentSessionState struct {
	session      *gestalt.AgentSession
	providerName string
	model        string
	options      string
}

type workflowStepResult struct {
	ID            string             `json:"id"`
	Status        string             `json:"status"`
	TurnID        string             `json:"turnId,omitempty"`
	SkippedReason string             `json:"skippedReason,omitempty"`
	Error         *workflowStepError `json:"error,omitempty"`
}

type workflowStepError struct {
	StepID  string `json:"stepId,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type workflowStepsResult struct {
	Version     int                  `json:"version"`
	Status      string               `json:"status"`
	Steps       []workflowStepResult `json:"steps"`
	Outputs     map[string]any       `json:"outputs"`
	FinalStepID string               `json:"finalStepId,omitempty"`
	FinalOutput any                  `json:"finalOutput,omitempty"`
	Error       *workflowStepError   `json:"error,omitempty"`
}

type StepResult = workflowStepResult
type StepError = workflowStepError
type StepsResult = workflowStepsResult

func workflowFailedStepResponse(result workflowStepsResult, stepID, code, message string) (*Response, error) {
	errValue := &workflowStepError{StepID: stepID, Code: code, Message: message}
	result.Status = "failed"
	result.Error = errValue
	if stepID != "" {
		result.Steps = append(result.Steps, workflowStepResult{ID: stepID, Status: "failed", Error: errValue})
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &Response{Status: http.StatusInternalServerError, Body: string(body)}, nil
}

func WorkflowAppOutputEnvelope(result *AppResult) map[string]any {
	status := 0
	body := ""
	headers := map[string][]string{}
	if result != nil {
		status = result.Status
		body = result.Body
		for key, values := range result.Headers {
			headers[key] = append([]string(nil), values...)
		}
	}
	bodyText := truncateWorkflowString(body, workflowStepOutputBodyMaxBytes)
	bodyEnvelope := map[string]any{
		"text":      bodyText,
		"truncated": len(body) != len(bodyText),
		"jsonValid": false,
	}
	if len(body) == len(bodyText) {
		var parsed any
		if err := json.Unmarshal([]byte(bodyText), &parsed); err == nil {
			bodyEnvelope["jsonValid"] = true
			bodyEnvelope["json"] = parsed
		}
	}
	return map[string]any{
		"version": 1,
		"kind":    "app",
		"app": map[string]any{
			"status":  status,
			"headers": headers,
			"body":    bodyEnvelope,
		},
	}
}

func WorkflowAgentOutputEnvelope(session *gestalt.AgentSession, turn *gestalt.AgentTurn) map[string]any {
	agent := map[string]any{}
	if session != nil {
		agent["sessionId"] = session.ID
	}
	if turn != nil {
		agent["turnId"] = turn.ID
		if output := turn.Output; output != nil {
			if text := output.Text; text != nil {
				agent["text"] = text.Text
			}
			if structured := output.Structured; structured != nil {
				agent["text"] = structured.Text
				agent["structuredOutput"] = maps.Clone(structured.Value)
			}
		}
	}
	return map[string]any{"version": 1, "kind": "agent", "agent": agent}
}

func workflowStepWhenMatches(when *gestalt.WorkflowStepWhen, req Request, outputs map[string]any, skipped map[string]struct{}) (bool, string, error) {
	if when == nil {
		return true, "", nil
	}
	if source := when.Value.StepOutput; source != nil {
		if _, ok := skipped[strings.TrimSpace(source.StepID)]; ok {
			return false, "missing_dependency", nil
		}
	}
	evalCtx := EvalContext{Request: req, Outputs: outputs}
	value, ok, err := evalCtx.EvaluateValue(when.Value)
	if err != nil {
		return false, "", WorkflowEvalError(err)
	}
	if !ok {
		return false, "", fmt.Errorf("workflow step when value did not resolve")
	}
	if !IsScalarJSON(value) || !IsScalarJSON(when.Equals) {
		return false, "", fmt.Errorf("workflow step when values must be scalar JSON values")
	}
	if ScalarEqual(value, when.Equals) {
		return true, "", nil
	}
	return false, "when_false", nil
}

func stableJSON(value any) string {
	if value == nil {
		return ""
	}
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(body)
}
