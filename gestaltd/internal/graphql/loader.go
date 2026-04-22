package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/provider"
)

func LoadDefinition(ctx context.Context, name, endpoint string, allowedOps map[string]*config.OperationOverride, selectionOverrides map[string]string) (*provider.Definition, error) {
	schema, err := introspect(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("introspecting %s: %w", endpoint, err)
	}
	return DefinitionFromSchema(name, endpoint, schema, allowedOps, selectionOverrides)
}

func StaticDefinition(name, endpoint string) *provider.Definition {
	def := &provider.Definition{
		Provider:   name,
		BaseURL:    strings.TrimRight(endpoint, "/"),
		Operations: map[string]provider.OperationDef{},
	}

	return def
}

func DefinitionFromSchema(name, endpoint string, schema *Schema, allowedOps map[string]*config.OperationOverride, selectionOverrides map[string]string) (*provider.Definition, error) {
	def := StaticDefinition(name, endpoint)
	def.Operations = make(map[string]provider.OperationDef)
	addOperations(schema, def, schema.QueryType, false, allowedOps, selectionOverrides)
	addOperations(schema, def, schema.MutationType, true, allowedOps, selectionOverrides)

	if len(def.Operations) == 0 {
		return nil, fmt.Errorf("no operations found for %s", endpoint)
	}

	return def, nil
}

func SchemaFromBody(body []byte) (*Schema, error) {
	var resp struct {
		Schema Schema `json:"__schema"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing introspection response: %w", err)
	}

	resp.Schema.buildIndex()
	return &resp.Schema, nil
}

func IntrospectionRequest() core.GraphQLRequest {
	return core.GraphQLRequest{Document: introspectionQuery}
}

func SchemaFromResult(result *core.OperationResult) (*Schema, error) {
	if result == nil {
		return nil, fmt.Errorf("graphql introspection returned no result")
	}
	if result.Status >= http.StatusBadRequest {
		return nil, fmt.Errorf("graphql introspection returned HTTP %d", result.Status)
	}
	return SchemaFromBody([]byte(result.Body))
}

func addOperations(schema *Schema, def *provider.Definition, root *TypeName, isMutation bool, allowedOps map[string]*config.OperationOverride, selectionOverrides map[string]string) {
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
		var allowedRoles []string
		if override := allowedOps[field.Name]; override != nil {
			if override.Description != "" {
				desc = override.Description
			}
			if override.Alias != "" {
				opName = override.Alias
			}
			allowedRoles = override.AllowedRoles
		}

		query := generateQuery(schema, field, isMutation, selectionOverrides[field.Name])

		opDef := provider.OperationDef{
			Description:  provider.TruncateDescription(desc),
			AllowedRoles: allowedRoles,
			Transport:    "graphql",
			Query:        query,
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
