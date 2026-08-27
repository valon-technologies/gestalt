package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type ServerFeatureFlagsConfig struct {
	GCS *FeatureFlagsGCSConfig `yaml:"gcs,omitempty"`
}

type FeatureFlagsGCSConfig struct {
	Bucket string `yaml:"bucket,omitempty"`
}

func (c *ServerFeatureFlagsConfig) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	var raw struct {
		GCS *FeatureFlagsGCSConfig `yaml:"gcs,omitempty"`
	}
	if err := decodeYAMLNodeKnownFields(value, &raw); err != nil {
		return err
	}
	c.GCS = raw.GCS
	return nil
}

func (c *FeatureFlagsGCSConfig) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	var raw struct {
		Bucket string `yaml:"bucket,omitempty"`
	}
	if err := decodeYAMLNodeKnownFields(value, &raw); err != nil {
		return err
	}
	bucket, err := normalizeGCSBucket(raw.Bucket)
	if err != nil {
		return fmt.Errorf("bucket: %w", err)
	}
	c.Bucket = bucket
	return nil
}

func (c ServerFeatureFlagsConfig) GCSBucket() string {
	if c.GCS == nil {
		return ""
	}
	return strings.TrimSpace(c.GCS.Bucket)
}
