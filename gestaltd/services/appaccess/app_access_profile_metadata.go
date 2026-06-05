package appaccess

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

type AppOperationProfile struct {
	CredentialMode core.ConnectionMode
	Delegation     AppAccessDelegation
}

func EffectiveAppOperationProfile(profiles AppAccessProfiles, app, operation string) (AppOperationProfile, bool) {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		return AppOperationProfile{}, false
	}
	profile, ok := appAccessProfileForApp(profiles, app)
	if !ok {
		return AppOperationProfile{}, false
	}
	mode, ok := profile.Operations[operation]
	if !ok {
		return AppOperationProfile{}, false
	}
	return AppOperationProfile{
		CredentialMode: core.NormalizeOptionalConnectionMode(mode),
		Delegation:     normalizeAppAccessDelegation(profile.OperationDelegations[operation]),
	}, true
}

func appAccessProfileForApp(profiles AppAccessProfiles, app string) (AppAccessProfile, bool) {
	app = strings.TrimSpace(app)
	if len(profiles) == 0 || app == "" {
		return AppAccessProfile{}, false
	}
	if app == AppAccessAllApps {
		return directAppAccessProfileForApp(profiles, app)
	}
	wildcard, wildcardOK := directAppAccessProfileForApp(profiles, AppAccessAllApps)
	profile, profileOK := directAppAccessProfileForApp(profiles, app)
	switch {
	case wildcardOK && profileOK:
		return mergeAppAccessProfile(wildcard, profile), true
	case profileOK:
		return profile, true
	case wildcardOK:
		return wildcard, true
	default:
		return AppAccessProfile{}, false
	}
}

func directAppAccessProfileForApp(profiles AppAccessProfiles, app string) (AppAccessProfile, bool) {
	app = strings.TrimSpace(app)
	if len(profiles) == 0 || app == "" {
		return AppAccessProfile{}, false
	}
	profile, ok := profiles[app]
	return profile, ok
}

func mergeAppAccessProfile(base, override AppAccessProfile) AppAccessProfile {
	merged := AppAccessProfile{}
	if len(base.Operations) > 0 || len(override.Operations) > 0 {
		merged.Operations = make(map[string]core.ConnectionMode, len(base.Operations)+len(override.Operations))
		for operation, mode := range base.Operations {
			merged.Operations[operation] = mode
		}
		for operation, mode := range override.Operations {
			if existing, ok := merged.Operations[operation]; !ok || mode != "" || existing == "" {
				merged.Operations[operation] = mode
			}
		}
	}
	if len(base.OperationDelegations) > 0 || len(override.OperationDelegations) > 0 {
		merged.OperationDelegations = make(map[string]AppAccessDelegation, len(base.OperationDelegations)+len(override.OperationDelegations))
		for operation, delegation := range base.OperationDelegations {
			merged.OperationDelegations[operation] = normalizeAppAccessDelegation(delegation)
		}
		for operation, delegation := range override.OperationDelegations {
			merged.OperationDelegations[operation] = normalizeAppAccessDelegation(delegation)
		}
	}
	return merged
}

func normalizeAppAccessDelegation(delegation AppAccessDelegation) AppAccessDelegation {
	return AppAccessDelegation{
		RunAs: core.NormalizeRunAsSubject(delegation.RunAs),
	}
}

func appAccessDelegationEmpty(delegation AppAccessDelegation) bool {
	delegation = normalizeAppAccessDelegation(delegation)
	return delegation.RunAs == nil
}
