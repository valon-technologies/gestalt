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
	instanceIDEnvVar           = "GESTALT_INSTANCE_ID"
	sourceVersionEnvVar        = "SOURCE_VERSION"
)

var (
	resolvedInstanceID     string
	resolvedInstanceIDOnce sync.Once
)

type CatalogPoller struct {
	ChangeRequests              *coredata.AppVersionChangeRequestService
	Materializations            *coredata.AppInstanceMaterializationService
	Rollouts                    *coredata.AppRolloutService
	RolloutOutcomes             *coredata.AppVersionRolloutOutcomeService
	Heartbeats                  *coredata.GestaltdInstanceHeartbeatService
	AppMaterializer             *Materializer
	AppRestarter                AppRestarter
	InstanceID                  string
	SourceVersion               string
	Interval                    time.Duration
	RestartDelay                time.Duration
	DisableRestartDelay         bool
	RestartReady                <-chan struct{}
	BootstrapReady              <-chan struct{}
	MaxReconcileAttempts        int
	Now                         func() time.Time
	OnRolloutTerminal           func(string)
	HeartbeatTTL                time.Duration
	HealthyStabilityWindow      time.Duration
	HeartbeatEvaluationInterval time.Duration

	startOnce sync.Once
	startMu   sync.Mutex
	cancel    context.CancelFunc
	done      chan struct{}
	passMu    sync.Mutex
	mu        sync.Mutex
	inflight  map[string]struct{}
	stoppedAt map[string]time.Time
	notify    chan struct{}
}

type CatalogPollerConfig struct {
	ChangeRequests              *coredata.AppVersionChangeRequestService
	Materializations            *coredata.AppInstanceMaterializationService
	Rollouts                    *coredata.AppRolloutService
	RolloutOutcomes             *coredata.AppVersionRolloutOutcomeService
	Heartbeats                  *coredata.GestaltdInstanceHeartbeatService
	AppMaterializer             *Materializer
	AppRestarter                AppRestarter
	InstanceID                  string
	SourceVersion               string
	Interval                    time.Duration
	RestartDelay                time.Duration
	DisableRestartDelay         bool
	RestartReady                <-chan struct{}
	BootstrapReady              <-chan struct{}
	MaxReconcileAttempts        int
	Now                         func() time.Time
	OnRolloutTerminal           func(string)
	HeartbeatTTL                time.Duration
	HealthyStabilityWindow      time.Duration
	HeartbeatEvaluationInterval time.Duration
}

func NewCatalogPoller(cfg CatalogPollerConfig) *CatalogPoller {
	return &CatalogPoller{
		ChangeRequests:              cfg.ChangeRequests,
		Materializations:            cfg.Materializations,
		Rollouts:                    cfg.Rollouts,
		RolloutOutcomes:             cfg.RolloutOutcomes,
		Heartbeats:                  cfg.Heartbeats,
		AppMaterializer:             cfg.AppMaterializer,
		AppRestarter:                cfg.AppRestarter,
		InstanceID:                  strings.TrimSpace(cfg.InstanceID),
		SourceVersion:               strings.TrimSpace(cfg.SourceVersion),
		Interval:                    cfg.Interval,
		RestartDelay:                cfg.RestartDelay,
		DisableRestartDelay:         cfg.DisableRestartDelay,
		RestartReady:                cfg.RestartReady,
		BootstrapReady:              cfg.BootstrapReady,
		MaxReconcileAttempts:        cfg.MaxReconcileAttempts,
		Now:                         cfg.Now,
		OnRolloutTerminal:           cfg.OnRolloutTerminal,
		HeartbeatTTL:                cfg.HeartbeatTTL,
		HealthyStabilityWindow:      cfg.HealthyStabilityWindow,
		HeartbeatEvaluationInterval: cfg.HeartbeatEvaluationInterval,
		inflight:                    make(map[string]struct{}),
		stoppedAt:                   make(map[string]time.Time),
		notify:                      make(chan struct{}, 1),
	}
}

func ResolveInstanceID() string {
	resolvedInstanceIDOnce.Do(func() {
		host, _ := os.Hostname()
		resolvedInstanceID = resolveInstanceID(os.Getenv(instanceIDEnvVar), host)
	})
	return resolvedInstanceID
}

func ResolveSourceVersion() string {
	return strings.TrimSpace(os.Getenv(sourceVersionEnvVar))
}

// ResolveRevision returns the immutable Cloud Run revision name when present.
func ResolveRevision() string {
	return strings.TrimSpace(os.Getenv("K_REVISION"))
}

