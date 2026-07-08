package agentmanager

import (
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

// AuditSubjectID returns the trusted caller subject for persisted created_by
// metadata from RequestContext.subject.
func AuditSubjectID(reqCtx *proto.RequestContext) string {
	if reqCtx != nil {
		return strings.TrimSpace(reqCtx.GetSubject().GetId())
	}
	return ""
}

// IdempotencySubjectID scopes agent create idempotency keys. Delegated provider
// calls may set top-level subject; otherwise identity comes from
// RequestContext.subject.
func IdempotencySubjectID(reqCtx *proto.RequestContext, subject *proto.SubjectContext) string {
	if id := strings.TrimSpace(subject.GetId()); id != "" {
		return id
	}
	if reqCtx != nil {
		return strings.TrimSpace(reqCtx.GetSubject().GetId())
	}
	return ""
}

// CallerSubjectIDFromPrincipal returns the authenticated caller subject id for
// manager and gateway paths when building provider RequestContext.
func CallerSubjectIDFromPrincipal(p *principal.Principal) string {
	p = principal.Canonicalized(p)
	if p == nil {
		return ""
	}
	if id := strings.TrimSpace(p.SubjectID); id != "" {
		return id
	}
	return strings.TrimSpace(principal.EffectiveCredentialSubjectID(p))
}
