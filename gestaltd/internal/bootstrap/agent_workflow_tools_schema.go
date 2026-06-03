package bootstrap

import (
	"maps"
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

func workflowSystemToolCreateScheduleSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"cron"}, map[string]any{
		"provider":     workflowSystemToolStringSchema("Workflow provider name."),
		"cron":         workflowSystemToolStringSchema("Cron expression."),
		"timezone":     workflowSystemToolStringSchema("IANA timezone."),
		"paused":       map[string]any{"type": "boolean"},
		"target":       workflowSystemToolTargetSchema(),
		"definitionId": workflowSystemToolStringSchema("Workflow definition ID to schedule."),
	})
}

func workflowSystemToolStartRunSchema() map[string]any {
	common := map[string]any{
		"provider":    workflowSystemToolStringSchema("Workflow provider name."),
		"workflowKey": workflowSystemToolStringSchema("Workflow key."),
	}
	targetProperties := maps.Clone(common)
	targetProperties["target"] = workflowSystemToolTargetSchema()
	definitionProperties := maps.Clone(common)
	definitionProperties["definitionId"] = workflowSystemToolStringSchema("Workflow definition ID to run.")
	return map[string]any{
		"type": "object",
		"oneOf": []any{
			workflowSystemToolObjectSchema([]string{"target"}, targetProperties),
			workflowSystemToolObjectSchema([]string{"definitionId"}, definitionProperties),
		},
	}
}

func workflowSystemToolUpdateScheduleSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"scheduleId"}, map[string]any{
		"scheduleId":   workflowSystemToolStringSchema("Schedule ID."),
		"provider":     workflowSystemToolStringSchema("Workflow provider name."),
		"cron":         workflowSystemToolStringSchema("Cron expression. If omitted, the existing cron is preserved."),
		"timezone":     workflowSystemToolStringSchema("IANA timezone. If omitted, the existing timezone is preserved."),
		"paused":       map[string]any{"type": "boolean", "description": "Paused state. If omitted, the existing paused state is preserved."},
		"target":       workflowSystemToolTargetSchema(),
		"definitionId": workflowSystemToolStringSchema("Workflow definition ID to schedule. If omitted with no target, the existing resolved target is preserved."),
	})
}

func workflowSystemToolListRunsSchema() map[string]any {
	return workflowSystemToolObjectSchema(nil, map[string]any{
		"pageSize":  map[string]any{"type": "integer", "minimum": 0, "description": "Maximum runs to return."},
		"pageToken": workflowSystemToolStringSchema("Pagination token from a previous workflow_runs_list response."),
		"app":       workflowSystemToolStringSchema("Target app name to filter by."),
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

func workflowSystemToolCreateDefinitionSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"target"}, map[string]any{
		"provider": workflowSystemToolStringSchema("Workflow provider name."),
		"target":   workflowSystemToolTargetSchema(),
	})
}

func workflowSystemToolUpdateDefinitionSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"definitionId", "target"}, map[string]any{
		"definitionId": workflowSystemToolStringSchema("Workflow definition ID."),
		"provider":     workflowSystemToolStringSchema("Workflow provider name."),
		"target":       workflowSystemToolTargetSchema(),
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
		"timeoutSeconds": map[string]any{"type": "integer", "minimum": 1, "description": "Execution budget in seconds. Required and positive for agent steps."},
		"metadata":       map[string]any{"type": "object"},
	})
	schema["oneOf"] = []any{
		map[string]any{
			"required": []string{"app"},
			"not":      map[string]any{"required": []string{"agent"}},
		},
		map[string]any{
			"required": []string{"agent", "timeoutSeconds"},
			"properties": map[string]any{
				"timeoutSeconds": map[string]any{"type": "integer", "minimum": 1},
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
