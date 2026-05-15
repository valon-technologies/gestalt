package graphql

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/operationexposure"
)

func strPtr(s string) *string { return &s }

func newTestSchema() Schema {
	return Schema{
		QueryType:    &TypeName{Name: "Query"},
		MutationType: &TypeName{Name: "Mutation"},
		Types: []FullType{
			{Kind: "OBJECT", Name: "Query", Fields: []Field{
				{
					Name:        "teams",
					Description: "List all teams",
					Args: []InputValue{
						{Name: "first", Type: TypeRef{Kind: "SCALAR", Name: strPtr("Int")}},
					},
					Type: TypeRef{Kind: "OBJECT", Name: strPtr("TeamConnection")},
				},
				{
					Name:        "issue",
					Description: "Get an issue by ID",
					Args: []InputValue{
						{Name: "id", Type: TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("String")}}},
					},
					Type: TypeRef{Kind: "OBJECT", Name: strPtr("Issue")},
				},
			}},
			{Kind: "OBJECT", Name: "Mutation", Fields: []Field{
				{
					Name:        "createIssue",
					Description: "Create a new issue",
					Args: []InputValue{
						{Name: "input", Type: TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "INPUT_OBJECT", Name: strPtr("CreateIssueInput")}}},
					},
					Type: TypeRef{Kind: "OBJECT", Name: strPtr("IssuePayload")},
				},
			}},
			{Kind: "OBJECT", Name: "TeamConnection", Fields: []Field{
				{Name: "nodes", Type: TypeRef{Kind: "LIST", OfType: &TypeRef{Kind: "OBJECT", Name: strPtr("Team")}}},
				{Name: "pageInfo", Type: TypeRef{Kind: "OBJECT", Name: strPtr("PageInfo")}},
			}},
			{Kind: "OBJECT", Name: "Team", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: "SCALAR", Name: strPtr("ID")}},
				{Name: "name", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
			}},
			{Kind: "OBJECT", Name: "Issue", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: "SCALAR", Name: strPtr("ID")}},
				{Name: "title", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
				{Name: "state", Type: TypeRef{Kind: "OBJECT", Name: strPtr("State")}},
			}},
			{Kind: "OBJECT", Name: "State", Fields: []Field{
				{Name: "name", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
			}},
			{Kind: "OBJECT", Name: "PageInfo", Fields: []Field{
				{Name: "hasNextPage", Type: TypeRef{Kind: "SCALAR", Name: strPtr("Boolean")}},
				{Name: "endCursor", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
			}},
			{Kind: "INPUT_OBJECT", Name: "CreateIssueInput", InputFields: []InputValue{
				{Name: "title", Type: TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("String")}}},
				{Name: "teamId", Type: TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("String")}}},
				{Name: "description", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
				{Name: "priority", Type: TypeRef{Kind: "ENUM", Name: strPtr("IssuePriority")}},
			}},
			{Kind: "ENUM", Name: "IssuePriority", EnumValues: []EnumValue{
				{Name: "noPriority"}, {Name: "urgent"}, {Name: "high"}, {Name: "medium"}, {Name: "low"},
			}},
			{Kind: "OBJECT", Name: "IssuePayload", Fields: []Field{
				{Name: "success", Type: TypeRef{Kind: "SCALAR", Name: strPtr("Boolean")}},
				{Name: "issue", Type: TypeRef{Kind: "OBJECT", Name: strPtr("Issue")}},
			}},
		},
	}
}

func startIntrospectionServer(t *testing.T, schema Schema) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"__schema": schema,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestLoadDefinitionAllOps(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newTestSchema())
	defer srv.Close()

	def, err := LoadDefinition(t.Context(), "test", srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}

	if def.Provider != "test" {
		t.Errorf("Provider: got %q, want test", def.Provider)
	}
	if def.BaseURL != srv.URL {
		t.Errorf("BaseURL: got %q, want %q", def.BaseURL, srv.URL)
	}

	if len(def.Operations) != 3 {
		t.Fatalf("Operations: got %d, want 3 (teams, issue, createIssue)", len(def.Operations))
	}

	teams := def.Operations["teams"]
	if teams.Transport != "graphql" {
		t.Errorf("teams.Transport: got %q, want graphql", teams.Transport)
	}
	if teams.Query == "" {
		t.Error("teams.Query should not be empty")
	}
	if teams.Description != "List all teams" {
		t.Errorf("teams.Description: got %q", teams.Description)
	}

	createIssue := def.Operations["createIssue"]
	if createIssue.Query == "" {
		t.Error("createIssue.Query should not be empty")
	}
}

