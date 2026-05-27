package graphql

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
	"github.com/wundergraph/graphql-go-tools/v2/pkg/ast"
	"github.com/wundergraph/graphql-go-tools/v2/pkg/astparser"
	"github.com/wundergraph/graphql-go-tools/v2/pkg/astprinter"
)

func StaticAllowedOperationsDefinition(name, endpoint string, allowedOps map[string]*operationexposure.OperationOverride) (*declarative.Definition, error) {
	graphQLNames := make([]string, 0)
	for opName, override := range allowedOps {
		if override == nil || override.GraphQL == nil {
			continue
		}
		graphQLNames = append(graphQLNames, opName)
	}
	if len(graphQLNames) == 0 {
		return nil, nil
	}

	sort.Strings(graphQLNames)
	def := StaticDefinition(name, endpoint)
	for _, opName := range graphQLNames {
		override := allowedOps[opName]
		opDef, exposedName, err := staticOperationDefinition(opName, override)
		if err != nil {
			return nil, err
		}
		if _, exists := def.Operations[exposedName]; exists {
			return nil, fmt.Errorf("graphql %s: duplicate exposed operation %q", name, exposedName)
		}
		def.Operations[exposedName] = opDef
	}

	return def, nil
}

func staticOperationDefinition(name string, override *operationexposure.OperationOverride) (declarative.OperationDef, string, error) {
	document, selected, err := parseStaticOperationDocument(name, override.GraphQL.Document, override.GraphQL.OperationName)
	if err != nil {
		return declarative.OperationDef{}, "", fmt.Errorf("graphql allowed operation %q: %w", name, err)
	}

	exposedName := name
	if override.Alias != "" {
		exposedName = override.Alias
	}
	return declarative.OperationDef{
		Description:   declarative.TruncateDescription(override.Description),
		AllowedRoles:  slices.Clone(override.AllowedRoles),
		Tags:          catalog.MergeTags(override.Tags),
		Transport:     "graphql",
		Query:         document,
		OperationName: strings.TrimSpace(override.GraphQL.OperationName),
		Parameters:    selected.parameters,
	}, exposedName, nil
}

type parsedStaticOperation struct {
	parameters []declarative.ParameterDef
}

func parseStaticOperationDocument(operationID, rawDocument, configuredOperationName string) (string, parsedStaticOperation, error) {
	document := strings.TrimSpace(rawDocument)
	if document == "" {
		return "", parsedStaticOperation{}, fmt.Errorf("graphql.document is required")
	}

	doc, report := astparser.ParseGraphqlDocumentString(document)
	if report.HasErrors() {
		return "", parsedStaticOperation{}, fmt.Errorf("graphql.document: %s", report.Error())
	}

	operationRef, err := selectStaticOperation(&doc, strings.TrimSpace(configuredOperationName))
	if err != nil {
		return "", parsedStaticOperation{}, err
	}
	operation := doc.OperationDefinitions[operationRef]
	if operation.OperationType == ast.OperationTypeSubscription {
		return "", parsedStaticOperation{}, fmt.Errorf("subscription operations are not supported")
	}
	if err := validateSameDocumentFragments(&doc); err != nil {
		return "", parsedStaticOperation{}, err
	}

	params, err := staticOperationParameters(&doc, operationRef)
	if err != nil {
		return "", parsedStaticOperation{}, fmt.Errorf("%s variables: %w", operationID, err)
	}
	stripVariableDescriptions(&doc)
	printedDocument, err := astprinter.PrintStringIndent(&doc, "  ")
	if err != nil {
		return "", parsedStaticOperation{}, fmt.Errorf("graphql.document: %w", err)
	}
	return strings.TrimSpace(printedDocument), parsedStaticOperation{parameters: params}, nil
}

