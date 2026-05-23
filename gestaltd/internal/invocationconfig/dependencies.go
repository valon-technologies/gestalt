package invocationconfig

import (
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowgrants"
)

func AppInvocationDependencies(deps []config.AppInvocationDependency) []invocation.AppInvocationDependency {
	if len(deps) == 0 {
		return nil
	}
	out := make([]invocation.AppInvocationDependency, 0, len(deps))
	for _, dep := range deps {
		out = append(out, invocation.AppInvocationDependency{
			App:                   dep.App,
			Operation:             dep.Operation,
			Surface:               dep.Surface,
			CredentialMode:        core.ConnectionMode(dep.CredentialMode),
			RunAs:                 dep.RunAsSubject(),
			RunAsExternalIdentity: dep.RunAsExternalIdentity(),
			RunAsExplicitOnly:     !dep.RunAsAppliesByDefault(),
		})
	}
	return out
}

func AppWorkflowManagerGrants(capabilities *config.AppCapabilitiesConfig) workflowgrants.Grants {
	if capabilities == nil || capabilities.Workflow == nil {
		return workflowgrants.Grants{}
	}
	return workflowgrants.DecodeClaims(capabilities.Workflow.Operations)
}
