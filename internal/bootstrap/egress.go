package bootstrap

import (
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/egress"
)

type EgressDeps struct {
	Resolver         *egress.Resolver
	CredentialGrants []config.EgressCredentialGrant
}

func newEgressDeps(cfg *config.Config) EgressDeps {
	staticEnforcer := buildStaticPolicyEnforcer(cfg.Egress)
	defaultAction := egress.PolicyAction(cfg.Egress.DefaultAction)
	hasStaticRules := len(cfg.Egress.Policies) > 0 || defaultAction == egress.PolicyDeny

	var policy egress.PolicyEnforcer
	if hasStaticRules {
		policy = staticEnforcer
	}

	return EgressDeps{
		Resolver: &egress.Resolver{
			Subjects: egress.ContextSubjectResolver{},
			Policy:   policy,
		},
		CredentialGrants: cfg.Egress.Credentials,
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
