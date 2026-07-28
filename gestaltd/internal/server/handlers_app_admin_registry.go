package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	PendingVersions   []appAdminPendingVersion   `json:"pendingVersions,omitempty"`
	FailedVersions    []appAdminFailedVersion    `json:"failedVersions,omitempty"`
	Rollout           *appAdminRollout           `json:"rollout,omitempty"`
	AutoDeploy        appAdminAutoDeploy         `json:"autoDeploy"`
	SelectionDisabled bool                       `json:"selectionDisabled"`
	DisabledReason    string                     `json:"disabledReason,omitempty"`
}

type appAdminAutoDeploy struct {
	Enabled        bool   `json:"enabled"`
	PendingVersion string `json:"pendingVersion,omitempty"`
	LastError      string `json:"lastError,omitempty"`
}

type appAdminPublishedVersion struct {
	Version                string                   `json:"version"`
	PublishedAt            string                   `json:"publishedAt"`
	PublishStartedAt       string                   `json:"publishStartedAt,omitempty"`
	PublishDurationSeconds *int64                   `json:"publishDurationSeconds,omitempty"`
	Platforms              []string                 `json:"platforms,omitempty"`
	SourceRef              string                   `json:"sourceRef,omitempty"`
	SourceURL              string                   `json:"sourceUrl,omitempty"`
	Publication            *appregistry.Publication `json:"publication,omitempty"`
	DeploymentState        string                   `json:"deploymentState,omitempty"`
	DeployableUntil        string                   `json:"deployableUntil,omitempty"`
	Current                bool                     `json:"current,omitempty"`
}

type appAdminPendingVersion struct {
	Version              string                   `json:"version"`
	StartedAt            string                   `json:"startedAt"`
	UpdatedAt            string                   `json:"updatedAt"`
	Phase                string                   `json:"phase"`
	PublishingForSeconds *int64                   `json:"publishingForSeconds,omitempty"`
	SourceRef            string                   `json:"sourceRef,omitempty"`
	SourceURL            string                   `json:"sourceUrl,omitempty"`
	Publication          *appregistry.Publication `json:"publication,omitempty"`
}

type appAdminFailedVersion struct {
	Version                string                   `json:"version"`
	StartedAt              string                   `json:"startedAt"`
	FailedAt               string                   `json:"failedAt"`
	Reason                 string                   `json:"reason"`
	PublishDurationSeconds *int64                   `json:"publishDurationSeconds,omitempty"`
	SourceRef              string                   `json:"sourceRef,omitempty"`
	SourceURL              string                   `json:"sourceUrl,omitempty"`
	Publication            *appregistry.Publication `json:"publication,omitempty"`
}

type appAdminRollout struct {
	Version             string `json:"version"`
	State               string `json:"state"`
	TargetSourceVersion string `json:"targetSourceVersion,omitempty"`
}

type appAdminRegistryVersionRequest struct {
	Version string `json:"version"`
}

type appAdminRegistryAutoDeployRequest struct {
	Enabled *bool `json:"enabled"`
}

type appAdminRegistryAutoDeployResponse struct {
	App        string             `json:"app"`
	AutoDeploy appAdminAutoDeploy `json:"autoDeploy"`
}

type appAdminRegistryVersionResponse struct {
	App            string          `json:"app"`
	Registry       string          `json:"registry"`
	FromVersion    string          `json:"fromVersion,omitempty"`
	DesiredVersion string          `json:"desiredVersion"`
	Rollout        appAdminRollout `json:"rollout"`
}

type appAdminRegistryHistoryResponse struct {
	App        string                     `json:"app"`
	Revisions  []appAdminRegistryRevision `json:"revisions"`
	NextCursor string                     `json:"nextCursor,omitempty"`
}

