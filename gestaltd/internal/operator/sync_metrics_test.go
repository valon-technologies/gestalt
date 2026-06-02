package operator

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncMetricsRecorderKeepsAllArchiveFetchesSorted(t *testing.T) {
	t.Parallel()

	recorder := NewSyncMetricsRecorder()
	for i := 0; i < 8; i++ {
		recorder.RecordArchiveFetch(syncArchiveMetricsEvent{
			Subject:  "fetch-" + string(rune('a'+i)),
			Duration: time.Duration(i) * time.Second,
		})
	}

	got := recorder.Snapshot().Archives.Fetches
	if len(got) != 8 {
		t.Fatalf("fetches len = %d, want 8", len(got))
	}
	for i, fetch := range got {
		want := "fetch-" + string(rune('a'+7-i))
		if fetch.Subject != want {
			t.Fatalf("fetches[%d].subject = %q, want %q", i, fetch.Subject, want)
		}
	}
}

func TestSyncMetricsRecorderKeepsAllArtifactItemsSorted(t *testing.T) {
	t.Parallel()

	recorder := NewSyncMetricsRecorder()
	for i := 0; i < 8; i++ {
		sourceKind := syncArtifactSourceRemoteArchive
		result := syncArtifactResultMaterialized
		if i%2 == 0 {
			sourceKind = syncArtifactSourceLocalSource
			result = syncArtifactResultReused
		}
		recorder.RecordArtifact(syncArtifactMetricsEvent{
			Subject:    "artifact-" + string(rune('a'+i)),
			Kind:       "app",
			Name:       "artifact-" + string(rune('a'+i)),
			SourceKind: sourceKind,
			Result:     result,
			Reason:     syncArtifactReasonFresh,
			Duration:   time.Duration(i) * time.Second,
		})
	}

	metrics := recorder.Snapshot()
	if len(metrics.Artifacts.Items) != 8 {
		t.Fatalf("artifact items len = %d, want 8", len(metrics.Artifacts.Items))
	}
	if metrics.Artifacts.Items[0].Name != "artifact-h" {
		t.Fatalf("first artifact item = %q, want artifact-h", metrics.Artifacts.Items[0].Name)
	}
}

func TestRecordOutputStatsRecordsAllRoots(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	alpha := filepath.Join(dir, "providers", "alpha")
	beta := filepath.Join(dir, "providers", "beta")
	if err := os.MkdirAll(alpha, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(beta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(alpha, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beta, "b.txt"), []byte("beta-beta"), 0o644); err != nil {
		t.Fatal(err)
	}

	recorder := NewSyncMetricsRecorder()
	recorder.RecordOutputStats(true, dir, []PreparedArtifactRoot{
		{Subject: "provider \"alpha\"", Kind: "app", Name: "alpha", DestDir: alpha},
		{Subject: "provider \"beta\"", Kind: "app", Name: "beta", DestDir: beta},
	})

	output := recorder.Snapshot().Output
	if !output.Measured {
		t.Fatalf("output measured = false, want true")
	}
	if output.Files != 2 || output.Bytes != 14 {
		t.Fatalf("output aggregate = %d files/%d bytes, want 2 files/14 bytes", output.Files, output.Bytes)
	}
	if len(output.Roots) != 2 {
		t.Fatalf("output roots len = %d, want 2", len(output.Roots))
	}
	if output.Roots[0].Name != "beta" || output.Roots[0].RelativePath != "providers/beta" {
		t.Fatalf("first output root = %+v, want beta sorted by bytes", output.Roots[0])
	}
}
