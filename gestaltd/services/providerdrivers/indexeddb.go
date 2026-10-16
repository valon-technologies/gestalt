package providerdrivers

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"gopkg.in/yaml.v3"
)

func IndexedDBFactory(node yaml.Node) (indexeddb.IndexedDB, error) {
	prepared, err := prepareYAMLExecution(node, componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindIndexedDB,
		Subject:              "indexeddb provider",
		SourceMissingMessage: "no Go, Rust, or Python indexeddb provider source package found",
	})
	if err != nil {
		return nil, err
	}
	cfg := prepared.YAMLConfig

	return indexeddbservice.NewExecutable(context.Background(), indexeddbservice.ExecConfig{
		Command:    cfg.Command,
		Args:       cfg.Args,
		Workdir:    cfg.Workdir,
		Env:        cfg.Env,
		Config:     cfg.Config,
		Egress:     cfg.EgressPolicy(""),
		HostBinary: cfg.HostBinary,
		Cleanup:    prepared.Cleanup,
		Name:       cfg.Name,
	})
}
