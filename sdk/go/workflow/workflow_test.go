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
		Request:     req,
		Inputs:      map[string]any{"thread": "123.456"},
		AllowInputs: true,
	}
	rendered, err := ctx.RenderTemplate("customer=${runInput.customer.id}; thread=${signalPayload.thread.ts}; input=${inputs.thread}; literal=$${x}")
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if want := "customer=cust_1; thread=123.456; input=123.456; literal=${x}"; rendered != want {
		t.Fatalf("rendered = %q, want %q", rendered, want)
	}
	value, ok, err := ctx.EvaluateValue(gestalt.WorkflowValue{RunInput: "customer.id"})
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
		ProviderName:    "indexeddb",
		RunID:           "run-1",
		InvocationToken: "token",
		Target: &gestalt.BoundWorkflowTarget{Steps: []gestalt.WorkflowStep{{
			ID: "send",
			App: &gestalt.WorkflowStepAppCall{
				Name:      "slack",
				Operation: "chat.postMessage",
				Input: gestalt.WorkflowValue{Object: map[string]gestalt.WorkflowValue{
					"text": {Template: &gestalt.WorkflowText{Template: "hello ${runInput.name}"}},
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

func TestExecutorRendersSignalPayloadIntoAgentTurnMessages(t *testing.T) {
	t.Parallel()

	agent := &recordingAgentClient{}
	var agentToken string
	executor := New(Config{
		NewAgent: func(token string) (AgentClient, error) {
			agentToken = token
			return agent, nil
		},
	})
	resp, err := executor.Execute(context.Background(), Request{
		ProviderName:    "workflow",
		RunID:           "run-1",
		InvocationToken: "token",
		Signals: []gestalt.WorkflowSignal{{
			ID:      "sig-1",
			Payload: map[string]any{"user_prompt": "please triage this Slack request"},
		}},
		Target: &gestalt.BoundWorkflowTarget{Steps: []gestalt.WorkflowStep{{
			ID: "agent",
			Agent: &gestalt.WorkflowStepAgentTurn{
				Provider: "managed",
				Model:    "deep",
				Messages: []gestalt.WorkflowAgentMessage{{
					Role: "system",
					Text: gestalt.WorkflowText{Template: "Handle the request from ${signalPayload.user_prompt}"},
				}},
				Prompt: gestalt.WorkflowText{Template: "Current request:\n${signalPayload.user_prompt}"},
			},
		}}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Status != 200 {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
	if agentToken != "token" {
		t.Fatalf("agent token = %q, want token", agentToken)
	}
	if got := agent.turn.Messages; len(got) != 2 {
		t.Fatalf("turn messages = %#v, want 2 messages", got)
	} else {
		if got[0].Role != "system" || got[0].Text != "Handle the request from please triage this Slack request" {
			t.Fatalf("system message = %#v", got[0])
		}
		if got[1].Role != "user" || got[1].Text != "Current request:\nplease triage this Slack request" {
			t.Fatalf("user message = %#v", got[1])
		}
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
	session gestalt.AgentCreateSession
	turn    gestalt.AgentCreateTurn
}

func (c *recordingAgentClient) CreateSession(_ context.Context, req gestalt.AgentCreateSession) (*gestalt.AgentSession, error) {
	c.session = req
	return &gestalt.AgentSession{ID: "session-1", ProviderName: req.ProviderName, Model: req.Model}, nil
}

func (c *recordingAgentClient) CreateTurn(_ context.Context, req gestalt.AgentCreateTurn) (*gestalt.AgentTurn, error) {
	c.turn = req
	return &gestalt.AgentTurn{
		ID:         "turn-1",
		SessionID:  req.SessionID,
		Model:      req.Model,
		Status:     gestalt.AgentExecutionStatusSucceeded,
		OutputText: "done",
	}, nil
}

func (c *recordingAgentClient) GetTurn(_ context.Context, req gestalt.AgentGetTurn) (*gestalt.AgentTurn, error) {
	return &gestalt.AgentTurn{ID: req.TurnID, Status: gestalt.AgentExecutionStatusSucceeded}, nil
}

func (c *recordingAgentClient) CancelTurn(_ context.Context, req gestalt.AgentCancelTurn) (*gestalt.AgentTurn, error) {
	return &gestalt.AgentTurn{ID: req.TurnID, Status: gestalt.AgentExecutionStatusCanceled}, nil
}
