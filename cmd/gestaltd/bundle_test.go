package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectLocalRefs_IncludesPluginPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "my-ui")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `integrations:
  console:
    plugin:
      package: ./my-ui
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	refs, err := collectLocalRefs(cfgPath)
	if err != nil {
		t.Fatalf("collectLocalRefs: %v", err)
	}

	found := false
	for _, ref := range refs {
		if ref == pluginDir {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %q in refs, got: %v", pluginDir, refs)
	}
}

func TestCollectLocalRefs_IncludesRuntimePluginPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "my-runtime")
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `runtimes:
  worker:
    plugin:
      package: ./my-runtime
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	refs, err := collectLocalRefs(cfgPath)
	if err != nil {
		t.Fatalf("collectLocalRefs: %v", err)
	}

	found := false
	for _, ref := range refs {
		if ref == runtimeDir {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %q in refs, got: %v", runtimeDir, refs)
	}
}

func TestCollectLocalRefs_ExcludesHTTPSPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `integrations:
  console:
    plugin:
      package: https://releases.example.com/pkg.tar.gz
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	refs, err := collectLocalRefs(cfgPath)
	if err != nil {
		t.Fatalf("collectLocalRefs: %v", err)
	}

	if len(refs) != 0 {
		t.Fatalf("expected no local refs for HTTPS package, got: %v", refs)
	}
}

func TestCollectLocalRefs_IncludesIconFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	iconPath := filepath.Join(dir, "assets", "icon.svg")
	if err := os.MkdirAll(filepath.Dir(iconPath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(iconPath, []byte("<svg/>"), 0644); err != nil {
		t.Fatalf("WriteFile icon: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `integrations:
  console:
    icon_file: ./assets/icon.svg
    plugin:
      command: /usr/bin/console
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	refs, err := collectLocalRefs(cfgPath)
	if err != nil {
		t.Fatalf("collectLocalRefs: %v", err)
	}

	found := false
	for _, ref := range refs {
		if ref == iconPath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %q in refs, got: %v", iconPath, refs)
	}
}

func TestCollectLocalRefs_ErrorsOnMissingPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `integrations:
  console:
    plugin:
      package: ./nonexistent-dir
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := collectLocalRefs(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing plugin package directory")
	}
}

func TestComputeSourceRoot_CommonAncestor(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "configs", "gestalt.yaml")
	pluginPath := filepath.Join(dir, "plugins", "ui")

	root, err := computeSourceRoot(cfgPath, []string{pluginPath})
	if err != nil {
		t.Fatalf("computeSourceRoot: %v", err)
	}
	if root != dir {
		t.Fatalf("root = %q, want %q", root, dir)
	}
}
