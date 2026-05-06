package authorization

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestExternalIdentityAssumptionSubjectRefsIncludesLegacyUserSubjectType(t *testing.T) {
	t.Parallel()

	refs := externalIdentityAssumptionSubjectRefs(&principal.Principal{
		UserID:    "user-123",
		SubjectID: principal.UserSubjectID("user-123"),
		Kind:      principal.KindUser,
	})
	if len(refs) != 2 {
		t.Fatalf("subject refs length = %d, want 2: %#v", len(refs), refs)
	}
	if refs[0].GetType() != subjectTypeSubject || refs[0].GetId() != "user:user-123" {
		t.Fatalf("subject ref[0] = %#v", refs[0])
	}
	if refs[1].GetType() != subjectTypeUser || refs[1].GetId() != "user:user-123" {
		t.Fatalf("subject ref[1] = %#v", refs[1])
	}
}
