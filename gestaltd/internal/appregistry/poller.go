package appregistry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

const (
	DefaultCatalogPollInterval = time.Minute
	DefaultCatalogRestartDelay = time.Minute
)

type InstallationMaterializer interface {
	Ensure(context.Context, *core.AppInstallation) (*core.AppMaterializationResult, error)
}

type CatalogPoller struct {
	ChangeRequests      *coredata.AppVersionChangeRequestService
	Materializations    *coredata.AppInstanceMaterializationService
	Rollouts            *coredata.AppRolloutService
	AppMaterializer     InstallationMaterializer
	AppRestarter        AppRestarter
	InstanceID          string
	Interval            time.Duration
	RestartDelay        time.Duration
	DisableRestartDelay bool
	RestartReady        <-chan struct{}
	Now                 func() time.Time

	startOnce sync.Once
	startMu   sync.Mutex
	cancel    context.CancelFunc
	done      chan struct{}
	passMu    sync.Mutex
	mu        sync.Mutex
	inflight  map[string]struct{}
	stoppedAt map[string]time.Time
}

type CatalogPollerConfig struct {
	ChangeRequests      *coredata.AppVersionChangeRequestService
	Materializations    *coredata.AppInstanceMaterializationService
	Rollouts            *coredata.AppRolloutService
	AppMaterializer     InstallationMaterializer
	AppRestarter        AppRestarter
	InstanceID          string
	Interval            time.Duration
	RestartDelay        time.Duration
	DisableRestartDelay bool
	RestartReady        <-chan struct{}
	Now                 func() time.Time
}

func NewCatalogPoller(cfg CatalogPollerConfig) *CatalogPoller {
	return &CatalogPoller{
		ChangeRequests:      cfg.ChangeRequests,
		Materializations:    cfg.Materializations,
		Rollouts:            cfg.Rollouts,
		AppMaterializer:     cfg.AppMaterializer,
		AppRestarter:        cfg.AppRestarter,
		InstanceID:          strings.TrimSpace(cfg.InstanceID),
		Interval:            cfg.Interval,
		RestartDelay:        cfg.RestartDelay,
		DisableRestartDelay: cfg.DisableRestartDelay,
		RestartReady:        cfg.RestartReady,
		Now:                 cfg.Now,
		inflight:            make(map[string]struct{}),
		stoppedAt:           make(map[string]time.Time),
	}
}

func ResolveInstanceID() string {
	host, err := os.Hostname()
	if err == nil {
		host = strings.TrimSpace(host)
		if host != "" {
			return host
		}
	}
	return uuid.NewString()
}

func (p *CatalogPoller) Start(ctx context.Context) {
	if p == nil || p.ChangeRequests == nil || p.Materializations == nil || p.Rollouts == nil {
		return
	}
	p.startOnce.Do(func() {
		if strings.TrimSpace(p.InstanceID) == "" {
			p.InstanceID = ResolveInstanceID()
		}
		loopCtx, cancel := context.WithCancel(ctx)
		p.startMu.Lock()
		p.cancel = cancel
		p.done = make(chan struct{})
		p.startMu.Unlock()
		go func() {
			defer close(p.done)
			p.loop(loopCtx)
		}()
	})
}

func (p *CatalogPoller) Stop() {
	if p == nil {
		return
	}
	p.startMu.Lock()
	cancel := p.cancel
	done := p.done
	p.startMu.Unlock()
	if cancel == nil || done == nil {
		return
	}
	cancel()
	<-done
	if p.AppRestarter != nil {
		p.AppRestarter.AbortRestarts()
	}
}

func (p *CatalogPoller) loop(ctx context.Context) {
	p.runOnce(ctx)
	ticker := time.NewTicker(p.pollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.runOnce(ctx)
		}
	}
}

