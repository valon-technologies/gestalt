package bootstrap

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

func executionReferencePrincipal(subjectID, credentialSubjectID string, permissions []core.AccessPermission) *principal.Principal {
	compiled := principal.CompilePermissions(permissions)
	value := &principal.Principal{
		SubjectID:           strings.TrimSpace(subjectID),
		CredentialSubjectID: strings.TrimSpace(credentialSubjectID),
		Scopes:              principal.PermissionPlugins(compiled),
		TokenPermissions:    compiled,
	}
	if value.CredentialSubjectID == "" && principal.IsSystemSubjectID(value.SubjectID) {
		value.CredentialSubjectID = value.SubjectID
	}
	return principal.Canonicalize(value)
}

func workflowExecutionReferencePrincipal(ref *coreworkflow.ExecutionReference) *principal.Principal {
	if ref == nil {
		return nil
	}
	compiled := principal.CompilePermissions(ref.Permissions)
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
