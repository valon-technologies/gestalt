package componentplugin

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"runtime"

	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"gopkg.in/yaml.v3"
)

type YAMLConfig struct {
	Name         string            `yaml:"name"`
	Command      string            `yaml:"command"`
	Args         []string          `yaml:"args"`
	Env          map[string]string `yaml:"env"`
	AllowedHosts []string          `yaml:"allowed_hosts"`
	HostBinary   string            `yaml:"host_binary"`
	ManifestPath string            `yaml:"manifest_path"`
	Config       map[string]any    `yaml:"config"`
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

func PrepareExecution(params PrepareParams) (PreparedConfig, error) {
	cfg := params.Config
	var cleanup func()

	if cfg.Command == "" && cfg.ManifestPath != "" {
		command, args, tempCleanup, err := pluginpkg.SourceComponentExecutionCommand(filepath.Dir(cfg.ManifestPath), params.Kind, runtime.GOOS, runtime.GOARCH)
		if errors.Is(err, pluginpkg.ErrNoSourceComponentPackage) {
			return PreparedConfig{}, fmt.Errorf("%s: %s", params.Subject, params.SourceMissingMessage)
		}
		if err != nil {
			return PreparedConfig{}, fmt.Errorf("%s: prepare synthesized source execution: %w", params.Subject, err)
		}
		execEnv, err := pluginpkg.SourceComponentExecutionEnv(filepath.Dir(cfg.ManifestPath), params.Kind, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return PreparedConfig{}, fmt.Errorf("%s: prepare synthesized source environment: %w", params.Subject, err)
		}
		if len(execEnv) > 0 {
			if cfg.Env == nil {
				cfg.Env = make(map[string]string, len(execEnv))
			}
			maps.Copy(cfg.Env, execEnv)
		}
		cfg.Command = command
		cfg.Args = args
		cleanup = tempCleanup
	}

	if cfg.Command == "" {
		return PreparedConfig{}, fmt.Errorf("%s: command is required", params.Subject)
	}

	return PreparedConfig{
		YAMLConfig: cfg,
		Cleanup:    cleanup,
	}, nil
}
