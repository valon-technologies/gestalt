package azure

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	VaultURL string `yaml:"vault_url"`
	Version  string `yaml:"version"`
}

var Factory bootstrap.SecretManagerFactory = func(node yaml.Node) (core.SecretManager, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("azure secrets: parsing config: %w", err)
	}
	if cfg.VaultURL == "" {
		return nil, fmt.Errorf("azure secrets: vault_url is required")
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azure secrets: creating credentials: %w", err)
	}

	client, err := azsecrets.NewClient(cfg.VaultURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("azure secrets: creating client: %w", err)
	}

	return &Provider{client: client, version: cfg.Version}, nil
}
