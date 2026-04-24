package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
)

type adminRuntimeProviderInfo struct {
	Name         string                   `json:"name"`
	Driver       string                   `json:"driver"`
	Default      bool                     `json:"default"`
	Loaded       bool                     `json:"loaded"`
	SessionCount *int                     `json:"sessionCount,omitempty"`
	Profile      *adminRuntimeProfilePair `json:"profile,omitempty"`
	Error        string                   `json:"error,omitempty"`
}

type adminRuntimeProfilePair struct {
	Advertised adminRuntimeProfile `json:"advertised"`
	Effective  adminRuntimeProfile `json:"effective"`
}

type adminRuntimeProfile struct {
	CanHostPlugins    bool                         `json:"canHostPlugins"`
	HostServiceAccess string                       `json:"hostServiceAccess"`
	EgressMode        string                       `json:"egressMode"`
	LaunchMode        string                       `json:"launchMode"`
	ExecutionTarget   *adminRuntimeExecutionTarget `json:"executionTarget,omitempty"`
}

type adminRuntimeExecutionTarget struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
}

type adminRuntimeSessionInfo struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Plugin string `json:"plugin,omitempty"`
}

type adminRuntimeSessionDiagnostics struct {
	Session   adminRuntimeSessionInfo `json:"session"`
	Logs      []adminRuntimeLogEntry  `json:"logs,omitempty"`
	Truncated bool                    `json:"truncated"`
}

type adminRuntimeLogEntry struct {
	Stream     string    `json:"stream"`
	Message    string    `json:"message"`
	ObservedAt time.Time `json:"observedAt"`
}

func (s *Server) mountAdminRuntimeRoutes(r chi.Router) {
	r.Get("/runtime/providers", s.listAdminRuntimeProviders)
	r.Get("/runtime/providers/{provider}/sessions", s.listAdminRuntimeProviderSessions)
	r.Get("/runtime/providers/{provider}/sessions/{session}/diagnostics", s.getAdminRuntimeSessionDiagnostics)
}

