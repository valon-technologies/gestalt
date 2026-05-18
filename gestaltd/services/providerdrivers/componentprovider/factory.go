package componentprovider

import (
	"fmt"

	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/plugins/providerpkg"
	"gopkg.in/yaml.v3"
)

type YAMLConfig struct {
	Name         string            `yaml:"name"`
	Command      string            `yaml:"command"`
	Args         []string          `yaml:"args"`
	Env          map[string]string `yaml:"env"`
	Egress       *YAMLEgressConfig `yaml:"egress,omitempty"`
	HostBinary   string            `yaml:"hostBinary"`
	ManifestPath string            `yaml:"manifestPath"`
	Config       map[string]any    `yaml:"config"`
}

type YAMLEgressConfig struct {
	AllowedHosts []string `yaml:"allowedHosts,omitempty"`
}

type PreparedConfig struct {
	YAMLConfig
	Cleanup func()
}

type PrepareParams struct {
	Kind                 string
	Subject              string
	SourceMissingMessage string
	Config               YAMLConfig
}

func DecodeYAMLConfig(node yaml.Node, subject string) (YAMLConfig, error) {
	var cfg YAMLConfig
	if err := node.Decode(&cfg); err != nil {
		return YAMLConfig{}, fmt.Errorf("%s: parsing config: %w", subject, err)
	}
	return cfg, nil
}

func (c YAMLConfig) EgressPolicy(defaultAction egress.PolicyAction) egress.Policy {
	if c.Egress == nil {
		return egress.Policy{DefaultAction: defaultAction}
	}
	return egress.Policy{
		AllowedHosts:  append([]string(nil), c.Egress.AllowedHosts...),
		DefaultAction: defaultAction,
	}
}

func PrepareExecution(params PrepareParams) (PreparedConfig, error) {
	cfg := params.Config
	var cleanup func()

	if cfg.Command == "" && cfg.ManifestPath != "" {
		command, args, err := providerpkg.SourceManifestExecutionCommand(cfg.ManifestPath, params.Kind, providerpkg.SourceBuildOptions{})
		if err != nil {
			return PreparedConfig{}, fmt.Errorf("%s: prepare source execution: %w", params.Subject, err)
		}
		cfg.Command = command
		cfg.Args = args
	}

	if cfg.Command == "" {
		return PreparedConfig{}, fmt.Errorf("%s: command is required", params.Subject)
	}

	return PreparedConfig{
		YAMLConfig: cfg,
		Cleanup:    cleanup,
	}, nil
}
