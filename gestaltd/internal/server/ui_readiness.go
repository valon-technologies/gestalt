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

type UIProbeResult struct {
	Mount      string  `json:"mount"`
	Ready      bool    `json:"ready"`
	StatusCode *int    `json:"status_code"`
	Error      *string `json:"error,omitempty"`
}

type FleetReadinessReport struct {
	ReleaseID     string          `json:"release_id"`
	SourceVersion string          `json:"source_version"`
	InstanceID    string          `json:"instance_id"`
	ProcessID     string          `json:"process_id"`
	ReportedAt    float64         `json:"reported_at"`
	UIs           []UIProbeResult `json:"uis"`
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
	report FleetReadinessReport
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
		if m.isReady() {
			return
		}
		ticker := time.NewTicker(m.recheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.evaluate()
				if m.isReady() {
					return
				}
			}
		}
	}()
}

func (m *UIReadinessMonitor) isReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ready
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

func (m *UIReadinessMonitor) Report() FleetReadinessReport {
	if m == nil {
		return FleetReadinessReport{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.report
}

func (m *UIReadinessMonitor) evaluate() {
	paths := make([]string, 0, len(m.mounted)+len(m.extraProbePaths))
	for index := range m.mounted {
		mounted := &m.mounted[index]
		if mounted.Handler != nil && strings.TrimSpace(mounted.Path) != "" {
			paths = append(paths, mountedUIProbePaths(*mounted)...)
		}
	}
	for _, path := range m.extraProbePaths {
		if path = strings.TrimSpace(path); path != "" {
			paths = append(paths, path)
		}
	}

	// These are ordinary read-only HTTP requests. Bound their concurrency so a
	// large inventory fits the instance startup window without a request burst.
	results := make([]UIProbeResult, len(paths))
	jobs := make(chan int, len(paths))
	for index := range paths {
		jobs <- index
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(8, len(paths)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				results[index] = probeMountedUI(m.handler, paths[index], m.probeBearer)
			}
		}()
	}
	workers.Wait()
	ready := true
	for _, result := range results {
		if !result.Ready {
			ready = false
		}
	}

	report := FleetReadinessReport{
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

func probeMountedUI(handler http.Handler, path string, bearer string) UIProbeResult {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	code := rec.Code
	result := UIProbeResult{
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
