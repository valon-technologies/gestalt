package bootstrap

import (
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowgrants"
)

func appAccessDependenciesFromInvokes(invokes []config.AppInvocationDependency) []appaccessservice.AppAccessDependency {
	if len(invokes) == 0 {
		return nil
	}
	dependencies := make([]appaccessservice.AppAccessDependency, 0, len(invokes))
	for _, invoke := range invokes {
		dependencies = append(dependencies, appaccessservice.AppAccessDependency{
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

func appAccessProfiles(apps map[string]*config.ProviderEntry) map[string]appaccessservice.AppAccessProfiles {
	if len(apps) == 0 {
		return nil
	}
	out := make(map[string]appaccessservice.AppAccessProfiles, len(apps))
	for name, entry := range apps {
		if entry == nil {
			continue
		}
		profiles := appaccessservice.ExactAppAccessProfilesFromDependencies(appAccessDependenciesFromInvokes(entry.Invokes))
		if len(profiles) > 0 {
			out[name] = profiles
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func appWorkflowGrants(capabilities *config.AppCapabilitiesConfig) workflowgrants.Grants {
	if capabilities == nil || capabilities.Workflow == nil || len(capabilities.Workflow.Operations) == 0 {
		return nil
	}
	grants := make(workflowgrants.Grants, len(capabilities.Workflow.Operations))
	for _, operation := range capabilities.Workflow.Operations {
		grants[operation] = struct{}{}
	}
	return grants
}
