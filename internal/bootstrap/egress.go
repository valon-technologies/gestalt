package bootstrap

import (
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/egress"
)

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
