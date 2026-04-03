package graphql

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
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
	if createIssue.InputSchema != nil {
		t.Fatalf("createIssue.InputSchema: got %s, want nil so schema synthesis can stay shallow", createIssue.InputSchema)
	}
	if len(createIssue.Parameters) != 1 {
		t.Fatalf("createIssue.Parameters: got %d, want 1", len(createIssue.Parameters))
	}
	if got := createIssue.Parameters[0].Type; got != "object" {
		t.Fatalf("createIssue.Parameters[0].Type = %q, want object", got)
	}
}

func TestLoadDefinitionWithAllowedOps(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newTestSchema())
	defer srv.Close()

	def, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]*config.OperationOverride{
		"teams": {Description: "My custom description"},
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