type appAdminRegistryRevision struct {
	ID                     string                   `json:"id"`
	Version                string                   `json:"version"`
	PreviousVersion        string                   `json:"previousVersion,omitempty"`
	DeployedAt             string                   `json:"deployedAt"`
	DeployedBy             string                   `json:"deployedBy,omitempty"`
	SourceRef              string                   `json:"sourceRef,omitempty"`
	SourceURL              string                   `json:"sourceUrl,omitempty"`
	Publication            *appregistry.Publication `json:"publication,omitempty"`
	DeploymentState        string                   `json:"deploymentState,omitempty"`
	DeployableUntil        string                   `json:"deployableUntil,omitempty"`
	Current                bool                     `json:"current,omitempty"`
	RolloutState           string                   `json:"rolloutState,omitempty"`
	RolloutForSeconds      *int64                   `json:"rolloutForSeconds,omitempty"`
	RolloutDurationSeconds *int64                   `json:"rolloutDurationSeconds,omitempty"`
	RolloutCompletedAt     string                   `json:"rolloutCompletedAt,omitempty"`
	RolloutFailedAt        string                   `json:"rolloutFailedAt,omitempty"`
}

func (s *Server) mountAppAdminRegistryRoutes(r chi.Router) {
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Get("/apps/{app}/admin/registry", s.getAppAdminRegistry)
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Get("/apps/{app}/admin/registry/history", s.getAppAdminRegistryHistory)
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Post("/apps/{app}/admin/registry/version", s.selectAppAdminRegistryVersion)
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Put("/apps/{app}/admin/registry/auto-deploy", s.updateAppAdminRegistryAutoDeploy)
}

