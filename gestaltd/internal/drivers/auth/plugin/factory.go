package plugin

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/drivers/componentplugin"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	componentplugin.YAMLConfig `yaml:",inline"`
	CallbackURL                string `yaml:"callbackUrl"`
}

var Factory bootstrap.AuthFactory = func(node yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("plugin auth: parsing config: %w", err)
	}
	prepared, err := componentplugin.PrepareExecution(componentplugin.PrepareParams{
		Kind:                 providermanifestv1.KindAuth,
		Subject:              "plugin auth",
		SourceMissingMessage: "no Go, Rust, or Python auth source package found",
		Config:               cfg.YAMLConfig,
	})
	if err != nil {
		return nil, err
	}
	cfg.YAMLConfig = prepared.YAMLConfig

	callbackURL := cfg.CallbackURL
	if callbackURL == "" && deps.BaseURL != "" {
		callbackURL = deps.BaseURL + config.AuthCallbackPath
	}
	return providerhost.NewExecutableAuthProvider(context.Background(), providerhost.AuthExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Config:       cfg.Config,
		AllowedHosts: cfg.AllowedHosts,
		HostBinary:   cfg.HostBinary,
		Cleanup:      prepared.Cleanup,
		Name:         cfg.Name,
		CallbackURL:  callbackURL,
		SessionKey:   deps.EncryptionKey,
	})
}