func TestLoadDefinitionWithAllowedOps(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newTestSchema())
	defer srv.Close()

	def, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
		"teams": {
			Description:  "My custom description",
			AllowedRoles: []string{"workspace-admin"},
			Tags:         []string{"workspace"},
			GraphQL:      &providermanifestv1.ManifestGraphQLOperation{SelectionSet: "nodes { id name } pageInfo { hasNextPage endCursor }"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}

	if len(def.Operations) != 1 {
		t.Fatalf("Operations: got %d, want 1 (only teams)", len(def.Operations))
	}

	teams := def.Operations["teams"]
	if teams.Description != "My custom description" {
		t.Errorf("teams.Description: got %q, want custom override", teams.Description)
	}
	if got, want := teams.Tags, []string{"workspace"}; !slices.Equal(got, want) {
		t.Errorf("teams.Tags: got %#v, want %#v", got, want)
	}
	if got, want := teams.AllowedRoles, []string{"workspace-admin"}; !slices.Equal(got, want) {
		t.Errorf("teams.AllowedRoles: got %#v, want %#v", got, want)
	}
	if strings.Contains(teams.Query, "createdAt") || !strings.Contains(teams.Query, "nodes { id name }") {
		t.Errorf("teams.Query should use allowed operation selectionSet; got: %s", teams.Query)
	}
}

func TestLoadDefinitionWithSelectionOverride(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newTestSchema())
	defer srv.Close()

	def, err := LoadDefinition(t.Context(), "test", srv.URL, nil, map[string]string{
		"createIssue": "success issue { id title }",
	})
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}

	createIssue, ok := def.Operations["createIssue"]
	if !ok {
		t.Fatal("createIssue missing from Operations")
	}
	if !strings.Contains(createIssue.Query, "{ success issue { id title } }") {
		t.Errorf("createIssue.Query should use override selection; got: %s", createIssue.Query)
	}

	teams := def.Operations["teams"]
	if strings.Contains(teams.Query, "success issue") {
		t.Errorf("override should only apply to named op; teams query: %s", teams.Query)
	}
}

func TestLoadDefinitionWithAllowedOpsRejectsLegacySelections(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newTestSchema())
	defer srv.Close()

	_, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
		"teams": {
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{SelectionSet: "nodes { id } pageInfo { hasNextPage endCursor }"},
		},
	}, map[string]string{
		"createIssue": "success",
	})
	if err == nil || !strings.Contains(err.Error(), "operationSelections cannot be combined with allowedOperations") {
		t.Fatalf("LoadDefinition error = %v, want operationSelections conflict", err)
	}
}

func TestLoadDefinitionWithAllowedOpsAllowsLegacySelectionOverrides(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newTestSchema())
	defer srv.Close()

	def, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
		"teams": nil,
	}, map[string]string{
		"teams": "nodes { id name } pageInfo { hasNextPage endCursor }",
	})
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}
	if len(def.Operations) != 1 {
		t.Fatalf("Operations: got %d, want 1", len(def.Operations))
	}
	if got := def.Operations["teams"].Query; !strings.Contains(got, "nodes { id name }") {
		t.Fatalf("teams query = %q, want legacy selection override", got)
	}
}

func TestLoadDefinitionWithAllowedOpsRequiresObjectSelection(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newTestSchema())
	defer srv.Close()

	_, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
		"teams": nil,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "teams.graphql.selectionSet") {
		t.Fatalf("LoadDefinition error = %v, want missing selectionSet", err)
	}
}

func TestLoadDefinitionWithAllowedOpsRejectsUnknownOperation(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newTestSchema())
	defer srv.Close()

	_, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
		"missing": {
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{SelectionSet: "id"},
		},
		"teams": {
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{SelectionSet: "nodes { id name } pageInfo { hasNextPage endCursor }"},
		},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), `allowed operation "missing" is not defined in schema`) {
		t.Fatalf("LoadDefinition error = %v, want unknown allowed operation", err)
	}
}

func TestLoadDefinitionWithAllowedOpsIgnoresNonGraphQLEntriesWhenGraphQLEntriesAreExplicit(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newTestSchema())
	defer srv.Close()

	def, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
		"teams": {
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{SelectionSet: "nodes { id name } pageInfo { hasNextPage endCursor }"},
		},
		"lookup": nil,
	}, nil)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}
	if len(def.Operations) != 1 {
		t.Fatalf("Operations: got %d, want 1", len(def.Operations))
	}
	if _, ok := def.Operations["teams"]; !ok {
		t.Fatalf("Operations = %#v, want teams", def.Operations)
	}
}

