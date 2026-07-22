package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type appAdminRegistryResponse struct {
	App               string                     `json:"app"`
	Registry          string                     `json:"registry"`
	DesiredVersion    string                     `json:"desiredVersion,omitempty"`
	KnownVersions     []adminAppInstallationInfo `json:"knownVersions"`
	PublishedVersions []appAdminPublishedVersion `json:"publishedVersions"`
	Rollout           *appAdminRollout           `json:"rollout,omitempty"`
	SelectionDisabled bool                       `json:"selectionDisabled"`
	DisabledReason    string                     `json:"disabledReason,omitempty"`
}

type appAdminPublishedVersion struct {
	Version     string                   `json:"version"`
	PublishedAt string                   `json:"publishedAt"`
	Platforms   []string                 `json:"platforms,omitempty"`
	SourceRef   string                   `json:"sourceRef,omitempty"`
	SourceURL   string                   `json:"sourceUrl,omitempty"`
	Publication *appregistry.Publication `json:"publication,omitempty"`
}

type appAdminRollout struct {
	Version string `json:"version"`
	State   string `json:"state"`
}

type appAdminRegistryVersionRequest struct {
	Version string `json:"version"`
}

type appAdminRegistryVersionResponse struct {
	App            string          `json:"app"`
	Registry       string          `json:"registry"`
	FromVersion    string          `json:"fromVersion,omitempty"`
	DesiredVersion string          `json:"desiredVersion"`
	Rollout        appAdminRollout `json:"rollout"`
}

func (s *Server) mountAppAdminRegistryRoutes(r chi.Router) {
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Get("/apps/{app}/admin/registry", s.getAppAdminRegistry)
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Post("/apps/{app}/admin/registry/version", s.selectAppAdminRegistryVersion)
}

func (s *Server) appAdminAuthorizationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authorization == nil {
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
		p := PrincipalFromContext(r.Context())
		if err := requireUserCaller(w, p); err != nil {
			return
		}
		subjectID := strings.TrimSpace(principal.Canonicalized(p).SubjectID)
		if subjectID == "" {
			writeError(w, http.StatusUnauthorized, "missing authorization")
			return
		}
		appName := strings.TrimSpace(chi.URLParam(r, "app"))
		allowed, err := s.hasExplicitAppAdmin(r.Context(), subjectID, appName)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "app access denied")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) hasExplicitAppAdmin(ctx context.Context, subjectID, appName string) (bool, error) {
	if s == nil || s.authorization == nil {
		return false, errors.New("authorization is unavailable")
	}
	pageToken := ""
	for {
		resp, err := s.authorization.ListRelationships(ctx, &proto.ListRelationshipsRequest{
			Filter: &proto.RelationshipFilter{
				Target: &proto.RelationshipTarget{
					Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
						Type: "subject",
						Id:   strings.TrimSpace(subjectID),
					}},
				},
				Resource: &proto.Resource{Type: "app", Id: strings.TrimSpace(appName)},
			},
			PageSize:  500,
			PageToken: pageToken,
		})
		if err != nil {
			return false, err
		}
		for _, relationship := range resp.GetRelationships() {
			if strings.TrimSpace(relationship.GetTuple().GetRelation()) == "admin" {
				return true, nil
			}
		}
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			return false, nil
		}
	}
}

