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

func TestSyncMetricsRecorderCountsCacheLookupAndPutSeparately(t *testing.T) {
	t.Parallel()

	recorder := NewSyncMetricsRecorder()
	recorder.RecordCacheEntry(syncCacheMetricsEvent{
		Subject:    `provider "alpha"`,
		SourceKind: syncArtifactSourceRemoteArchive,
		Result:     syncCacheResultMiss,
		Lookup:     true,
	})
	recorder.RecordCacheEntry(syncCacheMetricsEvent{
		Subject:    `provider "alpha"`,
		SourceKind: syncArtifactSourceRemoteArchive,
		Result:     syncCacheResultMiss,
		Put:        true,
		PutTimings: materializedCachePutTimings{
			LocalInspect:          1100 * time.Millisecond,
			LocalWrite:            2200 * time.Millisecond,
			RemoteExists:          300 * time.Millisecond,
			RemoteArchive:         4400 * time.Millisecond,
			RemoteUpload:          5500 * time.Millisecond,
			RemoteSkippedExisting: true,
		},
	})

	cache := recorder.Snapshot().Cache
	if cache.Eligible != 1 || cache.Misses != 1 || cache.Put.Successes != 1 {
		t.Fatalf("cache counts = eligible %d, misses %d, put successes %d; want 1, 1, 1", cache.Eligible, cache.Misses, cache.Put.Successes)
	}
	if cache.Put.LocalInspectSeconds != 1.1 ||
		cache.Put.LocalWriteSeconds != 2.2 ||
		cache.Put.RemoteExistsSeconds != 0.3 ||
		cache.Put.RemoteArchiveSeconds != 4.4 ||
		cache.Put.RemoteUploadSeconds != 5.5 ||
		cache.Put.RemoteSkippedExisting != 1 {
		t.Fatalf("cache put timings = %+v, want aggregated timing fields", cache.Put)
	}
	if len(cache.Entries) != 2 {
		t.Fatalf("cache entries len = %d, want 2", len(cache.Entries))
	}
}

func TestSyncMetricsRecorderAggregatesCachePrefetchAcrossPhases(t *testing.T) {
	t.Parallel()

	recorder := NewSyncMetricsRecorder()
	recorder.RecordCachePrefetch(materializedCachePrefetchStats{
		Duration:   1200 * time.Millisecond,
		Requests:   2,
		Eligible:   2,
		RemoteHits: 1,
		Keys:       []string{"a", "b"},
		Bytes:      100,
	})
	recorder.RecordCachePrefetch(materializedCachePrefetchStats{
		Duration:     300 * time.Millisecond,
		Requests:     2,
		Eligible:     2,
		LocalHits:    1,
		RemoteMisses: 1,
		Keys:         []string{"b", "c"},
	})

	prefetch := recorder.Snapshot().Cache.Prefetch
	if prefetch.DurationSeconds != 1.5 ||
		prefetch.Requests != 4 ||
		prefetch.Eligible != 4 ||
		prefetch.UniqueKeys != 3 ||
		prefetch.LocalHits != 1 ||
		prefetch.RemoteHits != 1 ||
		prefetch.RemoteMisses != 1 ||
		prefetch.Failures != 0 ||
		prefetch.Bytes != 100 {
		t.Fatalf("prefetch metrics = %+v, want aggregated counters with unique keys deduped", prefetch)
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
