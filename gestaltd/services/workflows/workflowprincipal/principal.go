package workflowprincipal

import (
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

func FromExecutionReference(ref *coreworkflow.ExecutionReference) *principal.Principal {
	if ref == nil {
		return nil
	}
	compiled := principal.CompilePermissions(ref.Permissions)
	if ref.Permissions != nil && compiled == nil {
		compiled = principal.PermissionSet{}
	}
	value := &principal.Principal{
		SubjectID:           strings.TrimSpace(ref.SubjectID),
		CredentialSubjectID: strings.TrimSpace(ref.CredentialSubjectID),
		DisplayName:         strings.TrimSpace(ref.DisplayName),
		Kind:                principal.Kind(strings.TrimSpace(ref.SubjectKind)),
		Scopes:              principal.PermissionPlugins(compiled),
		TokenPermissions:    compiled,
	}
	principal.SetAuthSource(value, ref.AuthSource)
	if value.CredentialSubjectID == "" && principal.IsSystemSubjectID(value.SubjectID) {
		value.CredentialSubjectID = value.SubjectID
	}
	return principal.Canonicalize(value)
}
