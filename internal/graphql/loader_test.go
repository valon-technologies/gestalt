package graphql

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func strPtr(s string) *string { return &s }

func newTestSchema() Schema {
	return Schema{
		QueryType:    &TypeName{Name: "Query"},
		MutationType: &TypeName{Name: "Mutation"},
		Types: []FullType{
			{Kind: "OBJECT", Name: "Query", Fields: []Field{
				{
					Name:        "records",
					Description: "List available records",
					Args: []InputValue{
						{Name: "first", Type: TypeRef{Kind: "SCALAR", Name: strPtr("Int")}},
					},
					Type: TypeRef{Kind: "OBJECT", Name: strPtr("RecordConnection")},
				},
				{
					Name:        "record",
					Description: "Fetch a record by ID",
					Args: []InputValue{
						{Name: "id", Type: TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("String")}}},
					},
					Type: TypeRef{Kind: "OBJECT", Name: strPtr("Record")},
				},
				{Name: "viewer", Description: "Get the current viewer", Type: TypeRef{Kind: "OBJECT", Name: strPtr("Viewer")}},
				{Name: "node", Description: "Fetch a recursive node", Type: TypeRef{Kind: "OBJECT", Name: strPtr("Node")}},
			}},
			{Kind: "OBJECT", Name: "Mutation", Fields: []Field{
				{
					Name:        "createRecord",
					Description: "Create a new record",
					Args: []InputValue{
						{Name: "input", Type: TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "INPUT_OBJECT", Name: strPtr("CreateRecordInput")}}},
					},
					Type: TypeRef{Kind: "OBJECT", Name: strPtr("RecordPayload")},
				},
			}},
			{Kind: "OBJECT", Name: "RecordConnection", Fields: []Field{
				{Name: "nodes", Type: TypeRef{Kind: "LIST", OfType: &TypeRef{Kind: "OBJECT", Name: strPtr("Record")}}},
				{Name: "pageInfo", Type: TypeRef{Kind: "OBJECT", Name: strPtr("PageInfo")}},
			}},
			{Kind: "OBJECT", Name: "Record", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: "SCALAR", Name: strPtr("ID")}},
				{Name: "label", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
				{Name: "status", Type: TypeRef{Kind: "OBJECT", Name: strPtr("Status")}},
			}},
			{Kind: "OBJECT", Name: "Status", Fields: []Field{
				{Name: "name", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
				{Name: "details", Type: TypeRef{Kind: "OBJECT", Name: strPtr("StatusDetails")}},
			}},
			{Kind: "OBJECT", Name: "StatusDetails", Fields: []Field{
				{Name: "summary", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
				{Name: "owner", Type: TypeRef{Kind: "OBJECT", Name: strPtr("Owner")}},
			}},
			{Kind: "OBJECT", Name: "Owner", Fields: []Field{
				{Name: "handle", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
			}},
			{Kind: "OBJECT", Name: "PageInfo", Fields: []Field{
				{Name: "hasNextPage", Type: TypeRef{Kind: "SCALAR", Name: strPtr("Boolean")}},
				{Name: "endCursor", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
			}},
			{Kind: "INPUT_OBJECT", Name: "CreateRecordInput", InputFields: []InputValue{
				{Name: "label", Type: TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("String")}}},
				{Name: "externalID", Type: TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("ID")}}},
				{Name: "notes", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
				{Name: "priority", Type: TypeRef{Kind: "ENUM", Name: strPtr("RecordPriority")}},
			}},
			{Kind: "ENUM", Name: "RecordPriority", EnumValues: []EnumValue{
				{Name: "noPriority"}, {Name: "urgent"}, {Name: "high"}, {Name: "medium"}, {Name: "low"},
			}},
			{Kind: "OBJECT", Name: "RecordPayload", Fields: []Field{
				{Name: "success", Type: TypeRef{Kind: "SCALAR", Name: strPtr("Boolean")}},
				{Name: "record", Type: TypeRef{Kind: "OBJECT", Name: strPtr("Record")}},
			}},
			{Kind: "OBJECT", Name: "Viewer", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: "SCALAR", Name: strPtr("ID")}},
				{Name: "email", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
			}},
			{Kind: "OBJECT", Name: "Node", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: "SCALAR", Name: strPtr("ID")}},
				{Name: "parent", Type: TypeRef{Kind: "OBJECT", Name: strPtr("Node")}},
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

