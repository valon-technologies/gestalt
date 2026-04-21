package bootstrap

import (
	"context"
	"fmt"

	modalruntime "github.com/valon-technologies/gestalt-providers/runtime/modal"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
)

func buildModalPluginRuntime(_ context.Context, _ string, entry *config.RuntimeProviderEntry, _ Deps) (pluginruntime.Provider, error) {
	cfg, err := decodeModalPluginRuntimeConfig(entry)
	if err != nil {
		return nil, err
	}
	return modalruntime.New(cfg)
}

func decodeModalPluginRuntimeConfig(entry *config.RuntimeProviderEntry) (modalruntime.Config, error) {
	if entry == nil {
		return modalruntime.Config{}, fmt.Errorf("modal runtime provider entry is required")
	}
	cfg, err := modalruntime.DecodeConfig(entry.Config)
	if err != nil {
		return modalruntime.Config{}, err
	}
	return cfg, nil
}
