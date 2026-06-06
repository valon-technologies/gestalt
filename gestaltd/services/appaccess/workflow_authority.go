package appaccess

import (
	"math"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type WorkflowStepInvocation struct {
	ID                   string
	Index                int
	ProviderName         string
	RunID                string
	DefinitionID         string
	DefinitionGeneration int64
	WorkflowKey          string
	WorkflowKeySet       bool
	Kind                 string
	App                  string
	Operation            string
	AgentProvider        string
	Model                string
	Connection           string
	Instance             string
	CredentialMode       string
}

func WorkflowStepInvocationFromContext(workflow map[string]any) (WorkflowStepInvocation, error) {
	providerName := workflowString(workflow["providerName"])
	runID := workflowString(workflow["runId"])
	definitionID := workflowString(workflow["definitionId"])
	definitionGeneration, ok := workflowInt64(workflow["definitionGeneration"])
	if providerName == "" || runID == "" || definitionID == "" || !ok || definitionGeneration <= 0 {
		return WorkflowStepInvocation{}, status.Error(codes.FailedPrecondition, "workflow run identity context is required")
	}
	workflowKey, workflowKeySet := workflow["workflowKey"]
	if !workflowKeySet {
		return WorkflowStepInvocation{}, status.Error(codes.FailedPrecondition, "workflow key context is required")
	}
	currentStep, ok := workflowMap(workflow["currentStep"])
	if !ok {
		return WorkflowStepInvocation{}, status.Error(codes.FailedPrecondition, "workflow current step context is required")
	}
	stepID := workflowString(currentStep["id"])
	stepIndex, ok := workflowIndex(currentStep["index"])
	if stepID == "" || !ok {
		return WorkflowStepInvocation{}, status.Error(codes.FailedPrecondition, "workflow current step id and index are required")
	}
	target, ok := workflowMap(workflow["target"])
	if !ok {
		return WorkflowStepInvocation{}, status.Error(codes.FailedPrecondition, "workflow target context is required")
	}
	steps, ok := workflowSteps(target["steps"])
	if !ok || stepIndex < 0 || stepIndex >= len(steps) {
		return WorkflowStepInvocation{}, status.Error(codes.FailedPrecondition, "workflow current step index is outside the target")
	}
	step, ok := workflowMap(steps[stepIndex])
	if !ok {
		return WorkflowStepInvocation{}, status.Error(codes.FailedPrecondition, "workflow target step context is invalid")
	}
	if targetStepID := workflowString(step["id"]); targetStepID != stepID {
		return WorkflowStepInvocation{}, status.Error(codes.FailedPrecondition, "workflow current step does not match the target")
	}
	return WorkflowStepInvocation{
		ID:                   stepID,
		Index:                stepIndex,
		ProviderName:         providerName,
		RunID:                runID,
		DefinitionID:         definitionID,
		DefinitionGeneration: definitionGeneration,
		WorkflowKey:          workflowString(workflowKey),
		WorkflowKeySet:       true,
		Kind:                 workflowString(step["kind"]),
		App:                  workflowString(step["app"]),
		Operation:            workflowString(step["operation"]),
		AgentProvider:        workflowString(step["agentProvider"]),
		Model:                workflowString(step["model"]),
		Connection:           workflowString(step["connection"]),
		Instance:             workflowString(step["instance"]),
		CredentialMode:       workflowString(step["credentialMode"]),
	}, nil
}

func workflowMap(value any) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	return m, ok
}

func workflowSteps(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for i := range typed {
			out = append(out, typed[i])
		}
		return out, true
	default:
		return nil, false
	}
}

func workflowString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func workflowIndex(value any) (int, bool) {
	index, ok := workflowInt64(value)
	if !ok || index < 0 || index > int64(int(index)) {
		return 0, false
	}
	return int(index), true
}

func workflowInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		if math.Trunc(typed) != typed {
			return 0, false
		}
		return int64(typed), true
	default:
		return 0, false
	}
}
