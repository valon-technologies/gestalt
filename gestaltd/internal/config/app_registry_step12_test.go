package config

import (
	"strings"
	"testing"
	"time"
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

func TestAppRegistryAuthValidation(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
appRegistries:
  toolshed:
    kind: gcs
    auth: gcpADC
    gcs:
      bucket: private-registry
server: {}
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.AppRegistries["toolshed"].Auth; got != AppRegistryAuthGCPADC {
		t.Fatalf("auth = %q, want %q", got, AppRegistryAuthGCPADC)
	}

	path = mustWriteConfigFile(t, `
appRegistries:
  toolshed:
    kind: gcs
    auth: signedUrl
    gcs:
      bucket: private-registry
server: {}
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), `auth must be "gcpADC" when set`) {
		t.Fatalf("Load error = %v, want unsupported auth error", err)
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

func TestAppRegistryAutoDeployPollInterval(t *testing.T) {
	t.Parallel()

	t.Run("defaults to one minute", func(t *testing.T) {
		t.Parallel()
		path := mustWriteConfigFile(t, `server: {}`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		got, err := cfg.Server.AppRegistry.AutoDeployPollIntervalDuration()
		if err != nil {
			t.Fatalf("AutoDeployPollIntervalDuration: %v", err)
		}
		if got != time.Minute {
			t.Fatalf("poll interval = %v, want %v", got, time.Minute)
		}
	})

	t.Run("accepts explicit duration", func(t *testing.T) {
		t.Parallel()
		path := mustWriteConfigFile(t, `
server:
  appRegistry:
    autoDeployPollInterval: 30s
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		got, err := cfg.Server.AppRegistry.AutoDeployPollIntervalDuration()
		if err != nil {
			t.Fatalf("AutoDeployPollIntervalDuration: %v", err)
		}
		if got != 30*time.Second {
			t.Fatalf("poll interval = %v, want 30s", got)
		}
	})

	for _, value := range []string{"0s", "-1s", "not-a-duration"} {
		value := value
		t.Run("rejects "+value, func(t *testing.T) {
			t.Parallel()
			path := mustWriteConfigFile(t, `
server:
  appRegistry:
    autoDeployPollInterval: `+value+`
`)
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), "server.appRegistry.autoDeployPollInterval") {
				t.Fatalf("Load error = %v, want field-qualified validation error", err)
			}
		})
	}
}
