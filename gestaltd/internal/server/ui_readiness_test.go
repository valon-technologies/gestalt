package server

import (
	"net/http"
	"net/http/httptest"
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
