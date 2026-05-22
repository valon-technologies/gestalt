package workflowprincipal

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func FromExecutionReference(ref *coreworkflow.ExecutionReference) *principal.Principal {
	if ref == nil {
		return nil
	}
	return principalFromExecutionReferenceFields(
		ref.SubjectID,
		ref.CredentialSubjectID,
		ref.DisplayName,
		ref.SubjectKind,
		ref.AuthSource,
		ref.Permissions,
	)
}

func RuntimePrincipalFromExecutionReference(ref *coreworkflow.ExecutionReference) *principal.Principal {
	if ref == nil {
		return nil
	}
	if runAs := core.NormalizeRunAsSubject(ref.RunAs); runAs != nil {
		return principalFromExecutionReferenceFields(
			runAs.SubjectID,
			runAs.CredentialSubjectID,
			runAs.DisplayName,
			runAs.SubjectKind,
			runAs.AuthSource,
			ref.Permissions,
		)
	}
	return FromExecutionReference(ref)
}

func principalFromExecutionReferenceFields(subjectID, credentialSubjectID, displayName, subjectKind, authSource string, permissions []core.AccessPermission) *principal.Principal {
	compiled := principal.CompilePermissions(permissions)
	if permissions != nil && compiled == nil {
		compiled = principal.PermissionSet{}
	}
	value := &principal.Principal{
		SubjectID:           strings.TrimSpace(subjectID),
		CredentialSubjectID: strings.TrimSpace(credentialSubjectID),
		DisplayName:         strings.TrimSpace(displayName),
		Kind:                principal.Kind(strings.TrimSpace(subjectKind)),
		Scopes:              principal.PermissionApps(compiled),
		TokenPermissions:    compiled,
	}
	principal.SetAuthSource(value, authSource)
	if value.CredentialSubjectID == "" && principal.IsSystemSubjectID(value.SubjectID) {
		value.CredentialSubjectID = value.SubjectID
	}
	return principal.Canonicalize(value)
}
