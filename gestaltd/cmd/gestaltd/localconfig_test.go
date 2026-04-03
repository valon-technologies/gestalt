package main

import (
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestGenerateDefaultConfigProducesValidConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath, err := generateDefaultConfig(dir)
	if err != nil {
		t.Fatalf("generateDefaultConfig: %v", err)
	}
	if cfgPath != filepath.Join(dir, "config.yaml") {
		t.Fatalf("config path = %q, want %q", cfgPath, filepath.Join(dir, "config.yaml"))
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load on generated config: %v", err)
	}
	if cfg.Auth.Provider != "none" {
		t.Fatalf("auth.provider = %q, want none", cfg.Auth.Provider)
	}
	if cfg.Datastore.Provider != "sqlite" {
		t.Fatalf("datastore.provider = %q, want sqlite", cfg.Datastore.Provider)
	}
	if cfg.Server.EncryptionKey == "" {
		t.Fatal("expected non-empty encryption key")
	}
}

func TestGenerateDefaultConfigHandlesSpecialCharsInPath(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "path: with #special")
	cfgPath, err := generateDefaultConfig(dir)
	if err != nil {
		t.Fatalf("generateDefaultConfig: %v", err)
	}

	if _, err := config.Load(cfgPath); err != nil {
		t.Fatalf("config.Load on generated config with special chars: %v", err)
	}
}
