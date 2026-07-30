package server

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
)

type adminRegistryAppSummary struct {
	App            string                  `json:"app"`
	Registry       string                  `json:"registry"`
	DesiredVersion string                  `json:"desiredVersion,omitempty"`
	Rollout        *adminAppRolloutInfo    `json:"rollout,omitempty"`
	Cohort         *adminRolloutCohortInfo `json:"cohort,omitempty"`
	FleetState     adminAppFleetStateInfo  `json:"fleetState"`
}

type adminRegistryAppDetail struct {
	adminRegistryAppSummary
	KnownVersions   []adminAppInstallationInfo `json:"knownVersions"`
	LatestPublished *adminPublishedVersionInfo `json:"latestPublished,omitempty"`
	FreshReplicas   []adminFleetReplicaInfo    `json:"freshReplicas"`
	StaleReplicas   []adminFleetReplicaInfo    `json:"staleReplicas"`
}

type adminPublishedVersionInfo struct {
	Version     string `json:"version"`
	PublishedAt string `json:"publishedAt"`
}

type adminAppRolloutInfo struct {
	App                 string `json:"app,omitempty"`
	Version             string `json:"version"`
	State               string `json:"state"`
	TargetSourceVersion string `json:"targetSourceVersion,omitempty"`
	CreatedAt           string `json:"createdAt"`
	EnrollmentEndsAt    string `json:"enrollmentEndsAt"`
	Deadline            string `json:"deadline"`
	CompletedAt         string `json:"completedAt,omitempty"`
	FailedAt            string `json:"failedAt,omitempty"`
}

type adminRolloutCohortInfo struct {
	Acknowledged int `json:"acknowledged"`
	Materialized int `json:"materialized"`
	Restarted    int `json:"restarted"`
	Failed       int `json:"failed"`
}

type adminAppFleetStateInfo struct {
	State                   string `json:"state"`
	SourceVersion           string `json:"sourceVersion,omitempty"`
	DesiredVersion          string `json:"desiredVersion,omitempty"`
	MinimumHealthyInstances int    `json:"minimumHealthyInstances"`
	LiveInstances           int    `json:"liveInstances"`
	RunningDesiredVersion   int    `json:"runningDesiredVersion"`
	Mismatched              int    `json:"mismatched"`
	Errors                  int    `json:"errors"`
	HeartbeatTTLSeconds     int64  `json:"heartbeatTtlSeconds"`
	EvaluatedAt             string `json:"evaluatedAt"`
	heartbeatTTL            time.Duration
	evaluatedAt             time.Time
}

type adminFleetReplicaInfo struct {
	InstanceID          string                       `json:"instanceId"`
	SourceVersion       string                       `json:"sourceVersion"`
	CurrentSource       bool                         `json:"currentSource"`
	Fresh               bool                         `json:"fresh"`
	StartedAt           string                       `json:"startedAt,omitempty"`
	HeartbeatAt         string                       `json:"heartbeatAt"`
	HeartbeatAgeSeconds int64                        `json:"heartbeatAgeSeconds"`
	AppObservation      adminFleetAppObservationInfo `json:"appObservation"`
}

type adminFleetAppObservationInfo struct {
	State          string `json:"state"`
	DesiredVersion string `json:"desiredVersion,omitempty"`
	RunningVersion string `json:"runningVersion,omitempty"`
	ObservedAt     string `json:"observedAt,omitempty"`
	LastError      string `json:"lastError,omitempty"`
}

type adminAppRolloutMaterializationsResponse struct {
	App              string                        `json:"app"`
	Version          string                        `json:"version"`
	RolloutState     string                        `json:"rolloutState,omitempty"`
	Materializations []adminAppMaterializationInfo `json:"materializations"`
}

type adminAppMaterializationInfo struct {
	InstanceID       string  `json:"instanceId"`
	SourceVersion    string  `json:"sourceVersion,omitempty"`
	AcknowledgedAt   *string `json:"acknowledgedAt"`
	MaterializedAt   *string `json:"materializedAt"`
	StoppedAt        *string `json:"stoppedAt"`
	RestartedAt      *string `json:"restartedAt"`
	AttemptCount     int     `json:"attemptCount"`
	LastErrorAt      *string `json:"lastErrorAt"`
	LastErrorMessage string  `json:"lastErrorMessage"`
	InCohort         bool    `json:"inCohort"`
	Converged        bool    `json:"converged"`
}

