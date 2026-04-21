package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
)

type adminRuntimeProviderInfo struct {
	Name         string                    `json:"name"`
	Driver       string                    `json:"driver"`
	Default      bool                      `json:"default"`
	Loaded       bool                      `json:"loaded"`
	SessionCount *int                      `json:"sessionCount,omitempty"`
	Capabilities *adminRuntimeCapabilities `json:"capabilities,omitempty"`
	Error        string                    `json:"error,omitempty"`
}

type adminRuntimeCapabilities struct {
	HostedPluginRuntime bool `json:"hostedPluginRuntime"`
	HostServiceTunnels  bool `json:"hostServiceTunnels"`
	ProviderGRPCTunnel  bool `json:"providerGRPCTunnel"`
	HostnameProxyEgress bool `json:"hostnameProxyEgress"`
	CIDREgress          bool `json:"cidrEgress"`
}

type adminRuntimeSessionInfo struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Plugin string `json:"plugin,omitempty"`
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
	for _, snapshot := range snapshots {
		row := adminRuntimeProviderInfo{
			Name:    snapshot.Name,
			Driver:  strings.TrimSpace(string(snapshot.Driver)),
			Default: snapshot.Default,
			Loaded:  snapshot.Loaded,
			Error:   strings.TrimSpace(snapshot.Error),
		}
		if snapshot.Loaded && row.Error == "" {
			sessionCount := len(snapshot.Sessions)
			row.SessionCount = &sessionCount
		}
		if snapshot.Loaded && row.Error == "" {
			row.Capabilities = adminRuntimeCapabilitiesFromSnapshot(snapshot)
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
	for _, snapshot := range snapshots {
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

func (s *Server) adminRuntimeSnapshots(r *http.Request) ([]bootstrap.RuntimeProviderSnapshot, error) {
	if s.pluginRuntimes == nil {
		return nil, nil
	}
	return s.pluginRuntimes.SnapshotPluginRuntimes(r.Context())
}

func adminRuntimeCapabilitiesFromSnapshot(snapshot bootstrap.RuntimeProviderSnapshot) *adminRuntimeCapabilities {
	return &adminRuntimeCapabilities{
		HostedPluginRuntime: snapshot.Capabilities.HostedPluginRuntime,
		HostServiceTunnels:  snapshot.Capabilities.HostServiceTunnels,
		ProviderGRPCTunnel:  snapshot.Capabilities.ProviderGRPCTunnel,
		HostnameProxyEgress: snapshot.Capabilities.HostnameProxyEgress,
		CIDREgress:          snapshot.Capabilities.CIDREgress,
	}
}
