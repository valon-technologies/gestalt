package bootstrap

import (
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/egress"
)

// EgressDeps carries shared outbound request resolution dependencies that can
// be reused by providers, bindings, and runtimes.
type EgressDeps struct {
	Resolver *egress.Resolver
}

func newEgressDeps(_ *config.Config, _ core.Datastore) EgressDeps {
	return EgressDeps{
		Resolver: &egress.Resolver{
			Subjects: egress.ContextSubjectResolver{},
		},
	}
}