func resolveInstanceID(instanceIDEnv, hostname string) string {
	if v := strings.TrimSpace(instanceIDEnv); v != "" {
		return v
	}
	host := strings.TrimSpace(hostname)
	// Cloud Run (and some container runtimes) report localhost for every
	// replica. A shared instance_id makes rollout convergence look fleet-wide
	// while only one process actually restarts the registry app.
	if host != "" && !strings.EqualFold(host, "localhost") {
		return host
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

// Notify requests an immediate local reconciliation pass. Notifications
// coalesce so callers never block while a pass is already running.
func (p *CatalogPoller) Notify(_ string) {
	if p == nil {
		return
	}
	select {
	case p.notify <- struct{}{}:
	default:
	}
}

func (p *CatalogPoller) loop(ctx context.Context) {
	if p.BootstrapReady != nil {
		select {
		case <-ctx.Done():
			return
		case <-p.BootstrapReady:
		}
	}
	p.runOnce(ctx)
	ticker := time.NewTicker(p.pollInterval())
	defer ticker.Stop()
	var heartbeatTicker *time.Ticker
	var heartbeatTick <-chan time.Time
	if interval := p.heartbeatEvaluationInterval(); interval > 0 {
		heartbeatTicker = time.NewTicker(interval)
		heartbeatTick = heartbeatTicker.C
		defer heartbeatTicker.Stop()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.runOnce(ctx)
		case <-p.notify:
			p.runOnce(ctx)
		case <-heartbeatTick:
			p.runHeartbeatEvaluations(ctx)
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

func (p *CatalogPoller) runHeartbeatEvaluations(ctx context.Context) {
	if !p.passMu.TryLock() {
		return
	}
	defer p.passMu.Unlock()

	if err := p.EvaluateHeartbeatRolloutsOnce(ctx); err != nil {
		slog.Warn("app registry heartbeat rollout evaluation finished with errors", "error", err)
	}
}

func (p *CatalogPoller) EvaluateHeartbeatRolloutsOnce(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if p.Rollouts == nil {
		return fmt.Errorf("app registry heartbeat rollout evaluator is not configured")
	}
	if !channelReady(p.BootstrapReady) {
		return nil
	}
	active, err := p.Rollouts.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("list active app rollouts for heartbeat evaluation: %w", err)
	}
	var errs []error
	for _, rollout := range active {
		if rollout == nil || rollout.Mode != core.AppRolloutModeHeartbeat {
			continue
		}
		if _, err := p.updateHeartbeatRolloutOutcome(ctx, rollout); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (p *CatalogPoller) ReconcileOnce(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if p.ChangeRequests == nil || p.Materializations == nil || p.Rollouts == nil {
		return fmt.Errorf("app registry catalog poller is not configured")
	}
	if !channelReady(p.BootstrapReady) {
		return nil
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
			errs = append(errs, p.recordFailure(ctx, instanceID, appName, installations, err))
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

	restartBlocked := false
	if rollout != nil {
		version := strings.TrimSpace(rollout.Version)
		if findInstallation(installations, version) == nil {
			if rollout.Mode == core.AppRolloutModeHeartbeat {
				_, err := p.updateHeartbeatRolloutOutcome(ctx, rollout)
				return err
			}
			if !p.now().Before(rollout.Deadline) {
				updated, err := p.Rollouts.MarkFailedForRollout(ctx, rollout, p.now())
				if errors.Is(err, coredata.ErrAppRolloutEpochMismatch) {
					return nil
				} else if err != nil {
					return fmt.Errorf("fail rollout without accepted version %s@%s: %w", appName, version, err)
				}
				p.recordRolloutOutcome(ctx, updated)
				p.notifyRolloutTerminal(appName)
			}
			return nil
		}
		if _, err := p.ensureAcknowledged(ctx, instanceID, appName, version, rollout.CreatedAt); err != nil {
			return err
		}
		if rollout.State == core.AppRolloutStateEnrolling {
			if p.now().Before(rollout.EnrollmentEndsAt) {
				restartBlocked = true
			} else {
				var err error
				rollout, err = p.Rollouts.MarkRestartingForRollout(ctx, rollout)
				if errors.Is(err, coredata.ErrAppRolloutEpochMismatch) {
					return nil
				}
				if err != nil {
					return fmt.Errorf("start rollout restart phase for %s@%s: %w", appName, version, err)
				}
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
		selectionEpoch := installation.UpdatedAt
		if rollout != nil && strings.TrimSpace(rollout.Version) == version {
			selectionEpoch = rollout.CreatedAt
		}
		materialization, err := p.ensureAcknowledged(ctx, instanceID, appName, version, selectionEpoch)
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

	driverVersion := coredata.LatestKnownVersion(installations)
	if driverVersion == "" {
		return fmt.Errorf("select desired version for app %s", appName)
	}
	desired := findInstallation(installations, driverVersion)
	if desired == nil {
		return fmt.Errorf("find desired installation for app %s@%s", appName, driverVersion)
	}
	if acceptor, ok := p.AppRestarter.(interface {
		AcceptsRegistry(string, string) bool
	}); ok {
		if !acceptor.AcceptsRegistry(appName, desired.Registry) {
			return fmt.Errorf("registry for %s@%s does not match configured source", appName, driverVersion)
		}
	}
	retryLimitReached := false
	if materialization, err := p.Materializations.Get(ctx, instanceID, appName, driverVersion); err == nil {
		retryLimitReached = materialization.AttemptCount >= p.maxReconcileAttempts()
	}

	if p.AppRestarter == nil {
		return nil
	}

	restartable, err := p.AppRestarter.Restartable(appName)
	if err != nil {
		return fmt.Errorf("determine restart mode for app %s: %w", appName, err)
	}
	if !restartable {
		if restartBlocked {
			return nil
		}
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

	if err := p.ensureDesiredMaterialized(ctx, instanceID, desired); err != nil {
		return fmt.Errorf("materialize desired version for app %s: %w", appName, err)
	}

	if restartBlocked {
		return nil
	}

	if retryLimitReached {
		if inspector, ok := p.AppRestarter.(interface {
			RunningVersion(string) (string, bool)
		}); ok {
			running, found := inspector.RunningVersion(appName)
			materialized, err := p.desiredVersionMaterialized(ctx, instanceID, desired)
			if err != nil {
				return err
			}
			pruned, err := p.supersededPruned(appName, driverVersion)
			if err != nil {
				return err
			}
			if found && running == driverVersion && materialized && pruned {
				return p.markAllRestarted(ctx, instanceID, appName, pending, p.now())
			}
		}
		return nil
	}

	if inspector, ok := p.AppRestarter.(interface {
		RunningVersion(string) (string, bool)
	}); ok {
		if running, found := inspector.RunningVersion(appName); found && running == driverVersion {
			if err := p.pruneSuperseded(appName, driverVersion); err != nil {
				return err
			}
			return p.markAllRestarted(ctx, instanceID, appName, pending, p.now())
		}
	}

	if !p.restartReady() {
		return nil
	}

	stoppedAt, alreadyStopped, err := p.earliestStoppedAt(ctx, instanceID, appName, pending)
	if err != nil {
		return err
	}
	if inspector, ok := p.AppRestarter.(interface {
		RunningVersion(string) (string, bool)
	}); ok {
		if running, found := inspector.RunningVersion(appName); found && running != driverVersion {
			alreadyStopped = false
		}
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
	if err := p.pruneSuperseded(appName, driverVersion); err != nil {
		return err
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

func (p *CatalogPoller) desiredVersionMaterialized(ctx context.Context, instanceID string, desired *core.AppInstallation) (bool, error) {
	if desired == nil || strings.TrimSpace(desired.Registry) == "" {
		return true, nil
	}
	appName := strings.TrimSpace(desired.AppName)
	version := strings.TrimSpace(desired.Version)
	materialization, err := p.Materializations.Get(ctx, instanceID, appName, version)
	if err != nil {
		return false, fmt.Errorf("load materialization for %s@%s: %w", appName, version, err)
	}
	if materialization.MaterializedAt.IsZero() || p.AppMaterializer == nil {
		return false, nil
	}
	path := MaterializedPath(p.AppMaterializer.ArtifactsDir, appName, version)
	return installedPackageReady(path, appName, version), nil
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
	if rollout != nil && rollout.Mode == core.AppRolloutModeHeartbeat {
		return p.updateHeartbeatRolloutOutcome(ctx, rollout)
	}
	materializations, err := p.Materializations.ListByAppVersion(ctx, rollout.App, rollout.Version)
	if err != nil {
		return false, fmt.Errorf("list rollout cohort for %s@%s: %w", rollout.App, rollout.Version, err)
	}
	converged := true
	cohortCount := 0
	for _, materialization := range materializations {
		if materialization.AcknowledgedAt.Before(rollout.CreatedAt) ||
			!materialization.AcknowledgedAt.Before(rollout.EnrollmentEndsAt) {
			continue
		}
		if target := strings.TrimSpace(rollout.TargetSourceVersion); target != "" &&
			strings.TrimSpace(materialization.SourceVersion) != target {
			continue
		}
		cohortCount++
		if materialization.RestartedAt.Before(rollout.CreatedAt) ||
			materialization.RestartedAt.After(rollout.Deadline) {
			converged = false
			break
		}
	}
	if cohortCount == 0 {
		converged = false
	}
	if converged {
		updated, err := p.Rollouts.MarkCompleteForRollout(ctx, rollout, p.now())
		if errors.Is(err, coredata.ErrAppRolloutEpochMismatch) {
			return false, nil
		} else if err != nil {
			return false, fmt.Errorf("complete rollout %s@%s: %w", rollout.App, rollout.Version, err)
		}
		p.recordRolloutOutcome(ctx, updated)
		p.notifyRolloutTerminal(rollout.App)
		return true, nil
	}
	if !p.now().Before(rollout.Deadline) {
		updated, err := p.Rollouts.MarkFailedForRollout(ctx, rollout, p.now())
		if errors.Is(err, coredata.ErrAppRolloutEpochMismatch) {
			return false, nil
		} else if err != nil {
			return false, fmt.Errorf("fail rollout %s@%s: %w", rollout.App, rollout.Version, err)
		}
		p.recordRolloutOutcome(ctx, updated)
		p.notifyRolloutTerminal(rollout.App)
		return true, nil
	}
	return false, nil
}

func (p *CatalogPoller) updateHeartbeatRolloutOutcome(ctx context.Context, rollout *core.AppRollout) (bool, error) {
	if p.Heartbeats == nil {
		return false, fmt.Errorf("evaluate heartbeat rollout %s@%s: heartbeat service is not configured", rollout.App, rollout.Version)
	}
	if p.HeartbeatTTL <= 0 || p.HealthyStabilityWindow <= 0 {
		return false, fmt.Errorf("evaluate heartbeat rollout %s@%s: heartbeat timing is not configured", rollout.App, rollout.Version)
	}
	now := p.now()
	heartbeats, err := p.Heartbeats.ListFreshBySourceVersion(
		ctx,
		rollout.TargetSourceVersion,
		now.Add(-p.HeartbeatTTL),
	)
	if err != nil {
		return false, fmt.Errorf("list heartbeat rollout fleet for %s@%s: %w", rollout.App, rollout.Version, err)
	}
	projection := EvaluateFleetState(FleetEvaluation{
		App:                     rollout.App,
		DesiredVersion:          rollout.Version,
		SourceVersion:           rollout.TargetSourceVersion,
		MinimumHealthyInstances: rollout.MinimumHealthyInstances,
		Cutoff:                  now.Add(-p.HeartbeatTTL),
		EvaluatedAt:             now,
		Heartbeats:              heartbeats,
	})
	updated, transitioned, err := p.Rollouts.EvaluateHeartbeatRollout(ctx, rollout, coredata.HeartbeatRolloutEvaluation{
		Healthy:         projection.State == core.AppFleetStateHealthy,
		StabilityWindow: p.HealthyStabilityWindow,
		EvaluatedAt:     now,
		FailureSummary: core.AppRolloutFailureSummary{
			LiveInstances:         projection.LiveInstances,
			RunningDesiredVersion: projection.RunningDesiredVersion,
			Mismatched:            projection.Mismatched,
			Errors:                projection.Errors,
		},
	})
	if errors.Is(err, coredata.ErrAppRolloutEpochMismatch) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("evaluate heartbeat rollout %s@%s: %w", rollout.App, rollout.Version, err)
	}
	if !transitioned {
		return false, nil
	}
	p.recordRolloutOutcome(ctx, updated)
	p.notifyRolloutTerminal(updated.App)
	return true, nil
}

func (p *CatalogPoller) notifyRolloutTerminal(app string) {
	if p != nil && p.OnRolloutTerminal != nil {
		p.OnRolloutTerminal(strings.TrimSpace(app))
	}
}

func (p *CatalogPoller) recordRolloutOutcome(ctx context.Context, rollout *core.AppRollout) {
	if p == nil || p.RolloutOutcomes == nil || p.ChangeRequests == nil || rollout == nil {
		return
	}
	var completedAt, failedAt time.Time
	switch rollout.State {
	case core.AppRolloutStateComplete:
		completedAt = rollout.CompletedAt
	case core.AppRolloutStateFailed:
		failedAt = rollout.FailedAt
	default:
		return
	}
	if completedAt.IsZero() && failedAt.IsZero() {
		return
	}
	changeRequestID, err := p.ChangeRequests.LatestRevisionIDForVersion(ctx, rollout.App, rollout.Version)
	if err != nil || changeRequestID == "" {
		slog.Warn("record rollout outcome: change request not found",
			"app", rollout.App,
			"version", rollout.Version,
			"error", err,
		)
		return
	}
	switch {
	case !completedAt.IsZero():
		if err := p.RolloutOutcomes.RecordComplete(ctx, changeRequestID, rollout.App, rollout.Version, completedAt); err != nil {
			slog.Warn("record rollout outcome: complete",
				"app", rollout.App,
				"version", rollout.Version,
				"change_request_id", changeRequestID,
				"error", err,
			)
		}
	case !failedAt.IsZero():
		if err := p.RolloutOutcomes.RecordFailed(ctx, changeRequestID, rollout.App, rollout.Version, failedAt); err != nil {
			slog.Warn("record rollout outcome: failed",
				"app", rollout.App,
				"version", rollout.Version,
				"change_request_id", changeRequestID,
				"error", err,
			)
		}
	}
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

func channelReady(ready <-chan struct{}) bool {
	if ready == nil {
		return true
	}
	select {
	case <-ready:
		return true
	default:
		return false
	}
}

func (p *CatalogPoller) ensureDesiredMaterialized(ctx context.Context, instanceID string, desired *core.AppInstallation) error {
	if p == nil || desired == nil {
		return nil
	}
	appName := strings.TrimSpace(desired.AppName)
	version := strings.TrimSpace(desired.Version)
	if appName == "" || version == "" || strings.TrimSpace(desired.Registry) == "" {
		return nil
	}
	if p.AppMaterializer == nil {
		return fmt.Errorf("app registry materializer is required for %s@%s", appName, version)
	}
	materialization, err := p.Materializations.Get(ctx, instanceID, appName, version)
	if err != nil {
		return fmt.Errorf("load materialization for %s@%s: %w", appName, version, err)
	}
	result, err := p.AppMaterializer.Ensure(ctx, desired)
	if err != nil {
		return fmt.Errorf("materialize %s@%s: %w", appName, version, err)
	}
	if !result.Changed && !materialization.MaterializedAt.IsZero() {
		return nil
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
	return nil
}

func (p *CatalogPoller) pruneSuperseded(appName, desiredVersion string) error {
	if p == nil || p.AppMaterializer == nil {
		return nil
	}
	if err := p.AppMaterializer.PruneSuperseded(appName, desiredVersion); err != nil {
		return fmt.Errorf("prune superseded versions for app %s: %w", appName, err)
	}
	return nil
}

func (p *CatalogPoller) supersededPruned(appName, desiredVersion string) (bool, error) {
	if p == nil || p.AppMaterializer == nil {
		return true, nil
	}
	pruned, err := p.AppMaterializer.SupersededPruned(appName, desiredVersion)
	if err != nil {
		return false, fmt.Errorf("inspect superseded versions for app %s: %w", appName, err)
	}
	return pruned, nil
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

func (p *CatalogPoller) ensureAcknowledged(ctx context.Context, instanceID, appName, version string, rolloutCreatedAt time.Time) (*core.AppInstanceMaterialization, error) {
	acknowledgedAt := p.now()
	input := &core.AppInstanceMaterialization{
		InstanceID:     instanceID,
		SourceVersion:  strings.TrimSpace(p.SourceVersion),
		App:            appName,
		Version:        version,
		AcknowledgedAt: acknowledgedAt,
	}
	var materialization *core.AppInstanceMaterialization
	var err error
	if rolloutCreatedAt.IsZero() {
		materialization, err = p.Materializations.Acknowledge(ctx, input)
	} else {
		materialization, err = p.Materializations.AcknowledgeForRollout(ctx, input, rolloutCreatedAt)
	}
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

func (p *CatalogPoller) heartbeatEvaluationInterval() time.Duration {
	if p != nil && p.HeartbeatEvaluationInterval > 0 {
		return p.HeartbeatEvaluationInterval
	}
	return 0
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

func (p *CatalogPoller) maxReconcileAttempts() int {
	if p != nil && p.MaxReconcileAttempts > 0 {
		return p.MaxReconcileAttempts
	}
	return 3
}

func (p *CatalogPoller) recordFailure(ctx context.Context, instanceID, appName string, installations []*core.AppInstallation, reconcileErr error) error {
	version := coredata.LatestKnownVersion(installations)
	if version == "" || p.Materializations == nil {
		return reconcileErr
	}
	if _, err := p.Materializations.RecordFailure(ctx, instanceID, appName, version, p.now(), reconcileErr.Error()); err != nil {
		return errors.Join(reconcileErr, fmt.Errorf("record reconciliation failure for %s@%s: %w", appName, version, err))
	}
	return reconcileErr
}
