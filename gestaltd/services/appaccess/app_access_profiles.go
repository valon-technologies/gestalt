package appaccess

import (
	"github.com/valon-technologies/gestalt/server/core"
)

const AppAccessAllApps = "*"

type AppAccessProfile struct {
	Operations           map[string]core.ConnectionMode
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