func (p *CatalogPoller) runOnce(ctx context.Context) {
	if !p.passMu.TryLock() {
		return
	}
	defer p.passMu.Unlock()

	if err := p.ReconcileOnce(ctx); err != nil {
		slog.Warn("app registry catalog poll finished with errors", "error", err)
	}
}

func (p *CatalogPoller) ReconcileOnce(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if p.ChangeRequests == nil || p.Materializations == nil || p.Rollouts == nil {
		return fmt.Errorf("app registry catalog poller is not configured")
	}
	instanceID := strings.TrimSpace(p.InstanceID)
	if instanceID == "" {
		return fmt.Errorf("app registry catalog poller: instance id is required")
	}

	known, err := p.ChangeRequests.ListAllKnownVersions(ctx)
	if err != nil {
		return fmt.Errorf("list catalog known versions: %w", err)
	}

	active, err := p.Rollouts.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("list active app rollouts: %w", err)
	}
	activeByApp := make(map[string]*core.AppRollout, len(active))
	for _, rollout := range active {
		if rollout != nil && strings.TrimSpace(rollout.App) != "" {
			activeByApp[strings.TrimSpace(rollout.App)] = rollout
		}
	}

	var errs []error
	byApp := groupInstallationsByApp(known)
	for appName, installations := range byApp {
		if err := p.reconcileApp(ctx, instanceID, appName, installations, activeByApp[appName]); err != nil {
			errs = append(errs, err)
		}
		delete(activeByApp, appName)
	}
	for appName, rollout := range activeByApp {
		if err := p.reconcileApp(ctx, instanceID, appName, nil, rollout); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func groupInstallationsByApp(known []*core.AppInstallation) map[string][]*core.AppInstallation {
	byApp := make(map[string][]*core.AppInstallation)
	for _, installation := range known {
		if installation == nil {
			continue
		}
		appName := strings.TrimSpace(installation.AppName)
		if appName == "" {
			continue
		}
		byApp[appName] = append(byApp[appName], installation)
	}
	for appName := range byApp {
		sort.Slice(byApp[appName], func(i, j int) bool {
			left := byApp[appName][i]
			right := byApp[appName][j]
			if left == nil || right == nil {
				return left != nil
			}
			if !left.UpdatedAt.Equal(right.UpdatedAt) {
				return left.UpdatedAt.Before(right.UpdatedAt)
			}
			return left.Version < right.Version
		})
	}
	return byApp
}

func (p *CatalogPoller) reconcileApp(ctx context.Context, instanceID, appName string, installations []*core.AppInstallation, rollout *core.AppRollout) error {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil
	}
	if !p.beginInflight(appName) {
		return nil
	}
	defer p.endInflight(appName)

	if rollout != nil {
		version := strings.TrimSpace(rollout.Version)
		if findInstallation(installations, version) == nil {
			if !p.now().Before(rollout.Deadline) {
				if _, err := p.Rollouts.MarkFailed(ctx, appName, version, p.now()); err != nil {
					return fmt.Errorf("fail rollout without accepted version %s@%s: %w", appName, version, err)
				}
			}
			return nil
		}
		if _, err := p.ensureAcknowledged(ctx, instanceID, appName, version); err != nil {
			return err
		}
		if rollout.State == core.AppRolloutStateEnrolling {
			if p.now().Before(rollout.EnrollmentEndsAt) {
				return nil
			}
			var err error
			rollout, err = p.Rollouts.MarkRestarting(ctx, appName, version)
			if err != nil {
				return fmt.Errorf("start rollout restart phase for %s@%s: %w", appName, version, err)
			}
		}
		if rollout.State == core.AppRolloutStateRestarting {
			terminal, err := p.updateRolloutOutcome(ctx, rollout)
			if err != nil {
				return err
			}
			if terminal {
				rollout = nil
			}
		}
	}

	if len(installations) == 0 {
		return nil
	}

	pending := make([]*core.AppInstallation, 0, len(installations))
	for _, installation := range installations {
		version := strings.TrimSpace(installation.Version)
		if version == "" {
			continue
		}
		materialization, err := p.ensureAcknowledged(ctx, instanceID, appName, version)
		if err != nil {
			return err
		}
		if materialization.RestartedAt.IsZero() {
			pending = append(pending, installation)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	if p.AppRestarter == nil {
		return nil
	}

	restartable, err := p.AppRestarter.Restartable(appName)
	if err != nil {
		return fmt.Errorf("determine restart mode for app %s: %w", appName, err)
	}
	if !restartable {
		convergedAt := p.now()
		if err := p.markCatalogConverged(ctx, instanceID, appName, pending, convergedAt); err != nil {
			return err
		}
		slog.Info(
			"app registry catalog poller marked non-local app converged",
			"app", appName,
			"pending_versions", len(pending),
			"instance_id", instanceID,
			"restarted_at", convergedAt,
		)
		return nil
	}

	if validator, ok := p.AppRestarter.(installationValidator); ok {
		for _, installation := range pending {
			if err := validator.ValidateInstallation(installation); err != nil {
				return fmt.Errorf("validate installation for app %s: %w", appName, err)
			}
		}
	}
	if err := p.ensurePendingMaterialized(ctx, instanceID, pending); err != nil {
		return fmt.Errorf("materialize pending versions for app %s: %w", appName, err)
	}

	if !p.restartReady() {
		return nil
	}

	driverVersion := strings.TrimSpace(pending[len(pending)-1].Version)
	runningVersion := ""
	if running, ok := p.AppRestarter.(runningVersionReporter); ok {
		runningVersion = running.RunningVersion(appName)
	}
	if runningVersion == driverVersion {
		restartedAt := p.now()
		if err := p.markAllRestarted(ctx, instanceID, appName, pending, restartedAt); err != nil {
			return err
		}
		if rollout != nil && rollout.State == core.AppRolloutStateRestarting {
			if _, err := p.updateRolloutOutcome(ctx, rollout); err != nil {
				return err
			}
		}
		return nil
	}

	stoppedAt, alreadyStopped, err := p.earliestStoppedAt(ctx, instanceID, appName, pending)
	if err != nil {
		return err
	}
	if alreadyStopped && runningVersion != "" {
		alreadyStopped = false
	}
	if !alreadyStopped {
		if err := p.AppRestarter.StopApp(ctx, appName); err != nil {
			return fmt.Errorf("stop app %s for %s@%s: %w", appName, appName, driverVersion, err)
		}
		stoppedAt = p.rememberStoppedAt(appName, p.now())
		slog.Info(
			"app registry catalog poller stopped app provider",
			"app", appName,
			"version", driverVersion,
			"pending_versions", len(pending),
			"instance_id", instanceID,
			"stopped_at", stoppedAt,
		)
	}
	if err := p.markPendingStopped(ctx, instanceID, appName, pending, stoppedAt); err != nil {
		return err
	}

	if delay := p.restartDelay(); delay > 0 {
		if p.now().Sub(stoppedAt) < delay {
			return nil
		}
	}

	if err := p.AppRestarter.StartApp(ctx, appName, driverVersion); err != nil {
		return fmt.Errorf("start app %s for %s@%s: %w", appName, appName, driverVersion, err)
	}
	restartedAt := p.now()
	if err := p.markAllRestarted(ctx, instanceID, appName, pending, restartedAt); err != nil {
		return err
	}
	p.forgetStoppedAt(appName)
	if rollout != nil && rollout.State == core.AppRolloutStateRestarting {
		if _, err := p.updateRolloutOutcome(ctx, rollout); err != nil {
			return err
		}
	}
	slog.Info(
		"app registry catalog poller restarted app provider",
		"app", appName,
		"version", driverVersion,
		"pending_versions", len(pending),
		"instance_id", instanceID,
		"restarted_at", restartedAt,
	)
	return nil
}

func findInstallation(installations []*core.AppInstallation, version string) *core.AppInstallation {
	version = strings.TrimSpace(version)
	for _, installation := range installations {
		if installation != nil && strings.TrimSpace(installation.Version) == version {
			return installation
		}
	}
	return nil
}

func (p *CatalogPoller) updateRolloutOutcome(ctx context.Context, rollout *core.AppRollout) (bool, error) {
	materializations, err := p.Materializations.ListByAppVersion(ctx, rollout.App, rollout.Version)
	if err != nil {
		return false, fmt.Errorf("list rollout cohort for %s@%s: %w", rollout.App, rollout.Version, err)
	}
	converged := true
	for _, materialization := range materializations {
		if !materialization.AcknowledgedAt.Before(rollout.EnrollmentEndsAt) {
			continue
		}
		if materialization.RestartedAt.IsZero() || materialization.RestartedAt.After(rollout.Deadline) {
			converged = false
			break
		}
	}
	if converged {
		if _, err := p.Rollouts.MarkComplete(ctx, rollout.App, rollout.Version, p.now()); err != nil {
			return false, fmt.Errorf("complete rollout %s@%s: %w", rollout.App, rollout.Version, err)
		}
		return true, nil
	}
	if !p.now().Before(rollout.Deadline) {
		if _, err := p.Rollouts.MarkFailed(ctx, rollout.App, rollout.Version, p.now()); err != nil {
			return false, fmt.Errorf("fail rollout %s@%s: %w", rollout.App, rollout.Version, err)
		}
		return true, nil
	}
	return false, nil
}

func (p *CatalogPoller) restartReady() bool {
	if p == nil || p.RestartReady == nil {
		return true
	}
	select {
	case <-p.RestartReady:
		return true
	default:
		return false
	}
}

func (p *CatalogPoller) ensurePendingMaterialized(ctx context.Context, instanceID string, pending []*core.AppInstallation) error {
	if p == nil || len(pending) == 0 {
		return nil
	}
	if p.AppMaterializer == nil {
		for _, installation := range pending {
			if installation != nil && strings.TrimSpace(installation.Registry) != "" {
				return fmt.Errorf("app registry materializer is required for %s@%s", installation.AppName, installation.Version)
			}
		}
		return nil
	}
	for _, installation := range pending {
		if installation == nil {
			continue
		}
		appName := strings.TrimSpace(installation.AppName)
		version := strings.TrimSpace(installation.Version)
		if appName == "" || version == "" {
			continue
		}
		if strings.TrimSpace(installation.Registry) == "" {
			continue
		}
		materialization, err := p.Materializations.Get(ctx, instanceID, appName, version)
		if err != nil {
			return fmt.Errorf("load materialization for %s@%s: %w", appName, version, err)
		}
		result, err := p.AppMaterializer.Ensure(ctx, installation)
		if err != nil {
			return fmt.Errorf("materialize %s@%s: %w", appName, version, err)
		}
		if !result.Changed && !materialization.MaterializedAt.IsZero() {
			continue
		}
		materializedAt := p.now()
		if _, err := p.Materializations.MarkMaterialized(ctx, instanceID, appName, version, materializedAt); err != nil {
			return fmt.Errorf("record materialization for %s@%s: %w", appName, version, err)
		}
		slog.Info(
			"app registry catalog poller materialized app artifact",
			"app", appName,
			"version", version,
			"instance_id", instanceID,
			"materialized_path", result.Path,
			"materialized_at", materializedAt,
		)
	}
	return nil
}

func (p *CatalogPoller) markCatalogConverged(ctx context.Context, instanceID, appName string, pending []*core.AppInstallation, convergedAt time.Time) error {
	return p.markAllRestarted(ctx, instanceID, appName, pending, convergedAt)
}

func (p *CatalogPoller) earliestStoppedAt(ctx context.Context, instanceID, appName string, pending []*core.AppInstallation) (time.Time, bool, error) {
	earliest, found := p.localStoppedAt(appName)
	for _, installation := range pending {
		version := strings.TrimSpace(installation.Version)
		if version == "" {
			continue
		}
		materialization, err := p.Materializations.Get(ctx, instanceID, appName, version)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("load materialization for %s@%s: %w", appName, version, err)
		}
		if materialization.StoppedAt.IsZero() {
			continue
		}
		if !found || materialization.StoppedAt.Before(earliest) {
			earliest = materialization.StoppedAt
			found = true
		}
	}
	return earliest, found, nil
}

func (p *CatalogPoller) localStoppedAt(app string) (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	value, ok := p.stoppedAt[app]
	return value, ok
}

func (p *CatalogPoller) rememberStoppedAt(app string, value time.Time) time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.stoppedAt[app]; ok {
		return existing
	}
	p.stoppedAt[app] = value
	return value
}

