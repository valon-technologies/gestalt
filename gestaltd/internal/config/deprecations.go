package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

// DeprecationWarnings returns stable-ordered deprecation guidance for transitional
// UIProvider constructs still accepted by gestaltd.
func (c *Config) DeprecationWarnings() []string {
	if c == nil {
		return nil
	}

	seen := make(map[string]struct{})
	var warnings []string
	add := func(msg string) {
		msg = strings.TrimSpace(msg)
		if msg == "" {
			return
		}
		if _, ok := seen[msg]; ok {
			return
		}
		seen[msg] = struct{}{}
		warnings = append(warnings, msg)
	}

	uiNames := slices.Sorted(maps.Keys(c.Providers.UI))
	for _, name := range uiNames {
		if c.Providers.UI[name] == nil {
			continue
		}
		add(fmt.Sprintf("providers.ui.%s is deprecated; migrate to apps.%s.static", name, name))
	}

	appNames := slices.Sorted(maps.Keys(c.Apps))
	for _, name := range appNames {
		entry := c.Apps[name]
		if entry == nil {
			continue
		}
		if strings.TrimSpace(entry.UI) != "" {
			add(fmt.Sprintf("apps.%s.ui is deprecated; migrate to apps.%s.static", name, name))
		}
		if spec := entry.ManifestSpec(); spec != nil && spec.UI != nil {
			add(fmt.Sprintf("apps.%s manifest spec.ui is deprecated; migrate to apps.%s.static", name, name))
		}
	}

	for _, name := range uiNames {
		entry := c.Providers.UI[name]
		if entry == nil || entry.ResolvedManifest == nil {
			continue
		}
		if providermanifestv1.NormalizeKind(entry.ResolvedManifest.Kind) != providermanifestv1.KindUI {
			continue
		}
		add(fmt.Sprintf("kind: ui manifest %q is deprecated; migrate to apps.%s.static", name, name))
	}

	for _, name := range appNames {
		entry := c.Apps[name]
		if entry == nil || entry.ResolvedManifest == nil {
			continue
		}
		if providermanifestv1.NormalizeKind(entry.ResolvedManifest.Kind) != providermanifestv1.KindUI {
			continue
		}
		add(fmt.Sprintf("kind: ui manifest %q is deprecated; migrate to apps.%s.static", name, name))
	}

	return warnings
}
