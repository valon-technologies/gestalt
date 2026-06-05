package access

import (
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func SubjectFromPrincipal(p *principal.Principal) *proto.Subject {
	subjectID := strings.TrimSpace(principal.EffectiveCredentialSubjectID(p))
	if subjectID == "" {
		return nil
	}
	return &proto.Subject{Type: "subject", Id: subjectID}
}
