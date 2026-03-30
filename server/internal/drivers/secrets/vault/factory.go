package vault

import (
	"fmt"

	vaultapi "github.com/hashicorp/vault/api"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

const defaultMountPath = "secret"

type yamlConfig struct {
	Address   string `yaml:"address"`
	Token     string `yaml:"token"`
	MountPath string `yaml:"mount_path"`
	Namespace string `yaml:"namespace"`
}

var Factory bootstrap.SecretManagerFactory = func(node yaml.Node) (core.SecretManager, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("vault secrets: parsing config: %w", err)
	}
	if cfg.Address == "" {
		return nil, fmt.Errorf("vault secrets: address is required")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("vault secrets: token is required")
	}
	if cfg.MountPath == "" {
		cfg.MountPath = defaultMountPath
	}

	vaultCfg := vaultapi.DefaultConfig()
	vaultCfg.Address = cfg.Address

	client, err := vaultapi.NewClient(vaultCfg)
	if err != nil {
		return nil, fmt.Errorf("vault secrets: creating client: %w", err)
	}
	client.SetToken(cfg.Token)

	if cfg.Namespace != "" {
		client.SetNamespace(cfg.Namespace)
	}

	return &Provider{client: client, mountPath: cfg.MountPath}, nil
}