func (s *Server) appAdminAuthorizationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authorization == nil {
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
		p := PrincipalFromContext(r.Context())
		if p == nil {
			writeError(w, http.StatusUnauthorized, "missing authorization")
			return
		}
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
	if s.appVersionChanges == nil || s.appRollouts == nil || s.autoDeploySettings == nil {
		writeError(w, http.StatusServiceUnavailable, "app registry installation services are unavailable")
		return
	}
	autoDeploy := appAdminAutoDeploy{}
	settings, err := s.autoDeploySettings.Get(r.Context(), app.name)
	if err == nil {
		autoDeploy = appAdminAutoDeployFromCore(settings)
	} else if !errors.Is(err, core.ErrNotFound) {
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
	pendingIndex, err := reader.FetchPendingIndex(r.Context(), publicRoot, app.name)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch app registry pending catalog")
		return
	}
	failedIndex, err := reader.FetchFailedIndex(r.Context(), publicRoot, app.name)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch app registry failed catalog")
		return
	}
	retentionIndex, err := reader.FetchRetentionIndex(r.Context(), publicRoot, app.name)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch app registry retention catalog")
		return
	}
	policy, err := retentionPolicyFromRegistry(registry)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "app registry is unavailable")
		return
	}
	now := time.Now().UTC()
	publishedKeys := appregistry.PublishedVersionKeys(index, app.name)
	pendingKeys := appregistry.PendingVersionKeys(pendingIndex)
	summaries := appregistry.VersionsFromIndex(index, app.name)
	published := make([]appAdminPublishedVersion, 0, len(summaries))
	desiredVersion := coredata.LatestKnownVersion(known)
	for i := range summaries {
		published = append(published, appAdminPublishedVersionFromSummary(summaries[i], desiredVersion, retentionIndex, policy, now))
	}
	pendingVersions := make([]appAdminPendingVersion, 0)
	for _, entry := range appregistry.PendingVersionsForAdmin(pendingIndex, publishedKeys) {
		pendingVersions = append(pendingVersions, appAdminPendingVersionFromEntry(entry, now))
	}
	failedVersions := make([]appAdminFailedVersion, 0)
	for _, entry := range appregistry.FailedVersionsForAdmin(failedIndex, publishedKeys, pendingKeys) {
		failedVersions = append(failedVersions, appAdminFailedVersionFromEntry(entry))
	}
	knownVersions := make([]adminAppInstallationInfo, 0, len(known))
	for _, installation := range known {
		knownVersions = append(knownVersions, adminAppInstallationFromCore(installation))
	}
	response := appAdminRegistryResponse{
		App:               app.name,
		Registry:          app.registry,
		DesiredVersion:    desiredVersion,
		KnownVersions:     knownVersions,
		PublishedVersions: published,
		PendingVersions:   pendingVersions,
		FailedVersions:    failedVersions,
		AutoDeploy:        autoDeploy,
	}
	rollout, err := s.appRollouts.Get(r.Context(), app.name)
	if err == nil {
		response.Rollout = &appAdminRollout{
			Version:             rollout.Version,
			State:               string(rollout.State),
			TargetSourceVersion: rollout.TargetSourceVersion,
		}
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

func (s *Server) updateAppAdminRegistryAutoDeploy(w http.ResponseWriter, r *http.Request) {
	app, _, ok := s.appAdminRegistryConfig(w, r)
	if !ok {
		return
	}
	if s.autoDeploySettings == nil {
		writeError(w, http.StatusServiceUnavailable, "app registry installation services are unavailable")
		return
	}
	if err := s.autoDeploySettings.EnsureStore(r.Context()); err != nil {
		slog.Error("app admin auto-deploy store unavailable", "app", app.name, "error", err)
		writeError(w, http.StatusServiceUnavailable, "app registry installation services are unavailable")
		return
	}
	var request appAdminRegistryAutoDeployRequest
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
	if request.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	settings, err := s.autoDeploySettings.Update(r.Context(), app.name, func(settings *core.AppAutoDeploySettings) error {
		settings.Enabled = *request.Enabled
		if settings.Enabled {
			settings.LastError = ""
			settings.LastSeenVersion = ""
		} else {
			settings.PendingVersion = ""
		}
		return nil
	})
	if err != nil {
		slog.Error("app admin auto-deploy update failed", "app", app.name, "error", err)
		writeError(w, http.StatusServiceUnavailable, "app registry installation services are unavailable")
		return
	}
	s.notifyAppAutoDeploy(app.name)
	writeJSON(w, http.StatusOK, appAdminRegistryAutoDeployResponse{
		App:        app.name,
		AutoDeploy: appAdminAutoDeployFromCore(settings),
	})
}

func appAdminAutoDeployFromCore(settings *core.AppAutoDeploySettings) appAdminAutoDeploy {
	if settings == nil {
		return appAdminAutoDeploy{}
	}
	return appAdminAutoDeploy{
		Enabled:        settings.Enabled,
		PendingVersion: settings.PendingVersion,
		LastError:      settings.LastError,
	}
}

func (s *Server) notifyAppAutoDeploy(app string) {
	if s == nil || s.appAutoDeployNotify == nil {
		return
	}
	app = strings.TrimSpace(app)
	if app == "" {
		return
	}
	s.appAutoDeployNotify(app)
}

func (s *Server) getAppAdminRegistryHistory(w http.ResponseWriter, r *http.Request) {
	app, registry, ok := s.appAdminRegistryConfig(w, r)
	if !ok {
		return
	}
	if s.appVersionChanges == nil {
		writeError(w, http.StatusServiceUnavailable, "app registry installation services are unavailable")
		return
	}
	limit := parseAppAdminHistoryLimit(r.URL.Query().Get("limit"))
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	page, err := s.appVersionChanges.ListRequestsByAppPage(r.Context(), app.name, limit, cursor)
	if err != nil {
		if strings.Contains(err.Error(), "invalid revision history cursor") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
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
	retentionIndex, err := reader.FetchRetentionIndex(r.Context(), publicRoot, app.name)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch app registry retention catalog")
		return
	}
	policy, err := retentionPolicyFromRegistry(registry)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "app registry is unavailable")
		return
	}

	known, err := s.appVersionChanges.ListKnownVersionsByApp(r.Context(), app.name)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "app registry installation services are unavailable")
		return
	}
	desiredVersion := coredata.LatestKnownVersion(known)
	currentRevisionID, err := s.appVersionChanges.LatestDesiredRevisionID(r.Context(), app.name, desiredVersion)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "app registry installation services are unavailable")
		return
	}
	publishedByVersion := publishedVersionSummaryMap(index, app.name)
	now := time.Now().UTC()

	var rollout *core.AppRollout
	if s.appRollouts != nil {
		currentRollout, rolloutErr := s.appRollouts.Get(r.Context(), app.name)
		if rolloutErr == nil {
			rollout = currentRollout
		} else if !errors.Is(rolloutErr, core.ErrNotFound) {
			writeError(w, http.StatusServiceUnavailable, "app registry installation services are unavailable")
			return
		}
	}

	changeRequestIDs := make([]string, 0, len(page.Requests))
	for _, request := range page.Requests {
		if request != nil && strings.TrimSpace(request.ID) != "" {
			changeRequestIDs = append(changeRequestIDs, request.ID)
		}
	}
	outcomesByID := map[string]*core.AppVersionRolloutOutcome{}
	if s.appRolloutOutcomes != nil && len(changeRequestIDs) > 0 {
		var outcomesErr error
		outcomesByID, outcomesErr = s.appRolloutOutcomes.GetMany(r.Context(), changeRequestIDs)
		if outcomesErr != nil {
			writeError(w, http.StatusServiceUnavailable, "app registry installation services are unavailable")
			return
		}
	}

	revisions := make([]appAdminRegistryRevision, 0, len(page.Requests))
	actorLabels := s.resolveRevisionActorLabels(r.Context(), page.Requests)
	for _, request := range page.Requests {
		if request == nil {
			continue
		}
		revision := appAdminRegistryRevisionFromRequest(
			request,
			desiredVersion,
			currentRevisionID,
			retentionIndex,
			policy,
			publishedByVersion,
			actorLabels[strings.TrimSpace(request.Actor)],
			now,
		)
		applyRevisionRolloutFields(&revision, request, rollout, outcomesByID[request.ID], now)
		revisions = append(revisions, revision)
	}
	writeJSON(w, http.StatusOK, appAdminRegistryHistoryResponse{
		App:        app.name,
		Revisions:  revisions,
		NextCursor: page.NextCursor,
	})
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
		Rollout: appAdminRollout{
			Version:             result.Rollout.Version,
			State:               string(result.Rollout.State),
			TargetSourceVersion: result.Rollout.TargetSourceVersion,
		},
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
		errors.Is(err, appregistry.ErrInstallValidationFailed),
		errors.Is(err, appregistry.ErrAppVersionExpired),
		errors.Is(err, appregistry.ErrAppVersionLocked):
		status = http.StatusBadRequest
	case errors.Is(err, appregistry.ErrRegistryDocumentNotFound):
		status = http.StatusNotFound
	case errors.Is(err, appregistry.ErrInstallVersionLocked),
		errors.Is(err, appregistry.ErrAppRolloutActive):
		status = http.StatusConflict
	case errors.Is(err, coredata.ErrGestaltdSourceVersionUnavailable):
		status = http.StatusServiceUnavailable
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

