package config

import "time"

// AppRegistryPublishConfig configures authenticated app-admin publish sessions.
type AppRegistryPublishConfig struct {
	Enabled           bool     `yaml:"enabled,omitempty"`
	UploadLeaseTTL    string   `yaml:"uploadLeaseTTL,omitempty"`
	MaxArtifacts      int      `yaml:"maxArtifacts,omitempty"`
	MaxArtifactBytes  int64    `yaml:"maxArtifactBytes,omitempty"`
	RequiredPlatforms []string `yaml:"requiredPlatforms,omitempty"`
	WritableRegistry  string   `yaml:"writableRegistry,omitempty"`
}

func (c AppRegistryPublishConfig) Limits() (AppRegistryPublishLimits, error) {
	limits := AppRegistryPublishLimits{
		MaxArtifacts:      c.MaxArtifacts,
		MaxArtifactBytes:  c.MaxArtifactBytes,
		RequiredPlatforms: append([]string(nil), c.RequiredPlatforms...),
	}
	if ttl := c.UploadLeaseTTL; ttl != "" {
		duration, err := ParseDuration(ttl)
		if err != nil {
			return AppRegistryPublishLimits{}, err
		}
		limits.UploadLeaseTTL = duration
	}
	return limits, nil
}

type AppRegistryPublishLimits struct {
	UploadLeaseTTL    time.Duration
	MaxArtifacts      int
	MaxArtifactBytes  int64
	RequiredPlatforms []string
}
