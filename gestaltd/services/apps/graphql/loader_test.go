package graphql

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
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
				{Name: "displayName", Args: []InputValue{
					{Name: "format", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
				}, Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
				{Name: "comments", Args: []InputValue{
					{Name: "first", Type: TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("Int")}}},
				}, Type: TypeRef{Kind: "OBJECT", Name: strPtr("CommentConnection")}},
			}},
			{Kind: "OBJECT", Name: "CommentConnection", Fields: []Field{
				{Name: "nodes", Type: TypeRef{Kind: "LIST", OfType: &TypeRef{Kind: "OBJECT", Name: strPtr("Comment")}}},
				{Name: "pageInfo", Type: TypeRef{Kind: "OBJECT", Name: strPtr("PageInfo")}},
			}},
			{Kind: "OBJECT", Name: "Comment", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: "SCALAR", Name: strPtr("ID")}},
				{Name: "body", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
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

	def, err := LoadDefinition(t.Context(), "test", srv.URL, nil)
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
	if !strings.Contains(teams.Query, "query($first: Int) { teams") {
		t.Errorf("teams.Query: got %q, want generated teams query", teams.Query)
	}
	if teams.Description != "List all teams" {
		t.Errorf("teams.Description: got %q", teams.Description)
	}

	createIssue := def.Operations["createIssue"]
	if !strings.Contains(createIssue.Query, "mutation($input: CreateIssueInput!) { createIssue") {
		t.Errorf("createIssue.Query: got %q, want generated mutation", createIssue.Query)
	}
}

func TestLoadDefinitionWithAllowedOps(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newTestSchema())
	defer srv.Close()

	def, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
		"teams": {
			Description: "My custom description",
			Tags:        []string{"workspace"},
		},
	})
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
	if got := teams.Parameters; len(got) != 1 || got[0].Name != "first" || got[0].Type != "integer" {
		t.Fatalf("teams.Parameters = %#v, want first integer param", got)
	}
}

func TestLoadDefinitionWithAllowedOpsIgnoresMixedSurfaceEntries(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newTestSchema())
	defer srv.Close()

	def, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
		"teams":     nil,
		"get_issue": nil,
	})
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

func TestLoadDefinitionWithAllowedOpsRejectsAmbiguousDuplicateRootField(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newDuplicateRootFieldSchema())
	defer srv.Close()

	_, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*operationexposure.OperationOverride{
		"projectUpdate": nil,
	})
	if err == nil || !strings.Contains(err.Error(), `use allowedOperations.projectUpdate.graphql.document`) {
		t.Fatalf("LoadDefinition error = %v, want duplicate root field ambiguity", err)
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
	})
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}
	if got := def.Operations["version"].Query; got != "query { version }" {
		t.Fatalf("version query = %q, want scalar query without selection", got)
	}
}

func TestIntrospectionQueryIncludesDeprecatedFields(t *testing.T) {
	t.Parallel()

	if !strings.Contains(IntrospectionRequest().Document, "fields(includeDeprecated: true)") {
		t.Fatal("introspection query should include deprecated fields")
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

	_, err := LoadDefinition(t.Context(), "test", srv.URL, nil)
	if err == nil {
		t.Fatal("expected error for empty schema")
	}
}
