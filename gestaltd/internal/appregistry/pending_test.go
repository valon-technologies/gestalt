package appregistry

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPrunePendingIndex_MovesStaleEntryToFailed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 24, 20, 0, 0, 0, time.UTC)
	staleUpdatedAt := now.Add(-31 * time.Minute)
	pending := &PendingIndex{
		SchemaVersion: PendingIndexSchemaVersion,
		App:           "traffic-cop",
		Pending: map[string]PendingVersion{
			"0.0.0-snapshot.gabc123": {
				Version:   "0.0.0-snapshot.gabc123",
				SourceRef: "abc123def456abc123def456abc123def456abcd",
				StartedAt: staleUpdatedAt,
				UpdatedAt: staleUpdatedAt,
				Phase:     PendingPhasePublishing,
			},
		},
	}
	failed := NewEmptyFailedIndex("traffic-cop")

	pendingChanged, failedChanged := PrunePendingIndex(pending, failed, NewEmptyIndex(), now)
	if !pendingChanged || !failedChanged {
		t.Fatalf("pendingChanged=%v failedChanged=%v", pendingChanged, failedChanged)
	}
	if len(pending.Pending) != 0 {
		t.Fatalf("pending = %#v", pending.Pending)
	}
	entry, ok := failed.Failed["0.0.0-snapshot.gabc123"]
	if !ok {
		t.Fatalf("failed = %#v", failed.Failed)
	}
	if entry.Reason != FailedReasonStale || !entry.FailedAt.Equal(now) {
		t.Fatalf("failed entry = %#v", entry)
	}
}

func TestPrunePendingIndex_DropsAlreadyPublishedVersion(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	pending := &PendingIndex{
		SchemaVersion: PendingIndexSchemaVersion,
		App:           "traffic-cop",
		Pending: map[string]PendingVersion{
			"0.0.0-snapshot.gabc123": {
				Version:   "0.0.0-snapshot.gabc123",
				SourceRef: "abc123def456abc123def456abc123def456abcd",
				StartedAt: now,
				UpdatedAt: now,
				Phase:     PendingPhasePublishing,
			},
		},
	}
	published := &Index{
		SchemaVersion: IndexSchemaVersion,
		Apps: map[string]AppVersions{
			"traffic-cop": {
				Versions: map[string]IndexVersion{
					"0.0.0-snapshot.gabc123": {
						Metadata:    "apps/traffic-cop/versions/0.0.0-snapshot.gabc123.json",
						PublishedAt: now,
					},
				},
			},
		},
	}

	pendingChanged, failedChanged := PrunePendingIndex(pending, NewEmptyFailedIndex("traffic-cop"), published, now)
	if !pendingChanged || failedChanged {
		t.Fatalf("pendingChanged=%v failedChanged=%v", pendingChanged, failedChanged)
	}
	if len(pending.Pending) != 0 {
		t.Fatalf("pending = %#v", pending.Pending)
	}
}

func TestPruneFailedIndex_RemovesOldAndPublishedEntries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	failed := &FailedIndex{
		SchemaVersion: FailedIndexSchemaVersion,
		App:           "traffic-cop",
		Failed: map[string]FailedVersion{
			"0.0.0-snapshot.gold": {
				Version:   "0.0.0-snapshot.gold",
				SourceRef: "abc123def456abc123def456abc123def456abcd",
				StartedAt: now.Add(-40 * 24 * time.Hour),
				FailedAt:  now.Add(-31 * 24 * time.Hour),
				Reason:    FailedReasonWorkflowFailed,
			},
			"0.0.0-snapshot.gpublished": {
				Version:   "0.0.0-snapshot.gpublished",
				SourceRef: "def456def456def456def456def456def456def4",
				StartedAt: now.Add(-time.Hour),
				FailedAt:  now.Add(-time.Hour),
				Reason:    FailedReasonWorkflowFailed,
			},
		},
	}
	published := &Index{
		SchemaVersion: IndexSchemaVersion,
		Apps: map[string]AppVersions{
			"traffic-cop": {
				Versions: map[string]IndexVersion{
					"0.0.0-snapshot.gpublished": {
						Metadata:    "apps/traffic-cop/versions/0.0.0-snapshot.gpublished.json",
						PublishedAt: now,
					},
				},
			},
		},
	}

	if !PruneFailedIndex(failed, published, now) {
		t.Fatal("expected prune to change failed index")
	}
	if len(failed.Failed) != 0 {
		t.Fatalf("failed = %#v", failed.Failed)
	}
}

