package workflow

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestDefinitionBelongsToApp(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id, app string
		want    bool
	}{
		{"app_ai-spend-tracker_ai_spend_tracker_sync", "ai-spend-tracker", true},
		{"app_ci-cd_ci_cd_pr_sync", "ci-cd", true},
		{"app_ci-cd_ci_cd_pr_sync", "ai-spend-tracker", false},
		{"app_ai-spend-tracker_x", "ai-spend", false}, // must not prefix-match shorter names
		{"cfg_slack_agent_default", "slack", false},
		{"", "ci-cd", false},
		{"app_ci-cd_x", "", false},
	}
	for _, tc := range cases {
		if got := DefinitionBelongsToApp(tc.id, tc.app); got != tc.want {
			t.Fatalf("DefinitionBelongsToApp(%q, %q) = %v, want %v", tc.id, tc.app, got, tc.want)
		}
	}
}

func TestOwnerKeyFromRunID(t *testing.T) {
	t.Parallel()
	payload, err := json.Marshal(map[string]any{
		"kind":      "temporal-run",
		"owner_key": "ai-spend-tracker",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	if got := OwnerKeyFromRunID(encoded); got != "ai-spend-tracker" {
		t.Fatalf("OwnerKeyFromRunID = %q, want ai-spend-tracker", got)
	}
	if got := OwnerKeyFromRunID("not-a-handle"); got != "" {
		t.Fatalf("OwnerKeyFromRunID(opaque) = %q, want empty", got)
	}
}

func TestRunMatchesTargetApp(t *testing.T) {
	t.Parallel()

	hydrated := &Run{
		ID:           "opaque",
		DefinitionID: "app_other_wf",
		Target: Target{Steps: []Step{{
			ID:  "sync",
			App: &AppCall{Name: "ai-spend-tracker", Operation: "runs.sync"},
		}}},
	}
	if !RunMatchesTargetApp(hydrated, "ai-spend-tracker") {
		t.Fatal("expected hydrated step app to match")
	}
	if RunMatchesTargetApp(hydrated, "ci-cd") {
		t.Fatal("expected non-matching step app to miss")
	}

	listSummary := &Run{
		ID: encodeOwnerHandle(t, "ai-spend-tracker"),
		DefinitionID: "app_ai-spend-tracker_ai_spend_tracker_sync_every_four_hours",
		Target:       Target{}, // list summaries omit steps
	}
	if !RunMatchesTargetApp(listSummary, "ai-spend-tracker") {
		t.Fatal("expected empty-target list summary to match via definition/owner")
	}
	if RunMatchesTargetApp(listSummary, "ci-cd") {
		t.Fatal("expected empty-target list summary not to match other apps")
	}

	ownerOnly := &Run{
		ID:           encodeOwnerHandle(t, "ci-cd"),
		DefinitionID: "cfg_not_app_owned",
		Target:       Target{},
	}
	if !RunMatchesTargetApp(ownerOnly, "ci-cd") {
		t.Fatal("expected owner_key handle to match when definition is not app-owned")
	}
}

func encodeOwnerHandle(t *testing.T, owner string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"kind":      "temporal-run",
		"owner_key": owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}
