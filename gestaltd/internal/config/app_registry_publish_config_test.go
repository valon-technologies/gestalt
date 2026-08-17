package config_test

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestAppRegistryPublishSettingsLimits(t *testing.T) {
	t.Parallel()

	settings := config.AppRegistryPublishSettings{
		UploadURLTTL:      "30m",
		MaxArtifacts:      8,
		MaxArtifactBytes:  1024,
		RequiredPlatforms: []string{"linux/amd64"},
	}
	limits, err := settings.PublishLimits()
	if err != nil {
		t.Fatalf("Limits: %v", err)
	}
	if limits.MaxArtifacts != 8 || len(limits.RequiredPlatforms) != 1 {
		t.Fatalf("limits = %#v", limits)
	}
}
