package config

import (
	"strings"
	"testing"
	"time"
)

func TestAppRegistryRetentionDefaults(t *testing.T) {
	t.Parallel()

	registry, err := NewGCSAppRegistry("gestalt-app-registry")
	if err != nil {
		t.Fatalf("NewGCSAppRegistry: %v", err)
	}
	unused, err := registry.UnusedRetentionDuration()
	if err != nil {
		t.Fatalf("UnusedRetentionDuration: %v", err)
	}
	if unused != 72*time.Hour {
		t.Fatalf("unused = %v", unused)
	}
	deployed, err := registry.DeployedRetentionDuration()
	if err != nil {
		t.Fatalf("DeployedRetentionDuration: %v", err)
	}
	if deployed != 720*time.Hour {
		t.Fatalf("deployed = %v", deployed)
	}
}

func TestAppRegistryRetentionValidation(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
appRegistries:
  toolshed:
    kind: gcs
    gcs:
      bucket: gs://gestalt-app-registry
    retention:
      unusedRetention: 48h
      deployedRetention: 30d
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	unused, err := cfg.AppRegistries["toolshed"].UnusedRetentionDuration()
	if err != nil {
		t.Fatalf("UnusedRetentionDuration: %v", err)
	}
	if unused != 48*time.Hour {
		t.Fatalf("unused = %v", unused)
	}
	deployed, err := cfg.AppRegistries["toolshed"].DeployedRetentionDuration()
	if err != nil {
		t.Fatalf("DeployedRetentionDuration: %v", err)
	}
	if deployed != 30*24*time.Hour {
		t.Fatalf("deployed = %v", deployed)
	}
}

func TestAppRegistryRetentionRejectsInvalidDuration(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
appRegistries:
  toolshed:
    kind: gcs
    gcs:
      bucket: gs://gestalt-app-registry
    retention:
      unusedRetention: not-a-duration
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "retention.unusedRetention") {
		t.Fatalf("Load error = %v", err)
	}
}
