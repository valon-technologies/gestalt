package config

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const AppRegistryKindGCS = "gcs"

type AppRegistryAuth string

const AppRegistryAuthGCPADC AppRegistryAuth = "gcpADC"

const (
	defaultGCSAppRegistryPublicURLPrefix = "https://storage.googleapis.com/"
	defaultAppRegistryUnusedRetention    = 72 * time.Hour
	defaultAppRegistryDeployedRetention  = 720 * time.Hour
)

type AppRegistryConfig struct {
	Kind      string                      `yaml:"kind,omitempty"`
	Auth      AppRegistryAuth             `yaml:"auth,omitempty"`
	GCS       *AppRegistryGCSConfig       `yaml:"gcs,omitempty"`
	Retention *AppRegistryRetentionConfig `yaml:"retention,omitempty"`
}

type AppRegistryRetentionConfig struct {
	UnusedRetention   string `yaml:"unusedRetention,omitempty"`
	DeployedRetention string `yaml:"deployedRetention,omitempty"`

	unusedRetentionSet   bool
	deployedRetentionSet bool
}

type AppRegistryGCSConfig struct {
	Bucket string `yaml:"bucket,omitempty"`
}

type appRegistryConfigYAML struct {
	Kind      string                      `yaml:"kind,omitempty"`
	Auth      AppRegistryAuth             `yaml:"auth,omitempty"`
	GCS       *AppRegistryGCSConfig       `yaml:"gcs,omitempty"`
	Retention *AppRegistryRetentionConfig `yaml:"retention,omitempty"`
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
				return fmt.Errorf("app registry publish targets are passed to gestaltd app registry publish via --bucket")
			}
		}
	}
	var raw appRegistryConfigYAML
	if err := decodeYAMLNodeKnownFields(value, &raw); err != nil {
		return err
	}
	c.Kind = raw.Kind
	c.Auth = raw.Auth
	c.GCS = raw.GCS
	c.Retention = raw.Retention
	return nil
}

func (c *AppRegistryRetentionConfig) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	var raw struct {
		UnusedRetention   string `yaml:"unusedRetention,omitempty"`
		DeployedRetention string `yaml:"deployedRetention,omitempty"`
	}
	if err := decodeYAMLNodeKnownFields(value, &raw); err != nil {
		return err
	}
	if strings.TrimSpace(raw.UnusedRetention) != "" {
		c.UnusedRetention = strings.TrimSpace(raw.UnusedRetention)
		c.unusedRetentionSet = true
	}
	if strings.TrimSpace(raw.DeployedRetention) != "" {
		c.DeployedRetention = strings.TrimSpace(raw.DeployedRetention)
		c.deployedRetentionSet = true
	}
	return nil
}

// UnusedRetentionDuration returns the configured unused snapshot retention window.
func (c AppRegistryConfig) UnusedRetentionDuration() (time.Duration, error) {
	if c.Retention == nil || strings.TrimSpace(c.Retention.UnusedRetention) == "" {
		return defaultAppRegistryUnusedRetention, nil
	}
	duration, err := ParseDuration(strings.TrimSpace(c.Retention.UnusedRetention))
	if err != nil {
		return 0, fmt.Errorf("retention.unusedRetention: %w", err)
	}
	return duration, nil
}

// DeployedRetentionDuration returns the configured historical redeploy window.
func (c AppRegistryConfig) DeployedRetentionDuration() (time.Duration, error) {
	if c.Retention == nil || strings.TrimSpace(c.Retention.DeployedRetention) == "" {
		return defaultAppRegistryDeployedRetention, nil
	}
	duration, err := ParseDuration(strings.TrimSpace(c.Retention.DeployedRetention))
	if err != nil {
		return 0, fmt.Errorf("retention.deployedRetention: %w", err)
	}
	return duration, nil
}

// RetentionPolicy returns both retention durations for one registry binding.
func (c AppRegistryConfig) RetentionPolicy() (unused, deployed time.Duration, err error) {
	unused, err = c.UnusedRetentionDuration()
	if err != nil {
		return 0, 0, err
	}
	deployed, err = c.DeployedRetentionDuration()
	if err != nil {
		return 0, 0, err
	}
	return unused, deployed, nil
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
