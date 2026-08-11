package remotepublish

import (
	"fmt"
	"slices"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

// BuildPublicationGroups builds a publication plan from config, not from a
// registry snapshot. Every app entry that builds locally and names a remote is
// included; the publisher workers await provider readiness at publish time.
func BuildPublicationGroups(cfg *config.Config) ([]PublicationGroup, error) {
	if cfg == nil || len(cfg.Server.Remotes) == 0 {
		return nil, nil
	}

	groupProviders := make(map[string][]ProviderPublication)
	for name, entry := range cfg.Apps {
		if entry == nil || !config.EntryBuildsLocal(entry) {
			continue
		}
		remoteName := config.EntryPlacementRemote(entry)
		if remoteName == "" {
			continue
		}
		if _, ok := cfg.Server.Remotes[remoteName]; !ok {
			return nil, fmt.Errorf("app %q names remote %q which is not defined under server.remotes", name, remoteName)
		}
		groupProviders[remoteName] = append(groupProviders[remoteName], ProviderPublication{
			Kind:       "app",
			Name:       name,
			Definition: appProviderDefinition(name, entry),
		})
	}

	names := make([]string, 0, len(groupProviders))
	for name := range groupProviders {
		names = append(names, name)
	}
	slices.Sort(names)

	groups := make([]PublicationGroup, 0, len(names))
	for _, name := range names {
		groups = append(groups, PublicationGroup{
			RemoteName: name,
			Remote:     cfg.Server.Remotes[name],
			Providers:  groupProviders[name],
		})
	}
	return groups, nil
}

func appProviderDefinition(name string, entry *config.ProviderEntry) map[string]any {
	def := map[string]any{
		"kind": "app",
		"name": name,
	}
	if entry != nil {
		if entry.DisplayName != "" {
			def["displayName"] = entry.DisplayName
		}
		if entry.Description != "" {
			def["description"] = entry.Description
		}
		if entry.ResolvedManifest != nil {
			if entry.ResolvedManifest.Version != "" {
				def["version"] = entry.ResolvedManifest.Version
			}
			if entry.ResolvedManifest.Source != "" {
				def["source"] = entry.ResolvedManifest.Source
			}
			if spec := entry.ResolvedManifest.Spec; spec != nil && len(spec.Headers) > 0 {
				headerNames := make([]string, 0, len(spec.Headers))
				for name := range spec.Headers {
					headerNames = append(headerNames, name)
				}
				slices.Sort(headerNames)
				def["staticHeaders"] = headerNames
			}
		}
	}
	return def
}
