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
	Profile      *adminRuntimeProfilePair  `json:"profile,omitempty"`
	Error        string                    `json:"error,omitempty"`
}

type adminRuntimeCapabilities struct {
	HostedPluginRuntime bool `json:"hostedPluginRuntime"`
	HostServiceTunnels  bool `json:"hostServiceTunnels"`
	ProviderGRPCTunnel  bool `json:"providerGRPCTunnel"`
	HostnameProxyEgress bool `json:"hostnameProxyEgress"`
	CIDREgress          bool `json:"cidrEgress"`
}

type adminRuntimeProfilePair struct {
	Advertised adminRuntimeProfile `json:"advertised"`
	Effective  adminRuntimeProfile `json:"effective"`
}

type adminRuntimeProfile struct {
	HostedExecution   bool                         `json:"hostedExecution"`
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
		if snapshot.Loaded && snapshot.CapabilitiesLoaded {
			row.Capabilities = adminRuntimeCapabilitiesFromSnapshot(snapshot)
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

func (s *Server) adminRuntimeSnapshots(r *http.Request) ([]bootstrap.RuntimeProviderSnapshot, error) {
	if s.pluginRuntimes == nil {
		return nil, nil
	}
	return s.pluginRuntimes.SnapshotPluginRuntimes(r.Context())
}

func adminRuntimeCapabilitiesFromSnapshot(snapshot *bootstrap.RuntimeProviderSnapshot) *adminRuntimeCapabilities {
	return &adminRuntimeCapabilities{
		HostedPluginRuntime: snapshot.Capabilities.HostedPluginRuntime,
		HostServiceTunnels:  snapshot.Capabilities.HostServiceTunnels,
		ProviderGRPCTunnel:  snapshot.Capabilities.ProviderGRPCTunnel,
		HostnameProxyEgress: snapshot.Capabilities.HostnameProxyEgress,
		CIDREgress:          snapshot.Capabilities.CIDREgress,
	}
}

func adminRuntimeProfilePairFromSnapshot(snapshot *bootstrap.RuntimeProviderSnapshot) *adminRuntimeProfilePair {
	return &adminRuntimeProfilePair{
		Advertised: adminRuntimeProfileFromBootstrap(snapshot.AdvertisedProfile),
		Effective:  adminRuntimeProfileFromBootstrap(snapshot.EffectiveProfile),
	}
}

func adminRuntimeProfileFromBootstrap(profile bootstrap.RuntimeProfile) adminRuntimeProfile {
	out := adminRuntimeProfile{
		HostedExecution:   profile.HostedExecution,
		HostServiceAccess: strings.TrimSpace(string(profile.HostServiceMode)),
		EgressMode:        strings.TrimSpace(string(profile.EgressMode)),
		LaunchMode:        strings.TrimSpace(string(profile.LaunchMode)),
	}
	if profile.ExecutionTarget.IsSet() {
		out.ExecutionTarget = &adminRuntimeExecutionTarget{
			GOOS:   strings.TrimSpace(profile.ExecutionTarget.GOOS),
			GOARCH: strings.TrimSpace(profile.ExecutionTarget.GOARCH),
		}
	}
	return out
}
