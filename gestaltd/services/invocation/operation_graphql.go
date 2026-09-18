package invocation

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/wundergraph/graphql-go-tools/v2/pkg/ast"
	"github.com/wundergraph/graphql-go-tools/v2/pkg/astparser"
)

// authorizeGraphQLOperation binds selected root fields to cataloged GraphQL
// operations before a raw request reaches a provider. It intentionally fails
// closed on malformed/ambiguous documents and denies a field if any matching
// configured operation is not exposed to this invocation context.
func authorizeGraphQLOperation(ctx context.Context, cat *catalog.Catalog, request GraphQLRequest) error {
	if cat == nil {
		return nil
	}
	configured := make(map[graphqlRootField][]catalog.CatalogOperation)
	unmatchableRestricted := false
	for _, op := range cat.Operations {
		if op.Transport != "graphql" {
			continue
		}
		if op.Query == "" {
			// A restricted GraphQL operation without its execution document cannot
			// be safely matched to a raw request, so fail closed rather than let a
			// field/alias bypass the restriction.
			if len(op.InternalCallers) > 0 || (op.API != nil && !*op.API) {
				unmatchableRestricted = true
			}
			continue
		}
		fields, err := graphqlRootFields(op.Query, op.OperationName)
		if err != nil {
			return fmt.Errorf("%w: configured graphql operation %q: %v", ErrOperationNotFound, op.ID, err)
		}
		for field := range fields {
			configured[field] = append(configured[field], op)
		}
	}
	if unmatchableRestricted {
		return fmt.Errorf("%w: restricted graphql operation has no execution document", ErrOperationNotFound)
	}
	if len(configured) == 0 {
		return nil
	}
	selected, err := graphqlRootFields(request.Document, request.OperationName)
	if err != nil {
		return fmt.Errorf("%w: graphql request: %v", ErrOperationNotFound, err)
	}
	for field := range selected {
		candidates := configured[field]
		if len(candidates) == 0 {
			return fmt.Errorf("%w: graphql root field %q", ErrOperationNotFound, field.name)
		}
		for _, op := range candidates {
			if !OperationExposedOnInvocationSurface(ctx, op) {
				return fmt.Errorf("%w: %q", ErrOperationNotFound, op.ID)
			}
		}
	}
	return nil
}

type graphqlRootField struct {
	kind ast.OperationType
	name string
}

func graphqlRootFields(document, operationName string) (map[graphqlRootField]struct{}, error) {
	if document == "" {
		return nil, fmt.Errorf("document is empty")
	}
	doc, report := astparser.ParseGraphqlDocumentString(document)
	if report.HasErrors() {
		return nil, fmt.Errorf("invalid document: %s", report.Error())
	}
	operationRef, err := selectGraphQLOperation(&doc, operationName)
	if err != nil {
		return nil, err
	}
	fields := make(map[graphqlRootField]struct{})
	visiting := make(map[int]bool)
	operation := doc.OperationDefinitions[operationRef]
	if err := collectGraphQLSelectionFields(&doc, operation.OperationType, operation.SelectionSet, fields, visiting); err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("operation selects no root fields")
	}
	return fields, nil
}

func selectGraphQLOperation(doc *ast.Document, operationName string) (int, error) {
	if operationName != "" {
		for ref := range doc.OperationDefinitions {
			if doc.OperationDefinitionNameString(ref) == operationName {
				return ref, nil
			}
		}
		return 0, fmt.Errorf("operation %q was not found", operationName)
	}
	if len(doc.OperationDefinitions) != 1 {
		return 0, fmt.Errorf("operation name is required when document contains multiple operations")
	}
	return 0, nil
}

func collectGraphQLSelectionFields(doc *ast.Document, operationType ast.OperationType, selectionSet int, fields map[graphqlRootField]struct{}, visiting map[int]bool) error {
	if selectionSet < 0 || selectionSet >= len(doc.SelectionSets) {
		return fmt.Errorf("selection set is invalid")
	}
	for _, selectionRef := range doc.SelectionSets[selectionSet].SelectionRefs {
		if selectionRef < 0 || selectionRef >= len(doc.Selections) {
			return fmt.Errorf("selection is invalid")
		}
		selection := doc.Selections[selectionRef]
		switch selection.Kind {
		case ast.SelectionKindField:
			if selection.Ref < 0 || selection.Ref >= len(doc.Fields) {
				return fmt.Errorf("field selection is invalid")
			}
			name := doc.FieldNameString(selection.Ref)
			if name == "" {
				return fmt.Errorf("field selection has no name")
			}
			fields[graphqlRootField{kind: operationType, name: name}] = struct{}{}
		case ast.SelectionKindInlineFragment:
			if selection.Ref < 0 || selection.Ref >= len(doc.InlineFragments) || !doc.InlineFragments[selection.Ref].HasSelections {
				return fmt.Errorf("inline fragment selection is invalid")
			}
			if err := collectGraphQLSelectionFields(doc, operationType, doc.InlineFragments[selection.Ref].SelectionSet, fields, visiting); err != nil {
				return err
			}
		case ast.SelectionKindFragmentSpread:
			if selection.Ref < 0 || selection.Ref >= len(doc.FragmentSpreads) {
				return fmt.Errorf("fragment spread selection is invalid")
			}
			fragmentName := doc.FragmentSpreadNameBytes(selection.Ref)
			fragmentRef, ok := doc.FragmentDefinitionRef(fragmentName)
			if !ok || fragmentRef < 0 || fragmentRef >= len(doc.FragmentDefinitions) || !doc.FragmentDefinitions[fragmentRef].HasSelections {
				return fmt.Errorf("fragment %q is not defined", string(fragmentName))
			}
			if visiting[fragmentRef] {
				return fmt.Errorf("fragment cycle is not supported")
			}
			visiting[fragmentRef] = true
			if err := collectGraphQLSelectionFields(doc, operationType, doc.FragmentDefinitions[fragmentRef].SelectionSet, fields, visiting); err != nil {
				return err
			}
			delete(visiting, fragmentRef)
		default:
			return fmt.Errorf("selection kind is unsupported")
		}
	}
	return nil
}
