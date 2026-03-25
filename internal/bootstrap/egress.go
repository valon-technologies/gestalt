package bootstrap

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/egress"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/registry"
)

type EgressDeps struct {
	Resolver *egress.Resolver
}

func newEgressDeps(cfg *config.Config, _ core.Datastore) EgressDeps {
	var policy egress.PolicyEnforcer
	defaultAction := egress.PolicyAction(cfg.Egress.DefaultAction)
	if len(cfg.Egress.Policies) > 0 || defaultAction == egress.PolicyDeny {
		policy = buildStaticPolicyEnforcer(cfg.Egress)
	}

	return EgressDeps{
		Resolver: &egress.Resolver{
			Subjects: egress.ContextSubjectResolver{},
			Policy:   policy,
		},
	}
}

func wireCredentialResolver(deps *EgressDeps, broker *invocation.Broker, providers *registry.PluginMap[core.Provider]) {
	if len(deps.CredentialGrants) == 0 {
		return
	}

	grants := make([]egress.CredentialGrant, len(deps.CredentialGrants))
	for i := range deps.CredentialGrants {
		g := &deps.CredentialGrants[i]
		grants[i] = egress.CredentialGrant{
			Instance: g.Instance,
			MatchCriteria: egress.MatchCriteria{
				SubjectKind: egress.SubjectKind(g.SubjectKind),
				SubjectID:   g.SubjectID,
				Provider:    g.Provider,
				Operation:   g.Operation,
				Method:      g.Method,
				Host:        g.Host,
				PathPrefix:  g.PathPrefix,
			},
		}
	}

	deps.Resolver.Credentials = &egress.ProviderCredentialResolver{
		TokenResolver: &brokerTokenResolver{broker: broker},
		Materializer:  &registryMaterializer{providers: providers},
		Grants:        grants,
	}
}

type brokerTokenResolver struct {
	broker *invocation.Broker
}

func (r *brokerTokenResolver) ResolveProviderToken(ctx context.Context, subject egress.Subject, provider, instance string) (string, error) {
	p, ok := egress.PrincipalForSubject(subject)
	if !ok {
		return "", fmt.Errorf("subject %s/%s cannot resolve provider tokens", subject.Kind, subject.ID)
	}
	return r.broker.ResolveToken(ctx, p, provider, instance)
}

type registryMaterializer struct {
	providers *registry.PluginMap[core.Provider]
}

type egressMaterializer interface {
	EgressMaterializeCredential(token string) (egress.CredentialMaterialization, error)
}

func (m *registryMaterializer) MaterializeProviderCredential(provider string, token string) (egress.CredentialMaterialization, error) {
	prov, err := m.providers.Get(provider)
	if err != nil {
		return egress.CredentialMaterialization{}, fmt.Errorf("loading provider %q for credential materialization: %w", provider, err)
	}
	for prov != nil {
		if em, ok := prov.(egressMaterializer); ok {
			return em.EgressMaterializeCredential(token)
		}
		unwrapper, ok := prov.(interface{ Inner() core.Provider })
		if !ok {
			break
		}
		prov = unwrapper.Inner()
	}
	return egress.MaterializeCredential(token, egress.AuthStyleBearer, nil)
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
