package main

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/indexeddbcodec"
)

func TestBuildResolverMapsPrefersMostRecentCleanName(t *testing.T) {
	rows := []mealRecord{
		{
			id:                 "meal-1",
			name:               "Ada Lovelace",
			createdBySubjectID: "user:abc",
			createdAt:          "2026-06-01T12:00:00Z",
		},
		{
			id:                 "meal-2",
			name:               "Ada L.",
			createdBySubjectID: "user:abc",
			createdAt:          "2026-06-03T12:00:00Z",
		},
		{
			id:                 "meal-3",
			name:               "ada@example.com",
			createdBySubjectID: "user:abc",
			createdAt:          "2026-06-04T12:00:00Z",
		},
	}

	maps := buildResolverMaps(rows)
	if got := maps.subjectIDToName["user:abc"]; got != "Ada L." {
		t.Fatalf("subject map = %q, want %q", got, "Ada L.")
	}
	if got := maps.emailToName["ada@example.com"]; got != "Ada L." {
		t.Fatalf("email map = %q, want %q", got, "Ada L.")
	}
}

func TestPlanPatchesNameAndClaimedBy(t *testing.T) {
	rows := []mealRecord{
		{
			pkHash:             []byte{1},
			id:                 "good",
			name:               "David Kim",
			createdBySubjectID: "user:good",
			createdAt:          "2026-06-01T12:00:00Z",
			payload:            indexeddbcodec.Record{"id": "good", "name": "David Kim"},
		},
		{
			pkHash:             []byte{2},
			id:                 "bad",
			name:               "david.kim@valon.com",
			claimedBy:          "david.kim@valon.com",
			createdBySubjectID: "user:good",
			createdAt:          "2026-06-04T12:00:00Z",
			payload: indexeddbcodec.Record{
				"id":                    "bad",
				"name":                  "david.kim@valon.com",
				"claimed_by":            "david.kim@valon.com",
				"created_by_subject_id": "user:good",
			},
		},
	}

	maps := buildResolverMaps(rows)
	summary, patches := planPatches(rows, maps)
	if summary.patched != 1 || summary.claimedByPatched != 1 {
		t.Fatalf("summary = %+v, want one name and one claimed_by patch", summary)
	}
	if len(patches) != 1 {
		t.Fatalf("patches = %d, want 1", len(patches))
	}
	if patches[0].newName != "David Kim" || patches[0].newClaimedBy != "David Kim" {
		t.Fatalf("patch = %+v", patches[0])
	}
}

func TestIsBadDisplayName(t *testing.T) {
	cases := []struct {
		name      string
		subjectID string
		want      bool
	}{
		{"david@valon.com", "user:1", true},
		{"user:deadbeef", "user:deadbeef", true},
		{"David Kim", "user:1", false},
		{"", "user:1", true},
	}
	for _, tc := range cases {
		if got := isBadDisplayName(tc.name, tc.subjectID); got != tc.want {
			t.Fatalf("isBadDisplayName(%q, %q) = %v, want %v", tc.name, tc.subjectID, got, tc.want)
		}
	}
}
