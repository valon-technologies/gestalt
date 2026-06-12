package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

const workflowStepOutputBodyMaxBytes = 64 * 1024

// Request is the workflow callback request an executor serves.
type Request struct {
	ProviderName         string
	RunID                string
	DefinitionID         string
	DefinitionGeneration int64
	WorkflowKey          string
	Target               *gestalt.BoundWorkflowTarget
	Trigger              *gestalt.WorkflowRunTrigger
	Input                map[string]any
	Metadata             map[string]any
	CreatedBySubjectID   string
	RunAs                *gestalt.Subject
	Signals              []gestalt.WorkflowSignal
}

// Response is the executor's reply to a workflow callback.
type Response struct {
	Status int
	Body   string
}

// StepExecutor executes one workflow step kind.
type StepExecutor interface {
	Execute(context.Context, Request) (*Response, error)
	ExecuteStep(context.Context, StepRequest) (*StepResponse, error)
	Close() error
}

// StepRequest carries one step invocation's inputs.
type StepRequest struct {
	Request        Request
	StepIndex      int
	Outputs        map[string]any
	StepInputs     map[string]any
	SkippedStepIDs []string
}

// StepResponse carries one step invocation's outputs.
type StepResponse struct {
	Status      int
	Step        StepResult
	Input       map[string]any
	Output      any
	Outputs     map[string]any
	StepInputs  map[string]any
	FinalStepID string
	FinalOutput any
}

// AppInvocation describes one app operation call a step makes.
type AppInvocation struct {
	App             string
	Operation       string
	Params          map[string]any
	Connection      string
	Instance        string
	CredentialMode  string
	IdempotencyKey  string
	WorkflowContext map[string]any
	Request         gestalt.Request
}

// AppResult is the raw result of one app operation call.
type AppResult struct {
	Status  int
	Body    string
	Headers map[string][]string
}

// AppInvoker performs app operation calls for steps.
type AppInvoker interface {
	InvokeWorkflowApp(context.Context, AppInvocation) (*AppResult, error)
}

// AgentClient performs the agent session and turn calls agent steps make.
type AgentClient interface {
	CreateSession(context.Context, gestalt.AgentCreateSession) (*gestalt.AgentSession, error)
	CreateTurn(context.Context, gestalt.AgentCreateTurn) (*gestalt.AgentTurn, error)
	GetTurn(context.Context, gestalt.AgentGetTurn) (*gestalt.AgentTurn, error)
	CancelTurn(context.Context, gestalt.AgentCancelTurn) (*gestalt.AgentTurn, error)
}

// Config wires an executor's step dependencies.
type Config struct {
	AppInvoker        AppInvoker
	NewAgent          func(gestalt.Request) (AgentClient, error)
	AgentPollInterval time.Duration
}

// Executor serves workflow callback requests by running their steps.
type Executor struct {
	appInvoker        AppInvoker
	newAgent          func(gestalt.Request) (AgentClient, error)
	agentPollInterval time.Duration
}

// New builds an executor from its configuration.
func New(cfg Config) *Executor {
	appInvoker := cfg.AppInvoker
	if appInvoker == nil {
		appInvoker = generatedAppInvoker{}
	}
	newAgent := cfg.NewAgent
	if newAgent == nil {
		newAgent = newGeneratedAgentClient
	}
	poll := cfg.AgentPollInterval
	if poll <= 0 {
		poll = time.Second
	}
	return &Executor{appInvoker: appInvoker, newAgent: newAgent, agentPollInterval: poll}
}

// Close releases the executor's pooled clients.
func (e *Executor) Close() error { return nil }

