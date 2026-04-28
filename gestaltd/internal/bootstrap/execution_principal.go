package bootstrap

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
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
