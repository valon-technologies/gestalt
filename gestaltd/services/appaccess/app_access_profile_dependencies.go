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
		if app == "" || operation == "" {
			continue
		}
		profile := profiles[app]
		if profile.Operations == nil {
			profile.Operations = make(map[string]core.ConnectionMode)
		}
		profile.Operations[operation] = core.NormalizeOptionalConnectionMode(dependency.CredentialMode)
		if dependency.ApplyByDefault {
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
