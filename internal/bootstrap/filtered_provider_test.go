package bootstrap

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/config"
)

type stubProviderWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubProviderWithOps) ListOperations() []core.Operation {
	return slices.Clone(s.ops)
}

func TestFilteredProviderListOperations(t *testing.T) {
	t.Parallel()

	inner := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test"},
		ops: []core.Operation{
			{Name: "list_items", Description: "List items"},
			{Name: "get_item", Description: "Get item"},
			{Name: "delete_item", Description: "Delete item"},
		},
	}

	allowed := map[string]*config.OperationOverride{
		"list_items": nil,
		"get_item":   {Description: "Fetch a single item"},
	}

	fp, err := newFilteredProvider(inner, allowed)
	if err != nil {
		t.Fatalf("newFilteredProvider: %v", err)
	}

	ops := fp.ListOperations()
	if len(ops) != 2 {
		t.Fatalf("ListOperations: got %d, want 2", len(ops))
	}
	if ops[0].Name != "list_items" {
		t.Errorf("ops[0].Name = %q, want %q", ops[0].Name, "list_items")
	}
	if ops[1].Name != "get_item" {
		t.Errorf("ops[1].Name = %q, want %q", ops[1].Name, "get_item")
	}
	if ops[1].Description != "Fetch a single item" {
		t.Errorf("ops[1].Description = %q, want %q", ops[1].Description, "Fetch a single item")
	}
}

func TestFilteredProviderAlias(t *testing.T) {
	t.Parallel()

	inner := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test",
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: op}, nil
			},
		},
		ops: []core.Operation{
			{Name: "list_items", Description: "List items"},
			{Name: "get_item", Description: "Get item"},
		},
	}

	allowed := map[string]*config.OperationOverride{
		"list_items": {Alias: "fetch_items"},
	}

	fp, err := newFilteredProvider(inner, allowed)
	if err != nil {
		t.Fatalf("newFilteredProvider: %v", err)
	}

	ops := fp.ListOperations()
	if len(ops) != 1 {
		t.Fatalf("ListOperations: got %d, want 1", len(ops))
	}
	if ops[0].Name != "fetch_items" {
		t.Errorf("ops[0].Name = %q, want %q", ops[0].Name, "fetch_items")
	}

	result, err := fp.Execute(context.Background(), "fetch_items", nil, "")
	if err != nil {
		t.Fatalf("Execute(fetch_items): %v", err)
	}
	if result.Body != "list_items" {
		t.Errorf("Execute forwarded to %q, want %q", result.Body, "list_items")
	}
}

func TestFilteredProviderBlocksDisallowed(t *testing.T) {
	t.Parallel()

	inner := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test"},
		ops: []core.Operation{
			{Name: "list_items"},
			{Name: "delete_item"},
		},
	}

	allowed := map[string]*config.OperationOverride{
		"list_items": nil,
	}

	fp, err := newFilteredProvider(inner, allowed)
	if err != nil {
		t.Fatalf("newFilteredProvider: %v", err)
	}

	result, err := fp.Execute(context.Background(), "delete_item", nil, "")
	if err != nil {
		t.Fatalf("Execute(delete_item): %v", err)
	}
	if result.Status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", result.Status, http.StatusNotFound)
	}
}

func TestFilteredProviderRejectsUnknownOp(t *testing.T) {
	t.Parallel()

	inner := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test"},
		ops:             []core.Operation{{Name: "list_items"}},
	}

	allowed := map[string]*config.OperationOverride{
		"nonexistent": nil,
	}

	_, err := newFilteredProvider(inner, allowed)
	if err == nil {
		t.Fatal("expected error for unknown operation")
	}
}

func TestFilteredProviderAliasCollision(t *testing.T) {
	t.Parallel()

	inner := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test"},
		ops: []core.Operation{
			{Name: "op_a"},
			{Name: "op_b"},
		},
	}

	allowed := map[string]*config.OperationOverride{
		"op_a": {Alias: "same_name"},
		"op_b": {Alias: "same_name"},
	}

	_, err := newFilteredProvider(inner, allowed)
	if err == nil {
		t.Fatal("expected error for alias collision")
	}
}
