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
		Artifacts: operator.SyncMetricsArtifacts{Considered: 2},
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
	if doc.Schema.Version != "1" {
		t.Fatalf("schema.version = %q, want 1", doc.Schema.Version)
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
		Archives: operator.SyncMetricsArchives{
			Requests: 3,
			Cache:    operator.SyncMetricsArchiveCache{Hits: 1},
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
		"Fetched 3 archives: 1 cache hits, 2 downloads.",
		"Archive cache:",
		"Downloads: 2 archives",
		"Prepared output: 4 files",
		"Prepared artifacts in /prepared in 1.234s.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("writeSyncText output missing %q:\n%s", want, got)
		}
	}
}
