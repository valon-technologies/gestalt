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

const (
	graphQLOperationTypeQuery    = "query"
	graphQLOperationTypeMutation = "mutation"
)

type rootAvailability struct {
	query       bool
	mutation    bool
	queryRef    TypeRef
	mutationRef TypeRef
}

func LoadDefinition(ctx context.Context, name, endpoint string, allowedOps map[string]*operationexposure.OperationOverride, selectionOverrides map[string]string) (*declarative.Definition, error) {
	schema, err := introspect(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("introspecting %s: %w", endpoint, err)
	}
	return DefinitionFromSchema(name, endpoint, schema, allowedOps, selectionOverrides)
}

func StaticDefinition(name, endpoint string) *declarative.Definition {
	def := &declarative.Definition{
		Provider:   name,
		BaseURL:    strings.TrimRight(endpoint, "/"),
		Operations: map[string]declarative.OperationDef{},
	}

	return def
}

func DefinitionFromSchema(name, endpoint string, schema *Schema, allowedOps map[string]*operationexposure.OperationOverride, selectionOverrides map[string]string) (*declarative.Definition, error) {
	if hasGraphQLAllowedOperationConfig(allowedOps) && len(selectionOverrides) > 0 {
		return nil, fmt.Errorf("graphql %s: operationSelections cannot be combined with allowedOperations", name)
	}
	allowedOps = graphQLAllowedOperations(schema, allowedOps, selectionOverrides)
	if err := validateAllowedOperationRoots(schema, allowedOps, selectionOverrides); err != nil {
		return nil, err
	}
	def := StaticDefinition(name, endpoint)
	def.Operations = make(map[string]declarative.OperationDef)
	if err := addOperations(schema, def, schema.QueryType, false, allowedOps, selectionOverrides); err != nil {
		return nil, err
	}
	if err := addOperations(schema, def, schema.MutationType, true, allowedOps, selectionOverrides); err != nil {
		return nil, err
	}

	if len(def.Operations) == 0 {
		return nil, fmt.Errorf("no operations found for %s", endpoint)
	}

	return def, nil
}

func graphQLAllowedOperations(schema *Schema, allowedOps map[string]*operationexposure.OperationOverride, selectionOverrides map[string]string) map[string]*operationexposure.OperationOverride {
	if len(allowedOps) == 0 {
		return allowedOps
	}

	if !hasGraphQLAllowedOperationConfig(allowedOps) {
		return legacyGraphQLAllowedOperations(schema, allowedOps, selectionOverrides)
	}

	filtered := make(map[string]*operationexposure.OperationOverride)
	for name, override := range allowedOps {
		if override != nil && override.GraphQL != nil {
			filtered[name] = override
		}
	}
	return filtered
}

func legacyGraphQLAllowedOperations(schema *Schema, allowedOps map[string]*operationexposure.OperationOverride, selectionOverrides map[string]string) map[string]*operationexposure.OperationOverride {
	roots := graphQLRootNames(schema)
	filtered := make(map[string]*operationexposure.OperationOverride)
	for name, override := range allowedOps {
		if _, ok := roots[name]; ok {
			filtered[name] = override
			continue
		}
		if _, ok := selectionOverrides[name]; ok {
			filtered[name] = override
		}
	}
	return filtered
}

func graphQLRootNames(schema *Schema) map[string]struct{} {
	roots := make(map[string]struct{})
	addRoot := func(root *TypeName) {
		if root == nil {
			return
		}
		rootType := schema.lookupType(root.Name)
		if rootType == nil {
			return
		}
		for _, field := range rootType.Fields {
			roots[field.Name] = struct{}{}
		}
	}
	addRoot(schema.QueryType)
	addRoot(schema.MutationType)
	return roots
}

