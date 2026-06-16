package config

import (
	"fmt"
	"slices"
	"strings"
)

// RetainAppsResult reports what RetainApps removed, for operator logging.
type RetainAppsResult struct {
	Kept             []string
	DroppedApps      []string
	DroppedWorkflows []string
}

// RetainApps prunes cfg in place to the named apps and their dependency
// closure, so a full fleet config can boot a single app without preparing or
// bootstrapping everything else.
//
// It keeps: the named apps; the UI bundles they mount; the indexeddb, cache,
// s3, and runtime providers they reference (plus any marked default and the
// server-selected default indexeddb); and all shared infra (authentication,
// authorization, secrets, telemetry, audit, externalCredentials) and
// orchestration (workflow, agent) providers. It drops every other app, their
// orphaned UI/storage/runtime providers, and any workflow definition that
// targets a dropped app.
//
// keep names must exist in cfg.Apps. Pruned references that a retained app
// still needs surface as ordinary validation errors downstream.
func RetainApps(cfg *Config, keep []string) (RetainAppsResult, error) {
	if cfg == nil || len(keep) == 0 {
		return RetainAppsResult{}, nil
	}

	retained := make(map[string]struct{}, len(keep))
	var unknown []string
	for _, name := range keep {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := cfg.Apps[name]; !ok {
			unknown = append(unknown, name)
			continue
		}
		retained[name] = struct{}{}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return RetainAppsResult{}, fmt.Errorf("--only names unknown apps: %s", strings.Join(unknown, ", "))
	}
	if len(retained) == 0 {
		return RetainAppsResult{}, fmt.Errorf("--only matched no apps")
	}

	keepUI := map[string]struct{}{}
	keepIndexedDB := map[string]struct{}{}
	keepCache := map[string]struct{}{}
	keepS3 := map[string]struct{}{}
	keepRuntime := map[string]struct{}{}

	for name := range retained {
		app := cfg.Apps[name]
		if app == nil {
			continue
		}
		if ui := strings.TrimSpace(app.UI); ui != "" {
			keepUI[ui] = struct{}{}
		}
		if app.IndexedDB != nil {
			if p := strings.TrimSpace(app.IndexedDB.Provider); p != "" {
				keepIndexedDB[p] = struct{}{}
			}
		}
		for _, c := range app.Cache {
			if c = strings.TrimSpace(c); c != "" {
				keepCache[c] = struct{}{}
			}
		}
		for _, s := range app.S3 {
			if s = strings.TrimSpace(s); s != "" {
				keepS3[s] = struct{}{}
			}
		}
		if app.Runtime != nil {
			if p := strings.TrimSpace(app.Runtime.Provider); p != "" {
				keepRuntime[p] = struct{}{}
			}
		}
	}

	for name, ui := range cfg.Providers.UI {
		if ui == nil {
			continue
		}
		if _, ok := retained[ui.OwnerApp]; ok {
			keepUI[name] = struct{}{}
		}
	}
	if sel := strings.TrimSpace(cfg.Server.Providers.IndexedDB); sel != "" {
		keepIndexedDB[sel] = struct{}{}
	}

	pruneMap(cfg.Apps, retained)
	pruneMap(cfg.Providers.UI, keepUI)
	pruneProviderMap(cfg.Providers.IndexedDB, keepIndexedDB)
	pruneProviderMap(cfg.Providers.Cache, keepCache)
	pruneProviderMap(cfg.Providers.S3, keepS3)
	pruneRuntimeMap(cfg.Runtime.Providers, keepRuntime)

	var droppedWorkflows []string
	for name, def := range cfg.Workflows.Definitions {
		if app := workflowReferencesDroppedApp(def, retained); app != "" {
			droppedWorkflows = append(droppedWorkflows, name)
			delete(cfg.Workflows.Definitions, name)
		}
	}

	result := RetainAppsResult{
		Kept:             sortedKeys(retained),
		DroppedWorkflows: droppedWorkflows,
	}
	slices.Sort(result.DroppedWorkflows)
	slices.Sort(result.Kept)
	return result, nil
}

// pruneMap deletes every UIEntry/ProviderEntry key not in keep.
func pruneMap[V any](m map[string]*V, keep map[string]struct{}) {
	for name := range m {
		if _, ok := keep[name]; !ok {
			delete(m, name)
		}
	}
}

// pruneProviderMap drops unreferenced provider entries but always retains
// entries marked default.
func pruneProviderMap(m map[string]*ProviderEntry, keep map[string]struct{}) {
	for name, entry := range m {
		if _, ok := keep[name]; ok {
			continue
		}
		if entry != nil && entry.Default {
			continue
		}
		delete(m, name)
	}
}

func pruneRuntimeMap(m map[string]*RuntimeProviderEntry, keep map[string]struct{}) {
	for name, entry := range m {
		if _, ok := keep[name]; ok {
			continue
		}
		if entry != nil && entry.Default {
			continue
		}
		delete(m, name)
	}
}

func workflowReferencesDroppedApp(def WorkflowDefinitionConfig, retained map[string]struct{}) string {
	dropped := func(app string) string {
		app = strings.TrimSpace(app)
		if app == "" {
			return ""
		}
		if _, ok := retained[app]; ok {
			return ""
		}
		return app
	}
	for _, step := range def.Steps {
		if step.App != nil {
			if app := dropped(step.App.Name); app != "" {
				return app
			}
		}
		if step.Agent != nil {
			for _, tool := range step.Agent.Tools {
				if app := dropped(tool.App); app != "" {
					return app
				}
			}
		}
	}
	return ""
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
