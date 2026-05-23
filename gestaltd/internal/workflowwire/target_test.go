package workflowwire

import (
	"errors"
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

func TestParseTargetMapRoundTrip(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"steps": []any{
			map[string]any{
				"id": "run",
				"app": map[string]any{
					"name":      "github",
					"operation": "createIssue",
					"input": map[string]any{
						"literal": map[string]any{"title": "hello"},
					},
				},
			},
		},
	}

	target, err := ParseTargetMap(raw, "target")
	if err != nil {
		t.Fatalf("ParseTargetMap() error = %v", err)
	}
	if len(target.Steps) != 1 || target.Steps[0].App == nil {
		t.Fatalf("ParseTargetMap() = %#v, want one app step", target)
	}

	encoded := EncodeTargetMap(target)
	again, err := ParseTargetMap(encoded, "target")
	if err != nil {
		t.Fatalf("ParseTargetMap(round-trip) error = %v", err)
	}
	if !coreworkflow.TargetsEqual(target, again) {
		t.Fatalf("round-trip target = %#v, want %#v", again, target)
	}
}

func TestParseStepsRejectsForwardStepOutputRef(t *testing.T) {
	t.Parallel()

	_, err := ParseSteps([]any{
		map[string]any{
			"id": "first",
			"app": map[string]any{
				"name":      "github",
				"operation": "createIssue",
				"input": map[string]any{
					"object": map[string]any{
						"title": map[string]any{
							"stepOutput": map[string]any{
								"stepId": "second",
								"path":   "app.body.text",
							},
						},
					},
				},
			},
		},
		map[string]any{
			"id": "second",
			"app": map[string]any{
				"name":      "slack",
				"operation": "reply",
			},
		},
	}, "target.steps")
	if err == nil {
		t.Fatal("ParseSteps() succeeded, want error")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("ParseSteps() error = %v, want ErrInvalid", err)
	}
}

func TestParseStepWhenRequiresValue(t *testing.T) {
	t.Parallel()

	_, err := ParseStepWhen(map[string]any{"equals": "ready"}, "target.steps[0].when")
	if err == nil {
		t.Fatal("ParseStepWhen() succeeded, want error")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("ParseStepWhen() error = %v, want ErrInvalid", err)
	}
}

func TestParseTargetMapClonesObjectArgs(t *testing.T) {
	t.Parallel()

	stepMetadata := map[string]any{"nested": map[string]any{"value": "before"}}
	responseSchema := map[string]any{"type": "object"}
	modelOptions := map[string]any{"temperature": 0.2}
	messageMetadata := map[string]any{"source": "before"}
	raw := map[string]any{
		"steps": []any{
			map[string]any{
				"id":       "agent",
				"metadata": stepMetadata,
				"agent": map[string]any{
					"messages": []any{
						map[string]any{
							"role":     "user",
							"text":     "hello",
							"metadata": messageMetadata,
						},
					},
					"responseSchema": responseSchema,
					"modelOptions":   modelOptions,
				},
			},
		},
	}

	target, err := ParseTargetMap(raw, "target")
	if err != nil {
		t.Fatalf("ParseTargetMap() error = %v", err)
	}
	stepMetadata["added"] = true
	stepMetadata["nested"].(map[string]any)["value"] = "after"
	responseSchema["type"] = "array"
	modelOptions["temperature"] = 1.0
	messageMetadata["source"] = "after"

	step := target.Steps[0]
	if _, ok := step.Metadata["added"]; ok {
		t.Fatalf("step metadata aliases input map: %#v", step.Metadata)
	}
	if nested := step.Metadata["nested"].(map[string]any); nested["value"] != "before" {
		t.Fatalf("step metadata nested value = %#v, want before", nested["value"])
	}
	if step.Agent.ResponseSchema["type"] != "object" {
		t.Fatalf("response schema = %#v, want original object type", step.Agent.ResponseSchema)
	}
	if step.Agent.ModelOptions["temperature"] != 0.2 {
		t.Fatalf("model options = %#v, want original temperature", step.Agent.ModelOptions)
	}
	if step.Agent.Messages[0].Metadata["source"] != "before" {
		t.Fatalf("message metadata = %#v, want original source", step.Agent.Messages[0].Metadata)
	}
}
