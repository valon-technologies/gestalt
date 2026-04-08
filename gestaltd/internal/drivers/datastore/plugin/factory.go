package plugin

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/drivers/componentplugin"
	"github.com/valon-technologies/gestalt/server/internal/pluginhost"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

var Factory bootstrap.DatastoreFactory = func(node yaml.Node, deps bootstrap.Deps) (core.Datastore, error) {
	cfg, err := componentplugin.DecodeYAMLConfig(node, "plugin datastore")
	if err != nil {
		return nil, err
	}
	prepared, err := componentplugin.PrepareExecution(componentplugin.PrepareParams{
		Kind:                 pluginmanifestv1.KindDatastore,
		Subject:              "plugin datastore",
		SourceMissingMessage: "no Go or Rust datastore source package found",
		Config:               cfg,
	})
	if err != nil {
		return nil, err
	}
	cfg = prepared.YAMLConfig
	return pluginhost.NewExecutableDatastore(context.Background(), pluginhost.DatastoreExecConfig{
		Command:       cfg.Command,
		Args:          cfg.Args,
		Env:           cfg.Env,
		Config:        cfg.Config,
		AllowedHosts:  cfg.AllowedHosts,
		HostBinary:    cfg.HostBinary,
		Cleanup:       prepared.Cleanup,
		Name:          cfg.Name,
		EncryptionKey: deps.EncryptionKey,
	})
}
