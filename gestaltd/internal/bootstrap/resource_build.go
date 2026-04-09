package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginhost"
)

// datastoreBuildResult holds started resource providers keyed by name.
type datastoreBuildResult struct {
	providers map[string]core.ResourceProvider
}

func (r *datastoreBuildResult) Close() error {
	if r == nil {
		return nil
	}
	var errs []error
	for _, p := range r.providers {
		if err := p.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func buildDatastores(ctx context.Context, cfg *config.Config, deps Deps) (*datastoreBuildResult, error) {
	if len(cfg.Datastores) == 0 {
		return &datastoreBuildResult{providers: map[string]core.ResourceProvider{}}, nil
	}

	type result struct {
		name     string
		provider core.ResourceProvider
		err      error
	}

	var wg sync.WaitGroup
	results := make(chan result, len(cfg.Datastores))

	for name := range cfg.Datastores {
		resDef := cfg.Datastores[name]
		wg.Add(1)
		go func(name string, resDef config.DatastoreDef) {
			defer wg.Done()
			provider, err := buildSingleResource(ctx, name, resDef, deps)
			results <- result{name: name, provider: provider, err: err}
		}(name, resDef)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	started := make(map[string]core.ResourceProvider, len(cfg.Datastores))
	var firstErr error
	for res := range results {
		if res.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("resource %q: %w", res.name, res.err)
			}
			continue
		}
		started[res.name] = res.provider
		slog.Info("resource started", "name", res.name, "capabilities", res.provider.Capabilities())
	}

	if firstErr != nil {
		for _, p := range started {
			_ = p.Close()
		}
		return nil, firstErr
	}

	return &datastoreBuildResult{providers: started}, nil
}

func buildSingleResource(ctx context.Context, name string, resDef config.DatastoreDef, deps Deps) (core.ResourceProvider, error) {
	if resDef.Provider == nil {
		return nil, fmt.Errorf("provider is required")
	}

	cfgMap, err := config.NodeToMap(resDef.Config)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	execCfg := pluginhost.ResourceExecConfig{
		Command:      resDef.Provider.Command,
		Args:         resDef.Provider.Args,
		Env:          resDef.Provider.Env,
		Config:       cfgMap,
		AllowedHosts: resDef.Provider.AllowedHosts,
		HostBinary:   resDef.Provider.HostBinary,
		Cleanup:      nil,
		Name:         name,
	}

	provider, err := pluginhost.NewExecutableResource(ctx, execCfg)
	if err != nil {
		return nil, err
	}

	for _, wantCap := range resDef.Capabilities {
		found := false
		for _, haveCap := range provider.Capabilities() {
			if string(haveCap) == wantCap {
				found = true
				break
			}
		}
		if !found {
			_ = provider.Close()
			return nil, fmt.Errorf("declared capability %q not reported by provider (has %v)", wantCap, provider.Capabilities())
		}
	}

	if err := provider.Ping(ctx); err != nil {
		_ = provider.Close()
		return nil, fmt.Errorf("health check failed: %w", err)
	}

	return provider, nil
}

// ResolveDatastoreHandles builds resource handles for a plugin's resource
// bindings by looking up the started resource providers and creating
// namespace-injecting wrappers.
func ResolveDatastoreHandles(bindings map[string]config.DatastoreBindingDef, pluginName string, providers map[string]core.ResourceProvider) (map[string]*core.ResourceHandle, error) {
	if len(bindings) == 0 {
		return nil, nil
	}

	handles := make(map[string]*core.ResourceHandle, len(bindings))
	for alias, binding := range bindings {
		provider, ok := providers[binding.Resource]
		if !ok {
			return nil, fmt.Errorf("resource %q not found for binding %q", binding.Resource, alias)
		}

		ns := binding.Namespace
		if ns == "" {
			ns = pluginName
		}

		cap := core.ResourceCapability(binding.Capability)
		handle := &core.ResourceHandle{
			ResourceName: binding.Resource,
			Namespace:    ns,
			Capability:   cap,
		}

		remote, ok := provider.(*pluginhost.RemoteResourceProvider)
		if !ok {
			return nil, fmt.Errorf("resource %q: unexpected provider type", binding.Resource)
		}

		switch cap {
		case core.ResourceCapabilityKeyValue:
			kvClient := remote.KVClient()
			if kvClient == nil {
				return nil, fmt.Errorf("resource %q does not support key_value capability", binding.Resource)
			}
			handle.KV = pluginhost.NewNamespacedKVStore(kvClient, ns)
		case core.ResourceCapabilitySQL:
			sqlClient := remote.SQLClient()
			if sqlClient == nil {
				return nil, fmt.Errorf("resource %q does not support sql capability", binding.Resource)
			}
			handle.SQL = pluginhost.NewNamespacedSQLStore(sqlClient, ns)
		case core.ResourceCapabilityBlobStore:
			blobClient := remote.BlobClient()
			if blobClient == nil {
				return nil, fmt.Errorf("resource %q does not support blob_store capability", binding.Resource)
			}
			handle.Blob = pluginhost.NewNamespacedBlobStore(blobClient, ns)
		}

		handles[alias] = handle
	}
	return handles, nil
}
