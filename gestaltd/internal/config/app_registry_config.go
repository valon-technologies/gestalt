package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const AppRegistryKindGCS = "gcs"

const defaultGCSAppRegistryPublicURLPrefix = "https://storage.googleapis.com/"

type AppRegistryConfig struct {
	Kind string                `yaml:"kind,omitempty"`
	GCS  *AppRegistryGCSConfig `yaml:"gcs,omitempty"`
}

type AppRegistryGCSConfig struct {
	Bucket string `yaml:"bucket,omitempty"`
}

type appRegistryConfigYAML struct {
	Kind string                `yaml:"kind,omitempty"`
	GCS  *AppRegistryGCSConfig `yaml:"gcs,omitempty"`
}

func (c *AppRegistryConfig) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	if value.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(value.Content); i += 2 {
			switch value.Content[i].Value {
			case "url":
				return fmt.Errorf("app registry url must be set as gcs.bucket")
			case "publicUrl":
				return fmt.Errorf("app registry publicUrl is derived from gcs.bucket")
			case "publish":
				return fmt.Errorf("app registry publish targets are passed to gestaltd app publish via --bucket")
			}
		}
	}
	var raw appRegistryConfigYAML
	if err := decodeYAMLNodeKnownFields(value, &raw); err != nil {
		return err
	}
	c.Kind = raw.Kind
	c.GCS = raw.GCS
	return nil
}

func (c *AppRegistryGCSConfig) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	if value.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(value.Content); i += 2 {
			if value.Content[i].Value == "publicUrl" {
				return fmt.Errorf("gcs.publicUrl is not supported; public URLs are derived from gcs.bucket")
			}
		}
	}
	var raw struct {
		Bucket string `yaml:"bucket,omitempty"`
	}
	if err := decodeYAMLNodeKnownFields(value, &raw); err != nil {
		return err
	}
	c.Bucket = raw.Bucket
	if c.Bucket != "" {
		normalized, err := normalizeGCSAppRegistryBucket(c.Bucket)
		if err != nil {
			return fmt.Errorf("gcs.bucket: %w", err)
		}
		c.Bucket = normalized
	}
	return nil
}

func normalizeGCSAppRegistryBucket(raw string) (string, error) {
	bucket := strings.TrimSpace(raw)
	if bucket == "" {
		return "", fmt.Errorf("bucket is required")
	}
	bucket = strings.TrimPrefix(bucket, "gs://")
	bucket = strings.Trim(bucket, "/")
	if bucket == "" {
		return "", fmt.Errorf("bucket is required")
	}
	if strings.Contains(bucket, "://") {
		return "", fmt.Errorf("must be a bucket name or gs:// URL")
	}
	if strings.Contains(bucket, "/") {
		return "", fmt.Errorf("must not include path segments")
	}
	return bucket, nil
}

func (c AppRegistryConfig) StorageURL() (string, error) {
	if strings.TrimSpace(c.Kind) != AppRegistryKindGCS {
		return "", fmt.Errorf("unsupported app registry kind %q", c.Kind)
	}
	if c.GCS == nil {
		return "", fmt.Errorf("gcs config is required")
	}
	bucket, err := normalizeGCSAppRegistryBucket(c.GCS.Bucket)
	if err != nil {
		return "", fmt.Errorf("gcs.bucket: %w", err)
	}
	return "gs://" + bucket, nil
}

func (c AppRegistryConfig) PublicURL() (string, error) {
	storageURL, err := c.StorageURL()
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(storageURL, "gs://") {
		return "", fmt.Errorf("invalid storage URL %q", storageURL)
	}
	bucket := strings.TrimPrefix(storageURL, "gs://")
	return strings.TrimRight(defaultGCSAppRegistryPublicURLPrefix+bucket, "/"), nil
}

func NewGCSAppRegistry(bucket string) (AppRegistryConfig, error) {
	registry := AppRegistryConfig{
		Kind: AppRegistryKindGCS,
		GCS:  &AppRegistryGCSConfig{Bucket: strings.TrimSpace(bucket)},
	}
	if _, err := registry.StorageURL(); err != nil {
		return AppRegistryConfig{}, err
	}
	return registry, nil
}
