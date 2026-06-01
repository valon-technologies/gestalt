package appaccess

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

type InvocationDependency struct {
	App            string
	Operation      string
	CredentialMode core.ConnectionMode
	RunAs          *core.RunAsSubject
	ApplyByDefault bool
}

func InvocationGrantsFromDependencies(dependencies []InvocationDependency) InvocationGrants {
	grants := AllInvocationGrants()
	for _, dependency := range dependencies {
		app := strings.TrimSpace(dependency.App)
		operation := strings.TrimSpace(dependency.Operation)
		if app == "" || operation == "" {
			continue
		}
		grant := grants[app]
		if mode := core.NormalizeOptionalConnectionMode(dependency.CredentialMode); mode != "" {
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
		if grant.Operations != nil || grant.OperationDelegations != nil {
			grants[app] = grant
		}
	}
	return grants
}
