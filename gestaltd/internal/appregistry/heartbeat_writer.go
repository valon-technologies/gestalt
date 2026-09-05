package appregistry

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

type HeartbeatService interface {
	Upsert(context.Context, *core.GestaltdInstanceHeartbeat) (*core.GestaltdInstanceHeartbeat, error)
	PruneBefore(context.Context, time.Time) (int, error)
}

type HeartbeatChangeRequests interface {
	ListAllKnownVersions(context.Context) ([]*core.AppInstallation, error)
}

type RuntimeSnapshotter interface {
	SnapshotRegistryApps() map[string]core.RegistryAppRuntimeObservation
}

type HeartbeatWriter struct {
	Heartbeats     HeartbeatService
	ChangeRequests HeartbeatChangeRequests
	ConfiguredApps map[string]*config.ProviderEntry
	Runtime        RuntimeSnapshotter
	InstanceID     string
	SourceVersion  string
	Revision       string
	StartupError   func() error
	Ready          <-chan struct{}
	Interval       time.Duration
	Retention      time.Duration
	Now            func() time.Time
	NewTicker      func(time.Duration) (<-chan time.Time, func())

	startOnce sync.Once
	stateMu   sync.Mutex
	cancel    context.CancelFunc
	done      chan struct{}
	startedAt time.Time
	nextPrune time.Time
	passMu    sync.Mutex
}

type HeartbeatWriterConfig struct {
	Heartbeats     HeartbeatService
	ChangeRequests HeartbeatChangeRequests
	ConfiguredApps map[string]*config.ProviderEntry
	Runtime        RuntimeSnapshotter
	InstanceID     string
	SourceVersion  string
	Revision       string
	StartupError   func() error
	Ready          <-chan struct{}
	Interval       time.Duration
	Retention      time.Duration
	Now            func() time.Time
	NewTicker      func(time.Duration) (<-chan time.Time, func())
}

func NewHeartbeatWriter(cfg HeartbeatWriterConfig) *HeartbeatWriter {
	return &HeartbeatWriter{
		Heartbeats:     cfg.Heartbeats,
		ChangeRequests: cfg.ChangeRequests,
		ConfiguredApps: cfg.ConfiguredApps,
		Runtime:        cfg.Runtime,
		InstanceID:     strings.TrimSpace(cfg.InstanceID),
		SourceVersion:  strings.TrimSpace(cfg.SourceVersion),
		Revision:       strings.TrimSpace(cfg.Revision),
		StartupError:   cfg.StartupError,
		Ready:          cfg.Ready,
		Interval:       cfg.Interval,
		Retention:      cfg.Retention,
		Now:            cfg.Now,
		NewTicker:      cfg.NewTicker,
	}
}

func (w *HeartbeatWriter) Start(ctx context.Context) {
	if w == nil || w.Heartbeats == nil || w.ChangeRequests == nil || w.Runtime == nil {
		return
	}
	w.startOnce.Do(func() {
		if w.InstanceID == "" {
			w.InstanceID = ResolveInstanceID()
		}
		loopCtx, cancel := context.WithCancel(ctx)
		w.stateMu.Lock()
		w.cancel = cancel
		w.done = make(chan struct{})
		// started_at is the heartbeat subsystem start, not an operating-system
		// process birth time. No reliable process-start source exists here.
		w.startedAt = w.now()
		w.stateMu.Unlock()
		go func() {
			defer close(w.done)
			w.loop(loopCtx)
		}()
	})
}

func (w *HeartbeatWriter) Stop() {
	if w == nil {
		return
	}
	w.stateMu.Lock()
	cancel := w.cancel
	done := w.done
	w.stateMu.Unlock()
	if cancel == nil || done == nil {
		return
	}
	cancel()
	<-done
}

func (w *HeartbeatWriter) loop(ctx context.Context) {
	if w.Ready != nil {
		select {
		case <-ctx.Done():
			return
		case <-w.Ready:
		}
	}
	w.writeAndLog(ctx)
	ticks, stop := w.newTicker(w.interval())
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			w.writeAndLog(ctx)
		}
	}
}

func (w *HeartbeatWriter) writeAndLog(ctx context.Context) {
	if err := w.WriteOnce(ctx); err != nil && ctx.Err() == nil {
		slog.Warn("runtime heartbeat write failed", "instance_id", w.InstanceID, "error", err)
	}
}

