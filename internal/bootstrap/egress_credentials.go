package bootstrap

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/egress"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/registry"
)

func wireCredentialResolver(deps *EgressDeps, broker *invocation.Broker, providers *registry.PluginMap[core.Provider], ds core.Datastore, sm core.SecretManager) {
	tokenResolver := &brokerTokenResolver{broker: broker}
	materializer := &registryMaterializer{providers: providers}

	var loaders []egress.CredentialGrantLoader

	if grantStore, ok := ds.(core.EgressCredentialGrantStore); ok {
		loaders = append(loaders, &credentialGrantStoreLoader{
			store:          grantStore,
			tokenResolver:  tokenResolver,
			materializer:   materializer,
			secretResolver: sm,
		})
	}

	if len(deps.CredentialGrants) > 0 {
		grants := make([]egress.CredentialGrant, len(deps.CredentialGrants))
		for i := range deps.CredentialGrants {
			g := &deps.CredentialGrants[i]
			grants[i] = egress.CredentialGrant{
				Instance:  g.Instance,
				SecretRef: g.SecretRef,
				AuthStyle: egress.AuthStyle(g.AuthStyle),
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
			grants[i].Source = hydrateCredentialSource(grants[i], tokenResolver, materializer, sm)
		}
		loaders = append(loaders, &egress.StaticCredentialGrantLoader{Grants: grants})
	}

	if len(loaders) == 0 {
		return
	}

	deps.Resolver.Credentials = &egress.CredentialGrantResolver{
		Loaders: loaders,
	}
}

type credentialGrantStoreLoader struct {
	store          core.EgressCredentialGrantStore
	tokenResolver  egress.ProviderTokenResolver
	materializer   egress.CredentialMaterializer
	secretResolver egress.SecretResolver
}

func (a *credentialGrantStoreLoader) LoadCredentialGrants(ctx context.Context) ([]egress.CredentialGrant, error) {
	grants, err := a.store.ListEgressCredentialGrants(ctx, core.EgressCredentialGrantFilter{})
	if err != nil {
		return nil, err
	}
	out := make([]egress.CredentialGrant, len(grants))
	for i, g := range grants {
		if err := egress.ValidateCredentialGrant(egress.CredentialGrantValidationInput{
			SubjectKind: g.SubjectKind,
			SubjectID:   g.SubjectID,
			Provider:    g.Provider,
			Instance:    g.Instance,
			Operation:   g.Operation,
			Method:      g.Method,
			Host:        g.Host,
			PathPrefix:  g.PathPrefix,
			AuthStyle:   g.AuthStyle,
		}); err != nil {
			return nil, fmt.Errorf("egress credentials: invalid saved grant %q: %w", g.ID, err)
		}
		out[i] = egress.CredentialGrant{
			Instance:  g.Instance,
			SecretRef: g.SecretRef,
			AuthStyle: egress.AuthStyle(g.AuthStyle),
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
		out[i].Source = hydrateCredentialSource(out[i], a.tokenResolver, a.materializer, a.secretResolver)
	}
	return out, nil
}

func hydrateCredentialSource(grant egress.CredentialGrant, tr egress.ProviderTokenResolver, mat egress.CredentialMaterializer, sr egress.SecretResolver) egress.CredentialSource {
	if grant.SecretRef != "" {
		return &egress.SecretCredentialSource{
			Resolver:  sr,
			SecretRef: grant.SecretRef,
			AuthStyle: grant.AuthStyle,
		}
	}
	return &egress.ProviderTokenCredentialSource{
		TokenResolver: tr,
		Materializer:  mat,
		Provider:      grant.Provider,
		Instance:      grant.Instance,
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