func appAdminPublishedVersionFromSummary(summary appregistry.VersionSummary, desiredVersion string, retention *appregistry.RetentionIndex, policy appregistry.RetentionPolicy, now time.Time) appAdminPublishedVersion {
	version := appAdminPublishedVersion{
		Version:     summary.Version,
		PublishedAt: formatAdminTime(summary.PublishedAt),
		Platforms:   append([]string(nil), summary.Platforms...),
		SourceRef:   summary.SourceRef,
		SourceURL:   appVersionSourceURL(summary.Repository, summary.SourceRef),
		Publication: summary.Publication,
	}
	state, deployableUntil := appregistry.VersionDeploymentState(summary.Version, desiredVersion, retention, policy, now)
	version.DeploymentState = state
	if deployableUntil != nil && !deployableUntil.IsZero() {
		version.DeployableUntil = formatAdminTime(*deployableUntil)
	}
	version.Current = summary.Version == desiredVersion && desiredVersion != ""
	if summary.PublishStartedAt != nil && !summary.PublishStartedAt.IsZero() {
		version.PublishStartedAt = formatAdminTime(*summary.PublishStartedAt)
		if seconds, ok := appregistry.DurationSecondsBetween(*summary.PublishStartedAt, summary.PublishedAt); ok {
			version.PublishDurationSeconds = &seconds
		}
	}
	return version
}

