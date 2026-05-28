package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimelogs"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimeprovider"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type adminRuntimeProviderInfo struct {
	Name    string               `json:"name"`
	Driver  string               `json:"driver"`
	Default bool                 `json:"default"`
	Loaded  bool                 `json:"loaded"`
	Profile *adminRuntimeProfile `json:"profile,omitempty"`
	Error   string               `json:"error,omitempty"`
}

type adminRuntimeProfile struct {
	CanHostApps bool   `json:"canHostApps"`
	EgressMode  string `json:"egressMode"`
}

type adminRuntimeSessionInfo struct {
	ID    string `json:"id"`
	State string `json:"state"`
	App   string `json:"app,omitempty"`
}

type adminRuntimeSessionListResponse struct {
	Sessions      []adminRuntimeSessionInfo `json:"sessions"`
	NextPageToken string                    `json:"nextPageToken,omitempty"`
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
			profile := adminRuntimeProfileFromBootstrap(snapshot.Profile)
			row.Profile = &profile
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
	listReq, ok := adminRuntimeSessionListRequestFromQuery(w, r)
	if !ok {
		return
	}
	resp, err := s.adminRuntimeSessions(r, name, listReq)
	if err != nil {
		switch {
		case errors.Is(err, runtimeprovider.ErrInvalidListSessionsPagination):
			writeError(w, http.StatusBadRequest, err.Error())
		case grpcstatus.Code(err) == codes.InvalidArgument:
			writeError(w, http.StatusBadRequest, "invalid runtime session page request")
		case errors.Is(err, bootstrap.ErrRuntimeProviderNotFound):
			writeError(w, http.StatusNotFound, "runtime provider not found")
		default:
			writeError(w, http.StatusServiceUnavailable, "runtime provider sessions are unavailable")
			return
		}
		return
	}
	if resp == nil {
		resp = &runtimeprovider.ListSessionsResponse{}
	}
	out := make([]adminRuntimeSessionInfo, 0, len(resp.Sessions))
	for _, session := range resp.Sessions {
		out = append(out, adminRuntimeSessionInfo{
			ID:    strings.TrimSpace(session.ID),
			State: strings.TrimSpace(string(session.State)),
			App:   adminRuntimeSessionApp(session),
		})
	}
	writeJSON(w, http.StatusOK, adminRuntimeSessionListResponse{
		Sessions:      out,
		NextPageToken: strings.TrimSpace(resp.NextPageToken),
	})
}

func adminRuntimeSessionApp(session runtimeprovider.Session) string {
	if app := strings.TrimSpace(session.Metadata["app"]); app != "" {
		return app
	}
	return strings.TrimSpace(session.Metadata["provider_name"])
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
	logs, err := s.pluginRuntimes.ListRuntimeSessionLogs(r.Context(), providerName, sessionID, afterSeq, limit)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) || errors.Is(err, runtimelogs.ErrSessionNotFound) {
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
	return s.pluginRuntimes.SnapshotRuntimes(r.Context())
}

func (s *Server) adminRuntimeSessions(r *http.Request, providerName string, req runtimeprovider.ListSessionsRequest) (*runtimeprovider.ListSessionsResponse, error) {
	if s.pluginRuntimes == nil {
		return nil, bootstrap.ErrRuntimeProviderNotFound
	}
	return s.pluginRuntimes.ListRuntimeSessions(r.Context(), providerName, req)
}

func adminRuntimeProfileFromBootstrap(profile bootstrap.RuntimePlacementPlan) adminRuntimeProfile {
	return adminRuntimeProfile{
		CanHostApps: profile.CanHostApps,
		EgressMode:  strings.TrimSpace(string(profile.EgressMode)),
	}
}

const (
	defaultAdminRuntimeSessionPageSize = 100
	maxAdminRuntimeSessionPageSize     = 200
	defaultAdminRuntimeLogLimit        = 200
	maxAdminRuntimeLogLimit            = 1000
)

func adminRuntimeSessionListRequestFromQuery(w http.ResponseWriter, r *http.Request) (runtimeprovider.ListSessionsRequest, bool) {
	rawPageSize := queryValue(r.URL.Query(), "pageSize", "page_size")
	pageToken := strings.TrimSpace(queryValue(r.URL.Query(), "pageToken", "page_token"))
	pageSize, ok := parseOptionalIntQuery(w, rawPageSize, "pageSize")
	if !ok {
		return runtimeprovider.ListSessionsRequest{}, false
	}
	if pageSize < 0 {
		writeError(w, http.StatusBadRequest, "pageSize must be non-negative")
		return runtimeprovider.ListSessionsRequest{}, false
	}
	if rawPageSize == "" && pageToken != "" {
		return runtimeprovider.ListSessionsRequest{PageToken: pageToken}, true
	}
	if pageSize == 0 {
		pageSize = defaultAdminRuntimeSessionPageSize
	}
	if pageSize > maxAdminRuntimeSessionPageSize {
		pageSize = maxAdminRuntimeSessionPageSize
	}
	return runtimeprovider.ListSessionsRequest{
		PageSize:  pageSize,
		PageToken: pageToken,
	}, true
}

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
