package graphql

import (
	"fmt"
	"strings"
)

const defaultMaxDepth = 2

func generateQuery(schema *Schema, field Field, isMutation bool) string {
	var b strings.Builder

	if isMutation {
		b.WriteString("mutation")
	} else {
		b.WriteString("query")
	}

	if len(field.Args) > 0 {
		b.WriteByte('(')
		for i, arg := range field.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "$%s: %s", arg.Name, formatTypeRef(arg.Type))
		}
		b.WriteByte(')')
	}

	b.WriteString(" { ")
	b.WriteString(field.Name)

	if len(field.Args) > 0 {
		b.WriteByte('(')
		for i, arg := range field.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s: $%s", arg.Name, arg.Name)
		}
		b.WriteByte(')')
	}

	selectionSet := buildSelectionSet(schema, field.Type, defaultMaxDepth, nil)
	if selectionSet != "" {
		b.WriteString(" ")
		b.WriteString(selectionSet)
	}

	b.WriteString(" }")
	return b.String()
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
			if depth <= 0 {
				continue
			}
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

	if len(parts) == 0 {
		return ""
	}
	return "{ " + strings.Join(parts, " ") + " }"
}

func formatTypeRef(ref TypeRef) string {
	switch ref.Kind {
	case KindNonNull:
		if ref.OfType != nil {
			return formatTypeRef(*ref.OfType) + "!"
		}
		return "Unknown!"
	case KindList:
		if ref.OfType != nil {
			return "[" + formatTypeRef(*ref.OfType) + "]"
		}
		return "[Unknown]"
	default:
		if ref.Name != nil {
			return *ref.Name
		}
		return "Unknown"
	}
}
