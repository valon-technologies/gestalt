package bootstrap

import (
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowgrants"
)

func appInvocationDependencies(invokes []config.AppInvocationDependency) []appaccessservice.InvocationDependency {
	if len(invokes) == 0 {
		return nil
	}
	dependencies := make([]appaccessservice.InvocationDependency, 0, len(invokes))
	for _, invoke := range invokes {
		dependencies = append(dependencies, appaccessservice.InvocationDependency{
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
