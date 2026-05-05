package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// DefaultAgentHarnessName is the conventional harness name selected when a
// provider has a single legacy localHarness or an explicit default harness.
const DefaultAgentHarnessName = "default"

// EffectiveAgentHarness is the resolved provider and harness launch
// configuration for an agent provider.
type EffectiveAgentHarness struct {
	ProviderName string
	HarnessName  string
	Harness      ProviderEntryHarnessConfig
}

// ResolveProviderEntryAgentHarness selects a named or default harness from an
// agent provider entry and returns a cloned launch configuration.
func ResolveProviderEntryAgentHarness(providerName string, entry *ProviderEntry, harnessName string) (EffectiveAgentHarness, error) {
	providerName = strings.TrimSpace(providerName)
	harnessName = strings.TrimSpace(harnessName)
	if entry == nil {
		return EffectiveAgentHarness{}, fmt.Errorf("providers.agent.%s is not configured", providerName)
	}

	selectedName := harnessName
	if selectedName == "" {
		selectedName = strings.TrimSpace(entry.DefaultHarness)
	}
	if selectedName == "" && entry.Harnesses != nil {
		if _, ok := entry.Harnesses[DefaultAgentHarnessName]; ok {
			selectedName = DefaultAgentHarnessName
		}
	}
	if selectedName == "" && len(entry.Harnesses) == 1 {
		for name := range entry.Harnesses {
			selectedName = strings.TrimSpace(name)
		}
	}
	if selectedName == "" && entry.LocalHarness != nil {
		selectedName = DefaultAgentHarnessName
	}
	if selectedName == "" {
		return EffectiveAgentHarness{}, fmt.Errorf("providers.agent.%s.harnesses is required for agent harness launch", providerName)
	}

	var harness *ProviderEntryHarnessConfig
	if entry.Harnesses != nil {
		harness = entry.Harnesses[selectedName]
	}
	if harness == nil && selectedName == DefaultAgentHarnessName && entry.LocalHarness != nil {
		harness = entry.LocalHarness
	}
	if harness == nil {
		return EffectiveAgentHarness{}, fmt.Errorf("providers.agent.%s.harnesses.%s is not configured", providerName, selectedName)
	}

	cloned := *harness
	cloned.Args = slices.Clone(harness.Args)
	cloned.Env = maps.Clone(harness.Env)
	cloned.RequiredCommands = slices.Clone(harness.RequiredCommands)
	if strings.TrimSpace(cloned.Command) == "" {
		return EffectiveAgentHarness{}, fmt.Errorf("providers.agent.%s.harnesses.%s.command is required", providerName, selectedName)
	}
	return EffectiveAgentHarness{
		ProviderName: providerName,
		HarnessName:  selectedName,
		Harness:      cloned,
	}, nil
}
