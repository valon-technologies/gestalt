package config

import "testing"

func TestWorkflowTargetToCoreClonesMutableMaps(t *testing.T) {
	t.Parallel()

	stepMetadata := map[string]any{"step": "original"}
	messageMetadata := map[string]any{"message": "original"}
	responseSchema := map[string]any{"schema": "original"}
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
				ResponseSchema: responseSchema,
				ModelOptions:   modelOptions,
			},
		}},
	})

	target.Steps[0].Metadata["step"] = "mutated"
	target.Steps[0].Agent.Messages[0].Metadata["message"] = "mutated"
	target.Steps[0].Agent.ResponseSchema["schema"] = "mutated"
	target.Steps[0].Agent.ModelOptions["option"] = "mutated"

	if stepMetadata["step"] != "original" {
		t.Fatalf("step metadata shares map with core target: %v", stepMetadata)
	}
	if messageMetadata["message"] != "original" {
		t.Fatalf("message metadata shares map with core target: %v", messageMetadata)
	}
	if responseSchema["schema"] != "original" {
		t.Fatalf("response schema shares map with core target: %v", responseSchema)
	}
	if modelOptions["option"] != "original" {
		t.Fatalf("model options shares map with core target: %v", modelOptions)
	}
}