func (w *HeartbeatWriter) WriteOnce(ctx context.Context) error {
	if w == nil || w.Heartbeats == nil || w.ChangeRequests == nil || w.Runtime == nil {
		return fmt.Errorf("runtime heartbeat writer is not configured")
	}
	if w.InstanceID == "" || w.SourceVersion == "" {
		return fmt.Errorf("runtime heartbeat writer requires instance id and source version")
	}
	w.passMu.Lock()
	defer w.passMu.Unlock()

	now := w.now()
	w.stateMu.Lock()
	if w.startedAt.IsZero() {
		w.startedAt = now
	}
	startedAt := w.startedAt
	w.stateMu.Unlock()

	known, err := w.ChangeRequests.ListAllKnownVersions(ctx)
	if err != nil {
		return fmt.Errorf("list desired app versions: %w", err)
	}
	knownByApp := make(map[string][]*core.AppInstallation)
	for _, installation := range known {
		if installation != nil {
			app := strings.TrimSpace(installation.AppName)
			knownByApp[app] = append(knownByApp[app], installation)
		}
	}
	desiredByApp := make(map[string]string, len(knownByApp))
	for app, installations := range knownByApp {
		desiredByApp[app] = coredata.LatestKnownVersion(installations)
	}

	runtimeApps := w.Runtime.SnapshotRegistryApps()
	apps := make(map[string]core.GestaltdInstanceAppHeartbeat)
	for app, entry := range w.ConfiguredApps {
		if entry == nil || !entry.Source.IsRegistry() {
			continue
		}
		observation, ok := runtimeApps[app]
		if !ok {
			observation = core.RegistryAppRuntimeObservation{
				State:     core.GestaltdInstanceAppStateUnknown,
				LastError: "configured registry app was absent from runtime snapshot",
			}
		}
		apps[app] = core.GestaltdInstanceAppHeartbeat{
			State:          observation.State,
			DesiredVersion: desiredByApp[app],
			RunningVersion: observation.RunningVersion,
			ObservedAt:     now,
			LastError:      observation.LastError,
		}
	}
	ready := true
	lastError := ""
	if w.StartupError != nil {
		if err := w.StartupError(); err != nil {
			ready = false
			lastError = err.Error()
		}
	}
	for app, observation := range apps {
		if ready && !appHeartbeatReady(observation) {
			ready = false
			lastError = fmt.Sprintf("registry app %q is %s at %q; desired %q", app, observation.State, observation.RunningVersion, observation.DesiredVersion)
		}
	}
	if _, err := w.Heartbeats.Upsert(ctx, &core.GestaltdInstanceHeartbeat{
		InstanceID:    w.InstanceID,
		SourceVersion: w.SourceVersion,
		Revision:      w.Revision,
		Ready:         ready,
		LastError:     lastError,
		StartedAt:     startedAt,
		HeartbeatAt:   now,
		Apps:          apps,
	}); err != nil {
		return fmt.Errorf("upsert runtime heartbeat: %w", err)
	}

	if w.Retention > 0 && w.pruneDue(now) {
		if _, err := w.Heartbeats.PruneBefore(ctx, now.Add(-w.Retention)); err != nil {
			return fmt.Errorf("prune runtime heartbeats: %w", err)
		}
	}
	return nil
}

// HeartbeatReady reports whether a fleet heartbeat passed every local app
// provider and registry app readiness check.
func HeartbeatReady(heartbeat *core.GestaltdInstanceHeartbeat) bool {
	return heartbeat != nil && heartbeat.Ready
}

func appHeartbeatReady(app core.GestaltdInstanceAppHeartbeat) bool {
	desired := strings.TrimSpace(app.DesiredVersion)
	if desired == "" {
		return app.State == core.GestaltdInstanceAppStateNotRunning
	}
	return app.State == core.GestaltdInstanceAppStateRunning && strings.TrimSpace(app.RunningVersion) == desired
}

func (w *HeartbeatWriter) pruneDue(now time.Time) bool {
	w.stateMu.Lock()
	defer w.stateMu.Unlock()
	if !w.nextPrune.IsZero() && now.Before(w.nextPrune) {
		return false
	}
	cadence := w.Retention / 4
	if cadence <= 0 || cadence > time.Hour {
		cadence = time.Hour
	}
	if cadence < w.interval() {
		cadence = w.interval()
	}
	w.nextPrune = now.Add(cadence)
	return true
}

func (w *HeartbeatWriter) interval() time.Duration {
	if w.Interval > 0 {
		return w.Interval
	}
	return config.DefaultAppRegistryHeartbeatInterval
}

func (w *HeartbeatWriter) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

func (w *HeartbeatWriter) newTicker(interval time.Duration) (<-chan time.Time, func()) {
	if w.NewTicker != nil {
		return w.NewTicker(interval)
	}
	ticker := time.NewTicker(interval)
	return ticker.C, ticker.Stop
}
