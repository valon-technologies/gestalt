package graphql

import (
	"reflect"
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
)

func TestStaticAllowedOperationsDefinitionParsesDocument(t *testing.T) {
	t.Parallel()

	document := `
query ListTeams(
  "How many records to return"
  $first: Int = 50
  "Cursor to continue from"
  $after: String
  $filters: TeamFilters
  $ids: [ID!]
  $requiredIds: [ID!]!
  $includeArchived: Boolean!
) {
  teams(first: $first, after: $after, filters: $filters, ids: $ids, requiredIds: $requiredIds, includeArchived: $includeArchived) {
    nodes {
      ...TeamFields
    }
  }
}

fragment TeamFields on Team {
  id
  name
}
`
	def, err := StaticAllowedOperationsDefinition("linear", "https://example.com/graphql", map[string]*operationexposure.OperationOverride{
		"listTeams": {
			Alias:        "teams",
			Description:  "List Linear teams",
			AllowedRoles: []string{"workspace-admin"},
			Tags:         []string{"workspace"},
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				Document:      document,
				OperationName: "ListTeams",
			},
		},
	})
	if err != nil {
		t.Fatalf("StaticAllowedOperationsDefinition: %v", err)
	}
	if def == nil {
		t.Fatal("StaticAllowedOperationsDefinition returned nil definition")
		return
	}

	op := def.Operations["teams"]
	if strings.Contains(op.Query, `"How many records to return"`) || strings.Contains(op.Query, `"Cursor to continue from"`) {
		t.Fatalf("op.Query = %q, want executable document without variable descriptions", op.Query)
	}
	if !strings.Contains(op.Query, "query ListTeams($first: Int = 50, $after: String, $filters: TeamFilters, $ids: [ID!], $requiredIds: [ID!]!, $includeArchived: Boolean!)") {
		t.Fatalf("op.Query = %q, want printed executable document", op.Query)
	}
	if op.OperationName != "ListTeams" {
		t.Fatalf("op.OperationName = %q, want ListTeams", op.OperationName)
	}
	if op.Description != "List Linear teams" {
		t.Fatalf("op.Description = %q, want override", op.Description)
	}

	assertParam(t, op.Parameters, "first", "integer", "How many records to return", false, int64(50))
	assertParam(t, op.Parameters, "after", "string", "Cursor to continue from", false, nil)
	assertParam(t, op.Parameters, "filters", "object", "", false, nil)
	assertParam(t, op.Parameters, "ids", "array", "", false, nil)
	assertParam(t, op.Parameters, "requiredIds", "array", "", true, nil)
	assertParam(t, op.Parameters, "includeArchived", "boolean", "", true, nil)
}

func TestStaticAllowedOperationsDefinitionRequiresOperationNameForMultipleOperations(t *testing.T) {
	t.Parallel()

	_, err := StaticAllowedOperationsDefinition("linear", "https://example.com/graphql", map[string]*operationexposure.OperationOverride{
		"teams": {
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				Document: `
query A { viewer { id } }
query B { teams { nodes { id } } }
`,
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "operationName is required") {
		t.Fatalf("StaticAllowedOperationsDefinition error = %v, want operationName requirement", err)
	}
}

func TestStaticAllowedOperationsDefinitionSelectsConfiguredOperation(t *testing.T) {
	t.Parallel()

	def, err := StaticAllowedOperationsDefinition("linear", "https://example.com/graphql", map[string]*operationexposure.OperationOverride{
		"teams": {
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				Document: `
query A($id: ID!) { viewer(id: $id) { id } }
query B($first: Int) { teams(first: $first) { nodes { id } } }
`,
				OperationName: "B",
			},
		},
	})
	if err != nil {
		t.Fatalf("StaticAllowedOperationsDefinition: %v", err)
	}
	if got := def.Operations["teams"].Parameters; len(got) != 1 || got[0].Name != "first" {
		t.Fatalf("Parameters = %#v, want selected operation B variable", got)
	}
}

func TestStaticAllowedOperationsDefinitionRejectsExternalFragments(t *testing.T) {
	t.Parallel()

	_, err := StaticAllowedOperationsDefinition("linear", "https://example.com/graphql", map[string]*operationexposure.OperationOverride{
		"teams": {
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				Document: `query Teams { teams { nodes { ...TeamFields } } }`,
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `fragment "TeamFields" is not defined in the same document`) {
		t.Fatalf("StaticAllowedOperationsDefinition error = %v, want same-document fragment error", err)
	}
}

func TestStaticAllowedOperationsDefinitionRejectsExternalFragmentsOutsideSelectedOperation(t *testing.T) {
	t.Parallel()

	_, err := StaticAllowedOperationsDefinition("linear", "https://example.com/graphql", map[string]*operationexposure.OperationOverride{
		"teams": {
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				OperationName: "Selected",
				Document: `
query Selected { teams { nodes { id } } }
query Other { teams { nodes { ...MissingFields } } }
`,
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `fragment "MissingFields" is not defined in the same document`) {
		t.Fatalf("StaticAllowedOperationsDefinition error = %v, want same-document fragment error", err)
	}
}

func TestStaticAllowedOperationsDefinitionRejectsExternalFragmentsFromUnusedFragment(t *testing.T) {
	t.Parallel()

	_, err := StaticAllowedOperationsDefinition("linear", "https://example.com/graphql", map[string]*operationexposure.OperationOverride{
		"teams": {
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				Document: `
query Teams { teams { nodes { id } } }
fragment Unused on Team { ...MissingFields }
`,
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `fragment "MissingFields" is not defined in the same document`) {
		t.Fatalf("StaticAllowedOperationsDefinition error = %v, want same-document fragment error", err)
	}
}

func TestStaticAllowedOperationsDefinitionRejectsSubscription(t *testing.T) {
	t.Parallel()

	_, err := StaticAllowedOperationsDefinition("linear", "https://example.com/graphql", map[string]*operationexposure.OperationOverride{
		"updates": {
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				Document: `subscription Updates { updates { id } }`,
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "subscription operations are not supported") {
		t.Fatalf("StaticAllowedOperationsDefinition error = %v, want subscription error", err)
	}
}

func assertParam(t *testing.T, params []declarative.ParameterDef, name, typ, description string, required bool, defaultValue any) {
	t.Helper()
	for _, param := range params {
		if param.Name != name {
			continue
		}
		if param.Type != typ || param.Description != description || param.Required != required || !reflect.DeepEqual(param.Default, defaultValue) {
			t.Fatalf("%s param = %#v, want type=%q description=%q required=%v default=%#v", name, param, typ, description, required, defaultValue)
		}
		return
	}
	t.Fatalf("missing param %q in %#v", name, params)
}