func (s *Server) mountAdminAppRolloutRoutes(r chi.Router) {
	r.Get("/registry-apps", s.listAdminRegistryApps)
	r.Get("/registry-apps/{app}", s.getAdminRegistryApp)
	r.Get("/app-rollouts", s.listAdminAppRollouts)
	r.Get("/app-rollouts/{app}/materializations", s.listAdminAppRolloutMaterializations)
}

func (s *Server) listAdminRegistryApps(w http.ResponseWriter, r *http.Request) {
	if !s.appObservabilityAvailable() {
		writeError(w, http.StatusServiceUnavailable, "app registry observability services are unavailable")
		return
	}
	apps := s.configuredRegistryApps()
	out := make([]adminRegistryAppSummary, 0, len(apps))
	for _, app := range apps {
		summary, err := s.loadAdminRegistryAppSummary(r, app, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load registry app state")
			return
		}
		out = append(out, summary)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getAdminRegistryApp(w http.ResponseWriter, r *http.Request) {
	if !s.appObservabilityAvailable() {
		writeError(w, http.StatusServiceUnavailable, "app registry observability services are unavailable")
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
	app, ok := s.registryApp(appName)
	if !ok {
		writeError(w, http.StatusNotFound, "registry app not found")
		return
	}
	known, err := s.appVersionChanges.ListKnownVersionsByApp(r.Context(), appName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load known app versions")
		return
	}
	summary, err := s.loadAdminRegistryAppSummary(r, app, known)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load registry app state")
		return
	}
	knownVersions := make([]adminAppInstallationInfo, 0, len(known))
	for _, installation := range known {
		knownVersions = append(knownVersions, adminAppInstallationFromCore(installation))
	}
	freshReplicas, staleReplicas, err := s.loadAdminFleetReplicas(r, appName, summary.FleetState)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load registry app fleet observations")
		return
	}
	writeJSON(w, http.StatusOK, adminRegistryAppDetail{
		adminRegistryAppSummary: summary,
		KnownVersions:           knownVersions,
		LatestPublished:         s.latestPublishedVersion(r, app),
		FreshReplicas:           freshReplicas,
		StaleReplicas:           staleReplicas,
	})
}

func (s *Server) listAdminAppRollouts(w http.ResponseWriter, r *http.Request) {
	if s.appRollouts == nil {
		writeError(w, http.StatusServiceUnavailable, "app rollout service is unavailable")
		return
	}
	appFilter := strings.TrimSpace(r.URL.Query().Get("app"))
	if appFilter != "" {
		if err := providerregistry.ValidateRepositoryName(appFilter); err != nil {
			writeError(w, http.StatusBadRequest, "invalid app name")
			return
		}
	}
	stateFilter := strings.TrimSpace(r.URL.Query().Get("state"))
	if stateFilter != "" && !validAppRolloutState(stateFilter) {
		writeError(w, http.StatusBadRequest, "invalid rollout state")
		return
	}
	rollouts, err := s.appRollouts.ListActiveAndRecentTerminal(r.Context(), s.now().Add(-24*time.Hour))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list app rollouts")
		return
	}
	slices.SortFunc(rollouts, func(a, b *core.AppRollout) int {
		if byApp := strings.Compare(a.App, b.App); byApp != 0 {
			return byApp
		}
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	out := make([]adminAppRolloutInfo, 0, len(rollouts))
	for _, rollout := range rollouts {
		if appFilter != "" && rollout.App != appFilter {
			continue
		}
		if stateFilter != "" && string(rollout.State) != stateFilter {
			continue
		}
		out = append(out, adminAppRolloutFromCore(rollout, true))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listAdminAppRolloutMaterializations(w http.ResponseWriter, r *http.Request) {
	if !s.appObservabilityAvailable() {
		writeError(w, http.StatusServiceUnavailable, "app registry observability services are unavailable")
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
	rollout, err := s.appRollouts.Get(r.Context(), appName)
	if err != nil && !errors.Is(err, core.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "failed to load app rollout")
		return
	}
	if errors.Is(err, core.ErrNotFound) {
		rollout = nil
	}
	version := strings.TrimSpace(r.URL.Query().Get("version"))
	if version == "" && rollout != nil {
		version = rollout.Version
	}
	if version == "" {
		known, listErr := s.appVersionChanges.ListKnownVersionsByApp(r.Context(), appName)
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to load known app versions")
			return
		}
		version = coredata.LatestKnownVersion(known)
	}
	if version == "" {
		writeError(w, http.StatusNotFound, "app rollout not found")
		return
	}
	rows, err := s.appMaterializations.ListByAppVersion(r.Context(), appName, version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list app materializations")
		return
	}
	slices.SortFunc(rows, func(a, b *core.AppInstanceMaterialization) int {
		if bySourceVersion := strings.Compare(a.SourceVersion, b.SourceVersion); bySourceVersion != 0 {
			return bySourceVersion
		}
		return strings.Compare(a.InstanceID, b.InstanceID)
	})
	out := make([]adminAppMaterializationInfo, 0, len(rows))
	for _, row := range rows {
		out = append(out, adminAppMaterializationFromCore(row, rollout))
	}
	response := adminAppRolloutMaterializationsResponse{
		App:              appName,
		Version:          version,
		Materializations: out,
	}
	if rollout != nil && rollout.Version == version {
		response.RolloutState = string(rollout.State)
	}
	writeJSON(w, http.StatusOK, response)
}

type configuredRegistryApp struct {
	name     string
	registry string
}

func (s *Server) configuredRegistryApps() []configuredRegistryApp {
	out := make([]configuredRegistryApp, 0)
	for name, entry := range s.pluginDefs {
		if entry == nil {
			continue
		}
		registry := strings.TrimSpace(entry.Source.Registry)
		if registry == "" {
			continue
		}
		out = append(out, configuredRegistryApp{name: name, registry: registry})
	}
	slices.SortFunc(out, func(a, b configuredRegistryApp) int {
		return strings.Compare(a.name, b.name)
	})
	return out
}

func (s *Server) registryApp(name string) (configuredRegistryApp, bool) {
	for _, app := range s.configuredRegistryApps() {
		if app.name == name {
			return app, true
		}
	}
	return configuredRegistryApp{}, false
}

func (s *Server) loadAdminRegistryAppSummary(r *http.Request, app configuredRegistryApp, known []*core.AppInstallation) (adminRegistryAppSummary, error) {
	var err error
	if known == nil {
		known, err = s.appVersionChanges.ListKnownVersionsByApp(r.Context(), app.name)
		if err != nil {
			return adminRegistryAppSummary{}, err
		}
	}
	summary := adminRegistryAppSummary{
		App:            app.name,
		Registry:       app.registry,
		DesiredVersion: coredata.LatestKnownVersion(known),
	}
	projection, err := s.appFleetProjector.Project(r.Context(), app.name)
	if err != nil {
		return adminRegistryAppSummary{}, err
	}
	summary.FleetState = adminAppFleetStateFromCore(projection)
	rollout, err := s.appRollouts.Get(r.Context(), app.name)
	if errors.Is(err, core.ErrNotFound) {
		return summary, nil
	}
	if err != nil {
		return adminRegistryAppSummary{}, err
	}
	rows, err := s.appMaterializations.ListByAppVersion(r.Context(), app.name, rollout.Version)
	if err != nil {
		return adminRegistryAppSummary{}, err
	}
	summary.Rollout = ptr(adminAppRolloutFromCore(rollout, false))
	summary.Cohort = ptr(adminRolloutCohortFromRows(rows, rollout))
	return summary, nil
}

func (s *Server) loadAdminFleetReplicas(
	r *http.Request,
	app string,
	fleetState adminAppFleetStateInfo,
) ([]adminFleetReplicaInfo, []adminFleetReplicaInfo, error) {
	rows, err := s.instanceHeartbeats.List(r.Context())
	if err != nil {
		return nil, nil, err
	}
	evaluatedAt := fleetState.evaluatedAt
	cutoff := evaluatedAt.Add(-fleetState.heartbeatTTL)
	fresh := make([]adminFleetReplicaInfo, 0, len(rows))
	stale := make([]adminFleetReplicaInfo, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		item := adminFleetReplicaFromCore(row, app, fleetState.SourceVersion, cutoff, evaluatedAt)
		if item.Fresh {
			fresh = append(fresh, item)
		} else {
			stale = append(stale, item)
		}
	}
	sortAdminFleetReplicas(fresh)
	sortAdminFleetReplicas(stale)
	return fresh, stale, nil
}

func adminAppFleetStateFromCore(projection *core.AppFleetProjection) adminAppFleetStateInfo {
	if projection == nil {
		return adminAppFleetStateInfo{State: string(core.AppFleetStateUnknown)}
	}
	return adminAppFleetStateInfo{
		State:                   string(projection.State),
		SourceVersion:           projection.SourceVersion,
		DesiredVersion:          projection.DesiredVersion,
		MinimumHealthyInstances: projection.MinimumHealthyInstances,
		LiveInstances:           projection.LiveInstances,
		RunningDesiredVersion:   projection.RunningDesiredVersion,
		Mismatched:              projection.Mismatched,
		Errors:                  projection.Errors,
		HeartbeatTTLSeconds:     int64(projection.HeartbeatTTL / time.Second),
		EvaluatedAt:             formatAdminTime(projection.EvaluatedAt),
		heartbeatTTL:            projection.HeartbeatTTL,
		evaluatedAt:             projection.EvaluatedAt,
	}
}

func adminFleetReplicaFromCore(
	heartbeat *core.GestaltdInstanceHeartbeat,
	app string,
	currentSource string,
	cutoff time.Time,
	evaluatedAt time.Time,
) adminFleetReplicaInfo {
	age := evaluatedAt.Sub(heartbeat.HeartbeatAt.UTC())
	if age < 0 {
		age = 0
	}
	out := adminFleetReplicaInfo{
		InstanceID:          heartbeat.InstanceID,
		SourceVersion:       heartbeat.SourceVersion,
		CurrentSource:       currentSource != "" && strings.TrimSpace(heartbeat.SourceVersion) == strings.TrimSpace(currentSource),
		Fresh:               !heartbeat.HeartbeatAt.Before(cutoff),
		StartedAt:           formatAdminTime(heartbeat.StartedAt),
		HeartbeatAt:         formatAdminTime(heartbeat.HeartbeatAt),
		HeartbeatAgeSeconds: int64(age / time.Second),
		AppObservation:      adminFleetAppObservationInfo{State: string(core.GestaltdInstanceAppStateUnknown)},
	}
	if observation, ok := heartbeat.Apps[app]; ok {
		out.AppObservation = adminFleetAppObservationInfo{
			State:          string(observation.State),
			DesiredVersion: observation.DesiredVersion,
			RunningVersion: observation.RunningVersion,
			ObservedAt:     formatAdminTime(observation.ObservedAt),
			LastError:      observation.LastError,
		}
	}
	return out
}

func sortAdminFleetReplicas(rows []adminFleetReplicaInfo) {
	slices.SortFunc(rows, func(a, b adminFleetReplicaInfo) int {
		if a.CurrentSource != b.CurrentSource {
			if a.CurrentSource {
				return -1
			}
			return 1
		}
		if byHeartbeat := strings.Compare(b.HeartbeatAt, a.HeartbeatAt); byHeartbeat != 0 {
			return byHeartbeat
		}
		return strings.Compare(a.InstanceID, b.InstanceID)
	})
}

func (s *Server) latestPublishedVersion(r *http.Request, app configuredRegistryApp) *adminPublishedVersionInfo {
	registry, ok := s.appRegistries[app.registry]
	if !ok || strings.TrimSpace(registry.Kind) != config.AppRegistryKindGCS {
		return nil
	}
	publicRoot, err := registry.PublicURL()
	if err != nil {
		return nil
	}
	reader := s.appRegistryReader
	if reader == nil {
		reader = &appregistry.RegistryReader{}
	}
	index, err := reader.FetchAppIndex(r.Context(), publicRoot, app.name)
	if err != nil {
		return nil
	}
	versions := appregistry.VersionsFromIndex(index, app.name)
	if len(versions) == 0 {
		return nil
	}
	return &adminPublishedVersionInfo{
		Version:     versions[0].Version,
		PublishedAt: formatAdminTime(versions[0].PublishedAt),
	}
}

func (s *Server) appObservabilityAvailable() bool {
	return s.appVersionChanges != nil &&
		s.appRollouts != nil &&
		s.appMaterializations != nil &&
		s.appFleetProjector != nil &&
		s.instanceHeartbeats != nil
}

func adminAppRolloutFromCore(rollout *core.AppRollout, includeApp bool) adminAppRolloutInfo {
	if rollout == nil {
		return adminAppRolloutInfo{}
	}
	out := adminAppRolloutInfo{
		Version:             rollout.Version,
		State:               string(rollout.State),
		TargetSourceVersion: rollout.TargetSourceVersion,
		CreatedAt:           formatAdminTime(rollout.CreatedAt),
		EnrollmentEndsAt:    formatAdminTime(rollout.EnrollmentEndsAt),
		Deadline:            formatAdminTime(rollout.Deadline),
		CompletedAt:         formatAdminTime(rollout.CompletedAt),
		FailedAt:            formatAdminTime(rollout.FailedAt),
	}
	if includeApp {
		out.App = rollout.App
	}
	return out
}

func adminRolloutCohortFromRows(rows []*core.AppInstanceMaterialization, rollout *core.AppRollout) adminRolloutCohortInfo {
	var out adminRolloutCohortInfo
	if rollout == nil {
		return out
	}
	for _, row := range rows {
		if !materializationInRolloutCohort(row, rollout) {
			continue
		}
		out.Acknowledged++
		if materializationTimestampIsCurrent(row.MaterializedAt, rollout) {
			out.Materialized++
		}
		if materializationTimestampIsCurrent(row.RestartedAt, rollout) {
			out.Restarted++
		}
		if !row.LastErrorAt.IsZero() && (row.RestartedAt.IsZero() || row.LastErrorAt.After(row.RestartedAt)) {
			out.Failed++
		}
	}
	return out
}

func adminAppMaterializationFromCore(row *core.AppInstanceMaterialization, rollout *core.AppRollout) adminAppMaterializationInfo {
	out := adminAppMaterializationInfo{
		InstanceID:       row.InstanceID,
		SourceVersion:    row.SourceVersion,
		AcknowledgedAt:   formatAdminTimePtr(row.AcknowledgedAt),
		MaterializedAt:   formatAdminTimePtr(row.MaterializedAt),
		StoppedAt:        formatAdminTimePtr(row.StoppedAt),
		RestartedAt:      formatAdminTimePtr(row.RestartedAt),
		AttemptCount:     row.AttemptCount,
		LastErrorAt:      formatAdminTimePtr(row.LastErrorAt),
		LastErrorMessage: row.LastErrorMessage,
		Converged:        !row.RestartedAt.IsZero(),
	}
	if rollout == nil || rollout.Version != row.Version {
		return out
	}
	out.InCohort = materializationInRolloutCohort(row, rollout)
	out.Converged = materializationTimestampIsCurrent(row.RestartedAt, rollout)
	if isActiveAdminRollout(rollout.State) {
		out.Converged = out.Converged && !row.RestartedAt.After(rollout.Deadline)
	}
	return out
}

func materializationInRolloutCohort(row *core.AppInstanceMaterialization, rollout *core.AppRollout) bool {
	return row != nil && rollout != nil &&
		(strings.TrimSpace(rollout.TargetSourceVersion) == "" ||
			strings.TrimSpace(row.SourceVersion) == strings.TrimSpace(rollout.TargetSourceVersion)) &&
		materializationTimestampIsCurrent(row.AcknowledgedAt, rollout) &&
		row.AcknowledgedAt.Before(rollout.EnrollmentEndsAt)
}

func materializationTimestampIsCurrent(value time.Time, rollout *core.AppRollout) bool {
	return rollout != nil && !value.IsZero() && !value.Before(rollout.CreatedAt)
}

func validAppRolloutState(state string) bool {
	switch core.AppRolloutState(state) {
	case core.AppRolloutStateEnrolling, core.AppRolloutStateRestarting, core.AppRolloutStateComplete, core.AppRolloutStateFailed:
		return true
	default:
		return false
	}
}

func isActiveAdminRollout(state core.AppRolloutState) bool {
	return state == core.AppRolloutStateEnrolling || state == core.AppRolloutStateRestarting
}

func formatAdminTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func formatAdminTimePtr(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	return ptr(formatAdminTime(value))
}

func ptr[T any](value T) *T {
	return &value
}
