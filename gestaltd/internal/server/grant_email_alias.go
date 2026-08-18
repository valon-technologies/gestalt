package server

import (
	"context"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

// attachGrantEmailSubjectAlias records the verified mailbox subject alongside
// the canonical user UUID so identity grant RPCs can list tokens stored under
// either alias.
func attachGrantEmailSubjectAlias(ctx context.Context, p *principal.Principal) context.Context {
	if p == nil || p.Identity == nil {
		return ctx
	}
	email := strings.TrimSpace(p.Identity.Email)
	if email == "" || !strings.Contains(email, "@") {
		return ctx
	}
	canonical := principal.Canonicalized(p)
	canonicalID := strings.TrimSpace(canonical.SubjectID)
	emailSubject := principal.UserSubjectID(email)
	if emailSubject == "" || emailSubject == canonicalID {
		return ctx
	}
	call := gestalt.IdentityCallContextFromContext(ctx)
	if strings.TrimSpace(call.CallerSubjectID) == "" {
		call.CallerSubjectID = canonicalID
	}
	call.Introspection = &gestalt.IntrospectResponse{
		Active:  true,
		Subject: emailSubject,
	}
	return gestalt.WithIdentityCallContext(ctx, call)
}
