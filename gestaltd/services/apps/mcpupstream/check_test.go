package mcpupstream

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
)

func newLazyTestUpstream(t *testing.T, allowed map[string]*operationexposure.OperationOverride) *Upstream {
	t.Helper()
	ts := newHeaderAuthenticatedHTTPTestServer(t, nil)
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeSubject, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if allowed != nil {
		// A lazily discovered upstream has no catalog at startup, so unknown
		// keys are only caught when discovery runs.
		if err := u.FilterOperations(allowed); err != nil {
			t.Fatalf("FilterOperations: %v", err)
		}
	}
	return u
}

func TestCheckReport_RecordsUnknownAllowedOperationsWithoutFailing(t *testing.T) {
	t.Parallel()

	u := newLazyTestUpstream(t, map[string]*operationexposure.OperationOverride{
		"run_query":  {Alias: "query"},
		"run_querry": nil,
		"drop_table": nil,
	})

	if _, err := u.CatalogForRequest(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "unknown operation") {
		t.Fatalf("normal discovery error = %v, want unknown operation failure", err)
	}

	report := &CheckReport{}
	cat, err := u.CatalogForRequest(WithCheckReport(context.Background(), report), "")
	if err != nil {
		t.Fatalf("check-mode discovery: %v", err)
	}
	if len(cat.Operations) != 1 || cat.Operations[0].ID != "query" {
		t.Fatalf("check-mode catalog = %+v, want only aliased query", cat.Operations)
	}

	checks := report.Checks()
	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want one", checks)
	}
	check := checks[0]
	if check.OK() {
		t.Fatal("check.OK() = true, want false")
	}
	if check.Upstream != "clickhouse" || !check.AllowedOperationsConfigured {
		t.Fatalf("check = %+v", check)
	}
	if want := []string{"list_databases", "run_query"}; !slices.Equal(check.Tools, want) {
		t.Errorf("Tools = %v, want %v", check.Tools, want)
	}
	if want := []string{"drop_table", "run_querry"}; !slices.Equal(check.UnknownAllowedOperations, want) {
		t.Errorf("UnknownAllowedOperations = %v, want %v", check.UnknownAllowedOperations, want)
	}
	if want := []string{"list_databases"}; !slices.Equal(check.UnexposedTools, want) {
		t.Errorf("UnexposedTools = %v, want %v", check.UnexposedTools, want)
	}
	if want := []ExposedTool{{Tool: "run_query", ExposedAs: "query"}}; !slices.Equal(check.Exposed, want) {
		t.Errorf("Exposed = %v, want %v", check.Exposed, want)
	}
}

func TestCheckReport_NoAllowedOperationsExposesEveryTool(t *testing.T) {
	t.Parallel()

	u := newLazyTestUpstream(t, nil)
	report := &CheckReport{}
	if _, err := u.CatalogForRequest(WithCheckReport(context.Background(), report), ""); err != nil {
		t.Fatalf("check-mode discovery: %v", err)
	}
	checks := report.Checks()
	if len(checks) != 1 || !checks[0].OK() || checks[0].AllowedOperationsConfigured {
		t.Fatalf("checks = %+v, want one passing unfiltered check", checks)
	}
	if len(checks[0].Exposed) != 2 || len(checks[0].UnexposedTools) != 0 || len(checks[0].UnknownAllowedOperations) != 0 {
		t.Fatalf("check = %+v", checks[0])
	}
}

func TestCheckReport_RecordsConnectionErrors(t *testing.T) {
	t.Parallel()

	ts := newAuthenticatedHTTPTestServer(t, "Bearer secret-token")
	t.Cleanup(ts.Close)
	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeSubject, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	report := &CheckReport{}
	ctx := WithCheckReport(context.Background(), report)
	if _, err := u.CatalogForRequest(ctx, "wrong-token"); err == nil {
		t.Fatal("expected discovery to fail with the wrong token")
	}
	checks := report.Checks()
	if len(checks) != 1 || checks[0].Error == "" || checks[0].OK() {
		t.Fatalf("checks = %+v, want one failed check", checks)
	}

	// A later successful listing for the same upstream replaces the failure.
	if _, err := u.CatalogForRequest(ctx, "secret-token"); err != nil {
		t.Fatalf("discovery with the right token: %v", err)
	}
	checks = report.Checks()
	if len(checks) != 1 || !checks[0].OK() {
		t.Fatalf("checks = %+v, want the success to replace the failure", checks)
	}
}

func TestCheckReport_InProcessClientIsListedLiveAndNotClosed(t *testing.T) {
	t.Parallel()

	u := newTestUpstream(t)
	t.Cleanup(func() { _ = u.Close() })

	report := &CheckReport{}
	if _, err := u.CatalogForRequest(WithCheckReport(context.Background(), report), ""); err != nil {
		t.Fatalf("check-mode discovery: %v", err)
	}
	if checks := report.Checks(); len(checks) != 1 || len(checks[0].Tools) != 2 {
		t.Fatalf("checks = %+v", checks)
	}
	// The shared client must still work after a check.
	if _, err := u.CallTool(context.Background(), "list_databases", nil); err != nil {
		t.Fatalf("CallTool after check: %v", err)
	}
}
