package config

import (
	"strings"
	"testing"
)

func TestAppRegistryPublishSettingsValidation(t *testing.T) {
	t.Parallel()

	registry, err := NewGCSAppRegistry("gestalt-app-registry")
	if err != nil {
		t.Fatalf("NewGCSAppRegistry: %v", err)
	}
	path := mustWriteConfigFile(t, `
appRegistries:
  toolshed:
    kind: gcs
    gcs:
      bucket: gestalt-app-registry
server:
  appRegistry:
    publish:
      enabled: true
      writableRegistry: toolshed
      uploadLeaseTTL: 30m
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Server.AppRegistry.Publish.Enabled {
		t.Fatal("publish should be enabled")
	}
	if cfg.AppRegistries["toolshed"].GCS.Bucket != registry.GCS.Bucket {
		t.Fatalf("registry bucket = %q", cfg.AppRegistries["toolshed"].GCS.Bucket)
	}

	missingRegistryPath := mustWriteConfigFile(t, `
server:
  appRegistry:
    publish:
      enabled: true
`)
	if _, err := Load(missingRegistryPath); err == nil || !strings.Contains(err.Error(), "writableRegistry") {
		t.Fatalf("Load missing writableRegistry error = %v", err)
	}
}
