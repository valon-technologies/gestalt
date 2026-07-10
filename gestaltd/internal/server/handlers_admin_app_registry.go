package server

import (
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
)

type adminAppRegistryInfo struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	PublicURL  string `json:"publicUrl,omitempty"`
	StorageURL string `json:"storageUrl,omitempty"`
}

type adminAppRegistryVersionsResponse struct {
	Registry string                       `json:"registry"`
	App      string                       `json:"app"`
	Versions []appregistry.VersionSummary `json:"versions"`
}

func (s *Server) mountAdminAppRegistryRoutes(r chi.Router) {
	r.Get("/app-registries", s.listAdminAppRegistries)
	r.Get("/app-registries/{registry}/apps/{app}/versions", s.listAdminAppRegistryAppVersions)
}

func (s *Server) listAdminAppRegistries(w http.ResponseWriter, r *http.Request) {
	registries := s.appRegistries
	if len(registries) == 0 {
		writeJSON(w, http.StatusOK, []adminAppRegistryInfo{})
		return
	}
	names := make([]string, 0, len(registries))
	for name := range registries {
		names = append(names, name)
	}
	slices.Sort(names)

	out := make([]adminAppRegistryInfo, 0, len(names))
	for _, name := range names {
		registry := registries[name]
		info := adminAppRegistryInfo{
			Name: name,
			Kind: strings.TrimSpace(registry.Kind),
		}
		if publicURL, err := registry.PublicURL(); err == nil {
			info.PublicURL = publicURL
		}
		if storageURL, err := registry.StorageURL(); err == nil {
			info.StorageURL = storageURL
		}
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listAdminAppRegistryAppVersions(w http.ResponseWriter, r *http.Request) {
	registryName := strings.TrimSpace(chi.URLParam(r, "registry"))
	appName := strings.TrimSpace(chi.URLParam(r, "app"))
	if registryName == "" {
		writeError(w, http.StatusBadRequest, "registry is required")
		return
	}
	if appName == "" {
		writeError(w, http.StatusBadRequest, "app is required")
		return
	}
	if err := providerregistry.ValidateRepositoryName(appName); err != nil {
		writeError(w, http.StatusBadRequest, "invalid app name")
		return
	}
	if len(s.appRegistries) == 0 {
		writeError(w, http.StatusNotFound, "app registry not found")
		return
	}
	registry, ok := s.appRegistries[registryName]
	if !ok {
		writeError(w, http.StatusNotFound, "app registry not found")
		return
	}
	if strings.TrimSpace(registry.Kind) != config.AppRegistryKindGCS {
		writeError(w, http.StatusBadRequest, "unsupported app registry kind")
		return
	}
	publicRoot, err := registry.PublicURL()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "app registry public URL is invalid")
		return
	}

	reader := s.appRegistryReader
	if reader == nil {
		reader = &appregistry.RegistryReader{}
	}
	index, err := reader.FetchAppIndex(r.Context(), publicRoot, appName)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch app registry index")
		return
	}
	writeJSON(w, http.StatusOK, adminAppRegistryVersionsResponse{
		Registry: registryName,
		App:      appName,
		Versions: appregistry.VersionsFromIndex(index, appName),
	})
}

func cloneAppRegistryConfig(src map[string]config.AppRegistryConfig) map[string]config.AppRegistryConfig {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]config.AppRegistryConfig, len(src))
	for name, registry := range src {
		out[name] = registry
	}
	return out
}
