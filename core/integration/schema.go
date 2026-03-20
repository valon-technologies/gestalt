package integration

import (
	"encoding/json"
	"strings"
)

const (
	schemaTypeObject  = "object"
	schemaTypeString  = "string"
	schemaTypeInteger = "integer"
	schemaTypeNumber  = "number"
	schemaTypeBoolean = "boolean"
	schemaTypeArray   = "array"
)

// SynthesizeInputSchema builds a JSON Schema object from flat CatalogParameters.
func SynthesizeInputSchema(params []CatalogParameter) json.RawMessage {
	if len(params) == 0 {
		return nil
	}

	properties := make(map[string]map[string]any, len(params))
	var required []string

	for _, p := range params {
		prop := map[string]any{
			"type": normalizeType(p.Type),
		}
		if p.Description != "" {
			prop["description"] = p.Description
		}
		if p.Default != nil {
			prop["default"] = p.Default
		}
		properties[p.Name] = prop

		if p.Required {
			required = append(required, p.Name)
		}
	}

	schema := map[string]any{
		"type":       schemaTypeObject,
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}

	data, _ := json.Marshal(schema)
	return data
}

func normalizeType(t string) string {
	switch strings.ToLower(t) {
	case schemaTypeInteger, "int":
		return schemaTypeInteger
	case schemaTypeNumber, "float", "double":
		return schemaTypeNumber
	case schemaTypeBoolean, "bool":
		return schemaTypeBoolean
	case schemaTypeArray:
		return schemaTypeArray
	case schemaTypeObject:
		return schemaTypeObject
	default:
		return schemaTypeString
	}
}

// AnnotationsFromMethod derives MCP operation annotations from an HTTP method.
func AnnotationsFromMethod(method string) OperationAnnotations {
	a := OperationAnnotations{
		OpenWorldHint: boolPtr(true),
	}
	switch strings.ToUpper(method) {
	case "GET", "HEAD":
		a.ReadOnlyHint = boolPtr(true)
	case "PUT":
		a.IdempotentHint = boolPtr(true)
	case "DELETE":
		a.DestructiveHint = boolPtr(true)
	}
	return a
}

func boolPtr(v bool) *bool { return &v }
