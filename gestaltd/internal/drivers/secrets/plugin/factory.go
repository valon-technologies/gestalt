package plugin

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/drivers/componentplugin"
	"github.com/valon-technologies/gestalt/server/internal/pluginhost"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

var Factory bootstrap.SecretManagerFactory = func(node yaml.Node) (core.SecretManager, error) {
	var cfg componentplugin.YAMLConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("plugin secrets: parsing config: %w", err)
	}
	prepared, err := componentplugin.PrepareExecution(componentplugin.PrepareParams{
		Kind:                 pluginmanifestv1.KindSecrets,
		Subject:              "plugin secrets",
		SourceMissingMessage: "no Go, Rust, or Python secrets source package found",
		Config:               cfg,
	})
	if err != nil {
		return nil, err
	}
	cfg = prepared.YAMLConfig

	return pluginhost.NewExecutableSecretManager(context.Background(), pluginhost.SecretsExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Config:       cfg.Config,
		AllowedHosts: cfg.AllowedHosts,
		HostBinary:   cfg.HostBinary,
		Cleanup:      prepared.Cleanup,
		Name:         cfg.Name,
	})
}
