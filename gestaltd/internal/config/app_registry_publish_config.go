package config

import (
	"fmt"
	"strings"
	"time"
)

// AppRegistryPublishSettings configures authenticated app-admin stateless publishing.
type AppRegistryPublishSettings struct {
	Enabled           bool     `yaml:"enabled,omitempty"`
	WritableRegistry  string   `yaml:"writableRegistry,omitempty"`
	UploadURLTTL      string   `yaml:"uploadURLTTL,omitempty"`
	MaxArtifacts      int      `yaml:"maxArtifacts,omitempty"`
	MaxArtifactBytes  int64    `yaml:"maxArtifactBytes,omitempty"`
	RequiredPlatforms []string `yaml:"requiredPlatforms,omitempty"`
}

func (c AppRegistryPublishSettings) Limits() (AppRegistryPublishLimits, error) {
	limits := AppRegistryPublishLimits{
		MaxArtifacts:      c.MaxArtifacts,
		MaxArtifactBytes:  c.MaxArtifactBytes,
		RequiredPlatforms: append([]string(nil), c.RequiredPlatforms...),
	}
	if ttl := c.UploadURLTTL; ttl != "" {
		duration, err := ParseDuration(ttl)
		if err != nil {
			return AppRegistryPublishLimits{}, err
		}
		limits.UploadURLTTL = duration
	}
	return limits, nil
}

func validateAppRegistryPublishSettings(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	publish := cfg.Server.AppRegistry.Publish
	if !publish.Enabled {
		return nil
	}
	registryName := strings.TrimSpace(publish.WritableRegistry)
	if registryName == "" {
		return fmt.Errorf("config validation: server.appRegistry.publish.writableRegistry is required when publish is enabled")
	}
	registry, ok := cfg.AppRegistries[registryName]
	if !ok {
		return fmt.Errorf("config validation: server.appRegistry.publish.writableRegistry %q is not configured under appRegistries", registryName)
	}
	if strings.TrimSpace(registry.Kind) != AppRegistryKindGCS {
		return fmt.Errorf("config validation: server.appRegistry.publish.writableRegistry %q must be a GCS app registry", registryName)
	}
	if _, err := registry.StorageURL(); err != nil {
		return fmt.Errorf("config validation: server.appRegistry.publish.writableRegistry %q: %w", registryName, err)
	}
	if _, err := publish.Limits(); err != nil {
		return fmt.Errorf("config validation: server.appRegistry.publish: %w", err)
	}
	return nil
}

type AppRegistryPublishLimits struct {
	UploadURLTTL      time.Duration
	MaxArtifacts      int
	MaxArtifactBytes  int64
	RequiredPlatforms []string
}
