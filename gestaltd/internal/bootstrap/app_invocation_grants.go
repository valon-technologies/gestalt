package bootstrap

import (
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
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
