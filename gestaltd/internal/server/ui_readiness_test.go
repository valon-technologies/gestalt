package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestUIReadinessMonitorMarksReadyAfterServingAndProbe(t *testing.T) {
	t.Parallel()
	servingReady := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/sample/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	monitor := NewUIReadinessMonitor(UIReadinessMonitorConfig{
		Handler: mux,
		MountedUIs: []MountedUI{{
			Path:    "/sample",
			Handler: mux,
		}},
		ReleaseID:     "release-a",
		SourceVersion: "sha-a",
		InstanceID:    "instance-a",
		ProcessID:     "process-a",
		ServingReady:  servingReady,
		Now:           func() time.Time { return time.Unix(1_700_000_000, 0) },
	})

	if reason := monitor.ReadinessReason(); reason != uiReadinessIncompleteReason {
		t.Fatalf("readiness before evaluation = %q, want %q", reason, uiReadinessIncompleteReason)
	}

	close(servingReady)
	monitor.evaluate()

	if reason := monitor.ReadinessReason(); reason != "" {
		t.Fatalf("readiness after successful probe = %q, want ready", reason)
	}

	report := monitor.Report()
	if report.ReleaseID != "release-a" || report.InstanceID != "instance-a" || len(report.UIs) != 1 || !report.UIs[0].Ready {
		t.Fatalf("unexpected fleet report: %+v", report)
	}
}

func TestUIReadinessMonitorStopsPollingAfterSuccessfulProbe(t *testing.T) {
	t.Parallel()
	servingReady := make(chan struct{})
	close(servingReady)
	var probes atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/sample/", func(w http.ResponseWriter, _ *http.Request) {
		probes.Add(1)
		w.WriteHeader(http.StatusOK)
	})

	monitor := NewUIReadinessMonitor(UIReadinessMonitorConfig{
		Handler: mux,
		MountedUIs: []MountedUI{{
			Path:    "/sample",
			Handler: mux,
		}},
		ServingReady:    servingReady,
		RecheckInterval: time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor.Start(ctx)

	deadline := time.Now().Add(time.Second)
	for probes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if probes.Load() != 1 {
		t.Fatalf("initial probe count = %d, want 1", probes.Load())
	}
	time.Sleep(20 * time.Millisecond)
	if got := probes.Load(); got != 1 {
		t.Fatalf("probe count after readiness = %d, want 1", got)
	}
}

func TestUIReadinessMonitorRejectsRedirect(t *testing.T) {
	t.Parallel()
	servingReady := make(chan struct{})
	close(servingReady)

	mux := http.NewServeMux()
	mux.HandleFunc("/sample/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusFound)
	})

	monitor := NewUIReadinessMonitor(UIReadinessMonitorConfig{
		Handler: mux,
		MountedUIs: []MountedUI{{
			Path:    "/sample",
			Handler: mux,
		}},
		ReleaseID:    "release-a",
		InstanceID:   "instance-a",
		ProcessID:    "process-a",
		ServingReady: servingReady,
	})
	monitor.evaluate()

	if reason := monitor.ReadinessReason(); reason != uiReadinessIncompleteReason {
		t.Fatalf("readiness with redirect = %q, want %q", reason, uiReadinessIncompleteReason)
	}
}

func TestReadinessCheckIncludesUIReadiness(t *testing.T) {
	t.Parallel()
	servingReady := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/sample/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	monitor := NewUIReadinessMonitor(UIReadinessMonitorConfig{
		Handler: mux,
		MountedUIs: []MountedUI{{
			Path:    "/sample",
			Handler: mux,
		}},
		ReleaseID:    "release-a",
		InstanceID:   "instance-a",
		ProcessID:    "process-a",
		ServingReady: servingReady,
	})
	srv := &Server{uiReadiness: monitor}

	rec := httptest.NewRecorder()
	srv.readinessCheck(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready before ui qualification = %d, want 503", rec.Code)
	}

	close(servingReady)
	monitor.evaluate()

	rec = httptest.NewRecorder()
	srv.readinessCheck(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("ready after ui qualification = %d, want 200", rec.Code)
	}
}

func TestFleetReadinessEndpoint(t *testing.T) {
	t.Parallel()
	servingReady := make(chan struct{})
	close(servingReady)

	mux := http.NewServeMux()
	mux.HandleFunc("/sample/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	monitor := NewUIReadinessMonitor(UIReadinessMonitorConfig{
		Handler: mux,
		MountedUIs: []MountedUI{{
			Path:    "/sample",
			Handler: mux,
		}},
		ReleaseID:    "release-a",
		InstanceID:   "instance-a",
		ProcessID:    "process-a",
		ServingReady: servingReady,
	})
	monitor.evaluate()

	srv := &Server{uiReadiness: monitor}
	rec := httptest.NewRecorder()
	srv.fleetReadinessReport(rec, httptest.NewRequest(http.MethodGet, "/fleet-readiness", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /fleet-readiness = %d, want 200", rec.Code)
	}
}

