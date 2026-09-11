package server

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestResolveUIReadinessSettingsDefaultsDisabled(t *testing.T) {
	settings := resolveUIReadinessSettings(&config.Config{}, "1.2.3", "sha-a")
	if settings.enabled {
		t.Fatal("ui readiness should default to disabled")
	}
	if settings.releaseID != "1.2.3@sha-a" {
		t.Fatalf("releaseID = %q, want 1.2.3@sha-a", settings.releaseID)
	}
}

func TestResolveUIReadinessSettingsHonorsConfig(t *testing.T) {
	enabled := true
	cfg := &config.Config{
		Server: config.ServerConfig{
			UIReadiness: &config.UIReadinessConfig{
				Enabled:         &enabled,
				ReleaseID:       "digest-a",
				ExtraProbePaths: []string{"/api/v1/apps/sample/operations"},
			},
		},
	}
	settings := resolveUIReadinessSettings(cfg, "1.2.3", "sha-a")
	if !settings.enabled {
		t.Fatal("ui readiness should be enabled from config")
	}
	if settings.releaseID != "digest-a" {
		t.Fatalf("releaseID = %q, want digest-a", settings.releaseID)
	}
	if len(settings.extraProbePaths) != 1 || settings.extraProbePaths[0] != "/api/v1/apps/sample/operations" {
		t.Fatalf("extraProbePaths = %#v", settings.extraProbePaths)
	}
}