func TestLoadDefinitionWithAllowedOpsDisambiguatesDuplicateRootField(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newDuplicateRootFieldSchema())
	defer srv.Close()

	def, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
		"projectUpdate": {
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				OperationType: "mutation",
				SelectionSet:  "success lastSyncId project { id }",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}
	if len(def.Operations) != 1 {
		t.Fatalf("Operations: got %d, want 1", len(def.Operations))
	}
	got := def.Operations["projectUpdate"].Query
	if !strings.HasPrefix(got, "mutation { projectUpdate") {
		t.Fatalf("projectUpdate query = %q, want mutation root", got)
	}
	if !strings.Contains(got, "{ success lastSyncId project { id } }") {
		t.Fatalf("projectUpdate query = %q, want mutation payload selection", got)
	}
}

func TestLoadDefinitionWithAllowedOpsDisambiguatesDuplicateRootFieldFromLegacySelection(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newDuplicateRootFieldSchema())
	defer srv.Close()

	def, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
		"projectUpdate": nil,
	}, map[string]string{
		"projectUpdate": "success lastSyncId project { id }",
	})
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}
	if len(def.Operations) != 1 {
		t.Fatalf("Operations: got %d, want 1", len(def.Operations))
	}
	got := def.Operations["projectUpdate"].Query
	if !strings.HasPrefix(got, "mutation { projectUpdate") {
		t.Fatalf("projectUpdate query = %q, want mutation root", got)
	}
}

func TestLoadDefinitionWithAllowedOpsRejectsAmbiguousDuplicateRootField(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newDuplicateRootFieldSchema())
	defer srv.Close()

	_, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
		"projectUpdate": {
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{SelectionSet: "success lastSyncId project { id }"},
		},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), `defined on both query and mutation roots`) {
		t.Fatalf("LoadDefinition error = %v, want duplicate root field ambiguity", err)
	}
}

func TestLoadDefinitionWithAllowedOpsRejectsUnsupportedOperationType(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newDuplicateRootFieldSchema())
	defer srv.Close()

	_, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
		"projectUpdate": {
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				OperationType: "subscription",
				SelectionSet:  "success lastSyncId project { id }",
			},
		},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), `unsupported graphql.operationType "subscription"`) {
		t.Fatalf("LoadDefinition error = %v, want unsupported operation type", err)
	}
}

func TestLoadDefinitionWithAllowedOpsAllowsScalarWithoutSelection(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, Schema{
		QueryType: &TypeName{Name: "Query"},
		Types: []FullType{
			{Kind: KindObject, Name: "Query", Fields: []Field{
				{Name: "version", Type: TypeRef{Kind: KindScalar, Name: strPtr("String")}},
			}},
		},
	})
	defer srv.Close()

	def, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
		"version": nil,
	}, nil)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}
	if got := def.Operations["version"].Query; got != "query { version }" {
		t.Fatalf("version query = %q, want scalar query without selection", got)
	}
}

func TestLoadDefinitionWithAllowedOpsValidatesSelectionSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		selection string
		wantErr   string
	}{
		{
			name:      "alias is validated by field name",
			selection: "nodes { teamName: name } pageInfo { hasNextPage endCursor }",
		},
		{
			name:      "unknown field",
			selection: "nodes { missing } pageInfo { hasNextPage endCursor }",
			wantErr:   `field "missing" does not exist on type "Team"`,
		},
		{
			name:      "field arguments rejected",
			selection: "nodes { name(format: SHORT) } pageInfo { hasNextPage endCursor }",
			wantErr:   "field arguments are not supported",
		},
		{
			name:      "fragment spreads rejected",
			selection: "nodes { ...TeamFields } pageInfo { hasNextPage endCursor }",
			wantErr:   "fragment spreads are not supported",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := startIntrospectionServer(t, newTestSchema())
			defer srv.Close()

			def, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
				"teams": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{SelectionSet: tc.selection},
				},
			}, nil)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("LoadDefinition error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadDefinition: %v", err)
			}
			if got := def.Operations["teams"].Query; !strings.Contains(got, "teamName: name") {
				t.Fatalf("teams query = %q, want alias selection", got)
			}
		})
	}
}