func retentionPolicyFromRegistry(registry config.AppRegistryConfig) (appregistry.RetentionPolicy, error) {
	unused, deployed, err := registry.RetentionPolicy()
	if err != nil {
		return appregistry.RetentionPolicy{}, err
	}
	return appregistry.RetentionPolicy{
		UnusedRetention:   unused,
		DeployedRetention: deployed,
	}, nil
}

func appAdminPendingVersionFromEntry(entry appregistry.PendingVersion, now time.Time) appAdminPendingVersion {
	version := appAdminPendingVersion{
		Version:     entry.Version,
		StartedAt:   formatAdminTime(entry.StartedAt),
		UpdatedAt:   formatAdminTime(entry.UpdatedAt),
		Phase:       entry.Phase,
		SourceRef:   entry.SourceRef,
		SourceURL:   appVersionSourceURL(entry.Repository, entry.SourceRef),
		Publication: entry.Publication,
	}
	if seconds, ok := appregistry.DurationSecondsBetween(entry.StartedAt, now); ok {
		version.PublishingForSeconds = &seconds
	}
	return version
}

func appAdminFailedVersionFromEntry(entry appregistry.FailedVersion) appAdminFailedVersion {
	version := appAdminFailedVersion{
		Version:     entry.Version,
		StartedAt:   formatAdminTime(entry.StartedAt),
		FailedAt:    formatAdminTime(entry.FailedAt),
		Reason:      entry.Reason,
		SourceRef:   entry.SourceRef,
		SourceURL:   appVersionSourceURL(entry.Repository, entry.SourceRef),
		Publication: entry.Publication,
	}
	if seconds, ok := appregistry.DurationSecondsBetween(entry.StartedAt, entry.FailedAt); ok {
		version.PublishDurationSeconds = &seconds
	}
	return version
}

func parseAppAdminHistoryLimit(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return coredata.DefaultAppVersionChangeRequestPageLimit
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return coredata.DefaultAppVersionChangeRequestPageLimit
	}
	return limit
}

func publishedVersionSummaryMap(index *appregistry.Index, appName string) map[string]appregistry.VersionSummary {
	summaries := appregistry.VersionsFromIndex(index, appName)
	out := make(map[string]appregistry.VersionSummary, len(summaries))
	for i := range summaries {
		out[summaries[i].Version] = summaries[i]
	}
	return out
}

func appAdminRegistryRevisionFromRequest(
	request *core.AppVersionChangeRequest,
	desiredVersion string,
	currentRevisionID string,
	retention *appregistry.RetentionIndex,
	policy appregistry.RetentionPolicy,
	publishedByVersion map[string]appregistry.VersionSummary,
	deployedBy string,
	now time.Time,
) appAdminRegistryRevision {
	version := strings.TrimSpace(request.ToVersion)
	revision := appAdminRegistryRevision{
		ID:         request.ID,
		Version:    version,
		DeployedAt: formatAdminTime(request.Timestamp),
		DeployedBy: deployedBy,
		Current:    request.ID == currentRevisionID,
	}
	fromVersion := strings.TrimSpace(request.FromVersion)
	if fromVersion != "" && fromVersion != appregistry.FirstInstallFromVersion {
		revision.PreviousVersion = fromVersion
	}

	installation := coredata.InstallationFromChangeRequest(request)
	sourceRef := ""
	repository := ""
	if installation != nil {
		sourceRef = strings.TrimSpace(installation.SourceRef)
	}
	if summary, ok := publishedByVersion[version]; ok {
		if sourceRef == "" {
			sourceRef = strings.TrimSpace(summary.SourceRef)
		}
		repository = strings.TrimSpace(summary.Repository)
		revision.Publication = summary.Publication
	}
	revision.SourceRef = sourceRef
	revision.SourceURL = appVersionSourceURL(repository, sourceRef)

	state, deployableUntil := appregistry.VersionDeploymentState(version, desiredVersion, retention, policy, now)
	revision.DeploymentState = historyDeploymentState(state)
	if deployableUntil != nil && !deployableUntil.IsZero() {
		revision.DeployableUntil = formatAdminTime(*deployableUntil)
	}
	return revision
}

