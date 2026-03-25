package inventory

import (
	_ "embed"

	"gopkg.in/yaml.v3"
)

//go:embed inventory.yaml
var rawYAML []byte

type ProviderSpec struct {
	AuthType       string   `yaml:"auth_type"`
	ConnectionMode string   `yaml:"connection_mode"`
	Operations     []string `yaml:"operations"`
}

type Inventory struct {
	Providers map[string]ProviderSpec `yaml:"providers"`
}

func Load() (*Inventory, error) {
	var inv Inventory
	if err := yaml.Unmarshal(rawYAML, &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}
