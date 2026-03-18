package gcp

import (
	"context"
	"fmt"
	"time"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	Project string `yaml:"project"`
	Version string `yaml:"version"`
}

var Factory bootstrap.SecretManagerFactory = func(node yaml.Node) (core.SecretManager, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("gcp secrets: parsing config: %w", err)
	}
	if cfg.Project == "" {
		return nil, fmt.Errorf("gcp secrets: project is required")
	}
	if cfg.Version == "" {
		cfg.Version = "latest"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcp secrets: creating client: %w", err)
	}
	return &Provider{client: client, project: cfg.Project, version: cfg.Version}, nil
}
