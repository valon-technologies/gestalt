package appregistry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

type recordingHeartbeatService struct {
	mu        sync.Mutex
	attempts  int
	failures  int
	writes    []*core.GestaltdInstanceHeartbeat
	pruneCuts []time.Time
}

func (s *recordingHeartbeatService) Upsert(_ context.Context, heartbeat *core.GestaltdInstanceHeartbeat) (*core.GestaltdInstanceHeartbeat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	if s.failures > 0 {
		s.failures--
		return nil, errors.New("write failed")
	}
	copy := *heartbeat
	copy.Apps = make(map[string]core.GestaltdInstanceAppHeartbeat, len(heartbeat.Apps))
	for app, observation := range heartbeat.Apps {
		copy.Apps[app] = observation
	}
	s.writes = append(s.writes, &copy)
	return &copy, nil
}

func (s *recordingHeartbeatService) PruneBefore(_ context.Context, cutoff time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneCuts = append(s.pruneCuts, cutoff)
	return 0, nil
}

func (s *recordingHeartbeatService) counts() (int, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts, len(s.writes), len(s.pruneCuts)
}

type staticHeartbeatChanges struct {
	known []*core.AppInstallation
}

func (s staticHeartbeatChanges) ListAllKnownVersions(context.Context) ([]*core.AppInstallation, error) {
	return s.known, nil
}

type staticRuntimeSnapshot map[string]core.RegistryAppRuntimeObservation

func (s staticRuntimeSnapshot) SnapshotRegistryApps() map[string]core.RegistryAppRuntimeObservation {
	return s
}

type testHeartbeatClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testHeartbeatClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testHeartbeatClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func TestHeartbeatWriterWaitsForReadyThenWritesImmediatelyAndPeriodically(t *testing.T) {
	t.Parallel()
	ready := make(chan struct{})
	ticks := make(chan time.Time, 2)
	service := &recordingHeartbeatService{}
	clock := &testHeartbeatClock{now: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)}
	writer := NewHeartbeatWriter(HeartbeatWriterConfig{
		Heartbeats:     service,
		ChangeRequests: staticHeartbeatChanges{},
		ConfiguredApps: map[string]*config.ProviderEntry{"app": {Source: config.ProviderSource{Registry: "toolshed"}}},
		Runtime:        staticRuntimeSnapshot{"app": {State: core.GestaltdInstanceAppStateNotRunning}},
		InstanceID:     "instance",
		SourceVersion:  "source",
		Ready:          ready,
		Interval:       15 * time.Second,
		Retention:      24 * time.Hour,
		Now:            clock.Now,
		NewTicker: func(interval time.Duration) (<-chan time.Time, func()) {
			if interval != 15*time.Second {
				t.Fatalf("ticker interval = %v", interval)
			}
			return ticks, func() {}
		},
	})
	writer.Start(context.Background())
	t.Cleanup(writer.Stop)

	time.Sleep(20 * time.Millisecond)
	if attempts, _, _ := service.counts(); attempts != 0 {
		t.Fatalf("attempts before ready = %d, want 0", attempts)
	}
	close(ready)
	waitForHeartbeatCounts(t, service, 1, 1)

	clock.Advance(15 * time.Second)
	ticks <- clock.Now()
	waitForHeartbeatCounts(t, service, 2, 2)
	writer.Stop()
}

func TestHeartbeatWriterRetriesAfterFailedWriteAndPrunesCoarsely(t *testing.T) {
	t.Parallel()
	ready := make(chan struct{})
	close(ready)
	ticks := make(chan time.Time, 3)
	service := &recordingHeartbeatService{failures: 1}
	clock := &testHeartbeatClock{now: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)}
	writer := NewHeartbeatWriter(HeartbeatWriterConfig{
		Heartbeats:     service,
		ChangeRequests: staticHeartbeatChanges{},
		ConfiguredApps: map[string]*config.ProviderEntry{},
		Runtime:        staticRuntimeSnapshot{},
		InstanceID:     "instance",
		SourceVersion:  "source",
		Ready:          ready,
		Interval:       15 * time.Second,
		Retention:      24 * time.Hour,
		Now:            clock.Now,
		NewTicker: func(time.Duration) (<-chan time.Time, func()) {
			return ticks, func() {}
		},
	})
	writer.Start(context.Background())
	t.Cleanup(writer.Stop)
	waitForHeartbeatCounts(t, service, 1, 0)

	clock.Advance(15 * time.Second)
	ticks <- clock.Now()
	waitForHeartbeatCounts(t, service, 2, 1)
	_, _, prunes := service.counts()
	if prunes != 1 {
		t.Fatalf("prunes after successful retry = %d, want 1", prunes)
	}

	clock.Advance(15 * time.Second)
	ticks <- clock.Now()
	waitForHeartbeatCounts(t, service, 3, 2)
	_, _, prunes = service.counts()
	if prunes != 1 {
		t.Fatalf("prunes on next heartbeat = %d, want coarse cadence", prunes)
	}
}

func TestHeartbeatWriterWritesRegistryAppsWithOneDesiredVersionLoad(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service := &recordingHeartbeatService{}
	writer := NewHeartbeatWriter(HeartbeatWriterConfig{
		Heartbeats: service,
		ChangeRequests: staticHeartbeatChanges{known: []*core.AppInstallation{
			{AppName: "app", Version: "v1", UpdatedAt: now.Add(-time.Hour)},
			{AppName: "app", Version: "v2", UpdatedAt: now},
		}},
		ConfiguredApps: map[string]*config.ProviderEntry{
			"app":    {Source: config.ProviderSource{Registry: "toolshed"}},
			"legacy": {},
		},
		Runtime: staticRuntimeSnapshot{
			"app": {State: core.GestaltdInstanceAppStateRunning, RunningVersion: "v2"},
		},
		InstanceID:    "instance",
		SourceVersion: "source",
		Now:           func() time.Time { return now },
	})
	if err := writer.WriteOnce(context.Background()); err != nil {
		t.Fatalf("WriteOnce: %v", err)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	got := service.writes[0]
	if len(got.Apps) != 1 {
		t.Fatalf("apps = %#v, want registry app only", got.Apps)
	}
	if got.Apps["app"].DesiredVersion != "v2" || got.Apps["app"].RunningVersion != "v2" {
		t.Fatalf("app observation = %#v", got.Apps["app"])
	}
}

func waitForHeartbeatCounts(t *testing.T, service *recordingHeartbeatService, attempts, writes int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		gotAttempts, gotWrites, _ := service.counts()
		if gotAttempts >= attempts && gotWrites >= writes {
			return
		}
		time.Sleep(time.Millisecond)
	}
	gotAttempts, gotWrites, _ := service.counts()
	t.Fatalf("heartbeat counts = attempts %d, writes %d; want at least %d, %d", gotAttempts, gotWrites, attempts, writes)
}
