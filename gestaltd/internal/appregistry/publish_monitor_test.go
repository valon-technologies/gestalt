package appregistry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPublicationPullRequestFromCommitTitle(t *testing.T) {
	t.Parallel()

	repo := "valon-technologies/toolshed"
	pr := publicationPullRequestFromCommitTitle(repo, "Add registry deploy banner again. (#3665)")
	if pr == nil || pr.Number != 3665 || pr.Title != "Add registry deploy banner again." ||
		pr.URL != "https://github.com/valon-technologies/toolshed/pull/3665" {
		t.Fatalf("squash merge publication = %#v", pr)
	}

	pr = publicationPullRequestFromCommitTitle(repo, "Merge pull request #3660 from valon-technologies/branch")
	if pr == nil || pr.Number != 3660 || pr.Title != "" {
		t.Fatalf("merge commit publication = %#v", pr)
	}
}

func TestSnapshotVersion(t *testing.T) {
	t.Parallel()

	got := SnapshotVersion("ABC123DEF456ABC123DEF456ABC123DEF456ABCD")
	want := "0.0.0-snapshot.gabc123def456abc123def456abc123def456abcd"
	if got != want {
		t.Fatalf("SnapshotVersion() = %q, want %q", got, want)
	}
}

func TestPublishMonitorListPending(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/valon-technologies/toolshed/actions/runs" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"workflow_runs": [
				{
					"head_sha": "abc123def456abc123def456abc123def456abcd",
					"html_url": "https://github.com/valon-technologies/toolshed/actions/runs/1",
					"status": "in_progress",
					"display_title": "Ship admin table. (#42)",
					"path": ".github/workflows/auto-publish-app-registry.yml",
					"created_at": "2026-07-23T14:00:00Z"
				},
				{
					"head_sha": "fedcba9876543210fedcba9876543210fedcba98",
					"html_url": "https://github.com/valon-technologies/toolshed/actions/runs/2",
					"status": "in_progress",
					"display_title": "Already published. (#43)",
					"path": ".github/workflows/auto-publish-app-registry.yml",
					"created_at": "2026-07-23T13:00:00Z"
				},
				{
					"head_sha": "2222222222222222222222222222222222222222",
					"html_url": "https://github.com/valon-technologies/toolshed/actions/runs/3",
					"status": "in_progress",
					"display_title": "Other workflow. (#44)",
					"path": ".github/workflows/ci.yml",
					"created_at": "2026-07-23T15:00:00Z"
				}
			]
		}`))
	}))
	t.Cleanup(server.Close)

	monitor := &PublishMonitor{
		HTTPClient: server.Client(),
		Token:      "test-token",
		APIBaseURL: server.URL,
	}
	published := map[string]struct{}{
		SnapshotVersion("fedcba9876543210fedcba9876543210fedcba98"): {},
	}
	pending, err := monitor.ListPending(context.Background(), PublishMonitorConfig{
		Repository: "valon-technologies/toolshed",
		Workflow:   "auto-publish-app-registry.yml",
		Branch:     "main",
	}, published)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %#v", pending)
	}
	if pending[0].ExpectedVersion != SnapshotVersion("abc123def456abc123def456abc123def456abcd") {
		t.Fatalf("expected version = %q", pending[0].ExpectedVersion)
	}
	if pending[0].TriggerPullRequest == nil || pending[0].TriggerPullRequest.Number != 42 {
		t.Fatalf("trigger pull request = %#v", pending[0].TriggerPullRequest)
	}
}

func TestPublishMonitorListPendingWithoutToken(t *testing.T) {
	t.Parallel()

	monitor := &PublishMonitor{}
	pending, err := monitor.ListPending(context.Background(), PublishMonitorConfig{
		Repository: "valon-technologies/toolshed",
		Workflow:   "auto-publish-app-registry.yml",
	}, nil)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if pending != nil {
		t.Fatalf("pending = %#v, want nil", pending)
	}
}

func TestPublishMonitorListPendingSortsNewestFirst(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"workflow_runs": [
				{
					"head_sha": "1111111111111111111111111111111111111111",
					"html_url": "https://github.com/example/actions/runs/1",
					"status": "queued",
					"display_title": "Older. (#1)",
					"path": ".github/workflows/auto-publish-app-registry.yml",
					"created_at": "2026-07-23T12:00:00Z"
				},
				{
					"head_sha": "2222222222222222222222222222222222222222",
					"html_url": "https://github.com/example/actions/runs/2",
					"status": "in_progress",
					"display_title": "Newer. (#2)",
					"path": ".github/workflows/auto-publish-app-registry.yml",
					"created_at": "2026-07-23T14:00:00Z"
				}
			]
		}`))
	}))
	t.Cleanup(server.Close)

	monitor := &PublishMonitor{
		HTTPClient: server.Client(),
		Token:      "token",
		APIBaseURL: server.URL,
	}
	pending, err := monitor.ListPending(context.Background(), PublishMonitorConfig{
		Repository: "example/repo",
		Workflow:   "auto-publish-app-registry.yml",
	}, map[string]struct{}{})
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 2 || pending[0].StartedAt != time.Date(2026, 7, 23, 14, 0, 0, 0, time.UTC) {
		t.Fatalf("pending = %#v", pending)
	}
}
