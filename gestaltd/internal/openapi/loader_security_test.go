package openapi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchLocalFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "openapi.yaml")
	content := []byte(`{"openapi":"3.0.0","info":{"title":"Test","version":"1.0"},"paths":{}}`)
	if err := os.WriteFile(specPath, content, 0644); err != nil {
		t.Fatalf("write spec file: %v", err)
	}

	got, err := fetch(context.Background(), specPath)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("got %q, want %q", got, content)
	}
}

func TestFetchLocalFileWithFileScheme(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "openapi.yaml")
	content := []byte(`{"openapi":"3.0.0","info":{"title":"Test","version":"1.0"},"paths":{}}`)
	if err := os.WriteFile(specPath, content, 0644); err != nil {
		t.Fatalf("write spec file: %v", err)
	}

	got, err := fetch(context.Background(), "file://"+specPath)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("got %q, want %q", got, content)
	}
}

func TestFetchMissingFileReturnsError(t *testing.T) {
	t.Parallel()

	_, err := fetch(context.Background(), "/nonexistent/path/openapi.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
