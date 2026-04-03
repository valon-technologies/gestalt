package aws

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

const defaultVersionStage = "AWSCURRENT"

type yamlConfig struct {
	Region       string `yaml:"region"`
	VersionStage string `yaml:"version_stage"`
	Endpoint     string `yaml:"endpoint"`
}

var Factory bootstrap.SecretManagerFactory = func(node yaml.Node) (core.SecretManager, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("aws secrets: parsing config: %w", err)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("aws secrets: region is required")
	}
	if cfg.VersionStage == "" {
		cfg.VersionStage = defaultVersionStage
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("aws secrets: loading config: %w", err)
	}

	var clientOpts []func(*secretsmanager.Options)
	if cfg.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *secretsmanager.Options) {
			o.BaseEndpoint = &cfg.Endpoint
		})
	}

	client := secretsmanager.NewFromConfig(awsCfg, clientOpts...)
	return &Provider{client: client, versionStage: cfg.VersionStage}, nil
}
