package daemon

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/operator"
)

func TestWriteSyncJSON(t *testing.T) {
	t.Parallel()

	metrics := operator.SyncMetrics{
		Sync: operator.SyncMetricsSync{
			Action:          "materialize",
			DurationSeconds: 1.234,
			ArtifactsDir:    "/prepared",
			LockfilePath:    "/app/gestalt.lock.json",
		},
		Inputs: operator.SyncMetricsInputs{
			ConfigPaths: []string{"/app/config.yaml"},
			Locked:      true,
			Check:       false,
			Parallelism: 4,
		},
		Artifacts: operator.SyncMetricsArtifacts{
			Considered: 2,
			Items: []operator.SyncMetricsArtifactRecord{{
				Subject:         "provider \"alpha\"",
				Kind:            "app",
				Name:            "alpha",
				SourceKind:      "local_source",
				Result:          "materialized",
				Reason:          "prepared_missing",
				DurationSeconds: 1.0,
			}},
		},
		Archives: operator.SyncMetricsArchives{
			Fetches: []operator.SyncMetricsArchiveFetch{{
				Subject:         "provider \"alpha\"",
				SourceKind:      "remote_release_metadata",
				DurationSeconds: 0.5,
			}},
		},
		Cache: operator.SyncMetricsCache{
			Configured:    true,
			Enabled:       true,
			Dir:           "/cache",
			Mode:          "materialized",
			BucketVersion: "materialized/v1",
			Hits:          1,
			Put: operator.SyncMetricsCachePut{
				Successes:             1,
				LocalInspectSeconds:   1.1,
				LocalWriteSeconds:     2.2,
				RemoteExistsSeconds:   0.3,
				RemoteArchiveSeconds:  4.4,
				RemoteUploadSeconds:   5.5,
				RemoteSkippedExisting: 1,
			},
			Entries: []operator.SyncMetricsCacheEntry{{
				Subject:         "provider \"alpha\"",
				SourceKind:      "remote_archive",
				Result:          "hit",
				Put:             "skipped",
				DurationSeconds: 0.1,
			}},
		},
		Output: operator.SyncMetricsOutput{
			Measured: true,
			Roots: []operator.SyncMetricsOutputRoot{{
				Subject:      "provider \"alpha\"",
				Kind:         "app",
				Name:         "alpha",
				RelativePath: "providers/alpha",
			}},
		},
	}

	var out bytes.Buffer
	if err := writeSyncJSON(&out, metrics); err != nil {
		t.Fatalf("writeSyncJSON: %v", err)
	}
	if strings.Contains(out.String(), "\n{") {
		t.Fatalf("writeSyncJSON output should be compact single-document JSON, got %q", out.String())
	}

	var doc syncOutputDocument
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal sync JSON: %v\n%s", err, out.String())
	}
	if doc.Schema.Version != "2" {
		t.Fatalf("schema.version = %q, want 2", doc.Schema.Version)
	}
	if doc.Command != "sync" {
		t.Fatalf("command = %q, want sync", doc.Command)
	}
	if doc.Sync.Action != "materialize" {
		t.Fatalf("sync.action = %q, want materialize", doc.Sync.Action)
	}
	if doc.Artifacts.Considered != 2 {
		t.Fatalf("artifacts.considered = %d, want 2", doc.Artifacts.Considered)
	}
	if len(doc.Archives.Fetches) != 1 {
		t.Fatalf("archives.fetches len = %d, want 1", len(doc.Archives.Fetches))
	}
	if len(doc.Cache.Entries) != 1 {
		t.Fatalf("cache.entries len = %d, want 1", len(doc.Cache.Entries))
	}
	if len(doc.Artifacts.Items) != 1 {
		t.Fatalf("artifacts.items len = %d, want 1", len(doc.Artifacts.Items))
	}
	if len(doc.Output.Roots) != 1 {
		t.Fatalf("output.roots len = %d, want 1", len(doc.Output.Roots))
	}
	var raw map[string]any
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal sync JSON map: %v", err)
	}
	archives, ok := raw["archives"].(map[string]any)
	if !ok {
		t.Fatalf("archives missing from JSON: %s", out.String())
	}
	if _, ok := archives["slowest_fetches"]; ok {
		t.Fatalf("archives.slowest_fetches should not be serialized: %s", out.String())
	}
	if _, ok := archives["cache"]; ok {
		t.Fatalf("archives.cache should not be serialized: %s", out.String())
	}
	cache, ok := raw["cache"].(map[string]any)
	if !ok {
		t.Fatalf("cache missing from JSON: %s", out.String())
	}
	put, ok := cache["put"].(map[string]any)
	if !ok || put["successes"] == nil || put["failures"] == nil {
		t.Fatalf("cache.put missing successes/failures: %#v", cache["put"])
	}
	for _, key := range []string{"local_inspect_seconds", "local_write_seconds", "remote_exists_seconds", "remote_archive_seconds", "remote_upload_seconds", "remote_skipped_existing"} {
		if _, ok := put[key]; !ok {
			t.Fatalf("cache.put.%s missing from JSON: %#v", key, put)
		}
	}
}

