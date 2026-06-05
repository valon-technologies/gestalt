package appaccess

import (
	"github.com/valon-technologies/gestalt/server/core"
)

const AppAccessAllApps = "*"

// AppAccessProfiles are execution-shaping metadata, not an authorization
// source. App invocation authorization is checked through AuthorizationProvider;
// these profiles only carry declarative credential-mode and runAs defaults for
// calls that have already been authorized.
type AppAccessProfile struct {
	Operations           map[string]core.ConnectionMode
	Surfaces             map[string]core.ConnectionMode
	OperationDelegations map[string]AppAccessDelegation
}

type AppAccessProfiles map[string]AppAccessProfile

type AppAccessDelegation struct {
	RunAs *core.RunAsSubject
}

func cloneAppAccessProfiles(src AppAccessProfiles) AppAccessProfiles {
	if len(src) == 0 {
		return nil
	}
	out := make(AppAccessProfiles, len(src))
	for app, profile := range src {
		cloned := AppAccessProfile{}
		if len(profile.Operations) > 0 {
			cloned.Operations = make(map[string]core.ConnectionMode, len(profile.Operations))
			for operation, mode := range profile.Operations {
				cloned.Operations[operation] = mode
			}
		}
		if len(profile.Surfaces) > 0 {
			cloned.Surfaces = make(map[string]core.ConnectionMode, len(profile.Surfaces))
			for surface, mode := range profile.Surfaces {
				cloned.Surfaces[surface] = mode
			}
		}
		if len(profile.OperationDelegations) > 0 {
			cloned.OperationDelegations = make(map[string]AppAccessDelegation, len(profile.OperationDelegations))
			for operation, delegation := range profile.OperationDelegations {
				cloned.OperationDelegations[operation] = normalizeAppAccessDelegation(delegation)
			}
		}
		out[app] = cloned
	}
	return out
}

func CloneAppAccessProfiles(src AppAccessProfiles) AppAccessProfiles {
	return cloneAppAccessProfiles(src)
}
