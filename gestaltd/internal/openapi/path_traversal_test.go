package openapi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefinitionFromLocalFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "openapi.yaml")
	spec := []byte(`{
		"openapi": "3.0.0",
		"info": {"title": "Local Test", "version": "1.0"},
		"servers": [{"url": "https://api.example.com"}],
		"paths": {
			"/items": {
				"get": {
					"operationId": "list_items",
					"summary": "List items"
				}
			}
		}
	}`)
	if err := os.WriteFile(specPath, spec, 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	def, err := LoadDefinition(context.Background(), "local-test", specPath, nil)
	if err != nil {
		t.Fatalf("LoadDefinition with bare path: %v", err)
	}
	if def.Provider != "local-test" {
		t.Fatalf("Provider = %q", def.Provider)
	}
	if def.DisplayName != "Local Test" {
		t.Fatalf("DisplayName = %q", def.DisplayName)
	}
	if _, ok := def.Operations["list_items"]; !ok {
		t.Fatal("expected list_items operation")
	}
}

func TestLoadDefinitionFromFileSchemeURL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "openapi.yaml")
	spec := []byte(`{
		"openapi": "3.0.0",
		"info": {"title": "File Scheme Test", "version": "1.0"},
		"servers": [{"url": "https://api.example.com"}],
		"paths": {
			"/items": {
				"get": {
					"operationId": "list_items",
					"summary": "List items"
				}
			}
		}
	}`)
	if err := os.WriteFile(specPath, spec, 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	def, err := LoadDefinition(context.Background(), "file-scheme-test", "file://"+specPath, nil)
	if err != nil {
		t.Fatalf("LoadDefinition with file:// URL: %v", err)
	}
	if def.DisplayName != "File Scheme Test" {
		t.Fatalf("DisplayName = %q", def.DisplayName)
	}
}

func TestLoadDefinitionRejectsMissingFile(t *testing.T) {
	t.Parallel()

	_, err := LoadDefinition(context.Background(), "missing", "/nonexistent/openapi.yaml", nil)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
