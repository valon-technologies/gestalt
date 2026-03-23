package graphql

import "encoding/json"

func inputTypeToJSONSchema(schema *Schema, ref TypeRef, visited map[string]bool) json.RawMessage {
	if ref.isNonNull() && ref.OfType != nil {
		return inputTypeToJSONSchema(schema, *ref.OfType, visited)
	}

	if ref.isList() && ref.OfType != nil {
		items := inputTypeToJSONSchema(schema, *ref.OfType, visited)
		return marshalSchema(map[string]any{
			"type":  "array",
			"items": items,
		})
	}

	typeName := ref.namedType()
	switch typeName {
	case "String", "DateTime", "Date", "URI", "URL", "UUID", "JSONString", "TimelessDate":
		return marshalSchema(map[string]any{"type": "string"})
	case "Int":
		return marshalSchema(map[string]any{"type": "integer"})
	case "Float":
		return marshalSchema(map[string]any{"type": "number"})
	case "Boolean":
		return marshalSchema(map[string]any{"type": "boolean"})
	case "ID":
		return marshalSchema(map[string]any{"type": "string"})
	}

	ft := schema.lookupType(typeName)
	if ft == nil {
		return marshalSchema(map[string]any{"type": "string"})
	}

	switch ft.Kind {
	case KindEnum:
		vals := make([]string, len(ft.EnumValues))
		for i, ev := range ft.EnumValues {
			vals[i] = ev.Name
		}
		return marshalSchema(map[string]any{
			"type": "string",
			"enum": vals,
		})

	case KindInputObject:
		if visited[typeName] {
			return marshalSchema(map[string]any{"type": "object"})
		}
		visited[typeName] = true
		defer delete(visited, typeName)

		props := make(map[string]json.RawMessage, len(ft.InputFields))
		var required []string
		for _, field := range ft.InputFields {
			props[field.Name] = inputTypeToJSONSchema(schema, field.Type, visited)
			if field.Type.isNonNull() {
				required = append(required, field.Name)
			}
		}
		result := map[string]any{
			"type":       "object",
			"properties": props,
		}
		if len(required) > 0 {
			result["required"] = required
		}
		return marshalSchema(result)

	case KindScalar:
		return marshalSchema(map[string]any{"type": "string"})

	default:
		return marshalSchema(map[string]any{"type": "string"})
	}
}

func argsToJSONSchema(schema *Schema, args []InputValue) json.RawMessage {
	if len(args) == 0 {
		return marshalSchema(map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		})
	}

	props := make(map[string]json.RawMessage, len(args))
	var required []string
	visited := make(map[string]bool)

	for _, arg := range args {
		propSchema := inputTypeToJSONSchema(schema, arg.Type, visited)

		if arg.Description != "" {
			propSchema = withDescription(propSchema, arg.Description)
		}

		props[arg.Name] = propSchema
		if arg.Type.isNonNull() {
			required = append(required, arg.Name)
		}
	}

	result := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		result["required"] = required
	}
	return marshalSchema(result)
}

func marshalSchema(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func withDescription(schema json.RawMessage, desc string) json.RawMessage {
	var m map[string]any
	if json.Unmarshal(schema, &m) != nil {
		return schema
	}
	m["description"] = desc
	return marshalSchema(m)
}
