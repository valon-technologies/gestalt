package bootstrap

import (
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/egress"
)

type EgressDeps struct {
	Resolver *egress.Resolver
}

func newEgressDeps(cfg *config.Config, sm core.SecretManager) EgressDeps {
	staticEnforcer := buildStaticPolicyEnforcer(cfg.Server.Egress)
	defaultAction := egress.PolicyAction(cfg.Server.Egress.DefaultAction)
	hasStaticRules := len(cfg.Server.Egress.Policies) > 0 || defaultAction == egress.PolicyDeny

	var policy egress.PolicyEnforcer
	if hasStaticRules {
		policy = staticEnforcer
	}

	var credentials egress.CredentialResolver
	if len(cfg.Server.Egress.Credentials) > 0 {
		grants := make([]egress.CredentialGrant, len(cfg.Server.Egress.Credentials))
		for i := range cfg.Server.Egress.Credentials {
			g := &cfg.Server.Egress.Credentials[i]
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
		credentials = &egress.CredentialGrantResolver{
			Loaders: []egress.CredentialGrantLoader{
				&egress.StaticCredentialGrantLoader{Grants: grants},
			},
			Secrets: sm,
		}
	}

	return EgressDeps{
		Resolver: &egress.Resolver{
			Subjects:    egress.ContextSubjectResolver{},
			Policy:      policy,
			Credentials: credentials,
		},
	}
}

func buildStaticPolicyEnforcer(cfg config.EgressConfig) egress.StaticPolicyEnforcer {
	defaultAction := egress.PolicyAction(cfg.DefaultAction)
	if defaultAction == "" {
		defaultAction = egress.PolicyAllow
	}

	rules := make([]egress.StaticPolicyRule, len(cfg.Policies))
	for i := range cfg.Policies {
		r := &cfg.Policies[i]
		rules[i] = egress.StaticPolicyRule{
			Action: egress.PolicyAction(r.Action),
			MatchCriteria: egress.MatchCriteria{
				SubjectKind: egress.SubjectKind(r.SubjectKind),
				SubjectID:   r.SubjectID,
				Provider:    r.Provider,
				Operation:   r.Operation,
				Method:      r.Method,
				Host:        r.Host,
				PathPrefix:  r.PathPrefix,
			},
		}
	}

	return egress.StaticPolicyEnforcer{
		DefaultAction: defaultAction,
		Rules:         rules,
	}
}
