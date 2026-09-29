package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

const uiReadinessIncompleteReason = "ui readiness incomplete"

// An instance that cannot admit itself reports which mounts held it back. The
// candidate is unreachable while it fails its startup probe -- no traffic, no
// URL -- so this log is the only account of why, and /ready alone cannot say.
const (
	unreadyMountLogLimit   = 10
	unreadyProbeErrorLimit = 80
)

// readinessProbeContextKey marks a request as an in-process UI readiness probe.
// UIReadinessMonitor synthesizes these requests and serves them straight through
// the handler chain, so the marker cannot be set by a network caller and grants
// nothing that is reachable from outside the process.
type readinessProbeContextKey struct{}

func withReadinessProbe(ctx context.Context) context.Context {
	return context.WithValue(ctx, readinessProbeContextKey{}, struct{}{})
}

func isReadinessProbe(ctx context.Context) bool {
	return ctx != nil && ctx.Value(readinessProbeContextKey{}) != nil
}

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
	unready := unreadyMounts(results)
	ready := len(unready) == 0
	if len(results) > 0 {
		if ready {
			slog.Info("ui readiness complete", "mounts", len(results))
		} else {
			slog.Warn(
				uiReadinessIncompleteReason,
				"ready", len(results)-len(unready),
				"mounts", len(results),
				"unready", summarizeUnreadyMounts(unready),
			)
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

func unreadyMounts(results []UIProbeResult) []UIProbeResult {
	unready := make([]UIProbeResult, 0, len(results))
	for _, result := range results {
		if !result.Ready {
			unready = append(unready, result)
		}
	}
	return unready
}

func summarizeUnreadyMounts(unready []UIProbeResult) string {
	shown := unready
	suffix := ""
	if len(shown) > unreadyMountLogLimit {
		shown = shown[:unreadyMountLogLimit]
		suffix = fmt.Sprintf(" and %d more", len(unready)-unreadyMountLogLimit)
	}
	described := make([]string, 0, len(shown))
	for _, result := range shown {
		described = append(described, describeUnreadyMount(result))
	}
	return strings.Join(described, ", ") + suffix
}

func describeUnreadyMount(result UIProbeResult) string {
	status := "no response"
	if result.StatusCode != nil {
		status = strconv.Itoa(*result.StatusCode)
	}
	message := ""
	if result.Error != nil {
		message = strings.Join(strings.Fields(*result.Error), " ")
		if runes := []rune(message); len(runes) > unreadyProbeErrorLimit {
			message = string(runes[:unreadyProbeErrorLimit]) + "..."
		}
	}
	if message == "" {
		return fmt.Sprintf("%s (%s)", result.Mount, status)
	}
	return fmt.Sprintf("%s (%s %s)", result.Mount, status, message)
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
	req = req.WithContext(withReadinessProbe(req.Context()))
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
