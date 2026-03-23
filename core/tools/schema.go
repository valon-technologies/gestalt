package tools

import "github.com/valon-technologies/gestalt/core"

// ToolDefinition represents a tool in a format suitable for LLM APIs.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// ParameterSchema converts a slice of core.Parameter into a JSON Schema object.
func ParameterSchema(params []core.Parameter) map[string]any {
	props := make(map[string]any, len(params))
	var required []string
	for _, p := range params {
		prop := map[string]any{"type": schemaType(p.Type)}
		if p.Description != "" {
			prop["description"] = p.Description
		}
		props[p.Name] = prop
		if p.Required {
			required = append(required, p.Name)
		}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// CapabilitiesToTools converts a list of capabilities into tool definitions.
// Tool names are formatted as "provider_operation" (e.g. "slack_list_channels").
func CapabilitiesToTools(caps []core.Capability) []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(caps))
	for _, cap := range caps {
		defs = append(defs, ToolDefinition{
			Name:        cap.Provider + "_" + cap.Operation,
			Description: cap.Description,
			InputSchema: ParameterSchema(cap.Parameters),
		})
	}
	return defs
}

func schemaType(t string) string {
	switch t {
	case "integer", "int":
		return "integer"
	case "number", "float", "double":
		return "number"
	case "boolean", "bool":
		return "boolean"
	case "array":
		return "array"
	case "object":
		return "object"
	default:
		return "string"
	}
}
