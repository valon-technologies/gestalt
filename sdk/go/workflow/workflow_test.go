package workflow

import (
	"context"
	"encoding/json"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func TestEvaluateWorkflowTemplateAndPaths(t *testing.T) {
	t.Parallel()

	req := Request{
		ProviderName: "indexeddb",
		RunID:        "run-1",
		Input:        map[string]any{"customer": map[string]any{"id": "cust_1"}},
		Signals: []gestalt.WorkflowSignal{{
			ID:      "sig-1",
			Payload: map[string]any{"thread": map[string]any{"ts": "123.456"}},
		}},
	}
	ctx := EvalContext{
		Request: req,
		Outputs: map[string]any{
			"collect": map[string]any{"body": map[string]any{"summary": "context"}},
		},
		StepInputs: map[string]any{
			"collect": map[string]any{"thread": "123.456"},
		},
	}
	rendered, err := ctx.RenderTemplate("customer=${{ input.customer.id }}; thread=${{ signal.thread.ts }}; step_input=${{ steps.collect.inputs.thread }}; step_output=${{ steps.collect.outputs.body.summary }}; literal=$${{ x }}")
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if want := "customer=cust_1; thread=123.456; step_input=123.456; step_output=context; literal=${{ x }}"; rendered != want {
		t.Fatalf("rendered = %q, want %q", rendered, want)
	}
	value, ok, err := ctx.EvaluateValue(gestalt.WorkflowValue{Input: "customer.id"})
	if err != nil {
		t.Fatalf("EvaluateValue: %v", err)
	}
	if !ok || value != "cust_1" {
		t.Fatalf("value = %#v, %v; want cust_1, true", value, ok)
	}
}

func TestExecutorInvokesAppStep(t *testing.T) {
	t.Parallel()

	invoker := &recordingAppInvoker{}
	executor := New(Config{AppInvoker: invoker})
	resp, err := executor.Execute(context.Background(), Request{
		ProviderName:         "indexeddb",
		RunID:                "run-1",
		DefinitionID:         "definition-1",
		DefinitionGeneration: 1,
		WorkflowKey:          "customer:ada",
		RunAs: &gestalt.Subject{
			ID:                  "service_account:workflow-runner",
			CredentialSubjectID: "service_account:workflow-runner",
		},
		Target: &gestalt.BoundWorkflowTarget{Steps: []gestalt.WorkflowStep{{
			ID: "send",
			App: &gestalt.WorkflowStepAppCall{
				Name:      "slack",
				Operation: "chat.postMessage",
				Input: gestalt.WorkflowValue{Object: map[string]gestalt.WorkflowValue{
					"text": {Template: &gestalt.WorkflowText{Template: "hello ${{ input.name }}"}},
				}},
			},
		}}},
		Input: map[string]any{"name": "Ada"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Status != 200 {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
	if invoker.call.App != "slack" || invoker.call.Operation != "chat.postMessage" {
		t.Fatalf("call = %#v", invoker.call)
	}
	if invoker.call.Params["text"] != "hello Ada" || invoker.call.Params["name"] != "Ada" {
		t.Fatalf("params = %#v", invoker.call.Params)
	}
	if invoker.call.Request.Caller.Kind != gestalt.RequestCallerKindWorkflow || invoker.call.Request.Caller.Name != "indexeddb" {
		t.Fatalf("request caller = %#v, want indexeddb workflow", invoker.call.Request.Caller)
	}
	if invoker.call.Request.Subject.ID != "service_account:workflow-runner" {
		t.Fatalf("request subject = %#v, want workflow runner", invoker.call.Request.Subject)
	}
	if invoker.call.Request.WorkflowContext["definitionId"] != "definition-1" || invoker.call.Request.WorkflowContext["workflowKey"] != "customer:ada" {
		t.Fatalf("request workflow context = %#v", invoker.call.Request.WorkflowContext)
	}
	currentStep := invoker.call.Request.WorkflowContext["currentStep"].(map[string]any)
	if currentStep["id"] != "send" || currentStep["index"] != float64(0) {
		t.Fatalf("current step = %#v, want send/0", currentStep)
	}
	runAs := invoker.call.WorkflowContext["runAs"].(map[string]any)
	if runAs["id"] != "service_account:workflow-runner" {
		t.Fatalf("workflow runAs = %#v", runAs)
	}
	var body struct {
		Status string         `json:"status"`
		Steps  []StepResult   `json:"steps"`
		Output map[string]any `json:"finalOutput"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &body); err != nil {
		t.Fatalf("response body: %v", err)
	}
	if body.Status != "succeeded" || len(body.Steps) != 1 || body.Steps[0].Status != "succeeded" {
		t.Fatalf("body = %#v", body)
	}
}

func TestExecutorExecuteStepUsesPriorOutput(t *testing.T) {
	t.Parallel()

	invoker := &recordingAppInvoker{}
	executor := New(Config{AppInvoker: invoker})
	resp, err := executor.ExecuteStep(context.Background(), StepRequest{
		Request: Request{
			ProviderName:         "indexeddb",
			RunID:                "run-1",
			DefinitionID:         "definition-1",
			DefinitionGeneration: 1,
			RunAs: &gestalt.Subject{
				ID:                  "service_account:workflow-runner",
				CredentialSubjectID: "service_account:workflow-runner",
			},
			Target: &gestalt.BoundWorkflowTarget{Steps: []gestalt.WorkflowStep{
				{
					ID: "collect",
					App: &gestalt.WorkflowStepAppCall{
						Name:      "github",
						Operation: "pullRequests.get",
					},
				},
				{
					ID: "notify",
					Inputs: map[string]gestalt.WorkflowValue{
						"summary": {StepOutput: &gestalt.WorkflowStepOutputSource{StepID: "collect", Path: "summary"}},
					},
					App: &gestalt.WorkflowStepAppCall{
						Name:      "slack",
						Operation: "chat.postMessage",
						Input: gestalt.WorkflowValue{Object: map[string]gestalt.WorkflowValue{
							"text": {Template: &gestalt.WorkflowText{Template: "summary=${{ steps.collect.outputs.summary }}"}},
						}},
					},
				},
			}},
		},
		StepIndex: 1,
		Outputs: map[string]any{
			"collect": map[string]any{"summary": "ready"},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteStep: %v", err)
	}
	if resp.Status != 200 || resp.Step.ID != "notify" || resp.Step.Status != "succeeded" {
		t.Fatalf("response = %#v", resp)
	}
	if invoker.call.Params["text"] != "summary=ready" {
		t.Fatalf("params = %#v", invoker.call.Params)
	}
	if resp.StepInputs["notify"].(map[string]any)["summary"] != "ready" {
		t.Fatalf("step inputs = %#v", resp.StepInputs)
	}
	if _, ok := resp.Outputs["collect"]; !ok {
		t.Fatalf("prior output was not preserved: %#v", resp.Outputs)
	}
	if _, ok := resp.Outputs["notify"]; !ok {
		t.Fatalf("current output was not recorded: %#v", resp.Outputs)
	}
}

func TestExecutorExecuteStepSkipsMissingDependency(t *testing.T) {
	t.Parallel()

	invoker := &recordingAppInvoker{}
	executor := New(Config{AppInvoker: invoker})
	resp, err := executor.ExecuteStep(context.Background(), StepRequest{
		Request: Request{
			ProviderName: "indexeddb",
			RunID:        "run-1",
			Target: &gestalt.BoundWorkflowTarget{Steps: []gestalt.WorkflowStep{
				{
					ID: "collect",
					App: &gestalt.WorkflowStepAppCall{
						Name:      "github",
						Operation: "pullRequests.get",
					},
				},
				{
					ID: "notify",
					When: &gestalt.WorkflowStepWhen{
						Value:  gestalt.WorkflowValue{StepOutput: &gestalt.WorkflowStepOutputSource{StepID: "collect", Path: "ok"}},
						Equals: true,
					},
					App: &gestalt.WorkflowStepAppCall{
						Name:      "slack",
						Operation: "chat.postMessage",
					},
				},
			}},
		},
		StepIndex:      1,
		SkippedStepIDs: []string{"collect"},
	})
	if err != nil {
		t.Fatalf("ExecuteStep: %v", err)
	}
	if resp.Status != 200 || resp.Step.ID != "notify" || resp.Step.Status != "skipped" || resp.Step.SkippedReason != "missing_dependency" {
		t.Fatalf("response = %#v", resp)
	}
	if invoker.call.App != "" {
		t.Fatalf("app was invoked: %#v", invoker.call)
	}
}

func TestExecutorInvokesAgentStepWithWorkflowRunAs(t *testing.T) {
	t.Parallel()

	agent := &recordingAgentClient{}
	executor := New(Config{
		NewAgent: func(req gestalt.Request) (AgentClient, error) {
			agent.request = req
			return agent, nil
		},
		AgentPollInterval: 0,
	})
	resp, err := executor.Execute(context.Background(), Request{
		ProviderName:         "indexeddb",
		RunID:                "run-1",
		DefinitionID:         "definition-1",
		DefinitionGeneration: 1,
		RunAs: &gestalt.Subject{
			ID:                  "service_account:workflow-runner",
			CredentialSubjectID: "service_account:workflow-runner",
		},
		Target: &gestalt.BoundWorkflowTarget{Steps: []gestalt.WorkflowStep{{
			ID:             "review",
			TimeoutSeconds: 45,
			Agent: &gestalt.WorkflowStepAgentTurn{
				Provider: "claude",
				Model:    "default",
				Prompt:   gestalt.WorkflowText{Template: "review ${{ input.name }}"},
			},
		}}},
		Input: map[string]any{"name": "Ada"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Status != 200 {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
	if agent.session.ProviderName != "claude" || agent.session.Model != "default" {
		t.Fatalf("session = %#v", agent.session)
	}
	if agent.turn.SessionID != "session-1" || agent.turn.Messages[0].Text != "review Ada" {
		t.Fatalf("turn = %#v", agent.turn)
	}
	if agent.turn.TimeoutSeconds != 45 {
		t.Fatalf("turn timeout = %d, want 45", agent.turn.TimeoutSeconds)
	}
	runAs := agent.turnWorkflow["runAs"].(map[string]any)
	if runAs["id"] != "service_account:workflow-runner" {
		t.Fatalf("workflow runAs = %#v", runAs)
	}
	if agent.getTurnWorkflow["runId"] != "run-1" {
		t.Fatalf("get turn workflow context = %#v", agent.getTurnWorkflow)
	}
	if agent.request.Caller.Kind != gestalt.RequestCallerKindWorkflow || agent.request.Caller.Name != "indexeddb" {
		t.Fatalf("agent request caller = %#v, want indexeddb workflow", agent.request.Caller)
	}
	if agent.request.WorkflowContext["currentStepId"] != "review" {
		t.Fatalf("agent request workflow context = %#v, want currentStepId=review", agent.request.WorkflowContext)
	}
}

type recordingAppInvoker struct {
	call AppInvocation
}

func (i *recordingAppInvoker) InvokeWorkflowApp(_ context.Context, call AppInvocation) (*AppResult, error) {
	i.call = call
	return &AppResult{Status: 200, Body: `{"ok":true}`}, nil
}

type recordingAgentClient struct {
	request         gestalt.Request
	session         gestalt.AgentCreateSession
	sessionWorkflow map[string]any
	turn            gestalt.AgentCreateTurn
	turnWorkflow    map[string]any
	getTurn         gestalt.AgentGetTurn
	getTurnWorkflow map[string]any
}

func (c *recordingAgentClient) CreateSession(ctx context.Context, input gestalt.AgentCreateSession) (*gestalt.AgentSession, error) {
	c.session = input
	c.sessionWorkflow = gestalt.WorkflowContextFromContext(ctx)
	return &gestalt.AgentSession{ID: "session-1", ProviderName: input.ProviderName, Model: input.Model}, nil
}

func (c *recordingAgentClient) CreateTurn(ctx context.Context, input gestalt.AgentCreateTurn) (*gestalt.AgentTurn, error) {
	c.turn = input
	c.turnWorkflow = gestalt.WorkflowContextFromContext(ctx)
	return &gestalt.AgentTurn{ID: "turn-1", SessionID: input.SessionID, Status: gestalt.AgentExecutionStatusRunning}, nil
}

func (c *recordingAgentClient) GetTurn(ctx context.Context, input gestalt.AgentGetTurn) (*gestalt.AgentTurn, error) {
	c.getTurn = input
	c.getTurnWorkflow = gestalt.WorkflowContextFromContext(ctx)
	return &gestalt.AgentTurn{ID: input.TurnID, SessionID: "session-1", Status: gestalt.AgentExecutionStatusSucceeded}, nil
}

func (c *recordingAgentClient) CancelTurn(context.Context, gestalt.AgentCancelTurn) (*gestalt.AgentTurn, error) {
	return nil, nil
}

func TestExecutorRejectsSessionReuseWithDifferentWorkspace(t *testing.T) {
	t.Parallel()

	agent := &recordingAgentClient{}
	executor := New(Config{
		NewAgent: func(req gestalt.Request) (AgentClient, error) {
			return agent, nil
		},
		AgentPollInterval: 0,
	})
	baseWorkspace := &gestalt.AgentWorkspace{
		Checkouts: []gestalt.AgentWorkspaceGitCheckout{{
			URL:  "https://github.com/valon-technologies/toolshed.git",
			Ref:  "main",
			Path: "toolshed",
		}},
		CWD: "toolshed",
	}
	otherWorkspace := &gestalt.AgentWorkspace{
		Checkouts: []gestalt.AgentWorkspaceGitCheckout{{
			URL:  "https://github.com/valon-technologies/gestalt.git",
			Ref:  "main",
			Path: "gestalt",
		}},
		CWD: "gestalt",
	}
	target := &gestalt.BoundWorkflowTarget{Steps: []gestalt.WorkflowStep{
		{
			ID: "first",
			Agent: &gestalt.WorkflowStepAgentTurn{
				Provider:   "claude",
				Model:      "default",
				SessionKey: "shared",
				Prompt:     gestalt.WorkflowText{Template: "first"},
				Workspace:  baseWorkspace,
			},
		},
		{
			ID: "second",
			Agent: &gestalt.WorkflowStepAgentTurn{
				Provider:   "claude",
				Model:      "default",
				SessionKey: "shared",
				Prompt:     gestalt.WorkflowText{Template: "second"},
				Workspace:  otherWorkspace,
			},
		},
	}}
	resp, err := executor.Execute(context.Background(), Request{
		ProviderName:         "indexeddb",
		RunID:                "run-1",
		DefinitionID:         "definition-1",
		DefinitionGeneration: 1,
		RunAs: &gestalt.Subject{
			ID:                  "service_account:workflow-runner",
		},
		Target: target,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Status == 200 {
		t.Fatalf("expected incompatible workspace failure, got status 200 body = %s", resp.Body)
	}
}
