package appaccess

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

type AppAccessDependency struct {
	App            string
	Operation      string
	Surface        string
	CredentialMode core.ConnectionMode
	RunAs          *core.RunAsSubject
	ApplyByDefault bool
}

func ExactAppAccessProfilesFromDependencies(dependencies []AppAccessDependency) AppAccessProfiles {
	if len(dependencies) == 0 {
		return nil
	}
	profiles := AppAccessProfiles{}
	for _, dependency := range dependencies {
		app := strings.TrimSpace(dependency.App)
		operation := strings.TrimSpace(dependency.Operation)
		surface := strings.ToLower(strings.TrimSpace(dependency.Surface))
		if app == "" || (operation == "" && surface == "") {
			continue
		}
		profile := profiles[app]
		mode := core.NormalizeOptionalConnectionMode(dependency.CredentialMode)
		if operation != "" {
			if profile.Operations == nil {
				profile.Operations = make(map[string]core.ConnectionMode)
			}
			profile.Operations[operation] = mode
		}
		if surface != "" {
			if profile.Surfaces == nil {
				profile.Surfaces = make(map[string]core.ConnectionMode)
			}
			profile.Surfaces[surface] = mode
		}
		if operation != "" && dependency.ApplyByDefault {
			delegation := normalizeAppAccessDelegation(AppAccessDelegation{
				RunAs: dependency.RunAs,
			})
			if !appAccessDelegationEmpty(delegation) {
				if profile.OperationDelegations == nil {
					profile.OperationDelegations = make(map[string]AppAccessDelegation)
				}
				profile.OperationDelegations[operation] = delegation
			}
		}
		profiles[app] = profile
	}
	if len(profiles) == 0 {
		return nil
	}
	return profiles
}
