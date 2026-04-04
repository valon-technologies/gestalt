package integration

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/catalog"
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
	t = strings.ToLower(strings.TrimSpace(t))
	switch t {
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
	}

	switch {
	case strings.HasPrefix(t, schemaTypeArray+"<"), strings.HasPrefix(t, schemaTypeArray+"["):
		return schemaTypeArray
	case strings.HasPrefix(t, schemaTypeObject+"{"):
		return schemaTypeObject
	default:
		return schemaTypeString
	}
}

type topLevelInputSchema struct {
	Properties map[string]struct {
		Type string `json:"type"`
	} `json:"properties"`
	Required []string `json:"required"`
}

// AnnotationsFromMethod derives MCP operation annotations from an HTTP method.
func AnnotationsFromMethod(method string) catalog.OperationAnnotations {
	a := catalog.OperationAnnotations{
		OpenWorldHint: boolPtr(true),
	}
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead:
		a.ReadOnlyHint = boolPtr(true)
	case http.MethodPut:
		a.IdempotentHint = boolPtr(true)
	case http.MethodDelete:
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
		if len(op.Parameters) == 0 {
			op.Parameters = parametersFromInputSchema(op.InputSchema)
		}
		if op.Annotations == (catalog.OperationAnnotations{}) {
			op.Annotations = AnnotationsFromMethod(op.Method)
		}
	}
}

func parametersFromInputSchema(raw json.RawMessage) []catalog.CatalogParameter {
	if len(raw) == 0 {
		return nil
	}

	var schema topLevelInputSchema
	if err := json.Unmarshal(raw, &schema); err != nil || len(schema.Properties) == 0 {
		return nil
	}

	required := make(map[string]bool, len(schema.Required))
	for _, name := range schema.Required {
		required[name] = true
	}

	names := slices.Sorted(maps.Keys(schema.Properties))
	params := make([]catalog.CatalogParameter, 0, len(names))
	for _, name := range names {
		params = append(params, catalog.CatalogParameter{
			Name:     name,
			Type:     NormalizeType(schema.Properties[name].Type),
			Required: required[name],
		})
	}
	return params
}

func boolPtr(v bool) *bool { return &v }
