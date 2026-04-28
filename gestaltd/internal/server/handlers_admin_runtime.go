package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/runtimelogs"
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
	CanHostPlugins    bool   `json:"canHostPlugins"`
	HostServiceAccess string `json:"hostServiceAccess"`
	EgressMode        string `json:"egressMode"`
}

type adminRuntimeSessionInfo struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Plugin string `json:"plugin,omitempty"`
}

type adminRuntimeLogEntry struct {
	Seq        int64      `json:"seq"`
	SourceSeq  int64      `json:"sourceSeq,omitempty"`
	Stream     string     `json:"stream"`
	Message    string     `json:"message"`
	ObservedAt *time.Time `json:"observedAt,omitempty"`
	AppendedAt *time.Time `json:"appendedAt,omitempty"`
}

func (s *Server) mountAdminRuntimeRoutes(r chi.Router) {
	r.Get("/runtime/providers", s.listAdminRuntimeProviders)
	r.Get("/runtime/providers/{provider}/sessions", s.listAdminRuntimeProviderSessions)
	r.Get("/runtime/providers/{provider}/sessions/{session}/logs", s.listAdminRuntimeProviderSessionLogs)
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
			out = append(out, adminRuntimeSessionInfo{
				ID:     strings.TrimSpace(session.ID),
				State:  strings.TrimSpace(string(session.State)),
				Plugin: strings.TrimSpace(session.Metadata["plugin"]),
			})
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	writeError(w, http.StatusNotFound, "runtime provider not found")
}

func (s *Server) listAdminRuntimeProviderSessionLogs(w http.ResponseWriter, r *http.Request) {
	providerName := strings.TrimSpace(chi.URLParam(r, "provider"))
	sessionID := strings.TrimSpace(chi.URLParam(r, "session"))
	if providerName == "" || sessionID == "" {
		writeError(w, http.StatusBadRequest, "provider and session are required")
		return
	}
	afterSeq, ok := parseAdminRuntimeLogCursor(w, r)
	if !ok {
		return
	}
	limit, ok := parseAdminRuntimeLogLimit(w, r)
	if !ok {
		return
	}
	if s.pluginRuntimes == nil {
		writeJSON(w, http.StatusOK, []adminRuntimeLogEntry{})
		return
	}
	logs, err := s.pluginRuntimes.ListPluginRuntimeSessionLogs(r.Context(), providerName, sessionID, afterSeq, limit)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) || errors.Is(err, runtimelogs.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "runtime session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load runtime session logs")
		return
	}
	out := make([]adminRuntimeLogEntry, 0, len(logs))
	for _, entry := range logs {
		value := adminRuntimeLogEntry{
			Seq:       entry.Seq,
			SourceSeq: entry.SourceSeq,
			Stream:    strings.TrimSpace(string(entry.Stream)),
			Message:   entry.Message,
		}
		if !entry.ObservedAt.IsZero() {
			observedAt := entry.ObservedAt
			value.ObservedAt = &observedAt
		}
		if !entry.AppendedAt.IsZero() {
			appendedAt := entry.AppendedAt
			value.AppendedAt = &appendedAt
		}
		out = append(out, value)
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
	return adminRuntimeProfile{
		CanHostPlugins:    behavior.CanHostPlugins,
		HostServiceAccess: strings.TrimSpace(string(behavior.HostServiceAccess)),
		EgressMode:        strings.TrimSpace(string(behavior.EgressMode)),
	}
}

const (
	defaultAdminRuntimeLogLimit = 200
	maxAdminRuntimeLogLimit     = 1000
)

func parseAdminRuntimeLogCursor(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("after"))
	if raw == "" {
		return 0, true
	}
	afterSeq, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || afterSeq < 0 {
		writeError(w, http.StatusBadRequest, "after must be a non-negative integer")
		return 0, false
	}
	return afterSeq, true
}

func parseAdminRuntimeLogLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return defaultAdminRuntimeLogLimit, true
	}
	limit64, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || limit64 < 1 {
		writeError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if limit64 > maxAdminRuntimeLogLimit {
		limit64 = maxAdminRuntimeLogLimit
	}
	return int(limit64), true
}
