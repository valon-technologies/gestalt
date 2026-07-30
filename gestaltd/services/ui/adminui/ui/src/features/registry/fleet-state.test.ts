import assert from "node:assert/strict";
import test from "node:test";
import {
  fleetCapacityLabel,
  fleetDiagnostic,
  fleetRolloutSeparation,
  fleetStateVariant,
  heartbeatAgeLabel,
  replicaSourceLabel,
} from "./fleet-state";
import type { FleetState } from "./types";

function fleet(overrides: Partial<FleetState> = {}): FleetState {
  return {
    state: "healthy",
    sourceVersion: "source-current",
    desiredVersion: "1.2.3",
    minimumHealthyInstances: 2,
    liveInstances: 2,
    runningDesiredVersion: 2,
    mismatched: 0,
    errors: 0,
    heartbeatTtlSeconds: 45,
    evaluatedAt: "2026-07-30T16:00:00Z",
    ...overrides,
  };
}

test("presents healthy fleet capacity independently of rollout state", () => {
  const current = fleet();
  assert.equal(fleetStateVariant(current.state), "success");
  assert.equal(fleetCapacityLabel(current), "2/2 live");
  assert.equal(fleetDiagnostic(current), "2/2 running desired version");
  assert.equal(
    fleetRolloutSeparation(current, "failed"),
    "Current fleet is healthy; the last rollout remains failed.",
  );
});

test("does not present insufficient capacity as healthy", () => {
  const current = fleet({
    state: "unknown",
    minimumHealthyInstances: 3,
    liveInstances: 2,
    runningDesiredVersion: 2,
  });
  assert.equal(fleetStateVariant(current.state), "muted");
  assert.equal(fleetDiagnostic(current), "Insufficient capacity: 2/3 live");
});

test("explains unknown fleet basis and degraded observations", () => {
  assert.equal(
    fleetDiagnostic(fleet({ state: "unknown", sourceVersion: undefined, minimumHealthyInstances: 0 })),
    "Fleet basis unavailable",
  );
  assert.equal(
    fleetDiagnostic(fleet({ state: "degraded", mismatched: 1, errors: 2, runningDesiredVersion: 0 })),
    "1 version mismatch · 2 errors",
  );
});

test("labels heartbeat freshness explicitly", () => {
  assert.equal(heartbeatAgeLabel(4, true), "4s ago");
  assert.equal(heartbeatAgeLabel(61, false), "stale · 61s ago");
  assert.equal(replicaSourceLabel("current"), "current source");
  assert.equal(replicaSourceLabel("superseded"), "superseded source");
  assert.equal(replicaSourceLabel("unavailable"), "source status unavailable");
});
