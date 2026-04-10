package graphql

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/provider"
)

func LoadDefinition(ctx context.Context, name, endpoint string, allowedOps map[string]*config.OperationOverride, client *http.Client) (*provider.Definition, error) {
	schema, err := introspect(ctx, endpoint, client)
	if err != nil {
		return nil, fmt.Errorf("introspecting %s: %w", endpoint, err)
	}

	def := &provider.Definition{
		Provider: name,
		BaseURL:  strings.TrimRight(endpoint, "/"),
	}

	def.Operations = make(map[string]provider.OperationDef)
	addOperations(schema, def, schema.QueryType, false, allowedOps)
	addOperations(schema, def, schema.MutationType, true, allowedOps)

	if len(def.Operations) == 0 {
		return nil, fmt.Errorf("no operations found for %s", endpoint)
	}

	return def, nil
}

func addOperations(schema *Schema, def *provider.Definition, root *TypeName, isMutation bool, allowedOps map[string]*config.OperationOverride) {
	if root == nil {
		return
	}
	rootType := schema.lookupType(root.Name)
	if rootType == nil {
		return
	}

	for _, field := range rootType.Fields {
		if strings.HasPrefix(field.Name, "__") {
			continue
		}

		if allowedOps != nil {
			if _, ok := allowedOps[field.Name]; !ok {
				continue
			}
		}

		desc := field.Description
		opName := field.Name
		if override := allowedOps[field.Name]; override != nil {
			if override.Description != "" {
				desc = override.Description
			}
			if override.Alias != "" {
				opName = override.Alias
			}
		}

		query := generateQuery(schema, field, isMutation)

		opDef := provider.OperationDef{
			Description: provider.TruncateDescription(desc),
			Transport:   "graphql",
			Query:       query,
		}

		opDef.Parameters = argsToParams(schema, field.Args)

		def.Operations[opName] = opDef
	}
}

func argsToParams(schema *Schema, args []InputValue) []provider.ParameterDef {
	if len(args) == 0 {
		return nil
	}
	params := make([]provider.ParameterDef, 0, len(args))
	for _, arg := range args {
		params = append(params, provider.ParameterDef{
			Name:        arg.Name,
			Type:        graphqlParamType(schema, arg.Type),
			Description: arg.Description,
			Required:    arg.Type.isNonNull(),
		})
	}
	return params
}

func graphqlParamType(schema *Schema, ref TypeRef) string {
	if ref.isList() {
		return "array"
	}

	typeName := ref.innerType().namedType()
	ft := schema.lookupType(typeName)
	if ft == nil || ft.Kind != KindInputObject {
		return graphqlTypeToSimple(schema, ref)
	}
	if len(ft.InputFields) == 0 {
		return "object"
	}

	fields := make([]string, 0, len(ft.InputFields))
	for _, field := range ft.InputFields {
		name := field.Name
		if field.Type.isNonNull() {
			name += "!"
		}
		fields = append(fields, name)
	}
	return "object{" + strings.Join(fields, ", ") + "}"
}

func graphqlTypeToSimple(schema *Schema, ref TypeRef) string {
	if ref.isList() {
		return "array"
	}
	inner := ref.innerType()
	typeName := inner.namedType()
	switch typeName {
	case "String", "ID", "DateTime", "Date", "URI", "URL", "UUID", "JSONString", "TimelessDate":
		return "string"
	case "Int":
		return "integer"
	case "Float":
		return "number"
	case "Boolean":
		return "boolean"
	default:
		if ft := schema.lookupType(typeName); ft != nil && (ft.Kind == KindEnum || ft.Kind == KindScalar) {
			return "string"
		}
		return "object"
	}
}
