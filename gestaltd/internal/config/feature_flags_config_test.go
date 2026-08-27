package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestServerFeatureFlagsConfigNormalizesGCSBucket(t *testing.T) {
	var cfg ServerFeatureFlagsConfig
	if err := yaml.Unmarshal([]byte("gcs:\n  bucket: gs://example-feature-flags/\n"), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := cfg.GCSBucket(); got != "example-feature-flags" {
		t.Fatalf("bucket = %q", got)
	}
}

func TestServerFeatureFlagsConfigRejectsInvalidGCSBucket(t *testing.T) {
	tests := []string{
		"gcs:\n  bucket: ''\n",
		"gcs:\n  bucket: gs://example-feature-flags/agent\n",
		"gcs:\n  bucket: https://storage.googleapis.com/example-feature-flags\n",
		"gcs:\n  unknown: value\n",
	}
	for _, input := range tests {
		var cfg ServerFeatureFlagsConfig
		if err := yaml.Unmarshal([]byte(input), &cfg); err == nil {
			t.Fatalf("unmarshal %q: expected error", strings.TrimSpace(input))
		}
	}
}

func TestServerFeatureFlagsConfigMayBeOmitted(t *testing.T) {
	var cfg ServerFeatureFlagsConfig
	if got := cfg.GCSBucket(); got != "" {
		t.Fatalf("bucket = %q, want empty", got)
	}
}
