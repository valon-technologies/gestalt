package agents

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestSubjectToProtoNormalizesRunAsIdentity(t *testing.T) {
	t.Parallel()

	subject := core.RunAsSubject{
		SubjectID:           " user:123 ",
		CredentialSubjectID: " user:123 ",
	}
	got := subjectToProto(subject)
	if got == nil {
		t.Fatal("subjectToProto returned nil")
	}
	if got.GetId() != "user:123" {
		t.Fatalf("Id = %q, want user:123", got.GetId())
	}
	if got.GetCredentialSubjectId() != "user:123" {
		t.Fatalf("CredentialSubjectId = %q, want user:123", got.GetCredentialSubjectId())
	}
}