func TestLoadDefinitionWithAllowedOpsValidatesInlineFragments(t *testing.T) {
	t.Parallel()

	schema := Schema{
		QueryType: &TypeName{Name: "Query"},
		Types: []FullType{
			{Kind: KindObject, Name: "Query", Fields: []Field{
				{Name: "node", Type: TypeRef{Kind: KindInterface, Name: strPtr("Node")}},
				{Name: "search", Type: TypeRef{Kind: KindUnion, Name: strPtr("SearchResult")}},
				{Name: "unresolved", Type: TypeRef{Kind: KindInterface, Name: strPtr("UnresolvedNode")}},
				{Name: "viewer", Type: TypeRef{Kind: KindObject, Name: strPtr("Viewer")}},
			}},
			{Kind: KindInterface, Name: "Node", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: KindScalar, Name: strPtr("ID")}},
			}, PossibleTypes: []TypeName{{Name: "User"}}},
			{Kind: KindInterface, Name: "UnresolvedNode", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: KindScalar, Name: strPtr("ID")}},
			}},
			{Kind: KindUnion, Name: "SearchResult", PossibleTypes: []TypeName{{Name: "User"}, {Name: "Team"}}},
			{Kind: KindObject, Name: "User", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: KindScalar, Name: strPtr("ID")}},
				{Name: "name", Type: TypeRef{Kind: KindScalar, Name: strPtr("String")}},
			}},
			{Kind: KindObject, Name: "Viewer", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: KindScalar, Name: strPtr("ID")}},
				{Name: "name", Type: TypeRef{Kind: KindScalar, Name: strPtr("String")}},
			}},
			{Kind: KindObject, Name: "Team", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: KindScalar, Name: strPtr("ID")}},
			}},
		},
	}

	tests := []struct {
		name      string
		selection string
		wantErr   string
	}{
		{
			name:      "valid inline fragment",
			selection: "__typename id ... on User { name }",
		},
		{
			name:      "valid interface inline fragment with overlapping possible types",
			selection: "__typename ... on Node { id }",
		},
		{
			name:      "invalid inline fragment type",
			selection: "__typename ... on Team { id }",
			wantErr:   `inline fragment type "Team" is not valid for parent type "Node"`,
		},
		{
			name:      "unknown inline fragment field",
			selection: "__typename ... on User { email }",
			wantErr:   `field "email" does not exist on type "User"`,
		},
		{
			name:      "object parent rejects unrelated inline fragment",
			selection: "__typename ... on User { id }",
			wantErr:   `inline fragment type "User" is not valid for parent type "Viewer"`,
		},
		{
			name:      "interface parent rejects fragment when possible types unavailable",
			selection: "__typename ... on User { id }",
			wantErr:   `inline fragment type "User" is not valid for parent type "UnresolvedNode"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := startIntrospectionServer(t, schema)
			defer srv.Close()

			operation := "node"
			if strings.Contains(tc.name, "object parent") {
				operation = "viewer"
			} else if strings.Contains(tc.name, "overlapping possible types") {
				operation = "search"
			} else if strings.Contains(tc.name, "possible types unavailable") {
				operation = "unresolved"
			}
			_, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
				operation: {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{SelectionSet: tc.selection},
				},
			}, nil)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("LoadDefinition error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadDefinition: %v", err)
			}
		})
	}
}

func newDuplicateRootFieldSchema() Schema {
	return Schema{
		QueryType:    &TypeName{Name: "Query"},
		MutationType: &TypeName{Name: "Mutation"},
		Types: []FullType{
			{Kind: KindObject, Name: "Query", Fields: []Field{
				{
					Name: "projectUpdate",
					Type: TypeRef{Kind: KindObject, Name: strPtr("ProjectUpdate")},
				},
			}},
			{Kind: KindObject, Name: "Mutation", Fields: []Field{
				{
					Name: "projectUpdate",
					Type: TypeRef{Kind: KindObject, Name: strPtr("ProjectPayload")},
				},
			}},
			{Kind: KindObject, Name: "ProjectUpdate", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: KindScalar, Name: strPtr("ID")}},
				{Name: "body", Type: TypeRef{Kind: KindScalar, Name: strPtr("String")}},
			}},
			{Kind: KindObject, Name: "ProjectPayload", Fields: []Field{
				{Name: "success", Type: TypeRef{Kind: KindScalar, Name: strPtr("Boolean")}},
				{Name: "lastSyncId", Type: TypeRef{Kind: KindScalar, Name: strPtr("Float")}},
				{Name: "project", Type: TypeRef{Kind: KindObject, Name: strPtr("Project")}},
			}},
			{Kind: KindObject, Name: "Project", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: KindScalar, Name: strPtr("ID")}},
			}},
		},
	}
}

func TestLoadDefinitionEmptySchemaErrors(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, Schema{
		QueryType: &TypeName{Name: "Query"},
		Types: []FullType{
			{Kind: "OBJECT", Name: "Query", Fields: []Field{}},
		},
	})
	defer srv.Close()

	_, err := LoadDefinition(t.Context(), "test", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("expected error for empty schema")
	}
}
