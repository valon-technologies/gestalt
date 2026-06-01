package invocation

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func ApplyDelegation(ctx context.Context, p *principal.Principal, runAs *core.RunAsSubject) (context.Context, *principal.Principal) {
	runAs = core.NormalizeRunAsSubject(runAs)
	if runAs != nil {
		ctx = WithRunAsAudit(ctx, AuditSubjectFromPrincipal(p), runAs)
		p = RunAsPrincipal(p, runAs)
		ctx = principal.WithPrincipal(ctx, p)
	}
	return ctx, p
}

func RunAsPrincipal(base *principal.Principal, runAs *core.RunAsSubject) *principal.Principal {
	base = principal.Canonicalized(base)
	runAs = core.NormalizeRunAsSubject(runAs)
	if runAs == nil {
		return base
	}
	if base == nil {
		base = &principal.Principal{}
	}
	value := &principal.Principal{
		SubjectID:           strings.TrimSpace(runAs.SubjectID),
		CredentialSubjectID: strings.TrimSpace(runAs.CredentialSubjectID),
		DisplayName:         strings.TrimSpace(runAs.DisplayName),
		Kind:                principal.Kind(strings.TrimSpace(runAs.SubjectKind)),
		Scopes:              append([]string(nil), base.Scopes...),
		TokenPermissions:    principal.ClonePermissionSet(base.TokenPermissions),
	}
	if value.Kind == principal.KindUser && value.SubjectID == strings.TrimSpace(base.SubjectID) {
		value.UserID = strings.TrimSpace(base.UserID)
		value.Identity = base.Identity
	}
	principal.SetAuthSource(value, runAs.AuthSource)
	if value.CredentialSubjectID == "" && principal.IsSystemSubjectID(value.SubjectID) {
		value.CredentialSubjectID = value.SubjectID
	}
	return principal.Canonicalize(value)
}

func AuditSubjectFromPrincipal(p *principal.Principal) *core.RunAsSubject {
	p = principal.Canonicalized(p)
	if p == nil {
		return nil
	}
	return core.NormalizeRunAsSubject(&core.RunAsSubject{
		SubjectID:           strings.TrimSpace(p.SubjectID),
		SubjectKind:         string(p.Kind),
		CredentialSubjectID: strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		DisplayName:         strings.TrimSpace(p.DisplayName),
		AuthSource:          p.AuthSource(),
	})
}