func selectStaticOperation(doc *ast.Document, operationName string) (int, error) {
	operationRefs := make([]int, 0, len(doc.RootNodes))
	for _, node := range doc.RootNodes {
		if node.Kind == ast.NodeKindOperationDefinition {
			operationRefs = append(operationRefs, node.Ref)
		}
	}
	if len(operationRefs) == 0 {
		return 0, fmt.Errorf("graphql.document must contain an executable operation")
	}

	if operationName != "" {
		for _, ref := range operationRefs {
			if doc.OperationDefinitionNameString(ref) == operationName {
				return ref, nil
			}
		}
		return 0, fmt.Errorf("graphql.operationName %q is not defined in graphql.document", operationName)
	}

	if len(operationRefs) != 1 {
		return 0, fmt.Errorf("graphql.operationName is required when graphql.document contains %d operations", len(operationRefs))
	}
	return operationRefs[0], nil
}

func validateSameDocumentFragments(doc *ast.Document) error {
	fragments := make(map[string]int, len(doc.FragmentDefinitions))
	for i := range doc.FragmentDefinitions {
		fragments[doc.FragmentDefinitionNameString(i)] = i
	}
	visited := make(map[string]struct{}, len(fragments))
	for i := range doc.OperationDefinitions {
		if err := validateSelectionSetFragments(doc, doc.OperationDefinitions[i].SelectionSet, fragments, visited); err != nil {
			return err
		}
	}
	for i := range doc.FragmentDefinitions {
		if err := validateSelectionSetFragments(doc, doc.FragmentDefinitions[i].SelectionSet, fragments, visited); err != nil {
			return err
		}
	}
	return nil
}

func validateSelectionSetFragments(doc *ast.Document, selectionSet int, fragments map[string]int, visited map[string]struct{}) error {
	if selectionSet < 0 || selectionSet >= len(doc.SelectionSets) {
		return nil
	}
	for _, selectionRef := range doc.SelectionSets[selectionSet].SelectionRefs {
		selection := doc.Selections[selectionRef]
		switch selection.Kind {
		case ast.SelectionKindField:
			field := doc.Fields[selection.Ref]
			if field.HasSelections {
				if err := validateSelectionSetFragments(doc, field.SelectionSet, fragments, visited); err != nil {
					return err
				}
			}
		case ast.SelectionKindInlineFragment:
			fragment := doc.InlineFragments[selection.Ref]
			if fragment.HasSelections {
				if err := validateSelectionSetFragments(doc, fragment.SelectionSet, fragments, visited); err != nil {
					return err
				}
			}
		case ast.SelectionKindFragmentSpread:
			name := doc.FragmentSpreadNameString(selection.Ref)
			fragmentRef, ok := fragments[name]
			if !ok {
				return fmt.Errorf("graphql.document fragment %q is not defined in the same document", name)
			}
			if _, ok := visited[name]; ok {
				continue
			}
			visited[name] = struct{}{}
			if err := validateSelectionSetFragments(doc, doc.FragmentDefinitions[fragmentRef].SelectionSet, fragments, visited); err != nil {
				return err
			}
		}
	}
	return nil
}

func stripVariableDescriptions(doc *ast.Document) {
	for i := range doc.VariableDefinitions {
		doc.VariableDefinitions[i].Description = ast.Description{}
	}
}

func staticOperationParameters(doc *ast.Document, operationRef int) ([]declarative.ParameterDef, error) {
	operation := doc.OperationDefinitions[operationRef]
	if !operation.HasVariableDefinitions {
		return nil, nil
	}

	params := make([]declarative.ParameterDef, 0, len(operation.VariableDefinitions.Refs))
	seen := make(map[string]struct{}, len(operation.VariableDefinitions.Refs))
	for _, variableDefinitionRef := range operation.VariableDefinitions.Refs {
		variableName := doc.VariableDefinitionNameString(variableDefinitionRef)
		if _, ok := seen[variableName]; ok {
			return nil, fmt.Errorf("duplicate variable %q", variableName)
		}
		seen[variableName] = struct{}{}

		variableDefinition := doc.VariableDefinitions[variableDefinitionRef]
		defaultValue, hasDefault, err := staticVariableDefault(doc, variableDefinition)
		if err != nil {
			return nil, fmt.Errorf("%s default value: %w", variableName, err)
		}
		params = append(params, declarative.ParameterDef{
			Name:        variableName,
			Type:        staticVariableParamType(doc, variableDefinition.Type),
			Description: staticDescription(doc, variableDefinition.Description),
			Required:    doc.TypeIsNonNull(variableDefinition.Type) && !hasDefault,
			Default:     defaultValue,
		})
	}
	return params, nil
}

