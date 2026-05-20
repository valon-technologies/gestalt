package graphql

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/declarative"
	"github.com/valon-technologies/gestalt/server/services/plugins/operationexposure"
)

func StaticAllowedOperationsDefinition(name, endpoint string, allowedOps map[string]*operationexposure.OperationOverride, selectionOverrides map[string]string) (*declarative.Definition, error) {
	graphQLNames := make([]string, 0)
	hasStaticArgs := false
	for opName, override := range allowedOps {
		if override == nil || override.GraphQL == nil {
			continue
		}
		graphQLNames = append(graphQLNames, opName)
		if override.GraphQL.Arguments != nil {
			hasStaticArgs = true
		}
	}
	if !hasStaticArgs {
		return nil, nil
	}
	if len(selectionOverrides) > 0 {
		return nil, fmt.Errorf("graphql %s: operationSelections cannot be combined with static allowedOperations", name)
	}

	sort.Strings(graphQLNames)
	for _, opName := range graphQLNames {
		if allowedOps[opName].GraphQL.Arguments == nil {
			return nil, fmt.Errorf("graphql %s: allowed operation %q must set graphql.arguments to use static GraphQL catalogs", name, opName)
		}
	}

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
	if !isGraphQLName(name) {
		return declarative.OperationDef{}, "", fmt.Errorf("graphql allowed operation %q is not a valid GraphQL field name", name)
	}
	operationType, err := graphQLOperationType(override)
	if err != nil {
		return declarative.OperationDef{}, "", fmt.Errorf("graphql allowed operation %q: %w", name, err)
	}
	if operationType == "" {
		operationType = graphQLOperationTypeQuery
	}

	query, params, err := staticOperationQuery(operationType, name, override.GraphQL)
	if err != nil {
		return declarative.OperationDef{}, "", fmt.Errorf("graphql allowed operation %q: %w", name, err)
	}

	exposedName := name
	if override.Alias != "" {
		exposedName = override.Alias
	}
	return declarative.OperationDef{
		Description:  declarative.TruncateDescription(override.Description),
		AllowedRoles: slices.Clone(override.AllowedRoles),
		Tags:         catalog.MergeTags(override.Tags),
		Transport:    "graphql",
		Query:        query,
		Parameters:   params,
	}, exposedName, nil
}

func staticOperationQuery(operationType, fieldName string, graphQLOp *providermanifestv1.ManifestGraphQLOperation) (string, []declarative.ParameterDef, error) {
	args, typeOverrides, err := staticInputValues(*graphQLOp.Arguments)
	if err != nil {
		return "", nil, err
	}

	selectionSet := strings.TrimSpace(graphQLOp.SelectionSet)
	if _, err := parseSelectionSet(selectionSet); err != nil {
		return "", nil, fmt.Errorf("graphql.selectionSet: %w", err)
	}

	field := Field{Name: fieldName, Args: args}
	query := generateQueryWithExplicitSelection(nil, field, operationType == graphQLOperationTypeMutation, selectionSet)
	return query, argsToParamsWithTypeOverrides(nil, args, typeOverrides), nil
}

func staticInputValues(args []providermanifestv1.ManifestGraphQLArgument) ([]InputValue, map[string]string, error) {
	if len(args) == 0 {
		return nil, nil, nil
	}
	values := make([]InputValue, 0, len(args))
	var typeOverrides map[string]string
	argNames := make(map[string]struct{}, len(args))
	for _, arg := range args {
		if !isGraphQLName(arg.Name) {
			return nil, nil, fmt.Errorf("argument %q is not a valid GraphQL name", arg.Name)
		}
		if _, exists := argNames[arg.Name]; exists {
			return nil, nil, fmt.Errorf("duplicate argument %q", arg.Name)
		}
		argNames[arg.Name] = struct{}{}

		typ, err := parseGraphQLType(arg.Type)
		if err != nil {
			return nil, nil, fmt.Errorf("argument %q type: %w", arg.Name, err)
		}
		values = append(values, InputValue{
			Name:        arg.Name,
			Description: arg.Description,
			Type:        typ,
		})
		if override := strings.TrimSpace(arg.ParameterType); override != "" {
			if typeOverrides == nil {
				typeOverrides = make(map[string]string)
			}
			typeOverrides[arg.Name] = override
		}
	}
	return values, typeOverrides, nil
}

func parseGraphQLType(raw string) (TypeRef, error) {
	p := selectionParser{input: strings.TrimSpace(raw)}
	typ, err := parseGraphQLTypeRef(&p)
	if err != nil {
		return TypeRef{}, err
	}
	p.skipIgnored()
	if !p.eof() {
		return TypeRef{}, fmt.Errorf("unexpected input %q", p.input[p.pos:])
	}
	return typ, nil
}

func parseGraphQLTypeRef(p *selectionParser) (TypeRef, error) {
	p.skipIgnored()
	if p.eof() {
		return TypeRef{}, fmt.Errorf("expected GraphQL type")
	}
	var typ TypeRef
	if p.consume("[") {
		inner, err := parseGraphQLTypeRef(p)
		if err != nil {
			return TypeRef{}, err
		}
		p.skipIgnored()
		if !p.consume("]") {
			return TypeRef{}, fmt.Errorf("missing closing bracket")
		}
		typ = TypeRef{Kind: KindList, OfType: &inner}
	} else {
		name, err := p.readName()
		if err != nil {
			return TypeRef{}, err
		}
		typ = TypeRef{Kind: KindScalar, Name: &name}
	}
	p.skipIgnored()
	if p.consume("!") {
		inner := typ
		typ = TypeRef{Kind: KindNonNull, OfType: &inner}
	}
	return typ, nil
}

func isGraphQLName(value string) bool {
	p := selectionParser{input: value}
	if _, err := p.readName(); err != nil {
		return false
	}
	return p.eof()
}
