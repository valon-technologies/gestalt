package appaccess

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

type OperationProfile struct {
	CredentialMode core.ConnectionMode
	Delegation     InvocationDelegation
}

func EffectiveOperationProfile(grants InvocationGrants, plugin, operation string) (OperationProfile, bool) {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		return OperationProfile{}, false
	}
	grant, ok := grantForApp(grants, plugin)
	if !ok {
		return OperationProfile{}, false
	}
	mode, operationAllowed := grant.Operations[operation]
	if !grant.AllOperations && !operationAllowed {
		return OperationProfile{}, false
	}
	return OperationProfile{
		CredentialMode: core.NormalizeOptionalConnectionMode(mode),
		Delegation:     normalizeInvocationDelegation(grant.OperationDelegations[operation]),
	}, true
}

func prepareChildInvocationGrants(candidate, parent InvocationGrants) (InvocationGrants, bool) {
	if len(candidate) == 0 {
		return cloneInvocationGrants(parent), true
	}
	out := cloneInvocationGrants(candidate)
	for plugin, grant := range out {
		parentGrant, ok := grantForApp(parent, plugin)
		if !ok {
			return nil, false
		}
		if grant.AllOperations && !parentGrant.AllOperations {
			return nil, false
		}
		for operation, mode := range grant.Operations {
			parentProfile, ok := EffectiveOperationProfile(parent, plugin, operation)
			if !ok || !operationProfileAllowsMode(parentProfile, mode) {
				return nil, false
			}
			if requestedDelegation := normalizeInvocationDelegation(grant.OperationDelegations[operation]); !invocationDelegationEmpty(requestedDelegation) && !invocationDelegationsEqual(requestedDelegation, parentProfile.Delegation) {
				return nil, false
			}
			if mode == "" {
				grant.Operations[operation] = parentProfile.CredentialMode
			}
			if !invocationDelegationEmpty(parentProfile.Delegation) {
				if grant.OperationDelegations == nil {
					grant.OperationDelegations = make(map[string]InvocationDelegation)
				}
				grant.OperationDelegations[operation] = parentProfile.Delegation
			}
		}
		for operation, delegation := range grant.OperationDelegations {
			if !grant.AllOperations {
				if _, ok := grant.Operations[operation]; !ok {
					continue
				}
			}
			parentProfile, ok := EffectiveOperationProfile(parent, plugin, operation)
			if !ok || !invocationDelegationsEqual(delegation, parentProfile.Delegation) {
				return nil, false
			}
		}
		for surface := range grant.Surfaces {
			if parentGrant.AllOperations {
				continue
			}
			if _, ok := parentGrant.Surfaces[surface]; !ok {
				return nil, false
			}
		}
		if grant.AllOperations {
			inheritInvocationGrantOperationMetadata(&grant, parentGrant)
		}
		out[plugin] = grant
	}
	inheritWildcardInvocationGrantSidecars(out, parent)
	return out, true
}

func operationProfileAllowsMode(profile OperationProfile, mode core.ConnectionMode) bool {
	mode = core.NormalizeOptionalConnectionMode(mode)
	return mode == "" || profile.CredentialMode == "" || mode == profile.CredentialMode
}

func grantForApp(grants InvocationGrants, plugin string) (InvocationGrant, bool) {
	plugin = strings.TrimSpace(plugin)
	if len(grants) == 0 || plugin == "" {
		return InvocationGrant{}, false
	}
	if plugin == InvocationGrantAllApps {
		return directGrantForApp(grants, plugin)
	}
	wildcard, wildcardOK := directGrantForApp(grants, InvocationGrantAllApps)
	grant, grantOK := directGrantForApp(grants, plugin)
	switch {
	case wildcardOK && grantOK:
		return mergeInvocationGrant(wildcard, grant), true
	case grantOK:
		return grant, true
	case wildcardOK:
		return wildcard, true
	default:
		return InvocationGrant{}, false
	}
}

func directGrantForApp(grants InvocationGrants, plugin string) (InvocationGrant, bool) {
	plugin = strings.TrimSpace(plugin)
	if len(grants) == 0 || plugin == "" {
		return InvocationGrant{}, false
	}
	grant, ok := grants[plugin]
	return grant, ok
}

func mergeInvocationGrant(base, override InvocationGrant) InvocationGrant {
	merged := InvocationGrant{AllOperations: base.AllOperations || override.AllOperations}
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
		merged.OperationDelegations = make(map[string]InvocationDelegation, len(base.OperationDelegations)+len(override.OperationDelegations))
		for operation, delegation := range base.OperationDelegations {
			merged.OperationDelegations[operation] = normalizeInvocationDelegation(delegation)
		}
		for operation, delegation := range override.OperationDelegations {
			merged.OperationDelegations[operation] = normalizeInvocationDelegation(delegation)
		}
	}
	if len(base.Surfaces) > 0 || len(override.Surfaces) > 0 {
		merged.Surfaces = make(map[string]struct{}, len(base.Surfaces)+len(override.Surfaces))
		for surface := range base.Surfaces {
			merged.Surfaces[surface] = struct{}{}
		}
		for surface := range override.Surfaces {
			merged.Surfaces[surface] = struct{}{}
		}
	}
	return merged
}

func inheritWildcardInvocationGrantSidecars(child, parent InvocationGrants) {
	wildcard, ok := child[InvocationGrantAllApps]
	if !ok || !wildcard.AllOperations {
		return
	}
	parentWildcard, _ := directGrantForApp(parent, InvocationGrantAllApps)
	inheritInvocationGrantOperationMetadata(&wildcard, parentWildcard)
	child[InvocationGrantAllApps] = wildcard
	for plugin, parentGrant := range parent {
		if plugin == InvocationGrantAllApps {
			continue
		}
		sidecar := child[plugin]
		inheritInvocationGrantOperationMetadata(&sidecar, parentGrant)
		if !invocationGrantEmpty(sidecar) {
			child[plugin] = sidecar
		}
	}
}

func inheritInvocationGrantOperationMetadata(grant *InvocationGrant, parent InvocationGrant) {
	if grant == nil {
		return
	}
	for operation, mode := range parent.Operations {
		if mode == "" {
			continue
		}
		if grant.Operations == nil {
			grant.Operations = make(map[string]core.ConnectionMode)
		}
		if grant.Operations[operation] == "" {
			grant.Operations[operation] = mode
		}
	}
	for operation, delegation := range parent.OperationDelegations {
		delegation = normalizeInvocationDelegation(delegation)
		if invocationDelegationEmpty(delegation) {
			continue
		}
		if grant.OperationDelegations == nil {
			grant.OperationDelegations = make(map[string]InvocationDelegation)
		}
		if invocationDelegationEmpty(grant.OperationDelegations[operation]) {
			grant.OperationDelegations[operation] = delegation
		}
	}
}

func invocationGrantEmpty(grant InvocationGrant) bool {
	return !grant.AllOperations && len(grant.Operations) == 0 && len(grant.OperationDelegations) == 0 && len(grant.Surfaces) == 0
}

func normalizeInvocationDelegation(delegation InvocationDelegation) InvocationDelegation {
	return InvocationDelegation{
		RunAs: core.NormalizeRunAsSubject(delegation.RunAs),
	}
}

func invocationDelegationEmpty(delegation InvocationDelegation) bool {
	delegation = normalizeInvocationDelegation(delegation)
	return delegation.RunAs == nil
}

func invocationDelegationsEqual(left, right InvocationDelegation) bool {
	left = normalizeInvocationDelegation(left)
	right = normalizeInvocationDelegation(right)
	return core.RunAsSubjectsEqual(left.RunAs, right.RunAs)
}
