package invocation

import "github.com/valon-technologies/gestalt/server/core"

// PluginInvocationDependency describes one plugin-to-plugin invocation grant.
// It is the service-level form of the config "invokes" entry.
type PluginInvocationDependency struct {
	Plugin         string
	Operation      string
	Surface        string
	CredentialMode core.ConnectionMode
}

func ClonePluginInvocationDependencies(src []PluginInvocationDependency) []PluginInvocationDependency {
	if len(src) == 0 {
		return nil
	}
	return append([]PluginInvocationDependency(nil), src...)
}

func ClonePluginInvocationDependencyMap(src map[string][]PluginInvocationDependency) map[string][]PluginInvocationDependency {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]PluginInvocationDependency, len(src))
	for pluginName, deps := range src {
		out[pluginName] = ClonePluginInvocationDependencies(deps)
	}
	return out
}
