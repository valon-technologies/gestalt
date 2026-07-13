package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
)

type adminAppInstallationInfo struct {
	AppName            string            `json:"app"`
	Version            string            `json:"version"`
	SourceRef          string            `json:"sourceRef,omitempty"`
	Registry           string            `json:"registry,omitempty"`
	ProviderReleaseURL string            `json:"providerReleaseUrl,omitempty"`
	ArtifactChecksums  map[string]string `json:"artifactChecksums,omitempty"`
	InstalledBy        string            `json:"installedBy,omitempty"`
	InstalledAt        string            `json:"installedAt,omitempty"`
	UpdatedAt          string            `json:"updatedAt,omitempty"`
}

type adminAppRegistryInstallRequest struct {
	Version string `json:"version"`
	Actor   string `json:"actor,omitempty"`
}

type adminAppRegistryInstallResponse struct {
	Registry     string                   `json:"registry"`
	App          string                   `json:"app"`
	Installation adminAppInstallationInfo `json:"installation"`
}

func (s *Server) mountAdminAppInstallReadRoutes(r chi.Router) {
	r.Get("/app-installations", s.listAdminAppInstallations)
	r.Get("/app-installations/{app}", s.getAdminAppInstallation)
}

func (s *Server) mountAdminAppInstallWriteRoutes(r chi.Router) {
	r.Post("/app-registries/{registry}/apps/{app}/install", s.installAdminAppRegistryApp)
}

func (s *Server) listAdminAppInstallations(w http.ResponseWriter, r *http.Request) {
	if s.appRegistryInstaller == nil || s.appRegistryInstaller.ChangeRequests == nil {
		writeError(w, http.StatusServiceUnavailable, "app installation service is unavailable")
		return
	}
	versions, err := s.appRegistryInstaller.ChangeRequests.ListAllKnownVersions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list app installations")
		return
	}
	out := make([]adminAppInstallationInfo, 0, len(versions))
	for _, installation := range versions {
		out = append(out, adminAppInstallationFromCore(installation))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getAdminAppInstallation(w http.ResponseWriter, r *http.Request) {
	if s.appRegistryInstaller == nil || s.appRegistryInstaller.ChangeRequests == nil {
		writeError(w, http.StatusServiceUnavailable, "app installation service is unavailable")
		return
	}
	appName := strings.TrimSpace(chi.URLParam(r, "app"))
	if appName == "" {
		writeError(w, http.StatusBadRequest, "app is required")
		return
	}
	if err := providerregistry.ValidateRepositoryName(appName); err != nil {
		writeError(w, http.StatusBadRequest, "invalid app name")
		return
	}
	versions, err := s.appRegistryInstaller.ChangeRequests.ListKnownVersionsByApp(r.Context(), appName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app installations")
		return
	}
	if len(versions) == 0 {
		writeError(w, http.StatusNotFound, "app installation not found")
		return
	}
	out := make([]adminAppInstallationInfo, 0, len(versions))
	for _, installation := range versions {
		out = append(out, adminAppInstallationFromCore(installation))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) installAdminAppRegistryApp(w http.ResponseWriter, r *http.Request) {
	if s.appRegistryInstaller == nil {
		writeError(w, http.StatusServiceUnavailable, "app registry installer is unavailable")
		return
	}
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
	if _, ok := s.appRegistries[registryName]; !ok {
		writeError(w, http.StatusNotFound, "app registry not found")
		return
	}

	var req adminAppRegistryInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		writeError(w, http.StatusBadRequest, "version is required")
		return
	}

	result, err := s.appRegistryInstaller.Install(r.Context(), appregistry.InstallInput{
		Registry: registryName,
		App:      appName,
		Version:  version,
		Actor:    strings.TrimSpace(req.Actor),
	})
	if err != nil {
		status := http.StatusBadGateway
		switch {
		case strings.Contains(err.Error(), "app registry not found"):
			status = http.StatusNotFound
		case strings.Contains(err.Error(), "invalid app name"),
			strings.Contains(err.Error(), "registry is required"),
			strings.Contains(err.Error(), "app is required"),
			strings.Contains(err.Error(), "version is required"),
			strings.Contains(err.Error(), "unsupported app registry kind"):
			status = http.StatusBadRequest
		case errors.Is(err, appregistry.ErrRegistryDocumentNotFound):
			status = http.StatusNotFound
		case errors.Is(err, appregistry.ErrInstallVersionLocked):
			status = http.StatusConflict
		case errors.Is(err, appregistry.ErrAppVersionAlreadyInstalled):
			status = http.StatusBadRequest
		case errors.Is(err, context.DeadlineExceeded):
			status = http.StatusGatewayTimeout
		}
		writeError(w, status, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, adminAppRegistryInstallResponse{
		Registry:     registryName,
		App:          appName,
		Installation: adminAppInstallationFromCore(result.Installation),
	})
}

func adminAppInstallationFromCore(installation *core.AppInstallation) adminAppInstallationInfo {
	if installation == nil {
		return adminAppInstallationInfo{}
	}
	info := adminAppInstallationInfo{
		AppName:            installation.AppName,
		Version:            installation.Version,
		SourceRef:          installation.SourceRef,
		Registry:           installation.Registry,
		ProviderReleaseURL: installation.ProviderReleaseURL,
		ArtifactChecksums:  installation.ArtifactChecksums,
		InstalledBy:        installation.InstalledBy,
	}
	if !installation.InstalledAt.IsZero() {
		info.InstalledAt = installation.InstalledAt.UTC().Format(time.RFC3339)
	}
	if !installation.UpdatedAt.IsZero() {
		info.UpdatedAt = installation.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return info
}

func newAppRegistryInstaller(cfg Config) *appregistry.Installer {
	if cfg.Services == nil || cfg.Services.AppVersionChangeRequests == nil || cfg.Services.AppVersionInstallLocks == nil {
		return nil
	}
	reader := cfg.AppRegistryReader
	if reader == nil {
		reader = &appregistry.RegistryReader{}
	}
	return &appregistry.Installer{
		Registries:     cloneAppRegistryConfig(cfg.AppRegistries),
		ConfigApps:     cfg.AppDefs,
		Reader:         reader,
		ChangeRequests: cfg.Services.AppVersionChangeRequests,
		Locks:          cfg.Services.AppVersionInstallLocks,
	}
}
