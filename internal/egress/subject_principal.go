package egress

import (
	"context"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/principal"
)

func SubjectForPrincipal(p *principal.Principal) (Subject, bool) {
	if p == nil {
		return Subject{}, false
	}
	if p.EgressClientID != "" {
		return Subject{Kind: SubjectAgent, ID: p.EgressClientID}, true
	}
	if p.UserID != "" && p.UserID != principal.IdentityPrincipal {
		return Subject{Kind: SubjectUser, ID: p.UserID}, true
	}
	if p.Identity != nil && p.Identity.Email != "" {
		return Subject{Kind: SubjectIdentity, ID: p.Identity.Email}, true
	}
	if p.UserID == principal.IdentityPrincipal {
		return Subject{Kind: SubjectIdentity, ID: principal.IdentityPrincipal}, true
	}
	return Subject{}, false
}

func PrincipalForSubject(s Subject) (*principal.Principal, bool) {
	switch s.Kind {
	case SubjectUser:
		if s.ID == "" {
			return nil, false
		}
		return &principal.Principal{UserID: s.ID}, true
	case SubjectIdentity:
		if s.ID == principal.IdentityPrincipal {
			return &principal.Principal{UserID: principal.IdentityPrincipal}, true
		}
		if s.ID == "" {
			return nil, false
		}
		return &principal.Principal{Identity: &core.UserIdentity{Email: s.ID}}, true
	case SubjectAgent:
		if s.ID == "" {
			return nil, false
		}
		return &principal.Principal{EgressClientID: s.ID}, true
	default:
		return nil, false
	}
}

func WithSubjectFromPrincipal(ctx context.Context, p *principal.Principal) context.Context {
	if _, ok := SubjectFromContext(ctx); ok {
		return ctx
	}
	subject, ok := SubjectForPrincipal(p)
	if !ok {
		return ctx
	}
	return WithSubject(ctx, subject)
}