func TestLoadDefinitionBuildsGraphQLOperations(t *testing.T) {
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
	if len(def.Operations) != 5 {
		t.Fatalf("Operations: got %d, want 5", len(def.Operations))
	}

	records := def.Operations["records"]
	if records.Transport != "graphql" {
		t.Errorf("records.Transport: got %q, want graphql", records.Transport)
	}
	if records.Query == "" {
		t.Fatal("records.Query should not be empty")
	}
	if records.Description != "List available records" {
		t.Errorf("records.Description: got %q", records.Description)
	}
	if !strings.Contains(records.Query, "query($first: Int)") {
		t.Fatalf("records.Query missing variable declaration: %s", records.Query)
	}
	if !strings.Contains(records.Query, "nodes { id label status { name } }") {
		t.Fatalf("records.Query missing connection selection set: %s", records.Query)
	}
	if !strings.Contains(records.Query, "pageInfo { hasNextPage endCursor }") {
		t.Fatalf("records.Query missing pageInfo selection: %s", records.Query)
	}
	if strings.Contains(records.Query, "details") || strings.Contains(records.Query, "owner") {
		t.Fatalf("records.Query should stop before deeper nested owner fields: %s", records.Query)
	}

	record := def.Operations["record"]
	if !strings.Contains(record.Query, "$id: String!") {
		t.Fatalf("record.Query missing non-null variable: %s", record.Query)
	}
	if !strings.Contains(record.Query, "record(id: $id)") {
		t.Fatalf("record.Query missing argument binding: %s", record.Query)
	}
	if !strings.Contains(record.Query, "status { name details { summary } }") {
		t.Fatalf("record.Query missing nested detail selection: %s", record.Query)
	}
	if strings.Contains(record.Query, "owner") {
		t.Fatalf("record.Query should stop before owner fields: %s", record.Query)
	}

	viewer := def.Operations["viewer"]
	if !strings.HasPrefix(viewer.Query, "query { viewer") {
		t.Fatalf("viewer.Query should be a simple query without variables: %s", viewer.Query)
	}

	node := def.Operations["node"]
	if strings.Contains(node.Query, "parent") {
		t.Fatalf("node.Query should avoid recursive self-selection: %s", node.Query)
	}

	createRecord := def.Operations["createRecord"]
	if !strings.HasPrefix(createRecord.Query, "mutation(") {
		t.Fatalf("createRecord.Query should be a mutation: %s", createRecord.Query)
	}
	if !strings.Contains(createRecord.Query, "$input: CreateRecordInput!") {
		t.Fatalf("createRecord.Query missing formatted input type: %s", createRecord.Query)
	}
	if createRecord.InputSchema != nil {
		t.Fatalf("createRecord.InputSchema: got %s, want nil so schema synthesis stays shallow", createRecord.InputSchema)
	}
	if len(createRecord.Parameters) != 1 {
		t.Fatalf("createRecord.Parameters: got %d, want 1", len(createRecord.Parameters))
	}
	if got := createRecord.Parameters[0].Type; got != "object" {
		t.Fatalf("createRecord.Parameters[0].Type = %q, want object", got)
	}
}

func TestLoadDefinitionWithAllowedOps(t *testing.T) {
	t.Parallel()

	srv := startIntrospectionServer(t, newTestSchema())
	defer srv.Close()

	def, err := LoadDefinition(t.Context(), "test", srv.URL, map[string]string{
		"records": "Custom record listing",
	})
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}

	if len(def.Operations) != 1 {
		t.Fatalf("Operations: got %d, want 1", len(def.Operations))
	}

	records := def.Operations["records"]
	if records.Description != "Custom record listing" {
		t.Errorf("records.Description: got %q, want custom override", records.Description)
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
