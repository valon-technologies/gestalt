package coredata_test

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestGestaltdInstanceHeartbeatServiceRoundTripAndReplace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).GestaltdInstanceHeartbeats
	startedAt := time.Date(2026, 7, 30, 13, 48, 41, 123456789, time.UTC)
	firstHeartbeatAt := startedAt.Add(15 * time.Second)
	heartbeat := &core.GestaltdInstanceHeartbeat{
		InstanceID:    " instance-1 ",
		SourceVersion: " source-a ",
		StartedAt:     startedAt,
		HeartbeatAt:   firstHeartbeatAt,
		Apps: map[string]core.GestaltdInstanceAppHeartbeat{
			"g-issues": {
				State:          core.GestaltdInstanceAppStateRunning,
				DesiredVersion: "v2",
				RunningVersion: "v2",
				ObservedAt:     firstHeartbeatAt,
			},
		},
	}

	stored, err := svc.Upsert(ctx, heartbeat)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if stored.InstanceID != "instance-1" || stored.SourceVersion != "source-a" {
		t.Fatalf("stored heartbeat = %#v", stored)
	}
	if stored.HeartbeatAt.Nanosecond() != 123000000 {
		t.Fatalf("heartbeat timestamp = %v, want millisecond precision", stored.HeartbeatAt)
	}

	heartbeat.HeartbeatAt = firstHeartbeatAt.Add(15 * time.Second)
	heartbeat.Apps["g-issues"] = core.GestaltdInstanceAppHeartbeat{
		State:          core.GestaltdInstanceAppStateStarting,
		DesiredVersion: "v3",
		ObservedAt:     heartbeat.HeartbeatAt,
	}
	if _, err := svc.Upsert(ctx, heartbeat); err != nil {
		t.Fatalf("replacement Upsert: %v", err)
	}

	got, err := svc.Get(ctx, "instance-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.HeartbeatAt.Equal(heartbeat.HeartbeatAt.UTC().Truncate(time.Millisecond)) {
		t.Fatalf("heartbeat at = %v, want %v", got.HeartbeatAt, heartbeat.HeartbeatAt)
	}
	if app := got.Apps["g-issues"]; app.State != core.GestaltdInstanceAppStateStarting || app.DesiredVersion != "v3" {
		t.Fatalf("app observation = %#v", app)
	}
	all, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("heartbeats = %d, want one row per instance", len(all))
	}
}

func TestGestaltdInstanceHeartbeatServiceListsBySourceVersion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).GestaltdInstanceHeartbeats
	now := time.Date(2026, 7, 30, 14, 0, 0, 0, time.UTC)
	for _, heartbeat := range []*core.GestaltdInstanceHeartbeat{
		{InstanceID: "instance-a", SourceVersion: "source-a", StartedAt: now, HeartbeatAt: now, Apps: map[string]core.GestaltdInstanceAppHeartbeat{}},
		{InstanceID: "instance-b", SourceVersion: "source-b", StartedAt: now, HeartbeatAt: now, Apps: map[string]core.GestaltdInstanceAppHeartbeat{}},
	} {
		if _, err := svc.Upsert(ctx, heartbeat); err != nil {
			t.Fatalf("Upsert(%s): %v", heartbeat.InstanceID, err)
		}
	}
	got, err := svc.ListBySourceVersion(ctx, "source-a")
	if err != nil {
		t.Fatalf("ListBySourceVersion: %v", err)
	}
	if len(got) != 1 || got[0].InstanceID != "instance-a" {
		t.Fatalf("heartbeats = %#v", got)
	}
}

func TestGestaltdInstanceHeartbeatServiceListsFreshAndPrunesByIndexedTime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := testutil.NewStubServices(t).GestaltdInstanceHeartbeats
	now := time.Date(2026, 7, 30, 14, 0, 0, 0, time.UTC)
	for _, heartbeat := range []*core.GestaltdInstanceHeartbeat{
		{InstanceID: "boundary", SourceVersion: "source-a", StartedAt: now.Add(-time.Hour), HeartbeatAt: now.Add(-45 * time.Second), Apps: map[string]core.GestaltdInstanceAppHeartbeat{}},
		{InstanceID: "stale", SourceVersion: "source-a", StartedAt: now.Add(-time.Hour), HeartbeatAt: now.Add(-46 * time.Second), Apps: map[string]core.GestaltdInstanceAppHeartbeat{}},
		{InstanceID: "other", SourceVersion: "source-b", StartedAt: now.Add(-time.Hour), HeartbeatAt: now, Apps: map[string]core.GestaltdInstanceAppHeartbeat{}},
	} {
		if _, err := svc.Upsert(ctx, heartbeat); err != nil {
			t.Fatalf("Upsert(%s): %v", heartbeat.InstanceID, err)
		}
	}
	fresh, err := svc.ListFreshBySourceVersion(ctx, "source-a", now.Add(-45*time.Second))
	if err != nil {
		t.Fatalf("ListFreshBySourceVersion: %v", err)
	}
	if len(fresh) != 1 || fresh[0].InstanceID != "boundary" {
		t.Fatalf("fresh heartbeats = %#v", fresh)
	}
	pruned, err := svc.PruneBefore(ctx, now.Add(-45*time.Second))
	if err != nil {
		t.Fatalf("PruneBefore: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned = %d, want 1", pruned)
	}
	all, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("remaining heartbeats = %#v", all)
	}
}

func TestGestaltdInstanceHeartbeatServiceValidatesRecord(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t).GestaltdInstanceHeartbeats
	now := time.Now()
	_, err := svc.Upsert(context.Background(), &core.GestaltdInstanceHeartbeat{
		InstanceID:    "instance-a",
		SourceVersion: "source-a",
		StartedAt:     now,
		HeartbeatAt:   now.Add(-time.Second),
	})
	if err == nil {
		t.Fatal("Upsert error = nil, want invalid timestamp error")
	}
}