func applyRevisionRolloutFields(
	revision *appAdminRegistryRevision,
	request *core.AppVersionChangeRequest,
	rollout *core.AppRollout,
	outcome *core.AppVersionRolloutOutcome,
	now time.Time,
) {
	if revision == nil || request == nil {
		return
	}
	version := strings.TrimSpace(request.ToVersion)
	if version == "" {
		return
	}

	var state core.AppRolloutState
	var completedAt, failedAt time.Time

	if rollout != nil && strings.TrimSpace(rollout.Version) == version {
		state = rollout.State
		completedAt = rollout.CompletedAt
		failedAt = rollout.FailedAt
	}
	if outcome != nil {
		if !outcome.CompletedAt.IsZero() {
			completedAt = outcome.CompletedAt
			state = core.AppRolloutStateComplete
		}
		if !outcome.FailedAt.IsZero() {
			failedAt = outcome.FailedAt
			state = core.AppRolloutStateFailed
		}
	}
	if state == "" {
		return
	}

	revision.RolloutState = string(state)
	switch state {
	case core.AppRolloutStateEnrolling, core.AppRolloutStateRestarting:
		if seconds := rolloutDurationSeconds(request.Timestamp, now); seconds != nil {
			revision.RolloutForSeconds = seconds
		}
	case core.AppRolloutStateComplete:
		if !completedAt.IsZero() {
			revision.RolloutCompletedAt = formatAdminTime(completedAt)
			revision.RolloutDurationSeconds = rolloutDurationSeconds(request.Timestamp, completedAt)
		}
	case core.AppRolloutStateFailed:
		if !failedAt.IsZero() {
			revision.RolloutFailedAt = formatAdminTime(failedAt)
			revision.RolloutDurationSeconds = rolloutDurationSeconds(request.Timestamp, failedAt)
		}
	}
}

func rolloutDurationSeconds(start, end time.Time) *int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return nil
	}
	seconds := int64(end.Sub(start).Round(time.Second) / time.Second)
	return &seconds
}

func historyDeploymentState(state string) string {
	switch state {
	case appregistry.DeploymentStateDesired:
		return "desired"
	case appregistry.DeploymentStateRedeployable:
		return "redeployable"
	case appregistry.DeploymentStateLocked:
		return "locked"
	default:
		return state
	}
}

func (s *Server) resolveRevisionActorLabels(ctx context.Context, requests []*core.AppVersionChangeRequest) map[string]string {
	labels := make(map[string]string)
	if s == nil {
		return labels
	}
	seen := make(map[string]struct{})
	for _, request := range requests {
		if request == nil {
			continue
		}
		actor := strings.TrimSpace(request.Actor)
		if actor == "" {
			continue
		}
		if _, ok := seen[actor]; ok {
			continue
		}
		seen[actor] = struct{}{}
		labels[actor] = s.resolveRevisionActorLabel(ctx, actor)
	}
	return labels
}

func (s *Server) resolveRevisionActorLabel(ctx context.Context, actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ""
	}
	if strings.HasPrefix(actor, "system:") {
		return actor
	}
	kind, id, ok := core.ParseSubjectID(actor)
	if !ok {
		return actor
	}
	switch kind {
	case string(principal.KindUser):
		if strings.Contains(id, "@") {
			return id
		}
		if s.users != nil {
			user, err := s.users.GetUser(ctx, id)
			if err == nil && user != nil {
				if email := strings.TrimSpace(user.Email); email != "" {
					return email
				}
			}
		}
		return id
	case coredata.ManagedSubjectKindServiceAccount:
		if s.managedSubjects != nil {
			subject, err := s.managedSubjects.GetManagedSubject(ctx, actor)
			if err == nil && subject != nil {
				if displayName := strings.TrimSpace(subject.DisplayName); displayName != "" {
					return displayName
				}
			}
		}
		return id
	default:
		return id
	}
}
