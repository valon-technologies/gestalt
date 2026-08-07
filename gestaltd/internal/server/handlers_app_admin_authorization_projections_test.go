package server

import (
	"testing"
)

func TestAppAdminGrantRosterPartition(t *testing.T) {
	t.Parallel()

	rows := []appAdminMemberRow{
		{SubjectID: "user:alice", SelectorKind: "subject_id", SelectorValue: "user:alice", Role: "admin", Source: "static", Effective: true},
		{SelectorKind: "subject_set", SelectorValue: "group:eng#member", Role: "viewer", Source: "static", Effective: true},
		{SubjectID: "service_account:slack-bot", SelectorKind: "subject_id", SelectorValue: "service_account:slack-bot", Role: "viewer", Source: "static", Effective: true},
		{SubjectID: "service_account:slack-bot", SelectorKind: "subject_id", SelectorValue: "service_account:slack-bot", Role: "viewer", Source: "dynamic", Effective: false, ShadowedBy: "static viewer grant"},
		{SubjectID: "user:bob", SelectorKind: "subject_id", SelectorValue: "user:bob", Role: "viewer", Source: "dynamic", Effective: true},
	}

	s := &Server{}
	humans := s.projectAppAdminHumanMemberRows(t.Context(), rows)
	identities := s.projectAppAdminIdentityRows(t.Context(), rows)

	if len(humans) != 3 {
		t.Fatalf("humans = %#v, want 3 rows", humans)
	}
	if len(identities) != 2 {
		t.Fatalf("identities = %#v, want 2 rows", identities)
	}
	for _, row := range humans {
		if isAppAdminServiceAccountRow(row) {
			t.Fatalf("service account leaked into humans: %#v", row)
		}
	}
	for _, row := range identities {
		if !isAppAdminServiceAccountRow(appAdminMemberRow{
			SubjectID:    row.SubjectID,
			SelectorKind: "subject_id",
		}) {
			t.Fatalf("non-service-account leaked into identities: %#v", row)
		}
	}
	if got := len(humans) + len(identities); got != len(rows) {
		t.Fatalf("partition lost or duplicated rows: humans=%d identities=%d input=%d", len(humans), len(identities), len(rows))
	}
}
