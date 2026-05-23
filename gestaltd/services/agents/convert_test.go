package agents

import (
	"testing"
	"time"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
)

func TestStructFromMap_NormalizesTimeValues(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 11, 20, 31, 30, 840502000, time.UTC)
	record := map[string]any{
		"created_at": now,
		"expires_at": &now,
		"nested": map[string]any{
			"updated_at": now,
		},
		"values": []any{now, &now},
	}

	s, err := structFromMap(record)
	if err != nil {
		t.Fatalf("structFromMap: %v", err)
	}

	got := s.AsMap()
	want := now.Format(time.RFC3339Nano)
	if got["created_at"] != want {
		t.Fatalf("created_at = %#v, want %#v", got["created_at"], want)
	}
	if got["expires_at"] != want {
		t.Fatalf("expires_at = %#v, want %#v", got["expires_at"], want)
	}
	nested, ok := got["nested"].(map[string]any)
	if !ok || nested["updated_at"] != want {
		t.Fatalf("nested = %#v, want updated_at=%#v", got["nested"], want)
	}
	values, ok := got["values"].([]any)
	if !ok || len(values) != 2 {
		t.Fatalf("values = %#v, want 2 normalized entries", got["values"])
	}
	if values[0] != want || values[1] != want {
		t.Fatalf("values = %#v, want both %#v", values, want)
	}
}

func TestSubjectToProtoPreservesWireFieldsWithoutNormalization(t *testing.T) {
	t.Parallel()

	subject := coreagent.SubjectContext{
		SubjectID:           " user:123 ",
		SubjectKind:         "",
		CredentialSubjectID: "",
		DisplayName:         " Example ",
		AuthSource:          " oauth ",
	}
	got := subjectToProto(subject)
	if got == nil {
		t.Fatal("subjectToProto returned nil")
	}
	if got.GetId() != subject.SubjectID {
		t.Fatalf("Id = %q, want %q", got.GetId(), subject.SubjectID)
	}
	if got.GetKind() != subject.SubjectKind {
		t.Fatalf("Kind = %q, want %q", got.GetKind(), subject.SubjectKind)
	}
	if got.GetCredentialSubjectId() != subject.CredentialSubjectID {
		t.Fatalf("CredentialSubjectId = %q, want empty", got.GetCredentialSubjectId())
	}
	if got.GetDisplayName() != subject.DisplayName {
		t.Fatalf("DisplayName = %q, want %q", got.GetDisplayName(), subject.DisplayName)
	}
	if got.GetAuthSource() != subject.AuthSource {
		t.Fatalf("AuthSource = %q, want %q", got.GetAuthSource(), subject.AuthSource)
	}
}