func (p *CatalogPoller) forgetStoppedAt(app string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.stoppedAt, app)
}

func (p *CatalogPoller) markPendingStopped(ctx context.Context, instanceID, appName string, pending []*core.AppInstallation, stoppedAt time.Time) error {
	for _, installation := range pending {
		version := strings.TrimSpace(installation.Version)
		if version == "" {
			continue
		}
		materialization, err := p.Materializations.Get(ctx, instanceID, appName, version)
		if err != nil {
			return fmt.Errorf("load materialization for %s@%s: %w", appName, version, err)
		}
		if !materialization.StoppedAt.IsZero() {
			continue
		}
		if _, err := p.Materializations.MarkStopped(ctx, instanceID, appName, version, stoppedAt); err != nil {
			return fmt.Errorf("record stop for %s@%s: %w", appName, version, err)
		}
	}
	return nil
}

func (p *CatalogPoller) markAllRestarted(ctx context.Context, instanceID, appName string, pending []*core.AppInstallation, restartedAt time.Time) error {
	for _, installation := range pending {
		version := strings.TrimSpace(installation.Version)
		materialization, err := p.Materializations.Get(ctx, instanceID, appName, version)
		if err != nil {
			return fmt.Errorf("load materialization for %s@%s: %w", appName, version, err)
		}
		if !materialization.RestartedAt.IsZero() {
			continue
		}
		if _, err := p.Materializations.MarkRestarted(ctx, instanceID, appName, version, restartedAt); err != nil {
			return fmt.Errorf("record restart for %s@%s: %w", appName, version, err)
		}
	}
	return nil
}

