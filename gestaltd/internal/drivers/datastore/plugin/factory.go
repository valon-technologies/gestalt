package plugin

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core/datastore"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/drivers/componentplugin"
	"github.com/valon-technologies/gestalt/server/internal/pluginhost"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

var Factory bootstrap.DatastoreFactory = func(node yaml.Node) (datastore.Datastore, error) {
	var cfg componentplugin.YAMLConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("plugin datastore: parsing config: %w", err)
	}
	prepared, err := componentplugin.PrepareExecution(componentplugin.PrepareParams{
		Kind:                 pluginmanifestv1.KindDatastore,
		Subject:              "plugin datastore",
		SourceMissingMessage: "no Go, Rust, or Python datastore source package found",
		Config:               cfg,
	})
	if err != nil {
		return nil, err
	}
	cfg = prepared.YAMLConfig

	return pluginhost.NewExecutableDatastore(context.Background(), pluginhost.DatastoreExecConfig{
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
