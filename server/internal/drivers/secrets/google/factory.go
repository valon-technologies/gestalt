package google

import (
	"context"
	"fmt"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	Project string `yaml:"project"`
	Version string `yaml:"version"`
}

var Factory bootstrap.SecretManagerFactory = func(node yaml.Node) (core.SecretManager, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("google secrets: parsing config: %w", err)
	}
	if cfg.Project == "" {
		return nil, fmt.Errorf("google secrets: project is required")
	}
	if cfg.Version == "" {
		cfg.Version = "latest"
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("google secrets: creating client: %w", err)
	}
	return &Provider{client: client, project: cfg.Project, version: cfg.Version}, nil
}
