package config

import (
	"strings"
	"testing"
)

func TestRegistryOnlyAppSourceValidation(t *testing.T) {
	t.Parallel()

	t.Run("accepts configured registry and defaults retries", func(t *testing.T) {
		t.Parallel()
		path := mustWriteConfigFile(t, `
appRegistries:
  toolshed:
    kind: gcs
    gcs:
      bucket: test-registry
apps:
  g-issues:
    source:
      registry: toolshed
server: {}
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := cfg.Apps["g-issues"].Source.Registry; got != "toolshed" {
			t.Fatalf("source.registry = %q", got)
		}
		if got := cfg.Server.AppRegistry.MaxReconcileAttempts; got != DefaultAppRegistryMaxReconcileAttempts {
			t.Fatalf("maxReconcileAttempts = %d", got)
		}
	})

	for _, tc := range []struct {
		name    string
		source  string
		wantErr string
	}{
		{"unknown registry", "registry: missing", `references unknown app registry "missing"`},
		{"mixed source modes", "registry: toolshed\n      path: ./app", "mutually exclusive"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := mustWriteConfigFile(t, `
appRegistries:
  toolshed:
    kind: gcs
    gcs:
      bucket: test-registry
apps:
  g-issues:
    source:
      `+tc.source+`
server: {}
`)
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Load error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestAppRegistryMaxReconcileAttemptsValidation(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"0", "-1"} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			path := mustWriteConfigFile(t, `
server:
  appRegistry:
    maxReconcileAttempts: `+value+`
`)
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), "must be a positive integer") {
				t.Fatalf("Load error = %v", err)
			}
		})
	}
}
