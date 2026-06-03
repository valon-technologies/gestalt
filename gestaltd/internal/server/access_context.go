package server

import (
	"fmt"
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func requireUserCaller(w http.ResponseWriter, p *principal.Principal) error {
	if !principal.IsNonUserPrincipal(p) {
		return nil
	}
	writeError(w, http.StatusForbidden, errUserRequired.Error())
	return errUserRequired
}

func managedSubjectCallerIsUnscoped(p *principal.Principal) bool {
	p = principal.Canonicalized(p)
	if p == nil || p.Source != principal.SourceAPIToken {
		return true
	}
	return p.TokenPermissions == nil && len(p.Scopes) == 0
}

func canonicalServiceAccountSubjectID(subjectID string) (string, error) {
	kind, id, ok := core.ParseSubjectID(subjectID)
	if !ok || kind != coredata.ManagedSubjectKindServiceAccount || !validManagedSubjectLocalID(id) {
		return "", fmt.Errorf("subjectId must be a canonical %s:<id> subject ID", coredata.ManagedSubjectKindServiceAccount)
	}
	return kind + ":" + id, nil
}

func validManagedSubjectLocalID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '.', ch == '_', ch == '-':
		default:
			return false
		}
	}
	return true
}