// Execute runs the workflow callback request to completion.
func (e *Executor) Execute(ctx context.Context, req Request) (*Response, error) {
	if e == nil {
		return nil, fmt.Errorf("workflow executor is not configured")
	}
	if req.Target == nil || len(req.Target.Steps) == 0 {
		return nil, fmt.Errorf("workflow target steps are required")
	}
	result := workflowStepsResult{
		Version: 1,
		Status:  "succeeded",
		Steps:   make([]workflowStepResult, 0, len(req.Target.Steps)),
		Outputs: map[string]any{},
	}
	skipped := map[string]struct{}{}
	stepInputs := map[string]any{}
	for i := range req.Target.Steps {
		stepResp, err := e.ExecuteStep(ctx, StepRequest{
			Request:        req,
			StepIndex:      i,
			Outputs:        result.Outputs,
			StepInputs:     stepInputs,
			SkippedStepIDs: skippedStepIDs(skipped),
		})
		if err != nil {
			return nil, err
		}
		result.Outputs = stepResp.Outputs
		if result.Outputs == nil {
			result.Outputs = map[string]any{}
		}
		stepInputs = stepResp.StepInputs
		if stepInputs == nil {
			stepInputs = map[string]any{}
		}
		stepID := strings.TrimSpace(stepResp.Step.ID)
		result.Steps = append(result.Steps, stepResp.Step)
		switch strings.ToLower(strings.TrimSpace(stepResp.Step.Status)) {
		case "skipped":
			skipped[stepID] = struct{}{}
		case "succeeded":
			result.FinalStepID = stepResp.FinalStepID
			result.FinalOutput = stepResp.FinalOutput
		case "failed":
			result.Status = "failed"
			result.Error = stepResp.Step.Error
			body, err := json.Marshal(result)
			if err != nil {
				return nil, err
			}
			status := stepResp.Status
			if status == 0 {
				status = http.StatusInternalServerError
			}
			return &Response{Status: status, Body: string(body)}, nil
		default:
			return workflowFailedStepResponse(result, stepID, "invalid_step_result", "workflow step returned invalid status")
		}
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &Response{Status: http.StatusOK, Body: string(body)}, nil
}

// ExecuteStep runs a single step invocation.
func (e *Executor) ExecuteStep(ctx context.Context, stepReq StepRequest) (*StepResponse, error) {
	if e == nil {
		return nil, fmt.Errorf("workflow executor is not configured")
	}
	req := stepReq.Request
	if req.Target == nil || len(req.Target.Steps) == 0 {
		return nil, fmt.Errorf("workflow target steps are required")
	}
	if stepReq.StepIndex < 0 || stepReq.StepIndex >= len(req.Target.Steps) {
		return nil, fmt.Errorf("workflow step index %d is out of range", stepReq.StepIndex)
	}
	outputs := cloneWorkflowMap(stepReq.Outputs)
	if outputs == nil {
		outputs = map[string]any{}
	}
	stepInputs := cloneWorkflowMap(stepReq.StepInputs)
	if stepInputs == nil {
		stepInputs = map[string]any{}
	}
	skipped := skippedStepIDMap(stepReq.SkippedStepIDs)
	step := req.Target.Steps[stepReq.StepIndex]
	stepID := strings.TrimSpace(step.ID)
	if stepID == "" {
		return failedStepResponse(stepID, "invalid_step", "workflow step id is required", "", nil, outputs, stepInputs), nil
	}
	runStep, skipReason, err := workflowStepWhenMatches(step.When, req, outputs, skipped)
	if err != nil {
		return failedStepResponse(stepID, "invalid_when", err.Error(), "", nil, outputs, stepInputs), nil
	}
	if !runStep {
		return &StepResponse{
			Status:     http.StatusOK,
			Step:       workflowStepResult{ID: stepID, Status: "skipped", SkippedReason: skipReason},
			Outputs:    outputs,
			StepInputs: stepInputs,
		}, nil
	}
	inputs, err := (EvalContext{Request: req, Outputs: outputs, StepInputs: stepInputs}).EvaluateStepInputs(step.Inputs)
	if err != nil {
		return failedStepResponse(stepID, "invalid_input", WorkflowEvalError(err).Error(), "", nil, outputs, stepInputs), nil
	}
	stepInputs[stepID] = inputs
	sessions, err := workflowAgentSessionsFromOutputs(req.Target, stepReq.StepIndex, outputs)
	if err != nil {
		return failedStepResponse(stepID, "invalid_agent_session", err.Error(), "", inputs, outputs, stepInputs), nil
	}
	stepCtx := ctx
	cancelStep := func() {}
	if step.TimeoutSeconds > 0 {
		stepCtx, cancelStep = context.WithTimeout(ctx, time.Duration(step.TimeoutSeconds)*time.Second)
	}
	scope := WorkflowStepInvocationScope(req)
	var output any
	var turnID string
	switch {
	case step.App != nil && step.Agent != nil:
		cancelStep()
		return failedStepResponse(stepID, "invalid_step", "workflow step cannot set both app and agent", "", inputs, outputs, stepInputs), nil
	case step.App != nil:
		output, err = e.invokeAppStep(stepCtx, req, stepReq.StepIndex, step.App, inputs, outputs, stepInputs, scope, stepID)
	case step.Agent != nil:
		output, turnID, err = e.invokeAgentStep(stepCtx, req, stepReq.StepIndex, step.Agent, inputs, outputs, stepInputs, sessions, scope, stepID, step.TimeoutSeconds, step.Metadata)
	default:
		err = fmt.Errorf("workflow step must set app or agent")
	}
	cancelStep()
	if err != nil {
		return failedStepResponse(stepID, "step_failed", err.Error(), turnID, inputs, outputs, stepInputs), nil
	}
	outputs[stepID] = output
	return &StepResponse{
		Status:      http.StatusOK,
		Step:        workflowStepResult{ID: stepID, Status: "succeeded", TurnID: turnID},
		Input:       inputs,
		Output:      output,
		Outputs:     outputs,
		StepInputs:  stepInputs,
		FinalStepID: stepID,
		FinalOutput: output,
	}, nil
}

func failedStepResponse(stepID, code, message, turnID string, input map[string]any, outputs map[string]any, stepInputs map[string]any) *StepResponse {
	errValue := &workflowStepError{StepID: stepID, Code: code, Message: message}
	return &StepResponse{
		Status:     http.StatusInternalServerError,
		Step:       workflowStepResult{ID: stepID, Status: "failed", TurnID: turnID, Error: errValue},
		Input:      input,
		Outputs:    outputs,
		StepInputs: stepInputs,
	}
}

func skippedStepIDs(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	return out
}

func skippedStepIDMap(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func cloneWorkflowMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func workflowAgentSessionsFromOutputs(target *gestalt.BoundWorkflowTarget, stepIndex int, outputs map[string]any) (map[string]workflowAgentSessionState, error) {
	sessions := map[string]workflowAgentSessionState{}
	if target == nil || stepIndex <= 0 {
		return sessions, nil
	}
	if stepIndex > len(target.Steps) {
		stepIndex = len(target.Steps)
	}
	for i := 0; i < stepIndex; i++ {
		step := target.Steps[i]
		if step.Agent == nil {
			continue
		}
		stepID := strings.TrimSpace(step.ID)
		output, ok := outputs[stepID]
		if !ok {
			continue
		}
		sessionID, ok := workflowAgentSessionIDFromOutput(output)
		if !ok {
			return nil, fmt.Errorf("workflow agent step %q output is missing agent.sessionId", stepID)
		}
		sessionKey := strings.TrimSpace(step.Agent.SessionKey)
		if sessionKey == "" {
			sessionKey = stepID
		}
		providerName := strings.TrimSpace(step.Agent.Provider)
		model := strings.TrimSpace(step.Agent.Model)
		optionsKey := workflowAgentSessionOptionsKey(step.Agent)
		state, exists := sessions[sessionKey]
		if exists {
			if state.providerName != providerName || state.model != model || state.options != optionsKey {
				return nil, fmt.Errorf("workflow agent session_key %q uses incompatible provider, model, or model_options", sessionKey)
			}
			continue
		}
		sessions[sessionKey] = workflowAgentSessionState{
			session:      &gestalt.AgentSession{ID: sessionID, ProviderName: providerName, Model: model},
			providerName: providerName,
			model:        model,
			options:      optionsKey,
		}
	}
	return sessions, nil
}

func workflowAgentSessionIDFromOutput(output any) (string, bool) {
	object, ok := output.(map[string]any)
	if !ok {
		return "", false
	}
	kind, _ := object["kind"].(string)
	if strings.TrimSpace(kind) != "agent" {
		return "", false
	}
	agent, ok := object["agent"].(map[string]any)
	if !ok {
		return "", false
	}
	sessionID, ok := agent["sessionId"].(string)
	sessionID = strings.TrimSpace(sessionID)
	return sessionID, ok && sessionID != ""
}

// WorkflowEvalError wraps a value-evaluation failure with its step context.
func WorkflowEvalError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrInvalidValue) {
		return fmt.Errorf("invalid workflow invocation: %v", err)
	}
	return err
}
