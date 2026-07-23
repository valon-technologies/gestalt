package daemon

import "testing"

func TestAppPublishPublication(t *testing.T) {
	t.Parallel()

	const sha = "abc123def456abc123def456abc123def456abcd"
	publication, err := appPublishPublication(
		"https://github.com/valon-technologies/valon-tools/actions/runs/123",
		3251,
		"https://github.com/valon-technologies/valon-tools/pull/3251",
		"Add registry admin table",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("appPublishPublication: %v", err)
	}
	if publication.TriggerPullRequest == nil || publication.TriggerPullRequest.Number != 3251 ||
		publication.TriggerPullRequest.Title != "Add registry admin table" || publication.TriggerCommit != nil {
		t.Fatalf("publication = %#v", publication)
	}

	publication, err = appPublishPublication(
		"https://github.com/valon-technologies/valon-tools/actions/runs/124",
		0,
		"",
		"",
		sha,
		"https://github.com/valon-technologies/valon-tools/commit/"+sha,
	)
	if err != nil {
		t.Fatalf("appPublishPublication commit: %v", err)
	}
	if publication.TriggerCommit == nil || publication.TriggerCommit.SHA != sha || publication.TriggerPullRequest != nil {
		t.Fatalf("commit publication = %#v", publication)
	}
}

func TestAppPublishPublicationRejectsAmbiguousTrigger(t *testing.T) {
	t.Parallel()

	const sha = "abc123def456abc123def456abc123def456abcd"
	_, err := appPublishPublication(
		"https://github.com/valon-technologies/valon-tools/actions/runs/123",
		3251,
		"https://github.com/valon-technologies/valon-tools/pull/3251",
		"",
		sha,
		"https://github.com/valon-technologies/valon-tools/commit/"+sha,
	)
	if err == nil {
		t.Fatal("expected ambiguous trigger error")
	}
}
