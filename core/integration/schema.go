package integration

import (
	"encoding/json"
	"strings"

	"github.com/valon-technologies/gestalt/core/catalog"
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
func SynthesizeInputSchema(params []catalog.CatalogParameter) json.RawMessage {
	if len(params) == 0 {
		return nil
	}

	properties := make(map[string]map[string]any, len(params))
	var required []string

	for _, p := range params {
		prop := map[string]any{
			"type": NormalizeType(p.Type),
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

func NormalizeType(t string) string {
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
func AnnotationsFromMethod(method string) catalog.OperationAnnotations {
	a := catalog.OperationAnnotations{
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

// CompileSchemas fills in InputSchema and Annotations for operations that lack them.
func CompileSchemas(c *catalog.Catalog) {
	if c == nil {
		return
	}
	for i := range c.Operations {
		op := &c.Operations[i]
		if op.InputSchema == nil && len(op.Parameters) > 0 {
			op.InputSchema = SynthesizeInputSchema(op.Parameters)
		}
		if op.Annotations == (catalog.OperationAnnotations{}) {
			op.Annotations = AnnotationsFromMethod(op.Method)
		}
	}
}

func boolPtr(v bool) *bool { return &v }
