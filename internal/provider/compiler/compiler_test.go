package compiler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/provider"
)

func TestCompileUsesPreparedArtifact(t *testing.T) {
	t.Parallel()

	preparedPath := writePreparedDefinition(t, provider.Definition{
		Provider:    "prepared",
		DisplayName: "Prepared Provider",
		Operations: map[string]provider.OperationDef{
			"list_users": {
				Method:      "GET",
				Path:        "/users",
				Description: "List users",
				Transport:   "rest",
			},
		},
	})

	result, err := Compile(context.Background(), "prepared", config.UpstreamDef{
		Type: config.UpstreamTypeREST,
	}, map[string]string{
		"prepared": preparedPath,
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if result.Definition == nil {
		t.Fatal("expected definition output")
	}
	if result.Catalog == nil {
		t.Fatal("expected catalog output")
	}
	if result.Definition.Provider != "prepared" {
		t.Fatalf("Definition.Provider = %q, want %q", result.Definition.Provider, "prepared")
	}
	if result.Catalog.Name != "prepared" {
		t.Fatalf("Catalog.Name = %q, want %q", result.Catalog.Name, "prepared")
	}
	if len(result.Catalog.Operations) != 1 {
		t.Fatalf("len(Catalog.Operations) = %d, want %d", len(result.Catalog.Operations), 1)
	}
	if result.Catalog.Operations[0].ID != "list_users" {
		t.Fatalf("Catalog.Operations[0].ID = %q, want %q", result.Catalog.Operations[0].ID, "list_users")
	}
}

func TestBuildProviderAppliesRuntimeOverridesAndAllowedOperations(t *testing.T) {
	t.Parallel()

	preparedPath := writePreparedDefinition(t, provider.Definition{
		Provider:    "prepared",
		DisplayName: "Prepared Provider",
		Description: "Prepared description",
		Auth: provider.AuthDef{
			Type: "manual",
		},
		Operations: map[string]provider.OperationDef{
			"delete_user": {
				Method:      "DELETE",
				Path:        "/users/{id}",
				Description: "Delete user",
				Transport:   "rest",
			},
			"list_users": {
				Method:      "GET",
				Path:        "/users",
				Description: "List users",
				Transport:   "rest",
			},
		},
	})

	built, err := BuildProvider(context.Background(), "prepared", config.IntegrationDef{
		DisplayName: "Runtime Provider",
		Description: "Runtime description",
	}, config.UpstreamDef{
		Type: config.UpstreamTypeREST,
		AllowedOperations: config.AllowedOps{
			"list_users": "List only active users",
		},
	}, map[string]string{
		"prepared": preparedPath,
	})
	if err != nil {
		t.Fatalf("BuildProvider: %v", err)
	}

	cp, ok := built.(core.CatalogProvider)
	if !ok {
		t.Fatal("expected built provider to expose a catalog")
	}
	cat := cp.Catalog()
	if cat == nil {
		t.Fatal("expected non-nil catalog")
	}
	if cat.DisplayName != "Runtime Provider" {
		t.Fatalf("Catalog.DisplayName = %q, want %q", cat.DisplayName, "Runtime Provider")
	}
	if cat.Description != "Runtime description" {
		t.Fatalf("Catalog.Description = %q, want %q", cat.Description, "Runtime description")
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("len(Catalog.Operations) = %d, want %d", len(cat.Operations), 1)
	}
	if cat.Operations[0].ID != "list_users" {
		t.Fatalf("Catalog.Operations[0].ID = %q, want %q", cat.Operations[0].ID, "list_users")
	}
	if cat.Operations[0].Description != "List only active users" {
		t.Fatalf("Catalog.Operations[0].Description = %q, want %q", cat.Operations[0].Description, "List only active users")
	}
}

func writePreparedDefinition(t *testing.T, def provider.Definition) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "prepared.json")
	data, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
