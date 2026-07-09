package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAppRegistryConfigGCSURLs(t *testing.T) {
	t.Parallel()

	for _, bucket := range []string{"gestalt-app-registry", "gs://gestalt-app-registry"} {
		t.Run(bucket, func(t *testing.T) {
			t.Parallel()

			registry := AppRegistryConfig{
				Kind: AppRegistryKindGCS,
				GCS:  &AppRegistryGCSConfig{Bucket: bucket},
			}
			storageURL, err := registry.StorageURL()
			if err != nil {
				t.Fatalf("StorageURL: %v", err)
			}
			if storageURL != "gs://gestalt-app-registry" {
				t.Fatalf("StorageURL = %q", storageURL)
			}
			publicURL, err := registry.PublicURL()
			if err != nil {
				t.Fatalf("PublicURL: %v", err)
			}
			if publicURL != "https://storage.googleapis.com/gestalt-app-registry" {
				t.Fatalf("PublicURL = %q", publicURL)
			}
		})
	}
}

func TestAppRegistryConfigRejectsTopLevelURLFields(t *testing.T) {
	t.Parallel()

	var registry AppRegistryConfig
	err := yaml.Unmarshal([]byte(`
kind: gcs
url: gs://gestalt-app-registry
`), &registry)
	if err == nil || !strings.Contains(err.Error(), "gcs.bucket") {
		t.Fatalf("UnmarshalYAML error = %v, want top-level url rejection", err)
	}
}

func TestAppRegistryGCSConfigRejectsPublicURL(t *testing.T) {
	t.Parallel()

	var gcs AppRegistryGCSConfig
	err := yaml.Unmarshal([]byte(`
bucket: gestalt-app-registry
publicUrl: https://storage.example.test/gestalt-app-registry
`), &gcs)
	if err == nil || !strings.Contains(err.Error(), "gcs.publicUrl is not supported") {
		t.Fatalf("UnmarshalYAML error = %v, want gcs.publicUrl rejection", err)
	}
}

func TestAppRegistryConfigRejectsPublishBlock(t *testing.T) {
	t.Parallel()

	var registry AppRegistryConfig
	err := yaml.Unmarshal([]byte(`
kind: gcs
gcs:
  bucket: gestalt-app-registry
publish:
  immutable: true
`), &registry)
	if err == nil || !strings.Contains(err.Error(), "gestaltd app publish") {
		t.Fatalf("UnmarshalYAML error = %v, want publish rejection", err)
	}
}

func TestNewGCSAppRegistry(t *testing.T) {
	t.Parallel()

	for _, bucket := range []string{"gestalt-app-registry", "gs://gestalt-app-registry"} {
		t.Run(bucket, func(t *testing.T) {
			t.Parallel()

			registry, err := NewGCSAppRegistry(bucket)
			if err != nil {
				t.Fatalf("NewGCSAppRegistry: %v", err)
			}
			storageURL, err := registry.StorageURL()
			if err != nil {
				t.Fatalf("StorageURL: %v", err)
			}
			if storageURL != "gs://gestalt-app-registry" {
				t.Fatalf("StorageURL = %q", storageURL)
			}
		})
	}
}

func TestAppRegistryGCSConfigNormalizesBucketOnUnmarshal(t *testing.T) {
	t.Parallel()

	var gcs AppRegistryGCSConfig
	if err := yaml.Unmarshal([]byte(`bucket: gs://gestalt-app-registry`), &gcs); err != nil {
		t.Fatalf("UnmarshalYAML: %v", err)
	}
	if gcs.Bucket != "gestalt-app-registry" {
		t.Fatalf("Bucket = %q", gcs.Bucket)
	}
}

func TestValidateAppRegistriesRequiresGCSBlock(t *testing.T) {
	t.Parallel()

	err := validateAppRegistries(&Config{
		AppRegistries: map[string]AppRegistryConfig{
			"toolshed": {Kind: AppRegistryKindGCS},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "gcs is required") {
		t.Fatalf("validateAppRegistries error = %v", err)
	}
}
