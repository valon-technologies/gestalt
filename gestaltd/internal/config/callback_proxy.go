package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type CallbackProxyConfig struct {
	Server ServerConfig `yaml:"server"`
}

type callbackProxyConfigFile struct {
	Server    ServerConfig `yaml:"server"`
	Providers yaml.Node    `yaml:"providers,omitempty"`
}

func LoadCallbackProxy(path string) (*CallbackProxyConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	resolved, firstMissing, err := expandEnvVariables(string(data), os.LookupEnv, true)
	if err != nil {
		return nil, err
	}
	if firstMissing != "" {
		return nil, fmt.Errorf("expanding config environment variables: environment variable %q not set; use ${%s:-} to allow an empty default", firstMissing, firstMissing)
	}

	var raw callbackProxyConfigFile
	dec := yaml.NewDecoder(strings.NewReader(resolved))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	cfg := CallbackProxyConfig{Server: raw.Server}
	cfg.Server.BaseURL = strings.TrimRight(cfg.Server.BaseURL, "/")
	cfg.Server.IntegrationCallbackBaseURL = strings.TrimRight(cfg.Server.IntegrationCallbackBaseURL, "/")
	if err := validateServerListeners(cfg.Server); err != nil {
		return nil, err
	}
	if cfg.Server.EncryptionKey == "" {
		return nil, fmt.Errorf("config validation: server.encryptionKey is required")
	}
	if strings.HasPrefix(cfg.Server.EncryptionKey, "secret://") {
		return nil, fmt.Errorf("config validation: callback proxy requires server.encryptionKey to be resolved before startup; use an environment-backed value instead of %q", cfg.Server.EncryptionKey)
	}

	return &cfg, nil
}
