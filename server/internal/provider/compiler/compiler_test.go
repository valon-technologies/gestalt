package compiler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/provider"
)

func TestCompileUsesPreparedArtifact(t *testing.T) {
	t.Parallel()

	preparedPath := writePreparedDefinition(t, provider.Definition{
		Provider:    "prepared",
		DisplayName: "Prepared Provider",
		Operations: map[string]provider.OperationDef{
			"list_users": {
				Method:      http.MethodGet,
				Path:        "/users",
				Description: "List users",
				Transport:   "rest",
			},
		},
	})

	result, err := Compile(context.Background(), "prepared", APISpec{
		Type: APITypeREST,
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

func TestBuildProviderAppliesRuntimeOverrides(t *testing.T) {
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
				Method:      http.MethodDelete,
				Path:        "/users/{id}",
				Description: "Delete user",
				Transport:   "rest",
			},
			"list_users": {
				Method:      http.MethodGet,
				Path:        "/users",
				Description: "List users",
				Transport:   "rest",
			},
		},
	})

	built, err := BuildProvider(context.Background(), "prepared", config.IntegrationDef{
		DisplayName: "Runtime Provider",
		Description: "Runtime description",
	}, APISpec{
		Type: APITypeREST,
	}, config.ConnectionDef{}, map[string]string{
		"prepared": preparedPath,
	}, nil)
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
	if len(cat.Operations) != 2 {
		t.Fatalf("len(Catalog.Operations) = %d, want %d", len(cat.Operations), 2)
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
