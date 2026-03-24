package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/provider"
)

func compileProviders(configFlag, outputDir string) error {
	configPath := resolveConfigPath(configFlag)

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %v", err)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	_, err = writeProviderArtifacts(context.Background(), cfg, outputDir)
	return err
}

func writeProviderArtifacts(ctx context.Context, cfg *config.Config, outputDir string) (map[string]string, error) {
	written := make(map[string]string)
	names := slices.Sorted(maps.Keys(cfg.Integrations))
	for _, name := range names {
		intg := cfg.Integrations[name]
		def, hasAPI, err := loadIntegrationAPIDefinition(ctx, name, intg, cfg.ProviderDirs)
		if err != nil {
			return nil, fmt.Errorf("compiling provider %q: %w", name, err)
		}
		if !hasAPI {
			log.Printf("skipping provider artifact for %s (no rest/graphql upstream)", name)
			continue
		}
		d := *def
		def = &d
		if err := provider.ApplyArtifactOverrides(def, intg); err != nil {
			return nil, fmt.Errorf("applying artifact overrides for %q: %w", name, err)
		}

		data, err := json.MarshalIndent(def, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshaling provider %q: %w", name, err)
		}
		data = append(data, '\n')

		outPath := filepath.Join(outputDir, name+".json")
		if err := os.WriteFile(outPath, data, 0644); err != nil {
			return nil, fmt.Errorf("writing provider %q: %w", name, err)
		}
		log.Printf("wrote provider artifact %s", outPath)
		written[name] = outPath
	}

	log.Printf("provider artifacts written: %d", len(written))
	return written, nil
}

func loadIntegrationAPIDefinition(ctx context.Context, name string, intg config.IntegrationDef, providerDirs []string) (*provider.Definition, bool, error) {
	var (
		def    *provider.Definition
		hasAPI bool
	)

	for _, us := range intg.Upstreams {
		switch us.Type {
		case config.UpstreamTypeREST, config.UpstreamTypeGraphQL:
			if hasAPI {
				return nil, false, fmt.Errorf("multiple api upstreams not supported")
			}
			loaded, err := loadAPIUpstream(ctx, name, us, providerDirs)
			if err != nil {
				return nil, false, err
			}
			def = loaded
			hasAPI = true
		}
	}

	return def, hasAPI, nil
}
