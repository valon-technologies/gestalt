package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/pluginapi"
)

func TestExecutableBigQueryProviderShapesResults(t *testing.T) {
	t.Parallel()

	prov := newBigQueryExecutableProvider(t, "rows")
	result, err := prov.Execute(context.Background(), "query", map[string]any{
		"project_id":     "sample",
		"query":          "SELECT 1",
		"max_results":    1,
		"timeout_ms":     1500,
		"use_legacy_sql": true,
	}, "test-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var body struct {
		Schema      []map[string]any `json:"schema"`
		Rows        []map[string]any `json:"rows"`
		TotalRows   uint64           `json:"total_rows"`
		JobComplete bool             `json:"job_complete"`
	}
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if result.Status != 200 {
		t.Fatalf("status = %d", result.Status)
	}
	if body.TotalRows != 2 || !body.JobComplete {
		t.Fatalf("body metadata = %+v", body)
	}
	if len(body.Rows) != 1 {
		t.Fatalf("rows = %d", len(body.Rows))
	}
	if got := body.Rows[0]["value"]; got != "12.345" {
		t.Fatalf("value = %#v, want exact decimal string", got)
	}
	if got := body.Rows[0]["stamp"]; got != "2026-03-25T09:30:00" {
		t.Fatalf("stamp = %#v", got)
	}
	items, ok := body.Rows[0]["items"].([]any)
	if !ok {
		t.Fatalf("items type = %T", body.Rows[0]["items"])
	}
	if len(items) != 2 || items[1] != "0.125" {
		t.Fatalf("items = %#v", items)
	}
	nested, ok := body.Rows[0]["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested type = %T", body.Rows[0]["nested"])
	}
	if nested["ratio"] != "1.001" {
		t.Fatalf("nested.ratio = %#v", nested["ratio"])
	}
	if len(body.Schema) != 4 || body.Schema[2]["mode"] != "REPEATED" {
		t.Fatalf("schema = %+v", body.Schema)
	}
}

func TestExecutableBigQueryProviderReturnsErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		scenario  string
		params    map[string]any
		operation string
		wantErr   string
	}{
		{
			name:      "unknown operation",
			scenario:  "rows",
			operation: "other",
			params:    map[string]any{},
			wantErr:   "unknown operation",
		},
		{
			name:      "missing project",
			scenario:  "rows",
			operation: "query",
			params:    map[string]any{"query": "SELECT 1"},
			wantErr:   "project_id is required",
		},
		{
			name:      "missing query",
			scenario:  "rows",
			operation: "query",
			params:    map[string]any{"project_id": "sample"},
			wantErr:   "query is required",
		},
		{
			name:      "backend error",
			scenario:  "backend_error",
			operation: "query",
			params:    map[string]any{"project_id": "sample", "query": "SELECT 1"},
			wantErr:   "backend unavailable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			prov := newBigQueryExecutableProvider(t, tc.scenario)
			_, err := prov.Execute(context.Background(), tc.operation, tc.params, "test-token")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func newBigQueryExecutableProvider(t *testing.T, scenario string) core.Provider {
	t.Helper()

	prov, err := pluginapi.NewExecutableProvider(context.Background(), pluginapi.ExecConfig{
		Command: buildBigQueryPluginBinary(t),
		Env: map[string]string{
			functionalTestScenarioEnv: scenario,
		},
	})
	if err != nil {
		t.Fatalf("NewExecutableProvider: %v", err)
	}
	t.Cleanup(func() {
		if closer, ok := prov.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	})
	return prov
}

func buildBigQueryPluginBinary(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "gestalt-plugin-bigquery")
	root := repoRoot(t)
	cmd := exec.Command("go", "build", "-tags", "functionaltest", "-o", bin, "./cmd/gestalt-plugin-bigquery")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build plugin binary: %v\n%s", err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
