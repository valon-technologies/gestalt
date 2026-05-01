package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestUpdateProviderVersionConstraintFindsProviderCollections(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gestalt.yaml")
	if err := os.WriteFile(path, []byte(`
apiVersion: gestaltd.config/v5
providers:
  authentication:
    authn:
      source:
        package: github.com/acme/providers/authn
        version: "1.0.0"
  ui:
    dashboard:
      path: /dashboard
      source:
        package: github.com/acme/providers/dashboard
        version: "1.0.0"
plugins:
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := updateProviderVersionConstraint(path, "", "dashboard", ">= 2.0.0"); err != nil {
		t.Fatalf("update ui version: %v", err)
	}
	if err := updateProviderVersionConstraint(path, providermanifestv1.KindAuthentication, "authn", "~1.4.0"); err != nil {
		t.Fatalf("update authentication version: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Providers.UI["dashboard"].Source.PackageVersionConstraint(); got != ">= 2.0.0" {
		t.Fatalf("ui version = %q, want >= 2.0.0", got)
	}
	if got := cfg.Providers.Authentication["authn"].Source.PackageVersionConstraint(); got != "~1.4.0" {
		t.Fatalf("authentication version = %q, want ~1.4.0", got)
	}
}

func TestUpdateProviderVersionConstraintRequiresKindForAmbiguousName(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gestalt.yaml")
	if err := os.WriteFile(path, []byte(`
apiVersion: gestaltd.config/v5
providers:
  ui:
    shared:
      path: /shared
      source:
        package: github.com/acme/providers/shared-ui
        version: "1.0.0"
plugins:
  shared:
    source:
      package: github.com/acme/providers/shared-plugin
      version: "1.0.0"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	err := updateProviderVersionConstraint(path, "", "shared", "2.0.0")
	if err == nil {
		t.Fatal("update version: expected ambiguity error, got nil")
	}
	if !strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), "--kind") {
		t.Fatalf("update version error = %v, want --kind ambiguity", err)
	}

	if err := updateProviderVersionConstraint(path, providermanifestv1.KindUI, "shared", "2.0.0"); err != nil {
		t.Fatalf("update ui version: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Providers.UI["shared"].Source.PackageVersionConstraint(); got != "2.0.0" {
		t.Fatalf("ui version = %q, want 2.0.0", got)
	}
	if got := cfg.Plugins["shared"].Source.PackageVersionConstraint(); got != "1.0.0" {
		t.Fatalf("plugin version = %q, want 1.0.0", got)
	}
}
