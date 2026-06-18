package config

import "testing"

func TestWorkflowTargetToCoreClonesMutableMaps(t *testing.T) {
	t.Parallel()

	stepMetadata := map[string]any{"step": "original"}
	messageMetadata := map[string]any{"message": "original"}
	schema := map[string]any{"schema": "original"}
	modelOptions := map[string]any{"option": "original"}

	target := WorkflowTargetToCore(&WorkflowTargetConfig{
		Steps: []WorkflowStepConfig{{
			ID:       "agent-step",
			Metadata: stepMetadata,
			Agent: &WorkflowStepAgentConfig{
				Messages: []WorkflowAgentMessage{{
					Role:     "user",
					Metadata: messageMetadata,
				}},
				Output: &WorkflowAgentOutputConfig{
					Structured: &WorkflowAgentStructuredOutputConfig{Schema: schema},
				},
				ModelOptions: modelOptions,
			},
		}},
	})

	target.Steps[0].Metadata["step"] = "mutated"
	target.Steps[0].Agent.Messages[0].Metadata["message"] = "mutated"
	target.Steps[0].Agent.Output.Structured.Schema["schema"] = "mutated"
	target.Steps[0].Agent.ModelOptions["option"] = "mutated"

	if stepMetadata["step"] != "original" {
		t.Fatalf("step metadata shares map with core target: %v", stepMetadata)
	}
	if messageMetadata["message"] != "original" {
		t.Fatalf("message metadata shares map with core target: %v", messageMetadata)
	}
	if schema["schema"] != "original" {
		t.Fatalf("response schema shares map with core target: %v", schema)
	}
	if modelOptions["option"] != "original" {
		t.Fatalf("model options shares map with core target: %v", modelOptions)
	}
}

func TestWorkflowAgentWorkspaceToCore(t *testing.T) {
	t.Parallel()

	target := WorkflowTargetToCore(&WorkflowTargetConfig{
		Steps: []WorkflowStepConfig{{
			ID: "agent-step",
			Agent: &WorkflowStepAgentConfig{
				Provider: "claude",
				Prompt:   WorkflowTextConfig{Template: "inspect"},
				Output: &WorkflowAgentOutputConfig{
					Text: &WorkflowAgentTextOutputConfig{},
				},
				Workspace: &WorkflowStepAgentWorkspaceConfig{
					Checkouts: []WorkflowStepAgentWorkspaceCheckoutConfig{{
						URL:  "https://github.com/valon-technologies/toolshed.git",
						Ref:  "main",
						Path: "toolshed",
					}},
					CWD: "toolshed",
				},
			},
		}},
	})

	workspace := target.Steps[0].Agent.Workspace
	if workspace == nil || workspace.CWD != "toolshed" || len(workspace.Checkouts) != 1 {
		t.Fatalf("workspace = %#v", workspace)
	}
	if workspace.Checkouts[0].URL != "https://github.com/valon-technologies/toolshed.git" {
		t.Fatalf("checkout url = %q", workspace.Checkouts[0].URL)
	}
}
