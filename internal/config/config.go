package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Auth              AuthConfig           `yaml:"auth"`
	Datastore         DatastoreConfig      `yaml:"datastore"`
	Integrations      []string             `yaml:"integrations"`
	IntegrationConfig map[string]yaml.Node `yaml:"integration_config"`
	Server            ServerConfig         `yaml:"server"`
}

type AuthConfig struct {
	Provider string    `yaml:"provider"`
	Config   yaml.Node `yaml:"config"`
}

type DatastoreConfig struct {
	Provider string    `yaml:"provider"`
	Config   yaml.Node `yaml:"config"`
}

type ServerConfig struct {
	Port          int    `yaml:"port"`
	EncryptionKey string `yaml:"encryption_key"`
	DevMode       bool   `yaml:"dev_mode"`
}

func Load(path string) (*Config, error) {
	return LoadWithMapping(path, os.Getenv)
}

func LoadWithMapping(path string, getenv func(string) string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	resolved := os.Expand(string(data), getenv)

	var cfg Config
	if err := yaml.Unmarshal([]byte(resolved), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	applyDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Datastore.Provider == "" {
		cfg.Datastore.Provider = "sqlite"
	}
}

func validate(cfg *Config) error {
	if cfg.Auth.Provider == "" {
		return fmt.Errorf("config validation: auth.provider is required")
	}
	if !cfg.Server.DevMode && cfg.Server.EncryptionKey == "" {
		return fmt.Errorf("config validation: server.encryption_key is required (set server.dev_mode to true to skip)")
	}
	return nil
}
