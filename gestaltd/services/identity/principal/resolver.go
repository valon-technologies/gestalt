package principal

import (
	"context"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

type Resolver struct {
	auth         core.AuthenticationProvider
	providerName string
}

func NewResolver(auth core.AuthenticationProvider) *Resolver {
	return NewResolverNamed("", auth)
}

func NewResolverNamed(providerName string, auth core.AuthenticationProvider) *Resolver {
	name := strings.TrimSpace(providerName)
	switch {
	case auth == nil && name == "":
		name = "none"
	case name == "":
		name = "authentication"
	}
	return &Resolver{auth: auth, providerName: name}
}

func (r *Resolver) ResolveToken(ctx context.Context, token string) (*Principal, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrInvalidToken
	}

	startedAt := time.Now()
	provider := r.providerName
	if r.auth == nil {
		metricutil.RecordAuthMetrics(ctx, startedAt, provider, "introspect", true)
		return nil, ErrInvalidToken
	}

	resp, err := r.auth.Introspect(ctx, &core.IntrospectRequest{Token: token})
	metricutil.RecordAuthMetrics(ctx, startedAt, provider, "introspect", err != nil || resp == nil || !resp.Active)
	if err != nil {
		return nil, err
	}
	if resp != nil && resp.Active {
		subject := strings.TrimSpace(resp.Subject)
		if _, _, ok := core.ParseSubjectID(subject); !ok {
			return nil, ErrInvalidToken
		}
		resp.Subject = subject
		return principalFromIntrospection(resp), nil
	}
	return nil, ErrInvalidToken
}

func (r *Resolver) ResolveEmail(email string) *Principal {
	return &Principal{
		Identity: &core.UserIdentity{Email: email},
		Kind:     KindUser,
		Source:   SourceEnv,
	}
}

func principalFromIntrospection(resp *core.IntrospectResponse) *Principal {
	if resp == nil || !resp.Active {
		return nil
	}
	p := &Principal{
		SubjectID: strings.TrimSpace(resp.Subject),
		Scopes:    ParseScopeString(resp.Scope),
		ClientID:  strings.TrimSpace(resp.ClientID),
		Audience:  append([]string(nil), resp.Audience...),
		Source:    SourceBearer,
	}
	if suffix := UserIDFromSubjectID(p.SubjectID); suffix != "" {
		if strings.Contains(suffix, "@") {
			p.Identity = &core.UserIdentity{Email: suffix}
			p.Kind = KindUser
		} else {
			p.UserID = suffix
			p.Kind = KindUser
		}
	} else if kind := KindFromSubjectID(p.SubjectID); kind != "" {
		p.Kind = kind
	}
	p.CredentialSubjectID = p.SubjectID
	return Canonicalize(p)
}

func ParseScopeString(scope string) []string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil
	}
	parts := strings.Fields(scope)
	if len(parts) == 0 {
		return nil
	}
	return parts
}
