package plugin

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/drivers/componentplugin"
	"github.com/valon-technologies/gestalt/server/internal/pluginhost"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

var Factory bootstrap.IndexedDBFactory = func(node yaml.Node) (indexeddb.IndexedDB, error) {
	var cfg componentplugin.YAMLConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("plugin datastore: parsing config: %w", err)
	}
	prepared, err := componentplugin.PrepareExecution(componentplugin.PrepareParams{
		Kind:                 pluginmanifestv1.KindIndexedDB,
		Subject:              "plugin datastore",
		SourceMissingMessage: "no Go, Rust, or Python datastore source package found",
		Config:               cfg,
	})
	if err != nil {
		return nil, err
	}
	cfg = prepared.YAMLConfig

	return pluginhost.NewExecutableIndexedDB(context.Background(), pluginhost.IndexedDBExecConfig{
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
