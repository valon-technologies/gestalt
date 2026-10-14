package graphql

import "strings"

const defaultMaxDepth = 2

func generateQuery(schema *Schema, field Field, isMutation bool) string {
	operation := "query"
	if isMutation {
		operation = "mutation"
	}

	variables := make([]string, len(field.Args))
	arguments := make([]string, len(field.Args))
	for i, arg := range field.Args {
		variables[i] = "$" + arg.Name + ": " + formatTypeRef(&arg.Type)
		arguments[i] = arg.Name + ": $" + arg.Name
	}
	call := field.Name
	if len(field.Args) > 0 {
		operation += "(" + strings.Join(variables, ", ") + ")"
		call += "(" + strings.Join(arguments, ", ") + ")"
	}

	if selectionSet := buildSelectionSet(schema, field.Type, defaultMaxDepth, nil); selectionSet != "" {
		call += " " + selectionSet
	}
	return operation + " { " + call + " }"
}

func buildSelectionSet(schema *Schema, ref TypeRef, depth int, ancestors map[string]bool) string {
	if depth < 0 {
		return ""
	}

	inner := ref.innerType()
	typeName := inner.namedType()
	if typeName == "" {
		return ""
	}

	ft := schema.lookupType(typeName)
	if ft == nil || ft.Kind != KindObject {
		return ""
	}

	if ancestors[typeName] {
		return ""
	}

	if ancestors == nil {
		ancestors = make(map[string]bool)
	}
	ancestors[typeName] = true
	defer delete(ancestors, typeName)

	if isConnectionType(ft) {
		return buildConnectionSelectionSet(schema, ft, depth-1, ancestors)
	}

	var fields []string
	for _, f := range ft.Fields {
		if strings.HasPrefix(f.Name, "__") {
			continue
		}
		// Generated selection sets cannot synthesize values for nested field
		// arguments, so arg-bearing nested fields must be opt-in via overrides.
		if len(f.Args) > 0 {
			continue
		}

		fieldInner := f.Type.innerType()
		fieldTypeName := fieldInner.namedType()
		fieldType := schema.lookupType(fieldTypeName)

		if fieldType == nil {
			fields = append(fields, f.Name)
			continue
		}

		switch fieldType.Kind {
		case KindScalar, KindEnum:
			fields = append(fields, f.Name)
		case KindObject:
			sub := buildSelectionSet(schema, f.Type, depth-1, ancestors)
			if sub != "" {
				fields = append(fields, f.Name+" "+sub)
			}
		}
	}

	if len(fields) == 0 {
		return ""
	}
	return "{ " + strings.Join(fields, " ") + " }"
}

func isConnectionType(ft *FullType) bool {
	if !strings.HasSuffix(ft.Name, "Connection") {
		return false
	}
	hasNodes := false
	hasPageInfo := false
	for _, f := range ft.Fields {
		switch f.Name {
		case "nodes":
			hasNodes = true
		case "pageInfo":
			hasPageInfo = true
		}
		if hasNodes && hasPageInfo {
			return true
		}
	}
	return false
}

func buildConnectionSelectionSet(schema *Schema, ft *FullType, depth int, ancestors map[string]bool) string {
	var parts []string

	for _, f := range ft.Fields {
		if f.Name == "nodes" {
			sub := buildSelectionSet(schema, f.Type, depth, ancestors)
			if sub != "" {
				parts = append(parts, "nodes "+sub)
			}
		}
		if f.Name == "pageInfo" {
			parts = append(parts, "pageInfo { hasNextPage endCursor }")
		}
	}

	return "{ " + strings.Join(parts, " ") + " }"
}

func formatTypeRef(ref *TypeRef) string {
	if ref == nil {
		return "Unknown"
	}
	switch ref.Kind {
	case KindNonNull:
		return formatTypeRef(ref.OfType) + "!"
	case KindList:
		return "[" + formatTypeRef(ref.OfType) + "]"
	default:
		if ref.Name != nil {
			return *ref.Name
		}
		return "Unknown"
	}
}
