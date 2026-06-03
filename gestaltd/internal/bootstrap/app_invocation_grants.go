package bootstrap

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
)

func appInvocationDependencies(invokes []config.AppInvocationDependency) []appaccessservice.InvocationDependency {
	if len(invokes) == 0 {
		return nil
	}
	dependencies := make([]appaccessservice.InvocationDependency, 0, len(invokes))
	for _, invoke := range invokes {
		dependencies = append(dependencies, appaccessservice.InvocationDependency{
			App:            invoke.App,
			Operation:      invoke.Operation,
			Surface:        invoke.Surface,
			CredentialMode: core.ConnectionMode(invoke.CredentialMode),
			RunAs:          invoke.RunAsSubject(),
			ApplyByDefault: invoke.RunAs != nil && invoke.RunAsAppliesByDefault(),
		})
	}
	return dependencies
}

func workflowAppInvocationGrants(apps map[string]*config.ProviderEntry) map[string]appaccessservice.InvocationGrants {
	if len(apps) == 0 {
		return nil
	}
	grants := make(map[string]appaccessservice.InvocationGrants, len(apps))
	for name, entry := range apps {
		name = strings.TrimSpace(name)
		if name == "" || entry == nil {
			continue
		}
		appGrants := appaccessservice.ExactInvocationGrantsFromDependencies(appInvocationDependencies(entry.Invokes))
		if len(appGrants) > 0 {
			grants[name] = appGrants
		}
	}
	if len(grants) == 0 {
		return nil
	}
	return grants
}
