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

type recordingAppInvoker struct {
	call AppInvocation
}

func (i *recordingAppInvoker) InvokeWorkflowApp(_ context.Context, call AppInvocation) (*AppResult, error) {
	i.call = call
	return &AppResult{Status: 200, Body: `{"ok":true}`}, nil
}
