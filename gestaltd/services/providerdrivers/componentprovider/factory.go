package componentprovider

import (
	"fmt"
	"maps"

	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"gopkg.in/yaml.v3"
)

type YAMLConfig struct {
	Name         string            `yaml:"name"`
	Command      string            `yaml:"command"`
	Args         []string          `yaml:"args"`
	Workdir      string            `yaml:"workdir"`
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
		execution, err := providerpkg.SourceManifestExecution(cfg.ManifestPath, params.Kind, providerpkg.SourceBuildOptions{})
		if err != nil {
			return PreparedConfig{}, fmt.Errorf("%s: prepare source execution: %w", params.Subject, err)
		}
		cfg.Command = execution.Command
		cfg.Args = execution.Args
		cfg.Workdir = execution.Workdir
		if len(execution.Env) > 0 {
			merged := maps.Clone(execution.Env)
			maps.Copy(merged, cfg.Env)
			cfg.Env = merged
		}
		cleanup = execution.Cleanup
	}

	if cfg.Command == "" {
		return PreparedConfig{}, fmt.Errorf("%s: command is required", params.Subject)
	}

	return PreparedConfig{
		YAMLConfig: cfg,
		Cleanup:    cleanup,
	}, nil
}
