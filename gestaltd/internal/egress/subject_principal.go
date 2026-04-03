package egress

import (
	"context"

	"github.com/valon-technologies/gestalt/server/internal/principal"
)

func SubjectForPrincipal(p *principal.Principal) (Subject, bool) {
	if p == nil {
		return Subject{}, false
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
