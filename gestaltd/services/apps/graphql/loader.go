package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
)

type rootAvailability struct {
	query    bool
	mutation bool
}

func LoadDefinition(ctx context.Context, name, endpoint string, allowedOps map[string]*operationexposure.OperationOverride) (*declarative.Definition, error) {
	schema, err := introspect(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("introspecting %s: %w", endpoint, err)
	}
	return DefinitionFromSchema(name, endpoint, schema, allowedOps)
}

func StaticDefinition(name, endpoint string) *declarative.Definition {
	def := &declarative.Definition{
		Provider:   name,
		BaseURL:    strings.TrimRight(endpoint, "/"),
		Operations: map[string]declarative.OperationDef{},
	}

	return def
}

func DefinitionFromSchema(name, endpoint string, schema *Schema, allowedOps map[string]*operationexposure.OperationOverride) (*declarative.Definition, error) {
	allowedOps, err := graphQLAllowedOperations(schema, allowedOps)
	if err != nil {
		return nil, err
	}
	def := StaticDefinition(name, endpoint)
	if err := addOperations(schema, def, schema.QueryType, false, allowedOps); err != nil {
		return nil, err
	}
	if err := addOperations(schema, def, schema.MutationType, true, allowedOps); err != nil {
		return nil, err
	}

	if len(def.Operations) == 0 {
		return nil, fmt.Errorf("no operations found for %s", endpoint)
	}

	return def, nil
}

func graphQLAllowedOperations(schema *Schema, allowedOps map[string]*operationexposure.OperationOverride) (map[string]*operationexposure.OperationOverride, error) {
	if len(allowedOps) == 0 {
		return allowedOps, nil
	}

	roots := graphQLRootAvailability(schema)
	names := make([]string, 0, len(allowedOps))
	for name := range allowedOps {
		names = append(names, name)
	}
	sort.Strings(names)

	filtered := make(map[string]*operationexposure.OperationOverride)
	for _, name := range names {
		availability, ok := roots[name]
		if !ok {
			continue
		}
		if availability.query && availability.mutation {
			return nil, fmt.Errorf("graphql allowed operation %q is defined on both query and mutation roots; use allowedOperations.%s.graphql.document", name, name)
		}
		filtered[name] = allowedOps[name]
	}
	return filtered, nil
}

func graphQLRootAvailability(schema *Schema) map[string]rootAvailability {
	roots := map[string]rootAvailability{}
	addRoot := func(root *TypeName, isMutation bool) {
		if root == nil {
			return
		}
		rootType := schema.lookupType(root.Name)
		if rootType == nil {
			return
		}
		for _, field := range rootType.Fields {
			availability := roots[field.Name]
			if isMutation {
				availability.mutation = true
			} else {
				availability.query = true
			}
			roots[field.Name] = availability
		}
	}
	addRoot(schema.QueryType, false)
	addRoot(schema.MutationType, true)
	return roots
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

func addOperations(schema *Schema, def *declarative.Definition, root *TypeName, isMutation bool, allowedOps map[string]*operationexposure.OperationOverride) error {
	if root == nil {
		return nil
	}
	rootType := schema.lookupType(root.Name)
	if rootType == nil {
		return nil
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
		var tags []string
		override := allowedOps[field.Name]
		if override != nil {
			if override.Description != "" {
				desc = override.Description
			}
			if override.Alias != "" {
				opName = override.Alias
			}
			allowedRoles = slices.Clone(override.AllowedRoles)
			tags = catalog.MergeTags(override.Tags)
		}

		opDef := declarative.OperationDef{
			Description:  declarative.TruncateDescription(desc),
			AllowedRoles: allowedRoles,
			Tags:         tags,
			Transport:    "graphql",
			Query:        generateQuery(schema, field, isMutation),
		}

		opDef.Parameters = argsToParams(schema, field.Args)

		def.Operations[opName] = opDef
	}
	return nil
}

func argsToParams(schema *Schema, args []InputValue) []declarative.ParameterDef {
	if len(args) == 0 {
		return nil
	}
	params := make([]declarative.ParameterDef, 0, len(args))
	for _, arg := range args {
		paramType := graphqlParamType(schema, arg.Type)
		params = append(params, declarative.ParameterDef{
			Name:        arg.Name,
			Type:        paramType,
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
	if schema != nil {
		ft := schema.lookupType(typeName)
		if ft != nil && ft.Kind == KindInputObject {
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
	}
	return graphqlTypeToSimple(schema, ref)
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
		if schema != nil {
			if ft := schema.lookupType(typeName); ft != nil && (ft.Kind == KindEnum || ft.Kind == KindScalar) {
				return "string"
			}
			return "object"
		}
		return "string"
	}
}