func (s *Server) listAdminRuntimeProviders(w http.ResponseWriter, r *http.Request) {
	snapshots, err := s.adminRuntimeSnapshots(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect runtime providers")
		return
	}

	out := make([]adminRuntimeProviderInfo, 0, len(snapshots))
	for i := range snapshots {
		snapshot := &snapshots[i]
		row := adminRuntimeProviderInfo{
			Name:    snapshot.Name,
			Driver:  strings.TrimSpace(string(snapshot.Driver)),
			Default: snapshot.Default,
			Loaded:  snapshot.Loaded,
			Error:   strings.TrimSpace(snapshot.Error),
		}
		if snapshot.Loaded && snapshot.SupportLoaded {
			row.Profile = adminRuntimeProfilePairFromSnapshot(snapshot)
		}
		if snapshot.Loaded && row.Error == "" {
			sessionCount := len(snapshot.Sessions)
			row.SessionCount = &sessionCount
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listAdminRuntimeProviderSessions(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "provider"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}

	snapshots, err := s.adminRuntimeSnapshots(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect runtime providers")
		return
	}
	for i := range snapshots {
		snapshot := &snapshots[i]
		if snapshot.Name != name {
			continue
		}
		if strings.TrimSpace(snapshot.Error) != "" {
			writeError(w, http.StatusServiceUnavailable, "runtime provider inspection is unavailable")
			return
		}
		out := make([]adminRuntimeSessionInfo, 0, len(snapshot.Sessions))
		for _, session := range snapshot.Sessions {
			out = append(out, adminRuntimeSessionInfoFromRuntime(session))
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	writeError(w, http.StatusNotFound, "runtime provider not found")
}

func (s *Server) getAdminRuntimeSessionDiagnostics(w http.ResponseWriter, r *http.Request) {
	providerName := strings.TrimSpace(chi.URLParam(r, "provider"))
	if providerName == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "session"))
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session is required")
		return
	}

	tailEntries := pluginruntime.DefaultSessionDiagnosticsTailEntries
	if raw := strings.TrimSpace(r.URL.Query().Get("tailEntries")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			writeError(w, http.StatusBadRequest, "tailEntries must be a non-negative integer")
			return
		}
		if value > pluginruntime.MaxSessionDiagnosticsTailEntries {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("tailEntries must be less than or equal to %d", pluginruntime.MaxSessionDiagnosticsTailEntries))
			return
		}
		tailEntries = value
	}

	if s.pluginRuntimes == nil {
		writeError(w, http.StatusNotFound, "runtime provider not found")
		return
	}
	diagnostics, err := s.pluginRuntimes.GetPluginRuntimeSessionDiagnostics(r.Context(), providerName, sessionID, tailEntries)
	if err != nil {
		switch {
		case errors.Is(err, pluginruntime.ErrProviderUnavailable):
			writeError(w, http.StatusNotFound, "runtime provider not found")
		case errors.Is(err, pluginruntime.ErrSessionUnavailable):
			writeError(w, http.StatusNotFound, "runtime session not found")
		case errors.Is(err, pluginruntime.ErrDiagnosticsUnavailable):
			writeError(w, http.StatusServiceUnavailable, "runtime session diagnostics are unavailable")
		default:
			writeError(w, http.StatusServiceUnavailable, "runtime session diagnostics are unavailable")
		}
		return
	}
	if diagnostics == nil {
		writeError(w, http.StatusNotFound, "runtime session not found")
		return
	}

	out := adminRuntimeSessionDiagnostics{
		Session:   adminRuntimeSessionInfoFromRuntime(diagnostics.Session),
		Truncated: diagnostics.Truncated,
	}
	if len(diagnostics.Logs) > 0 {
		out.Logs = make([]adminRuntimeLogEntry, 0, len(diagnostics.Logs))
		for _, entry := range diagnostics.Logs {
			out.Logs = append(out.Logs, adminRuntimeLogEntry{
				Stream:     strings.TrimSpace(string(entry.Stream)),
				Message:    entry.Message,
				ObservedAt: entry.ObservedAt,
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) adminRuntimeSnapshots(r *http.Request) ([]bootstrap.RuntimeProviderSnapshot, error) {
	if s.pluginRuntimes == nil {
		return nil, nil
	}
	return s.pluginRuntimes.SnapshotPluginRuntimes(r.Context())
}

func adminRuntimeProfilePairFromSnapshot(snapshot *bootstrap.RuntimeProviderSnapshot) *adminRuntimeProfilePair {
	return &adminRuntimeProfilePair{
		Advertised: adminRuntimeProfileFromBootstrap(snapshot.Advertised),
		Effective:  adminRuntimeProfileFromBootstrap(snapshot.Effective),
	}
}

func adminRuntimeProfileFromBootstrap(behavior bootstrap.RuntimeBehavior) adminRuntimeProfile {
	out := adminRuntimeProfile{
		CanHostPlugins:    behavior.CanHostPlugins,
		HostServiceAccess: strings.TrimSpace(string(behavior.HostServiceAccess)),
		EgressMode:        strings.TrimSpace(string(behavior.EgressMode)),
		LaunchMode:        strings.TrimSpace(string(behavior.LaunchMode)),
	}
	if behavior.ExecutionTarget.IsSet() {
		out.ExecutionTarget = &adminRuntimeExecutionTarget{
			GOOS:   strings.TrimSpace(behavior.ExecutionTarget.GOOS),
			GOARCH: strings.TrimSpace(behavior.ExecutionTarget.GOARCH),
		}
	}
	return out
}

func adminRuntimeSessionInfoFromRuntime(session pluginruntime.Session) adminRuntimeSessionInfo {
	return adminRuntimeSessionInfo{
		ID:     strings.TrimSpace(session.ID),
		State:  strings.TrimSpace(string(session.State)),
		Plugin: strings.TrimSpace(session.Metadata["plugin"]),
	}
}
