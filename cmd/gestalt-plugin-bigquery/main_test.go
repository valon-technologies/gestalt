package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/core"
)

func TestProviderMetadata(t *testing.T) {
	t.Parallel()
	p := &bigQueryProvider{}

	if got := p.Name(); got != "bigquery" {
		t.Errorf("Name() = %q, want %q", got, "bigquery")
	}
	if got := p.DisplayName(); got != "BigQuery Query" {
		t.Errorf("DisplayName() = %q, want %q", got, "BigQuery Query")
	}
	if got := p.Description(); got != "BigQuery SQL query execution" {
		t.Errorf("Description() = %q, want %q", got, "BigQuery SQL query execution")
	}
	if got := p.ConnectionMode(); got != core.ConnectionModeUser {
		t.Errorf("ConnectionMode() = %q, want %q", got, core.ConnectionModeUser)
	}
}

func TestListOperations(t *testing.T) {
	t.Parallel()
	p := &bigQueryProvider{}
	ops := p.ListOperations()

	if len(ops) != 1 {
		t.Fatalf("ListOperations() returned %d operations, want 1", len(ops))
	}

	op := ops[0]
	if op.Name != "query" {
		t.Errorf("operation name = %q, want %q", op.Name, "query")
	}
	if op.Method != http.MethodPost {
		t.Errorf("operation method = %q, want %q", op.Method, http.MethodPost)
	}

	requiredParams := map[string]bool{
		"project_id": true,
		"query":      true,
	}
	for _, param := range op.Parameters {
		if expected, ok := requiredParams[param.Name]; ok && param.Required != expected {
			t.Errorf("parameter %q: Required = %v, want %v", param.Name, param.Required, expected)
		}
	}
}

func TestExecuteMissingProjectID(t *testing.T) {
	t.Parallel()
	p := &bigQueryProvider{}
	params := map[string]any{
		"query": "SELECT 1",
	}

	_, err := p.Execute(context.Background(), "query", params, "dummy-token")
	if err == nil {
		t.Fatal("expected error for missing project_id, got nil")
	}
}

func TestExecuteMissingQuery(t *testing.T) {
	t.Parallel()
	p := &bigQueryProvider{}
	params := map[string]any{
		"project_id": "test-project",
	}

	_, err := p.Execute(context.Background(), "query", params, "dummy-token")
	if err == nil {
		t.Fatal("expected error for missing query, got nil")
	}
}

func TestExecuteUnknownOperation(t *testing.T) {
	t.Parallel()
	p := &bigQueryProvider{}
	_, err := p.Execute(context.Background(), "nonexistent", nil, "dummy-token")
	if err == nil {
		t.Fatal("expected error for unknown operation, got nil")
	}
}