func staticDescription(doc *ast.Document, description ast.Description) string {
	if !description.IsDefined {
		return ""
	}
	return doc.Input.ByteSliceString(description.Content)
}

func staticVariableParamType(doc *ast.Document, typeRef int) string {
	if doc.TypeIsList(typeRef) {
		return "array"
	}

	name := staticNamedType(doc, typeRef)
	switch name {
	case "Int":
		return "integer"
	case "Float":
		return "number"
	case "Boolean":
		return "boolean"
	case "JSON", "JSONObject":
		return "object"
	case "String", "ID", "DateTime", "Date", "URI", "URL", "UUID", "JSONString", "TimelessDate":
		return "string"
	default:
		if strings.HasSuffix(name, "Input") || strings.HasSuffix(name, "Filter") || strings.HasSuffix(name, "Filters") {
			return "object"
		}
		return "string"
	}
}

func staticNamedType(doc *ast.Document, typeRef int) string {
	for typeRef >= 0 {
		typ := doc.Types[typeRef]
		if typ.TypeKind == ast.TypeKindNamed {
			return doc.TypeNameString(typeRef)
		}
		typeRef = typ.OfType
	}
	return ""
}

func staticVariableDefault(doc *ast.Document, variableDefinition ast.VariableDefinition) (any, bool, error) {
	if !variableDefinition.DefaultValue.IsDefined {
		return nil, false, nil
	}
	value, err := staticGraphQLValue(doc, variableDefinition.DefaultValue.Value)
	return value, true, err
}

func staticGraphQLValue(doc *ast.Document, value ast.Value) (any, error) {
	switch value.Kind {
	case ast.ValueKindNull:
		return nil, nil
	case ast.ValueKindString:
		return doc.StringValueContentString(value.Ref), nil
	case ast.ValueKindBoolean:
		return bool(doc.BooleanValue(value.Ref)), nil
	case ast.ValueKindInteger:
		return doc.IntValueAsInt(value.Ref), nil
	case ast.ValueKindFloat:
		raw := doc.Input.ByteSliceString(doc.FloatValues[value.Ref].Raw)
		if doc.FloatValueIsNegative(value.Ref) {
			raw = "-" + raw
		}
		return strconv.ParseFloat(raw, 64)
	case ast.ValueKindEnum:
		return doc.EnumValueNameString(value.Ref), nil
	case ast.ValueKindList:
		out := make([]any, 0, len(doc.ListValues[value.Ref].Refs))
		for _, ref := range doc.ListValues[value.Ref].Refs {
			item, err := staticGraphQLValue(doc, doc.Values[ref])
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	case ast.ValueKindObject:
		out := make(map[string]any, len(doc.ObjectValues[value.Ref].Refs))
		for _, ref := range doc.ObjectValues[value.Ref].Refs {
			item, err := staticGraphQLValue(doc, doc.ObjectFieldValue(ref))
			if err != nil {
				return nil, err
			}
			out[doc.ObjectFieldNameString(ref)] = item
		}
		return out, nil
	case ast.ValueKindVariable:
		return nil, fmt.Errorf("variable references are not supported")
	default:
		data, err := doc.PrintValueBytes(value, nil)
		if err != nil {
			return nil, err
		}
		var out any
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
}