func TestWriteSyncJSONIncludesEmptyMetricArrays(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := writeSyncJSON(&out, operator.SyncMetrics{}); err != nil {
		t.Fatalf("writeSyncJSON: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal sync JSON map: %v", err)
	}
	archives := raw["archives"].(map[string]any)
	if _, ok := archives["slowest_fetches"]; ok {
		t.Fatalf("archives.slowest_fetches should not be serialized: %s", out.String())
	}
	for parent, keys := range map[string][]string{
		"archives":  {"fetches"},
		"cache":     {"entries"},
		"artifacts": {"items"},
		"output":    {"roots"},
	} {
		obj := raw[parent].(map[string]any)
		for _, key := range keys {
			array, ok := obj[key].([]any)
			if !ok {
				t.Fatalf("%s.%s = %#v, want empty array", parent, key, obj[key])
			}
			if len(array) != 0 {
				t.Fatalf("%s.%s len = %d, want 0", parent, key, len(array))
			}
		}
	}
}

func TestWriteSyncText(t *testing.T) {
	t.Parallel()

	metrics := operator.SyncMetrics{
		Sync: operator.SyncMetricsSync{
			Action:          "materialize",
			DurationSeconds: 1.234,
			ArtifactsDir:    "/prepared",
		},
		Artifacts: operator.SyncMetricsArtifacts{Considered: 2},
		Cache: operator.SyncMetricsCache{
			Configured: true,
			Enabled:    true,
			Dir:        "/cache",
			Hits:       1,
			Put: operator.SyncMetricsCachePut{
				Successes:             1,
				LocalInspectSeconds:   1.1,
				LocalWriteSeconds:     2.2,
				RemoteExistsSeconds:   0.3,
				RemoteArchiveSeconds:  4.4,
				RemoteUploadSeconds:   5.5,
				RemoteSkippedExisting: 1,
			},
		},
		Archives: operator.SyncMetricsArchives{
			Requests: 3,
			Downloads: operator.SyncMetricsDownloads{
				Count: 2,
				Bytes: 4096,
			},
		},
		Output: operator.SyncMetricsOutput{Measured: true, Files: 4, Bytes: 8192},
	}

	var out bytes.Buffer
	if err := writeSyncText(&out, metrics, true); err != nil {
		t.Fatalf("writeSyncText: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Loaded 2 prepared artifacts from lock/config.",
		"Fetched 3 archives: 2 downloads.",
		"Cache:",
		"Cache put timing: inspect 1.100s, local write 2.200s, remote exists 0.300s, archive 4.400s, upload 5.500s, skipped existing 1.",
		"Downloads: 2 archives",
		"Prepared output: 4 files",
		"Prepared artifacts in /prepared in 1.234s.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("writeSyncText output missing %q:\n%s", want, got)
		}
	}
}
