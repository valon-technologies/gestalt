package bootstrap

import (
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

func workflowSystemToolStartRunSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"definitionId"}, map[string]any{
		"provider":     workflowSystemToolStringSchema("Workflow provider name."),
		"workflowKey":  workflowSystemToolStringSchema("Workflow key."),
		"definitionId": workflowSystemToolStringSchema("Workflow definition ID to run."),
	})
}

func workflowSystemToolListRunsSchema() map[string]any {
	return workflowSystemToolObjectSchema(nil, map[string]any{
		"pageSize":     map[string]any{"type": "integer", "minimum": 0, "description": "Maximum runs to return."},
		"pageToken":    workflowSystemToolStringSchema("Pagination token from a previous workflow_runs_list response."),
		"app":          workflowSystemToolStringSchema("Target app name to filter by."),
		"definitionId": workflowSystemToolStringSchema("Workflow definition ID to filter by."),
		"status": map[string]any{
			"type":        "string",
			"description": "Workflow run status to filter by.",
			"enum": []any{
				string(coreworkflow.RunStatusPending),
				string(coreworkflow.RunStatusRunning),
				string(coreworkflow.RunStatusSucceeded),
				string(coreworkflow.RunStatusFailed),
				string(coreworkflow.RunStatusCanceled),
			},
		},
	})
}

func workflowSystemToolApplyDefinitionSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"definitionId", "target"}, map[string]any{
		"definitionId": workflowSystemToolStringSchema("Workflow definition ID."),
		"provider":     workflowSystemToolStringSchema("Workflow provider name."),
		"target":       workflowSystemToolTargetSchema(),
		"paused":       map[string]any{"type": "boolean"},
	})
}

func workflowSystemToolTargetSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"steps"}, map[string]any{
		"steps": map[string]any{"type": "array", "items": workflowSystemToolStepSchema(), "minItems": 1},
	})
}

func workflowSystemToolStepSchema() map[string]any {
	schema := workflowSystemToolObjectSchema([]string{"id"}, map[string]any{
		"id":             workflowSystemToolStringSchema("Stable step ID."),
		"inputs":         map[string]any{"type": "object"},
		"app":            workflowSystemToolAppCallSchema("App name."),
		"agent":          workflowSystemToolAgentTurnSchema(),
		"when":           workflowSystemToolStepWhenSchema(),
		"timeoutSeconds": map[string]any{"type": "integer", "minimum": 0, "description": "Optional execution budget in seconds. Providers choose their own timeout when omitted or zero."},
		"metadata":       map[string]any{"type": "object"},
	})
	schema["oneOf"] = []any{
		map[string]any{
			"required": []string{"app"},
			"not":      map[string]any{"required": []string{"agent"}},
		},
		map[string]any{
			"required": []string{"agent"},
			"properties": map[string]any{
				"timeoutSeconds": map[string]any{"type": "integer", "minimum": 0},
			},
			"not": map[string]any{"required": []string{"app"}},
		},
	}
	return schema
}

func workflowSystemToolAppCallSchema(nameDescription string) map[string]any {
	return workflowSystemToolObjectSchema([]string{"name", "operation"}, map[string]any{
		"name":           workflowSystemToolStringSchema(nameDescription),
		"operation":      workflowSystemToolStringSchema("App operation."),
		"connection":     workflowSystemToolStringSchema("Connection name."),
		"instance":       workflowSystemToolStringSchema("Instance name."),
		"credentialMode": workflowSystemToolStringSchema("Optional credential mode."),
		"input":          map[string]any{"type": "object"},
	})
}

func workflowSystemToolAgentTurnSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"output"}, map[string]any{
		"provider":     workflowSystemToolStringSchema("Agent provider name."),
		"model":        workflowSystemToolStringSchema("Agent model."),
		"sessionKey":   workflowSystemToolStringSchema("Agent session key."),
		"prompt":       map[string]any{},
		"messages":     map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"tools":        map[string]any{"type": "array", "items": map[string]any{"type": "object"}, "description": "Agent tool references. If omitted, the created workflow agent inherits the current agent turn's tool references."},
		"output":       workflowSystemToolAgentOutputSchema(),
		"modelOptions": map[string]any{"type": "object"},
	})
}

func workflowSystemToolAgentOutputSchema() map[string]any {
	schema := workflowSystemToolObjectSchema([]string{}, map[string]any{
		"text": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
		},
		"structured": workflowSystemToolObjectSchema([]string{"schema"}, map[string]any{
			"schema": map[string]any{"type": "object"},
		}),
	})
	schema["oneOf"] = []any{
		map[string]any{"required": []string{"text"}},
		map[string]any{"required": []string{"structured"}},
	}
	return schema
}

func workflowSystemToolStepWhenSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"value", "equals"}, map[string]any{
		"value":  map[string]any{},
		"equals": map[string]any{},
	})
}

func workflowSystemToolObjectSchema(required []string, properties map[string]any) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = append([]string(nil), required...)
	}
	return schema
}

func workflowSystemToolStringSchema(description string) map[string]any {
	schema := map[string]any{"type": "string"}
	if strings.TrimSpace(description) != "" {
		schema["description"] = description
	}
	return schema
}
