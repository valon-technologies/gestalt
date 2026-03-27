package bootstrap

import (
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/egress"
)

func wireCredentialResolver(deps *EgressDeps, sm core.SecretManager) {
	var loaders []egress.CredentialGrantLoader

	if len(deps.CredentialGrants) > 0 {
		grants := make([]egress.CredentialGrant, len(deps.CredentialGrants))
		for i := range deps.CredentialGrants {
			g := &deps.CredentialGrants[i]
			grants[i] = egress.CredentialGrant{
				SecretRef: g.SecretRef,
				AuthStyle: egress.AuthStyle(g.AuthStyle),
				MatchCriteria: egress.MatchCriteria{
					SubjectKind: egress.SubjectKind(g.SubjectKind),
					SubjectID:   g.SubjectID,
					Operation:   g.Operation,
					Method:      g.Method,
					Host:        g.Host,
					PathPrefix:  g.PathPrefix,
				},
			}
		}
		loaders = append(loaders, &egress.StaticCredentialGrantLoader{Grants: grants})
	}

	if len(loaders) == 0 {
		return
	}

	deps.Resolver.Credentials = &egress.CredentialGrantResolver{
		Loaders:        loaders,
		SecretResolver: sm,
	}
}
