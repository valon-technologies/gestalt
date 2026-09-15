package providerdrivers

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	identityservice "github.com/valon-technologies/gestalt/server/services/identity"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	componentprovider.YAMLConfig `yaml:",inline"`
	CallbackURL                  string `yaml:"callbackUrl"`
}

func IdentityFactory(node yaml.Node, deps IdentityDeps) (core.IdentityProvider, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("identity provider: parsing config: %w", err)
	}
	prepared, err := componentprovider.PrepareExecution(componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindIdentity,
		Subject:              "identity provider",
		SourceMissingMessage: "no Go, Rust, Python, or TypeScript identity provider source package found",
		Config:               cfg.YAMLConfig,
	})
	if err != nil {
		return nil, err
	}
	cfg.YAMLConfig = prepared.YAMLConfig

	callbackURL := cfg.CallbackURL
	if callbackURL == "" {
		callbackURL = deps.DefaultCallbackURL
	}
	return identityservice.NewExecutable(context.Background(), identityservice.ExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Workdir:      cfg.Workdir,
		Env:          cfg.Env,
		Config:       cfg.Config,
		Egress:       cfg.EgressPolicy(""),
		HostBinary:   cfg.HostBinary,
		Cleanup:      prepared.Cleanup,
		HostServices: deps.HostServices,
		Name:         cfg.Name,
		CallbackURL:  callbackURL,
	})
}