func hasGraphQLAllowedOperationConfig(allowedOps map[string]*operationexposure.OperationOverride) bool {
	for _, override := range allowedOps {
		if override != nil && override.GraphQL != nil {
			return true
		}
	}
	return false
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

func addOperations(schema *Schema, def *declarative.Definition, root *TypeName, isMutation bool, allowedOps map[string]*operationexposure.OperationOverride, selectionOverrides map[string]string) error {
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
			if !allowedOperationMatchesRoot(allowedOps[field.Name], isMutation) {
				continue
			}
			if !legacySelectionMatchesRoot(schema, field, allowedOps[field.Name], selectionOverrides[field.Name]) {
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

		query, err := operationQuery(schema, field, isMutation, allowedOps, override, selectionOverrides)
		if err != nil {
			return err
		}

		opDef := declarative.OperationDef{
			Description:  declarative.TruncateDescription(desc),
			AllowedRoles: allowedRoles,
			Tags:         tags,
			Transport:    "graphql",
			Query:        query,
		}

		opDef.Parameters = argsToParams(schema, field.Args)

		def.Operations[opName] = opDef
	}
	return nil
}

func operationQuery(schema *Schema, field Field, isMutation bool, allowedOps map[string]*operationexposure.OperationOverride, override *operationexposure.OperationOverride, selectionOverrides map[string]string) (string, error) {
	if len(allowedOps) == 0 {
		return generateQuery(schema, field, isMutation, selectionOverrides[field.Name]), nil
	}

	selectionSet := ""
	if override != nil && override.GraphQL != nil {
		selectionSet = strings.TrimSpace(override.GraphQL.SelectionSet)
	}
	if selectionSet == "" {
		selectionSet = strings.TrimSpace(selectionOverrides[field.Name])
	}
	if selectionSet == "" && explicitSelectionRequired(schema, field.Type) {
		return "", fmt.Errorf("graphql operation %q requires allowedOperations.%s.graphql.selectionSet", field.Name, field.Name)
	}
	if err := validateExplicitSelectionSet(schema, field.Name, field.Type, selectionSet); err != nil {
		return "", err
	}
	return generateQueryWithExplicitSelection(schema, field, isMutation, selectionSet), nil
}

func validateAllowedOperationRoots(schema *Schema, allowedOps map[string]*operationexposure.OperationOverride, selectionOverrides map[string]string) error {
	if len(allowedOps) == 0 {
		return nil
	}

	roots := map[string]rootAvailability{}
	collectRootFields := func(root *TypeName, isMutation bool) {
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
				availability.mutationRef = field.Type
			} else {
				availability.query = true
				availability.queryRef = field.Type
			}
			roots[field.Name] = availability
		}
	}
	collectRootFields(schema.QueryType, false)
	collectRootFields(schema.MutationType, true)

	names := make([]string, 0, len(allowedOps))
	for name := range allowedOps {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		availability, ok := roots[name]
		if !ok {
			return fmt.Errorf("graphql allowed operation %q is not defined in schema", name)
		}
		operationType, err := graphQLOperationType(allowedOps[name])
		if err != nil {
			return fmt.Errorf("graphql allowed operation %q: %w", name, err)
		}
		switch operationType {
		case graphQLOperationTypeQuery:
			if !availability.query {
				return fmt.Errorf("graphql allowed operation %q declares operationType %q but is not defined on the query root", name, operationType)
			}
		case graphQLOperationTypeMutation:
			if !availability.mutation {
				return fmt.Errorf("graphql allowed operation %q declares operationType %q but is not defined on the mutation root", name, operationType)
			}
		default:
			if availability.query && availability.mutation {
				if legacySelectionDisambiguatesRoot(schema, name, availability, allowedOps[name], selectionOverrides[name]) {
					continue
				}
				return fmt.Errorf("graphql allowed operation %q is defined on both query and mutation roots; set allowedOperations.%s.graphql.operationType", name, name)
			}
		}
	}
	return nil
}

func allowedOperationMatchesRoot(override *operationexposure.OperationOverride, isMutation bool) bool {
	operationType, _ := graphQLOperationType(override)
	switch operationType {
	case graphQLOperationTypeQuery:
		return !isMutation
	case graphQLOperationTypeMutation:
		return isMutation
	default:
		return true
	}
}

func graphQLOperationType(override *operationexposure.OperationOverride) (string, error) {
	if override == nil || override.GraphQL == nil {
		return "", nil
	}
	operationType := strings.ToLower(strings.TrimSpace(override.GraphQL.OperationType))
	switch operationType {
	case "", graphQLOperationTypeQuery, graphQLOperationTypeMutation:
		return operationType, nil
	default:
		return "", fmt.Errorf("unsupported graphql.operationType %q", override.GraphQL.OperationType)
	}
}

func legacySelectionDisambiguatesRoot(schema *Schema, name string, availability rootAvailability, override *operationexposure.OperationOverride, selectionOverride string) bool {
	if override != nil && override.GraphQL != nil {
		return false
	}
	selectionOverride = strings.TrimSpace(selectionOverride)
	if selectionOverride == "" {
		return false
	}
	matches := 0
	if availability.query && validateExplicitSelectionSet(schema, name, availability.queryRef, selectionOverride) == nil {
		matches++
	}
	if availability.mutation && validateExplicitSelectionSet(schema, name, availability.mutationRef, selectionOverride) == nil {
		matches++
	}
	return matches == 1
}

func legacySelectionMatchesRoot(schema *Schema, field Field, override *operationexposure.OperationOverride, selectionOverride string) bool {
	if override != nil && override.GraphQL != nil {
		return true
	}
	selectionOverride = strings.TrimSpace(selectionOverride)
	if selectionOverride == "" {
		return true
	}
	if !rootFieldIsAmbiguous(schema, field.Name) {
		return true
	}
	return validateExplicitSelectionSet(schema, field.Name, field.Type, selectionOverride) == nil
}

func rootFieldIsAmbiguous(schema *Schema, name string) bool {
	if schema == nil {
		return false
	}
	foundQuery := rootHasField(schema, schema.QueryType, name)
	foundMutation := rootHasField(schema, schema.MutationType, name)
	return foundQuery && foundMutation
}

func rootHasField(schema *Schema, root *TypeName, name string) bool {
	if root == nil {
		return false
	}
	rootType := schema.lookupType(root.Name)
	if rootType == nil {
		return false
	}
	return lookupField(rootType, name) != nil
}

func argsToParams(schema *Schema, args []InputValue) []declarative.ParameterDef {
	return argsToParamsWithTypeOverrides(schema, args, nil)
}

func argsToParamsWithTypeOverrides(schema *Schema, args []InputValue, typeOverrides map[string]string) []declarative.ParameterDef {
	if len(args) == 0 {
		return nil
	}
	params := make([]declarative.ParameterDef, 0, len(args))
	for _, arg := range args {
		paramType := graphqlParamType(schema, arg.Type)
		if override := strings.TrimSpace(typeOverrides[arg.Name]); override != "" {
			paramType = declarative.NormalizeType(override)
		}
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
