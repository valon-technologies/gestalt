package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
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

func (s *Server) mountAdminRuntimeRoutes(r chi.Router) {
	r.Get("/runtime/providers", s.listAdminRuntimeProviders)
	r.Get("/runtime/providers/{provider}/sessions", s.listAdminRuntimeProviderSessions)
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
		resp = &proto.ListRuntimeSessionsResponse{}
	}
	out := make([]adminRuntimeSessionInfo, 0, len(resp.GetSessions()))
	for _, session := range resp.GetSessions() {
		out = append(out, adminRuntimeSessionInfo{
			ID:    strings.TrimSpace(session.GetId()),
			State: strings.TrimSpace(session.GetState()),
			App:   adminRuntimeSessionApp(session),
		})
	}
	writeJSON(w, http.StatusOK, adminRuntimeSessionListResponse{
		Sessions:      out,
		NextPageToken: strings.TrimSpace(resp.GetNextPageToken()),
	})
}

func adminRuntimeSessionApp(session *proto.RuntimeSession) string {
	if app := strings.TrimSpace(session.GetMetadata()["app"]); app != "" {
		return app
	}
	return strings.TrimSpace(session.GetMetadata()["provider_name"])
}

func (s *Server) adminRuntimeSnapshots(r *http.Request) ([]bootstrap.RuntimeProviderSnapshot, error) {
	if s.pluginRuntimes == nil {
		return nil, nil
	}
	return s.pluginRuntimes.SnapshotRuntimes(r.Context())
}

func (s *Server) adminRuntimeSessions(r *http.Request, providerName string, req *proto.ListRuntimeSessionsRequest) (*proto.ListRuntimeSessionsResponse, error) {
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
)

func adminRuntimeSessionListRequestFromQuery(w http.ResponseWriter, r *http.Request) (*proto.ListRuntimeSessionsRequest, bool) {
	rawPageSize := queryValue(r.URL.Query(), "pageSize", "page_size")
	pageToken := strings.TrimSpace(queryValue(r.URL.Query(), "pageToken", "page_token"))
	pageSize, ok := parseOptionalIntQuery(w, rawPageSize, "pageSize")
	if !ok {
		return nil, false
	}
	if pageSize < 0 {
		writeError(w, http.StatusBadRequest, "pageSize must be non-negative")
		return nil, false
	}
	if rawPageSize == "" && pageToken != "" {
		return &proto.ListRuntimeSessionsRequest{PageToken: pageToken}, true
	}
	if pageSize == 0 {
		pageSize = defaultAdminRuntimeSessionPageSize
	}
	if pageSize > maxAdminRuntimeSessionPageSize {
		pageSize = maxAdminRuntimeSessionPageSize
	}
	return &proto.ListRuntimeSessionsRequest{
		PageSize:  int32(pageSize),
		PageToken: pageToken,
	}, true
}
