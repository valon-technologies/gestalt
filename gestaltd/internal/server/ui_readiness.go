package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

const uiReadinessIncompleteReason = "ui readiness incomplete"

type uiProbeResult struct {
	Mount      string  `json:"mount"`
	Ready      bool    `json:"ready"`
	StatusCode *int    `json:"status_code"`
	Error      *string `json:"error,omitempty"`
}

type fleetReadinessReport struct {
	ReleaseID     string          `json:"release_id"`
	SourceVersion string          `json:"source_version"`
	InstanceID    string          `json:"instance_id"`
	ProcessID     string          `json:"process_id"`
	ReportedAt    float64         `json:"reported_at"`
	UIs           []uiProbeResult `json:"uis"`
}

type UIReadinessMonitor struct {
	handler         http.Handler
	mounted         []MountedUI
	extraProbePaths []string
	probeBearer     string
	releaseID       string
	sourceVersion   string
	instanceID      string
	processID       string
	servingReady    <-chan struct{}
	recheckInterval time.Duration
	now             func() time.Time

	mu     sync.RWMutex
	report fleetReadinessReport
	ready  bool
}

func NewUIReadinessMonitor(cfg UIReadinessMonitorConfig) *UIReadinessMonitor {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	interval := cfg.RecheckInterval
	if interval <= 0 {
		interval = defaultUIReadinessRecheckEvery
	}
	return &UIReadinessMonitor{
		handler:         cfg.Handler,
		mounted:         append([]MountedUI(nil), cfg.MountedUIs...),
		extraProbePaths: append([]string(nil), cfg.ExtraProbePaths...),
		probeBearer:     strings.TrimSpace(cfg.ProbeBearer),
		releaseID:       strings.TrimSpace(cfg.ReleaseID),
		sourceVersion:   strings.TrimSpace(cfg.SourceVersion),
		instanceID:      strings.TrimSpace(cfg.InstanceID),
		processID:       strings.TrimSpace(cfg.ProcessID),
		servingReady:    cfg.ServingReady,
		recheckInterval: interval,
		now:             now,
	}
}

type UIReadinessMonitorConfig struct {
	Handler         http.Handler
	MountedUIs      []MountedUI
	ExtraProbePaths []string
	ProbeBearer     string
	ReleaseID       string
	SourceVersion   string
	InstanceID      string
	ProcessID       string
	ServingReady    <-chan struct{}
	RecheckInterval time.Duration
	Now             func() time.Time
}

func (m *UIReadinessMonitor) Start(ctx context.Context) {
	if m == nil {
		return
	}
	go func() {
		if !m.waitForServingReady(ctx) {
			return
		}
		m.evaluate()
		ticker := time.NewTicker(m.recheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.evaluate()
			}
		}
	}()
}

func (m *UIReadinessMonitor) waitForServingReady(ctx context.Context) bool {
	if m.servingReady == nil {
		return true
	}
	select {
	case <-m.servingReady:
		return true
	case <-ctx.Done():
		return false
	}
}

func (m *UIReadinessMonitor) ReadinessReason() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	ready := m.ready
	m.mu.RUnlock()
	if !ready {
		return uiReadinessIncompleteReason
	}
	return ""
}

func (m *UIReadinessMonitor) Report() fleetReadinessReport {
	if m == nil {
		return fleetReadinessReport{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.report
}

func (m *UIReadinessMonitor) evaluate() {
	results := make([]uiProbeResult, 0, len(m.mounted)+len(m.extraProbePaths))
	ready := true

	for _, mounted := range m.mounted {
		if mounted.Handler == nil || strings.TrimSpace(mounted.Path) == "" {
			continue
		}
		for _, path := range mountedUIProbePaths(mounted) {
			result := probeMountedUI(m.handler, path, m.probeBearer)
			results = append(results, result)
			if !result.Ready {
				ready = false
			}
		}
	}
	for _, path := range m.extraProbePaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		result := probeMountedUI(m.handler, path, m.probeBearer)
		results = append(results, result)
		if !result.Ready {
			ready = false
		}
	}

	report := fleetReadinessReport{
		ReleaseID:     m.releaseID,
		SourceVersion: m.sourceVersion,
		InstanceID:    m.instanceID,
		ProcessID:     m.processID,
		ReportedAt:    float64(m.now().Unix()),
		UIs:           results,
	}

	m.mu.Lock()
	m.report = report
	m.ready = ready && len(results) > 0
	m.mu.Unlock()
}

func mountedUIProbePaths(mounted MountedUI) []string {
	mount := strings.TrimRight(strings.TrimSpace(mounted.Path), "/")
	if mount == "" {
		mount = "/"
	}
	paths := []string{mount + "/"}
	if mounted.ThemeStylesheet != "" {
		stylesheetPath, _ := mountedUIThemePaths(mount)
		paths = append(paths, stylesheetPath)
	}
	return paths
}

func probeMountedUI(handler http.Handler, path string, bearer string) uiProbeResult {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	code := rec.Code
	result := uiProbeResult{
		Mount:      path,
		Ready:      uiProbeStatusReady(code),
		StatusCode: &code,
	}
	if !result.Ready {
		msg := strings.TrimSpace(rec.Body.String())
		if msg == "" {
			msg = http.StatusText(rec.Code)
		}
		result.Error = &msg
	}
	return result
}

func uiProbeStatusReady(statusCode int) bool {
	return statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
}