func (s *Server) getAppAdminRegistry(w http.ResponseWriter, r *http.Request) {
	app, registry, ok := s.appAdminRegistryConfig(w, r)
	if !ok {
		return
	}
	if s.appVersionChanges == nil || s.appRollouts == nil {
		writeError(w, http.StatusServiceUnavailable, "app registry installation services are unavailable")
		return
	}
	known, err := s.appVersionChanges.ListKnownVersionsByApp(r.Context(), app.name)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "app registry installation services are unavailable")
		return
	}
	publicRoot, err := registry.PublicURL()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "app registry is unavailable")
		return
	}
	reader := s.appRegistryReader
	if reader == nil {
		reader = &appregistry.RegistryReader{}
	}
	index, err := reader.FetchAppIndex(r.Context(), publicRoot, app.name)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch app registry index")
		return
	}
	summaries := appregistry.VersionsFromIndex(index, app.name)
	published := make([]appAdminPublishedVersion, 0, len(summaries))
	for _, summary := range summaries {
		published = append(published, appAdminPublishedVersion{
			Version:     summary.Version,
			PublishedAt: formatAdminTime(summary.PublishedAt),
			Platforms:   append([]string(nil), summary.Platforms...),
			SourceRef:   summary.SourceRef,
			SourceURL:   appVersionSourceURL(summary.Repository, summary.SourceRef),
			Publication: summary.Publication,
		})
	}
	knownVersions := make([]adminAppInstallationInfo, 0, len(known))
	for _, installation := range known {
		knownVersions = append(knownVersions, adminAppInstallationFromCore(installation))
	}
	response := appAdminRegistryResponse{
		App:               app.name,
		Registry:          app.registry,
		DesiredVersion:    coredata.LatestKnownVersion(known),
		KnownVersions:     knownVersions,
		PublishedVersions: published,
	}
	rollout, err := s.appRollouts.Get(r.Context(), app.name)
	if err == nil {
		response.Rollout = &appAdminRollout{Version: rollout.Version, State: string(rollout.State)}
		response.SelectionDisabled = isActiveAdminRollout(rollout.State)
		if response.SelectionDisabled {
			response.DisabledReason = "rollout in progress"
		}
	} else if !errors.Is(err, core.ErrNotFound) {
		writeError(w, http.StatusServiceUnavailable, "app registry installation services are unavailable")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) selectAppAdminRegistryVersion(w http.ResponseWriter, r *http.Request) {
	app, _, ok := s.appAdminRegistryConfig(w, r)
	if !ok {
		return
	}
	if s.appRegistryInstaller == nil {
		writeError(w, http.StatusServiceUnavailable, "app registry installer is unavailable")
		return
	}
	var request appAdminRegistryVersionRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	version := strings.TrimSpace(request.Version)
	if version == "" {
		writeError(w, http.StatusBadRequest, "version is required")
		return
	}
	subjectID := strings.TrimSpace(principal.Canonicalized(PrincipalFromContext(r.Context())).SubjectID)
	result, err := s.appRegistryInstaller.Select(r.Context(), appregistry.InstallInput{
		Registry: app.registry,
		App:      app.name,
		Version:  version,
		Actor:    subjectID,
	})
	if err != nil {
		writeAppAdminRegistryInstallError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, appAdminRegistryVersionResponse{
		App:            app.name,
		Registry:       app.registry,
		FromVersion:    result.FromVersion,
		DesiredVersion: result.Installation.Version,
		Rollout:        appAdminRollout{Version: result.Rollout.Version, State: string(result.Rollout.State)},
	})
}

func (s *Server) appAdminRegistryConfig(w http.ResponseWriter, r *http.Request) (configuredRegistryApp, config.AppRegistryConfig, bool) {
	appName := strings.TrimSpace(chi.URLParam(r, "app"))
	if appName == "" || providerregistry.ValidateRepositoryName(appName) != nil {
		writeError(w, http.StatusNotFound, "registry app not found")
		return configuredRegistryApp{}, config.AppRegistryConfig{}, false
	}
	app, ok := s.registryApp(appName)
	if !ok {
		writeError(w, http.StatusNotFound, "registry app not found")
		return configuredRegistryApp{}, config.AppRegistryConfig{}, false
	}
	registry, ok := s.appRegistries[app.registry]
	if !ok || strings.TrimSpace(registry.Kind) != config.AppRegistryKindGCS {
		writeError(w, http.StatusServiceUnavailable, "app registry is unavailable")
		return configuredRegistryApp{}, config.AppRegistryConfig{}, false
	}
	return app, registry, true
}

func writeAppAdminRegistryInstallError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, appregistry.ErrAppVersionAlreadyInstalled),
		errors.Is(err, appregistry.ErrInstallValidationFailed):
		status = http.StatusBadRequest
	case errors.Is(err, appregistry.ErrRegistryDocumentNotFound):
		status = http.StatusNotFound
	case errors.Is(err, appregistry.ErrInstallVersionLocked),
		errors.Is(err, appregistry.ErrAppRolloutActive):
		status = http.StatusConflict
	case errors.Is(err, appregistry.ErrRegistrySourceMismatch):
		status = http.StatusNotFound
	case strings.Contains(err.Error(), "not configured"):
		status = http.StatusServiceUnavailable
	}
	writeError(w, status, err.Error())
}

func appVersionSourceURL(repository, sourceRef string) string {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	sourceRef = strings.TrimSpace(sourceRef)
	if repository == "" || sourceRef == "" {
		return ""
	}
	if !strings.Contains(repository, "://") {
		repository = "https://" + repository
	}
	return repository + "/commit/" + sourceRef
}
