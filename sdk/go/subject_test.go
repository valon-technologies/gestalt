package gestalt

import (
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestSubjectProtoRoundTrip(t *testing.T) {
	original := &Subject{
		ID:                  "user:abc-123",
		CredentialSubjectID: "cred-subject-1",
		Email:               "ada@example.com",
		DisplayName:         "Ada Lovelace",
	}

	roundTripped := subjectFromProto(subjectToProto(original))
	if roundTripped == nil {
		t.Fatal("subjectFromProto returned nil")
	}
	if *roundTripped != *original {
		t.Fatalf("round trip = %+v, want %+v", *roundTripped, *original)
	}
}

func TestSubjectFromProtoNil(t *testing.T) {
	if subjectFromProto(nil) != nil {
		t.Fatal("expected nil for nil proto")
	}
}

func TestSubjectToProtoNil(t *testing.T) {
	if subjectToProto(nil) != nil {
		t.Fatal("expected nil for nil subject")
	}
}

func TestSubjectFromProtoEmptyDisplayName(t *testing.T) {
	got := subjectFromProto(&proto.SubjectContext{
		Id:    "service-account:triage-bot",
		Email: "",
	})
	if got == nil || got.DisplayName != "" {
		t.Fatalf("DisplayName = %q, want empty", got.DisplayName)
	}
}
