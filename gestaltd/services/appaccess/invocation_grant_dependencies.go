package appaccess

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

type InvocationDependency struct {
	App            string
	Operation      string
	Surface        string
	CredentialMode core.ConnectionMode
	RunAs          *core.RunAsSubject
	ApplyByDefault bool
}

func InvocationGrantsFromDependencies(dependencies []InvocationDependency) InvocationGrants {
	grants := AllInvocationGrants()
	for _, dependency := range dependencies {
		app := strings.TrimSpace(dependency.App)
		operation := strings.TrimSpace(dependency.Operation)
		surface := strings.ToLower(strings.TrimSpace(dependency.Surface))
		if app == "" || operation == "" && surface == "" {
			continue
		}
		grant := grants[app]
		if mode := core.NormalizeOptionalConnectionMode(dependency.CredentialMode); operation != "" && mode != "" {
			if grant.Operations == nil {
				grant.Operations = make(map[string]core.ConnectionMode)
			}
			grant.Operations[operation] = mode
		}
		if dependency.ApplyByDefault {
			delegation := normalizeInvocationDelegation(InvocationDelegation{
				RunAs: dependency.RunAs,
			})
			if !invocationDelegationEmpty(delegation) {
				if grant.OperationDelegations == nil {
					grant.OperationDelegations = make(map[string]InvocationDelegation)
				}
				grant.OperationDelegations[operation] = delegation
			}
		}
		if surface != "" {
			if grant.Surfaces == nil {
				grant.Surfaces = make(map[string]struct{})
			}
			grant.Surfaces[surface] = struct{}{}
		}
		if grant.Operations != nil || grant.OperationDelegations != nil || grant.Surfaces != nil {
			grants[app] = grant
		}
	}
	return grants
}

func ExactInvocationGrantsFromDependencies(dependencies []InvocationDependency) InvocationGrants {
	if len(dependencies) == 0 {
		return nil
	}
	grants := InvocationGrants{}
	for _, dependency := range dependencies {
		app := strings.TrimSpace(dependency.App)
		operation := strings.TrimSpace(dependency.Operation)
		surface := strings.ToLower(strings.TrimSpace(dependency.Surface))
		if app == "" || operation == "" && surface == "" {
			continue
		}
		grant := grants[app]
		if operation != "" {
			if grant.Operations == nil {
				grant.Operations = make(map[string]core.ConnectionMode)
			}
			grant.Operations[operation] = core.NormalizeOptionalConnectionMode(dependency.CredentialMode)
			if dependency.ApplyByDefault {
				delegation := normalizeInvocationDelegation(InvocationDelegation{
					RunAs: dependency.RunAs,
				})
				if !invocationDelegationEmpty(delegation) {
					if grant.OperationDelegations == nil {
						grant.OperationDelegations = make(map[string]InvocationDelegation)
					}
					grant.OperationDelegations[operation] = delegation
				}
			}
		}
		if surface != "" {
			if grant.Surfaces == nil {
				grant.Surfaces = make(map[string]struct{})
			}
			grant.Surfaces[surface] = struct{}{}
		}
		if !invocationGrantEmpty(grant) {
			grants[app] = grant
		}
	}
	if len(grants) == 0 {
		return nil
	}
	return grants
}