func TestUIReadinessProbesUseBoundedConcurrencyAndWaitForEveryResult(t *testing.T) {
	t.Parallel()
	started := make(chan struct{}, 16)
	release := make(chan struct{})
	var active, peak atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); current > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		if r.URL.Path == "/probe/15" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	paths := make([]string, 16)
	for index := range paths {
		paths[index] = fmt.Sprintf("/probe/%d", index)
	}
	monitor := NewUIReadinessMonitor(UIReadinessMonitorConfig{
		Handler: handler, ExtraProbePaths: paths,
	})
	done := make(chan struct{})
	go func() { monitor.evaluate(); close(done) }()
	for range 8 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("the bounded probe batch did not start concurrently")
		}
	}
	if monitor.ReadinessReason() == "" || monitor.Report().ReportedAt != 0 {
		t.Error("unfinished probe batch was admitted")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("probe evaluation did not finish")
	}
	if peak.Load() != 8 {
		t.Fatalf("peak probe concurrency = %d, want 8", peak.Load())
	}
	report := monitor.Report()
	if len(report.UIs) != len(paths) || monitor.ReadinessReason() == "" {
		t.Fatalf("missing results or failed probe admitted: %+v", report)
	}
	for index, result := range report.UIs {
		if result.Mount != paths[index] || result.Ready != (index != 15) {
			t.Fatalf("probe result %d = %+v", index, result)
		}
	}
}

// An app UI that is not public requires a user principal that a readiness probe
// can never present. Before the probe was marked as in-process, every such mount
// answered 401 and startup admission could never complete.
func TestUIReadinessProbeIsAdmittedToNonPublicAppUI(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/private-app/", func(w http.ResponseWriter, r *http.Request) {
		if !isReadinessProbe(r.Context()) {
			http.Error(w, "missing authorization", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	result := probeMountedUI(mux, "/private-app/", "")
	if !result.Ready {
		t.Fatalf("probe of non-public app UI = %+v, want ready", result)
	}
}

func TestReadinessProbeMarkerIsAbsentOnOrdinaryRequests(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/private-app/", nil)
	if isReadinessProbe(req.Context()) {
		t.Fatal("ordinary request is marked as a readiness probe")
	}
	if !isReadinessProbe(withReadinessProbe(req.Context())) {
		t.Fatal("marked context is not recognized as a readiness probe")
	}
	if isReadinessProbe(context.Background()) {
		t.Fatal("background context is marked as a readiness probe")
	}
}

func unreadyProbe(mount string, status int, message string) UIProbeResult {
	result := UIProbeResult{Mount: mount, Ready: false}
	if status != 0 {
		result.StatusCode = &status
	}
	if message != "" {
		result.Error = &message
	}
	return result
}

func TestSummarizeUnreadyMountsNamesTheFailingMount(t *testing.T) {
	t.Parallel()
	results := []UIProbeResult{
		{Mount: "/g-issues/", Ready: true},
		unreadyProbe("/scenario-library/", http.StatusServiceUnavailable, "app unavailable\n"),
	}
	unready := unreadyMounts(results)
	if len(unready) != 1 {
		t.Fatalf("unreadyMounts = %d entries, want 1", len(unready))
	}
	got := summarizeUnreadyMounts(unready)
	want := "/scenario-library/ (503 app unavailable)"
	if got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestSummarizeUnreadyMountsHandlesAMissingResponse(t *testing.T) {
	t.Parallel()
	got := summarizeUnreadyMounts([]UIProbeResult{unreadyProbe("/atlas/", 0, "")})
	want := "/atlas/ (no response)"
	if got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestSummarizeUnreadyMountsTruncatesALongError(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("e", unreadyProbeErrorLimit+20)
	got := summarizeUnreadyMounts([]UIProbeResult{unreadyProbe("/a/", 500, long)})
	want := fmt.Sprintf("/a/ (500 %s...)", strings.Repeat("e", unreadyProbeErrorLimit))
	if got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestSummarizeUnreadyMountsBoundsTheList(t *testing.T) {
	t.Parallel()
	unready := make([]UIProbeResult, 0, unreadyMountLogLimit+3)
	for i := range unreadyMountLogLimit + 3 {
		unready = append(unready, unreadyProbe(fmt.Sprintf("/app%d/", i), 503, ""))
	}
	got := summarizeUnreadyMounts(unready)
	if strings.Count(got, "/app") != unreadyMountLogLimit {
		t.Fatalf("summary lists %d mounts, want %d: %s", strings.Count(got, "/app"), unreadyMountLogLimit, got)
	}
	if !strings.HasSuffix(got, " and 3 more") {
		t.Fatalf("summary = %q, want a remainder suffix", got)
	}
}
