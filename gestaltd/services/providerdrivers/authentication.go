package providerdrivers

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	authenticationservice "github.com/valon-technologies/gestalt/server/services/authentication"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	componentprovider.YAMLConfig `yaml:",inline"`
	CallbackURL                  string `yaml:"callbackUrl"`
}

var AuthenticationFactory bootstrap.AuthFactory = func(node yaml.Node, deps bootstrap.Deps) (core.AuthenticationProvider, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("authentication provider: parsing config: %w", err)
	}
	prepared, err := componentprovider.PrepareExecution(componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindAuthentication,
		Subject:              "authentication provider",
		SourceMissingMessage: "no Go, Rust, or Python authentication provider source package found",
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
	return authenticationservice.NewExecutable(context.Background(), authenticationservice.ExecConfig{
		Command:     cfg.Command,
		Args:        cfg.Args,
		Env:         cfg.Env,
		Config:      cfg.Config,
		Egress:      cfg.EgressPolicy(""),
		HostBinary:  cfg.HostBinary,
		Cleanup:     prepared.Cleanup,
		Name:        cfg.Name,
		CallbackURL: callbackURL,
		SessionKey:  deps.EncryptionKey,
	})
}
