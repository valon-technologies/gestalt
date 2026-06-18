package principal

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/authentication"
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
		return r.enrichPrincipalWithUserInfo(ctx, token, principalFromIntrospection(resp)), nil
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

func (r *Resolver) enrichPrincipalWithUserInfo(ctx context.Context, accessToken string, p *Principal) *Principal {
	p = Canonicalized(p)
	accessToken = strings.TrimSpace(accessToken)
	if r.auth == nil || p == nil || accessToken == "" || strings.TrimSpace(p.SubjectID) == "" {
		return p
	}

	startedAt := time.Now()
	userInfoCtx := authentication.WithCallerBearerToken(ctx, accessToken)
	userInfo, err := r.auth.UserInfo(userInfoCtx, &core.UserInfoRequest{})
	metricutil.RecordAuthMetrics(ctx, startedAt, r.providerName, "userinfo", userInfoLookupFailed(err))
	if err != nil {
		if !errors.Is(err, core.ErrNotFound) {
			slog.WarnContext(ctx, "authentication provider userinfo lookup failed", "error", err)
		}
		return p
	}
	if userInfo == nil {
		return p
	}

	introspectedSubject := strings.TrimSpace(p.SubjectID)
	if subjectID := strings.TrimSpace(userInfo.SubjectID); subjectID != "" && subjectID != introspectedSubject {
		slog.WarnContext(ctx, "authentication provider userinfo subject mismatch",
			"introspected_subject", introspectedSubject,
			"userinfo_subject", subjectID,
		)
		return p
	}

	clone := *p
	if clone.Identity == nil {
		clone.Identity = &core.UserIdentity{}
	}
	if email := strings.TrimSpace(userInfo.Email); email != "" {
		clone.Identity.Email = email
	}
	if name := strings.TrimSpace(userInfo.Name); name != "" {
		clone.Identity.DisplayName = name
		clone.DisplayName = name
	}
	if clone.Identity.Email == "" && clone.UserID == "" {
		if suffix := UserIDFromSubjectID(clone.SubjectID); strings.Contains(suffix, "@") {
			clone.Identity.Email = suffix
		}
	}
	return Canonicalize(&clone)
}

func userInfoLookupFailed(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, core.ErrNotFound)
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