func TestUpsertPendingVersion_PreservesStartedAt(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 7, 24, 19, 0, 0, 0, time.UTC)
	updatedAt := startedAt.Add(2 * time.Minute)
	index := &PendingIndex{
		SchemaVersion: PendingIndexSchemaVersion,
		App:           "traffic-cop",
		Pending: map[string]PendingVersion{
			"0.0.0-snapshot.gabc123": {
				Version:   "0.0.0-snapshot.gabc123",
				SourceRef: "abc123def456abc123def456abc123def456abcd",
				StartedAt: startedAt,
				UpdatedAt: startedAt,
				Phase:     PendingPhasePublishing,
			},
		},
	}

	updated, changed := UpsertPendingVersion(index, "traffic-cop", PendingVersion{
		Version:   "0.0.0-snapshot.gabc123",
		SourceRef: "abc123def456abc123def456abc123def456abcd",
	}, updatedAt)
	if !changed {
		t.Fatal("expected upsert to report change")
	}
	entry := updated.Pending["0.0.0-snapshot.gabc123"]
	if !entry.StartedAt.Equal(startedAt) || !entry.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestDecodePendingAndFailedIndexRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 24, 19, 0, 0, 0, time.UTC)
	pending := &PendingIndex{
		SchemaVersion: PendingIndexSchemaVersion,
		App:           "traffic-cop",
		Pending: map[string]PendingVersion{
			"0.0.0-snapshot.gabc123": {
				Version:    "0.0.0-snapshot.gabc123",
				SourceRef:  "abc123def456abc123def456abc123def456abcd",
				Repository: "github.com/valon-technologies/toolshed",
				StartedAt:  now,
				UpdatedAt:  now,
				Phase:      PendingPhasePublishing,
			},
		},
	}
	pendingData, err := json.Marshal(pending)
	if err != nil {
		t.Fatalf("marshal pending: %v", err)
	}
	decodedPending, err := DecodePendingIndex(pendingData)
	if err != nil {
		t.Fatalf("DecodePendingIndex: %v", err)
	}
	if len(decodedPending.Pending) != 1 {
		t.Fatalf("decoded pending = %#v", decodedPending.Pending)
	}

	failed := &FailedIndex{
		SchemaVersion: FailedIndexSchemaVersion,
		App:           "traffic-cop",
		Failed: map[string]FailedVersion{
			"0.0.0-snapshot.gabc123": {
				Version:   "0.0.0-snapshot.gabc123",
				SourceRef: "abc123def456abc123def456abc123def456abcd",
				StartedAt: now,
				FailedAt:  now.Add(time.Minute),
				Reason:    FailedReasonStale,
			},
		},
	}
	failedData, err := json.Marshal(failed)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	decodedFailed, err := DecodeFailedIndex(failedData)
	if err != nil {
		t.Fatalf("DecodeFailedIndex: %v", err)
	}
	if len(decodedFailed.Failed) != 1 {
		t.Fatalf("decoded failed = %#v", decodedFailed.Failed)
	}
}

func TestRecordFailedVersion_IsIdempotent(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	pending := PendingVersion{
		Version:   "0.0.0-snapshot.gabc123",
		SourceRef: "abc123def456abc123def456abc123def456abcd",
		StartedAt: now,
		UpdatedAt: now,
		Phase:     PendingPhasePublishing,
	}
	index := NewEmptyFailedIndex("traffic-cop")

	updated, changed := RecordFailedVersion(index, "traffic-cop", pending, now, FailedReasonWorkflowFailed)
	if !changed {
		t.Fatal("expected first record to change failed index")
	}
	_, changed = RecordFailedVersion(updated, "traffic-cop", pending, now.Add(time.Minute), FailedReasonWorkflowFailed)
	if changed {
		t.Fatal("expected second record to be a no-op")
	}
}

func TestPublishStartedAtFromPending(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 7, 24, 19, 0, 0, 0, time.UTC)
	index := &PendingIndex{
		Pending: map[string]PendingVersion{
			"0.0.1": {Version: "0.0.1", StartedAt: startedAt},
		},
	}

	got, ok := PublishStartedAtFromPending(index, "0.0.1")
	if !ok || !got.Equal(startedAt) {
		t.Fatalf("PublishStartedAtFromPending() = (%v, %v), want (%v, true)", got, ok, startedAt)
	}
	if _, ok := PublishStartedAtFromPending(index, "missing"); ok {
		t.Fatal("expected missing version to return false")
	}
	if _, ok := PublishStartedAtFromPending(nil, "0.0.1"); ok {
		t.Fatal("expected nil index to return false")
	}
}

func TestUpsertAppIndex_CopiesPublishStartedAt(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 7, 24, 19, 0, 0, 0, time.UTC)
	entry := Entry{
		SchemaVersion:    EntrySchemaVersion,
		App:              "traffic-cop",
		Version:          "0.0.1",
		SourceRef:        "651a5c30feb995c9364c38f63d0d5c3880bc2055",
		ManifestPath:     "apps/traffic-cop/manifest.yaml",
		Repository:       "github.com/valon-technologies/valon-tools",
		PublishedAt:      time.Date(2026, 7, 24, 19, 4, 32, 0, time.UTC),
		PublishStartedAt: startedAt,
		Artifacts: map[string]Artifact{
			"linux/amd64": {
				URL:       "https://example.com/artifact.tar.gz",
				PublicURL: "https://example.com/artifact.tar.gz",
				SHA256:    "deadbeef",
			},
		},
	}

	index, changed, err := UpsertAppIndex(NewEmptyIndex(), entry, "apps/traffic-cop/versions/0.0.1.json", "", "")
	if err != nil || !changed {
		t.Fatalf("UpsertAppIndex() = changed %v, err %v", changed, err)
	}
	got := index.Apps["traffic-cop"].Versions["0.0.1"]
	if !got.PublishStartedAt.Equal(startedAt) {
		t.Fatalf("index publishStartedAt = %v, want %v", got.PublishStartedAt, startedAt)
	}
}