func (p *CatalogPoller) ensureAcknowledged(ctx context.Context, instanceID, appName, version string) (*core.AppInstanceMaterialization, error) {
	acknowledgedAt := p.now()
	materialization, err := p.Materializations.Acknowledge(ctx, &core.AppInstanceMaterialization{
		InstanceID:     instanceID,
		App:            appName,
		Version:        version,
		AcknowledgedAt: acknowledgedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("acknowledge %s@%s: %w", appName, version, err)
	}
	return materialization, nil
}

func (p *CatalogPoller) beginInflight(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inflight == nil {
		p.inflight = make(map[string]struct{})
	}
	if _, ok := p.inflight[key]; ok {
		return false
	}
	p.inflight[key] = struct{}{}
	return true
}

func (p *CatalogPoller) endInflight(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.inflight, key)
}

func (p *CatalogPoller) pollInterval() time.Duration {
	if p != nil && p.Interval > 0 {
		return p.Interval
	}
	return DefaultCatalogPollInterval
}

func (p *CatalogPoller) restartDelay() time.Duration {
	if p == nil || p.DisableRestartDelay {
		return 0
	}
	if p.RestartDelay > 0 {
		return p.RestartDelay
	}
	return DefaultCatalogRestartDelay
}

func (p *CatalogPoller) now() time.Time {
	if p != nil && p.Now != nil {
		return p.Now().UTC().Truncate(time.Millisecond)
	}
	return time.Now().UTC().Truncate(time.Millisecond)
}
