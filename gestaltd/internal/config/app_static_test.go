package config

import (
	"strings"
	"testing"
)

func TestAppStaticMountDefaultsAndNormalization(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Apps: map[string]*ProviderEntry{
			"alpha": {
				Static: &AppStaticConfig{},
			},
			"beta": {
				Static: &AppStaticConfig{Mount: "/beta/"},
			},
		},
	}
	if err := CanonicalizeStructure(cfg); err != nil {
		t.Fatalf("CanonicalizeStructure: %v", err)
	}
	if got, want := cfg.Apps["alpha"].Static.Mount, "/alpha"; got != want {
		t.Fatalf("alpha mount = %q, want %q", got, want)
	}
	if got, want := cfg.Apps["beta"].Static.Mount, "/beta"; got != want {
		t.Fatalf("beta mount = %q, want %q", got, want)
	}
}

func TestAppStaticAndUIBindingConflict(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Apps: map[string]*ProviderEntry{
			"alpha": {
				MountPath: "/alpha",
				Static:    &AppStaticConfig{},
			},
		},
	}
	if err := CanonicalizeStructure(cfg); err != nil {
		t.Fatalf("CanonicalizeStructure: %v", err)
	}
	if err := validateApp(cfg, "alpha", cfg.Apps["alpha"]); err == nil || !strings.Contains(err.Error(), "cannot set both static and ui") {
		t.Fatalf("validateApp error = %v, want static+ui conflict", err)
	}
}

func TestAppStaticMountCollisions(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Providers: ProvidersConfig{
			UI: map[string]*UIEntry{
				"portal": {Path: "/alpha"},
			},
		},
		Apps: map[string]*ProviderEntry{
			"alpha": {
				Static: &AppStaticConfig{Mount: "/alpha"},
			},
		},
	}
	if err := normalizeAppStaticMounts(cfg); err != nil {
		t.Fatalf("normalizeAppStaticMounts: %v", err)
	}
	if err := validateMountedUICollisions(cfg, map[string]struct{}{}); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("validateMountedUICollisions error = %v, want mount collision", err)
	}
}
