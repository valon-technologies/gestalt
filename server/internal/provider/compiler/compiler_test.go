package compiler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

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

	result, err := Compile(context.Background(), "prepared", APISurface{
		Type: "rest",
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

func TestLoadDefinitionUsesPreparedArtifact(t *testing.T) {
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

	def, err := LoadDefinition(context.Background(), "prepared", APISurface{
		Type: "rest",
	}, map[string]string{
		"prepared": preparedPath,
	})
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}

	if def.Provider != "prepared" {
		t.Fatalf("Definition.Provider = %q, want %q", def.Provider, "prepared")
	}
	if len(def.Operations) != 2 {
		t.Fatalf("len(Operations) = %d, want %d", len(def.Operations), 2)
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
