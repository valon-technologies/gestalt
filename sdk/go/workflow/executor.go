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

type Request struct {
	ProviderName    string
	RunID           string
	Target          *gestalt.BoundWorkflowTarget
	Trigger         *gestalt.WorkflowRunTrigger
	Input           map[string]any
	Metadata        map[string]any
	CreatedBy       *gestalt.WorkflowActor
	ExecutionRef    string
	InvocationToken string
	Signals         []gestalt.WorkflowSignal
}

type Response struct {
	Status int
	Body   string
}

type StepExecutor interface {
	Execute(context.Context, Request) (*Response, error)
	Close() error
}

type AppInvocation struct {
	Token           string
	App             string
	Operation       string
	Params          map[string]any
	Connection      string
	Instance        string
	CredentialMode  string
	IdempotencyKey  string
	WorkflowContext map[string]any
}

type AppResult struct {
	Status  int
	Body    string
	Headers map[string][]string
}

type AppInvoker interface {
	InvokeWorkflowApp(context.Context, AppInvocation) (*AppResult, error)
}

type AgentClient interface {
	CreateSession(context.Context, gestalt.AgentCreateSession) (*gestalt.AgentSession, error)
	CreateTurn(context.Context, gestalt.AgentCreateTurn) (*gestalt.AgentTurn, error)
	GetTurn(context.Context, gestalt.AgentGetTurn) (*gestalt.AgentTurn, error)
	CancelTurn(context.Context, gestalt.AgentCancelTurn) (*gestalt.AgentTurn, error)
}

type Config struct {
	AppInvoker        AppInvoker
	NewApp            func(string) (gestalt.App, error)
	NewAgent          func(string) (AgentClient, error)
	AgentPollInterval time.Duration
}

type Executor struct {
	appInvoker        AppInvoker
	newAgent          func(string) (AgentClient, error)
	agentPollInterval time.Duration
}

func New(cfg Config) *Executor {
	appInvoker := cfg.AppInvoker
	if appInvoker == nil {
		newApp := cfg.NewApp
		if newApp == nil {
			newApp = gestalt.NewApp
		}
		appInvoker = appClientInvoker{newApp: newApp}
	}
	newAgent := cfg.NewAgent
	if newAgent == nil {
		newAgent = func(token string) (AgentClient, error) { return gestalt.NewAgent(token) }
	}
	poll := cfg.AgentPollInterval
	if poll <= 0 {
		poll = time.Second
	}
	return &Executor{appInvoker: appInvoker, newAgent: newAgent, agentPollInterval: poll}
}

func (e *Executor) Close() error { return nil }

func (e *Executor) Execute(ctx context.Context, req Request) (*Response, error) {
	if e == nil {
		return nil, fmt.Errorf("workflow executor is not configured")
	}
	if req.Target == nil || len(req.Target.Steps) == 0 {
		return nil, fmt.Errorf("workflow target steps are required")
	}
	token := strings.TrimSpace(req.InvocationToken)
	if token == "" {
		return nil, fmt.Errorf("workflow invocation token is required")
	}
	result := workflowStepsResult{
		Version: 1,
		Status:  "succeeded",
		Steps:   make([]workflowStepResult, 0, len(req.Target.Steps)),
		Outputs: map[string]any{},
	}
	skipped := map[string]struct{}{}
	sessions := map[string]workflowAgentSessionState{}
	scope := WorkflowStepInvocationScope(req)
	for i := range req.Target.Steps {
		step := req.Target.Steps[i]
		stepID := strings.TrimSpace(step.ID)
		if stepID == "" {
			return workflowFailedStepResponse(result, stepID, "invalid_step", "workflow step id is required")
		}
		runStep, skipReason, err := workflowStepWhenMatches(step.When, req, result.Outputs, skipped)
		if err != nil {
			return workflowFailedStepResponse(result, stepID, "invalid_when", err.Error())
		}
		if !runStep {
			result.Steps = append(result.Steps, workflowStepResult{ID: stepID, Status: "skipped", SkippedReason: skipReason})
			skipped[stepID] = struct{}{}
			continue
		}
		inputs, err := (EvalContext{Request: req, Outputs: result.Outputs}).EvaluateStepInputs(step.Inputs)
		if err != nil {
			return workflowFailedStepResponse(result, stepID, "invalid_input", WorkflowEvalError(err).Error())
		}
		stepCtx := ctx
		cancelStep := func() {}
		if step.TimeoutSeconds > 0 {
			stepCtx, cancelStep = context.WithTimeout(ctx, time.Duration(step.TimeoutSeconds)*time.Second)
		}
		var output any
		var turnID string
		switch {
		case step.App != nil && step.Agent != nil:
			cancelStep()
			return workflowFailedStepResponse(result, stepID, "invalid_step", "workflow step cannot set both app and agent")
		case step.App != nil:
			output, err = e.invokeAppStep(stepCtx, req, token, step.App, inputs, result.Outputs, scope, stepID)
		case step.Agent != nil:
			output, turnID, err = e.invokeAgentStep(stepCtx, req, token, step.Agent, inputs, sessions, scope, stepID, step.TimeoutSeconds, step.Metadata)
		default:
			err = fmt.Errorf("workflow step must set app or agent")
		}
		cancelStep()
		if err != nil {
			return workflowFailedStepResponse(result, stepID, "step_failed", err.Error())
		}
		result.Outputs[stepID] = output
		result.FinalStepID = stepID
		result.FinalOutput = output
		result.Steps = append(result.Steps, workflowStepResult{ID: stepID, Status: "succeeded", TurnID: turnID})
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &Response{Status: http.StatusOK, Body: string(body)}, nil
}

func WorkflowEvalError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrInvalidValue) {
		return fmt.Errorf("invalid workflow invocation: %v", err)
	}
	return err
}
