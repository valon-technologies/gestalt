package server_test

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability"
)

type adminMetricsSampleReader struct {
	samples []observability.InvocationRecord
}

func (r adminMetricsSampleReader) RecentInvocations(_ string, limit int) []observability.InvocationRecord {
	if limit > len(r.samples) {
		limit = len(r.samples)
	}
	return r.samples[:limit]
}

func TestAppAdminMetricsFiltersByProvider(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "app", "g-issues"),
		},
	}
	scrape := `
gestaltd_operation_count_total{gestalt_provider="g-issues",gestalt_operation="list"} 4
gestaltd_operation_count_total{gestalt_provider="slack",gestalt_operation="post"} 9
gestaltd_operation_error_count_total{gestalt_provider="g-issues",gestalt_operation="list"} 1
`

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
		cfg.PrometheusMetrics = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			_, _ = w.Write([]byte(scrape))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/metrics", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET metrics: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", response.StatusCode, body)
	}
	var payload struct {
		App        string  `json:"app"`
		Requests   float64 `json:"requests"`
		Errors     float64 `json:"errors"`
		Operations []struct {
			Operation string  `json:"operation"`
			Requests  float64 `json:"requests"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.App != "g-issues" || payload.Requests != 4 || payload.Errors != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	if len(payload.Operations) != 1 || payload.Operations[0].Operation != "list" {
		t.Fatalf("operations = %#v", payload.Operations)
	}
}

func TestAppAdminMetricsIncludesRecentRequestSamples(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "app", "g-issues"),
		},
	}
	startedAt := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
		cfg.PrometheusMetrics = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("gestaltd_operation_count_total{gestalt_provider=\"g-issues\",gestalt_operation=\"list\"} 4\n"))
		})
		cfg.InvocationRecords = adminMetricsSampleReader{samples: []observability.InvocationRecord{
			{
				ID:        1,
				Operation: "list",
				Outcome:   observability.InvocationPassed,
				Status:    http.StatusOK,
				Duration:  125 * time.Millisecond,
				Timestamp: startedAt,
			},
			{
				ID:        2,
				Operation: "update",
				Outcome:   observability.InvocationFailed,
				Status:    http.StatusInternalServerError,
				Duration:  2500 * time.Millisecond,
				Timestamp: startedAt.Add(time.Second),
			},
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/metrics", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET metrics: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", response.StatusCode, body)
	}
	var payload struct {
		RecentRequests []struct {
			ID         uint64  `json:"id"`
			Operation  string  `json:"operation"`
			Outcome    string  `json:"outcome"`
			Status     int     `json:"status"`
			DurationMs float64 `json:"durationMs"`
			Timestamp  string  `json:"timestamp"`
		} `json:"recentRequests"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.RecentRequests) != 2 {
		t.Fatalf("recentRequests = %#v", payload.RecentRequests)
	}
	if got := payload.RecentRequests[1]; got.ID != 2 || got.Operation != "update" || got.Outcome != "failed" || got.Status != http.StatusInternalServerError || got.DurationMs != 2500 || got.Timestamp != startedAt.Add(time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("recentRequests[1] = %#v", got)
	}
}

func TestAppAdminMetricsIgnoresCallerGzipAccept(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "app", "g-issues"),
		},
	}
	scrape := `gestaltd_operation_count_total{gestalt_provider="g-issues",gestalt_operation="list"} 4
gestaltd_operation_count_total{gestalt_provider="slack",gestalt_operation="post"} 9
`
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
		cfg.PrometheusMetrics = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				w.Header().Set("Content-Encoding", "gzip")
				gz := gzip.NewWriter(w)
				_, _ = gz.Write([]byte(scrape))
				_ = gz.Close()
				return
			}
			_, _ = w.Write([]byte(scrape))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/metrics", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	request.Header.Set("Accept-Encoding", "gzip")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET metrics: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", response.StatusCode, body)
	}
	var payload struct {
		App      string  `json:"app"`
		Requests float64 `json:"requests"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.App != "g-issues" || payload.Requests != 4 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestAppAdminMetricsDecodesGzipScrape(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "app", "g-issues"),
		},
	}
	scrape := `gestaltd_operation_count_total{gestalt_provider="g-issues",gestalt_operation="list"} 4
`
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
		cfg.PrometheusMetrics = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte(scrape))
			_ = gz.Close()
		})
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/metrics", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET metrics: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", response.StatusCode, body)
	}
	var payload struct {
		App      string  `json:"app"`
		Requests float64 `json:"requests"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.App != "g-issues" || payload.Requests != 4 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestAppAdminMetricsForbiddenWithoutAppAdmin(t *testing.T) {
	t.Parallel()

	viewerID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(viewerID, "viewer", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("viewer-token", viewerID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
		cfg.PrometheusMetrics = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("gestaltd_operation_count_total 1\n"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/metrics", nil)
	request.Header.Set("Authorization", "Bearer viewer-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET metrics: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want 403: %s", response.StatusCode, body)
	}
}

func TestAppAdminMetricsUnavailableWhenTelemetryDisabled(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/metrics", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET metrics: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want 503: %s", response.StatusCode, body)
	}
}

func TestAdminPrometheusMetricsEndpoint(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.PrometheusMetrics = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			_, _ = w.Write([]byte("gestaltd_operation_count_total 7\n"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/metrics")
	if err != nil {
		t.Fatalf("GET admin metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "gestaltd_operation_count_total 7") {
		t.Fatalf("body = %s", body)
	}
}

func TestAdminPrometheusMetricsRequiresSessionWhenAdminAuthorizationEnabled(t *testing.T) {
	t.Parallel()

	ts := newAuthorizedAdminTestServer(t, true)
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/metrics")
	if err != nil {
		t.Fatalf("GET admin metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 401: %s", resp.StatusCode, body)
	}
}

func TestAppAdminMetricsRejectsMalformedPrometheus(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
		cfg.PrometheusMetrics = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not a metric line {\n"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/metrics", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET metrics: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want 503: %s", response.StatusCode, body)
	}
}
