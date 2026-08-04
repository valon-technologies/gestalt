package workflow

import "testing"

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

func TestAppDefinitionID(t *testing.T) {
	t.Parallel()
	got := AppDefinitionID("ai-spend-tracker", "ai_spend_tracker_sync")
	want := "app_ai-spend-tracker_ai_spend_tracker_sync"
	if got != want {
		t.Fatalf("AppDefinitionID = %q, want %q", got, want)
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
		ID:           "opaque-list-id",
		DefinitionID: "app_ai-spend-tracker_ai_spend_tracker_sync_every_four_hours",
		Target:       Target{}, // list summaries omit steps
	}
	if !RunMatchesTargetApp(listSummary, "ai-spend-tracker") {
		t.Fatal("expected empty-target list summary to match via definition ownership")
	}
	if RunMatchesTargetApp(listSummary, "ci-cd") {
		t.Fatal("expected empty-target list summary not to match other apps")
	}

	cfgOwned := &Run{
		ID:           "opaque",
		DefinitionID: "cfg_slack_agent_default",
		Target:       Target{},
	}
	if RunMatchesTargetApp(cfgOwned, "slack") {
		t.Fatal("cfg_* definitions are not app-owned; empty targets must not match by name alone")
	}
}
