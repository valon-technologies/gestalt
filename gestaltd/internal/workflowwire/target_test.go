package workflowwire

import (
	"errors"
	"strings"
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

	_, err := parseSteps([]any{
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
		t.Fatal("parseSteps() succeeded, want error")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("parseSteps() error = %v, want ErrInvalid", err)
	}
}

func TestParseStepsRejectsEmptyStepID(t *testing.T) {
	t.Parallel()

	_, err := parseSteps([]any{
		map[string]any{
			"app": map[string]any{
				"name":      "github",
				"operation": "createIssue",
			},
		},
	}, "target.steps")
	if err == nil {
		t.Fatal("parseSteps() succeeded, want error")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("parseSteps() error = %v, want ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "target.steps[0].id is required") {
		t.Fatalf("parseSteps() error = %v, want step id required message", err)
	}
}

func TestParseStepWhenRequiresValue(t *testing.T) {
	t.Parallel()

	_, err := parseStepWhen(map[string]any{"equals": "ready"}, "target.steps[0].when")
	if err == nil {
		t.Fatal("parseStepWhen() succeeded, want error")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("parseStepWhen() error = %v, want ErrInvalid", err)
	}
}

func TestParseTargetMapClonesObjectArgs(t *testing.T) {
	t.Parallel()

	stepMetadata := map[string]any{"nested": map[string]any{"value": "before"}}
	schema := map[string]any{"type": "object"}
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
					"output": map[string]any{
						"structured": map[string]any{"schema": schema},
					},
					"modelOptions": modelOptions,
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
	schema["type"] = "array"
	modelOptions["temperature"] = 1.0
	messageMetadata["source"] = "after"

	step := target.Steps[0]
	if _, ok := step.Metadata["added"]; ok {
		t.Fatalf("step metadata aliases input map: %#v", step.Metadata)
	}
	if nested := step.Metadata["nested"].(map[string]any); nested["value"] != "before" {
		t.Fatalf("step metadata nested value = %#v, want before", nested["value"])
	}
	if step.Agent.Output.Structured.Schema["type"] != "object" {
		t.Fatalf("response schema = %#v, want original object type", step.Agent.Output.Structured.Schema)
	}
	if step.Agent.ModelOptions["temperature"] != 0.2 {
		t.Fatalf("model options = %#v, want original temperature", step.Agent.ModelOptions)
	}
	if step.Agent.Messages[0].Metadata["source"] != "before" {
		t.Fatalf("message metadata = %#v, want original source", step.Agent.Messages[0].Metadata)
	}
}

func TestParseAndEncodeTargetMapPreservesEmptyObjectArgs(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"steps": []any{
			map[string]any{
				"id":       "agent",
				"metadata": map[string]any{},
				"agent": map[string]any{
					"messages": []any{
						map[string]any{
							"role":     "user",
							"text":     "hello",
							"metadata": map[string]any{},
						},
					},
					"output": map[string]any{
						"structured": map[string]any{"schema": map[string]any{}},
					},
					"modelOptions": map[string]any{},
				},
			},
		},
	}

	target, err := ParseTargetMap(raw, "target")
	if err != nil {
		t.Fatalf("ParseTargetMap() error = %v", err)
	}
	step := target.Steps[0]
	if step.Metadata == nil || len(step.Metadata) != 0 {
		t.Fatalf("step metadata = %#v, want non-nil empty map", step.Metadata)
	}
	if step.Agent.Messages[0].Metadata == nil || len(step.Agent.Messages[0].Metadata) != 0 {
		t.Fatalf("message metadata = %#v, want non-nil empty map", step.Agent.Messages[0].Metadata)
	}
	if step.Agent.Output.Structured.Schema == nil || len(step.Agent.Output.Structured.Schema) != 0 {
		t.Fatalf("response schema = %#v, want non-nil empty map", step.Agent.Output.Structured.Schema)
	}
	if step.Agent.ModelOptions == nil || len(step.Agent.ModelOptions) != 0 {
		t.Fatalf("model options = %#v, want non-nil empty map", step.Agent.ModelOptions)
	}

	encoded := EncodeTargetMap(target)
	encodedStep := encoded["steps"].([]map[string]any)[0]
	if metadata, ok := encodedStep["metadata"].(map[string]any); !ok || len(metadata) != 0 {
		t.Fatalf("encoded step metadata = %#v, want empty object", encodedStep["metadata"])
	}
	encodedAgent := encodedStep["agent"].(map[string]any)
	encodedOutput := encodedAgent["output"].(map[string]any)
	encodedStructured := encodedOutput["structured"].(map[string]any)
	if schema, ok := encodedStructured["schema"].(map[string]any); !ok || len(schema) != 0 {
		t.Fatalf("encoded response schema = %#v, want empty object", encodedStructured["schema"])
	}
	if options, ok := encodedAgent["modelOptions"].(map[string]any); !ok || len(options) != 0 {
		t.Fatalf("encoded model options = %#v, want empty object", encodedAgent["modelOptions"])
	}
	encodedMessages := encodedAgent["messages"].([]map[string]any)
	if metadata, ok := encodedMessages[0]["metadata"].(map[string]any); !ok || len(metadata) != 0 {
		t.Fatalf("encoded message metadata = %#v, want empty object", encodedMessages[0]["metadata"])
	}
}

func TestParseTargetMapAgentWorkspaceRoundTrip(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"steps": []any{
			map[string]any{
				"id": "diagnosis",
				"agent": map[string]any{
					"provider": "claude",
					"model":    "default",
					"tools":    []any{},
					"output":   map[string]any{"text": map[string]any{}},
					"workspace": map[string]any{
						"checkouts": []any{
							map[string]any{
								"url":  "https://github.com/valon-technologies/toolshed.git",
								"ref":  "main",
								"path": "toolshed",
							},
							map[string]any{
								"url":  "https://github.com/valon-technologies/gestalt.git",
								"ref":  "main",
								"path": "gestalt",
							},
						},
						"cwd": "toolshed",
					},
				},
			},
		},
	}

	target, err := ParseTargetMap(raw, "target")
	if err != nil {
		t.Fatalf("ParseTargetMap() error = %v", err)
	}
	workspace := target.Steps[0].Agent.Workspace
	if workspace == nil || workspace.CWD != "toolshed" || len(workspace.Checkouts) != 2 {
		t.Fatalf("workspace = %#v, want toolshed cwd with two checkouts", workspace)
	}
	if workspace.Checkouts[0].URL != "https://github.com/valon-technologies/toolshed.git" ||
		workspace.Checkouts[0].Ref != "main" ||
		workspace.Checkouts[0].Path != "toolshed" {
		t.Fatalf("first checkout = %#v", workspace.Checkouts[0])
	}

	encoded := EncodeTargetMap(target)
	again, err := ParseTargetMap(encoded, "target")
	if err != nil {
		t.Fatalf("ParseTargetMap(round-trip) error = %v", err)
	}
	if !coreworkflow.TargetsEqual(target, again) {
		t.Fatalf("round-trip target = %#v, want %#v", again, target)
	}

	workspaceRaw := raw["steps"].([]any)[0].(map[string]any)["agent"].(map[string]any)["workspace"].(map[string]any)
	checkoutsRaw := workspaceRaw["checkouts"].([]any)
	workspaceRaw["checkouts"] = append(checkoutsRaw, map[string]any{
		"url":  "https://github.com/valon-technologies/gestalt-providers.git",
		"ref":  "main",
		"path": "gestalt-providers",
	})
	if len(target.Steps[0].Agent.Workspace.Checkouts) != 2 {
		t.Fatalf("parsed workspace checkouts = %#v, want isolated from later input mutation", target.Steps[0].Agent.Workspace.Checkouts)
	}
}
