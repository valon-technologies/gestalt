# Runtime Heartbeats and Fleet State

Per-replica runtime heartbeats for current fleet health, replacement-aware app rollouts, and recovery visibility after a rollout failure.

## Overview

The current rollout model freezes a cohort of process identities during an enrollment window. That is not a stable representation of a Cloud Run fleet. An instance can acknowledge a rollout, terminate before restart, and be replaced by another instance on the same Toolshed source version. The departed identity remains in the frozen cohort and causes the rollout to fail even when the replacement and every currently serving replica run the desired app version.

Runtime heartbeats make the live fleet the source of truth for current health:

- Every `gestaltd` process periodically reports its source version and the registry-app versions it is actually serving.
- A freshness lease determines which processes are currently live.
- Rollout completion requires a stable, healthy live fleet, not convergence by every process identity that existed during a fixed window.
- Replacement processes on the target source version can satisfy capacity vacated by terminated processes.
- Historical rollout outcomes remain immutable, while a separate recovery observation records that the current fleet became healthy after a failure.

This design replaces rollout enrollment for new admissions. It does not turn heartbeats into app admission, desired-version selection, or artifact materialization.

## Motivation

The failure mode is:

1. An app version is admitted and an app rollout targets the current Toolshed `SOURCE_VERSION`.
2. A replica acknowledges during `enrolling`.
3. The replica terminates because of an unrelated app or process failure.
4. Cloud Run starts a replacement on the same source version.
5. The replacement materializes and runs the desired app version.
6. The original replica remains a required member of the frozen cohort.
7. The rollout reaches its deadline and is marked `failed`.

`app_instance_materializations` cannot answer whether the fleet is healthy afterward. Its timestamps are historical rollout-progress writes: `restarted_at` means that a process reconciled through a catalog change, not that the process is still alive or currently serving that exact version.

The admin UI must therefore present two independent facts:

- **Rollout outcome** — what happened during one admitted change.
- **Current fleet state** — what live replicas on the current Toolshed source version are serving now.

## Goals

- Report the observed running version of every registry-only app on every live replica.
- Expire terminated replicas without relying on graceful shutdown.
- Prevent a reduced surviving set from appearing healthy after capacity loss.
- Allow a replacement replica to satisfy the capacity formerly occupied by a terminated replica.
- Complete a rollout only after the target-source fleet has remained healthy for a stability window.
- Preserve failed rollout history while showing and recording later recovery.
- Expose current fleet health on both embedded fleet admin and app-admin surfaces.
- Keep heartbeat write volume proportional to replica count, not replica-count times app-count.

## Terminology

- **Desired version** — `LatestKnownVersion` from `app_version_change_requests`.
- **Current source version** — Toolshed source selected by deployment activation in `gestaltd_source_version_state`.
- **Live replica** — a heartbeat for the current source version whose `heartbeat_at` is within the freshness lease.
- **Minimum healthy instances** — the minimum number of live replicas required before fleet health may be `healthy`.
- **Observed running version** — the exact registry version verified from the provider registry, running-version map, and `active-version` marker on one process.
- **Current fleet state** — a derived view over live replica heartbeats for one app.
- **Rollout outcome** — the immutable `complete` or `failed` result of one admitted version change.
- **Recovery observation** — a persisted fact that a failed rollout's desired version later became healthy across the current fleet.

## Heartbeat Model

### Store: `gestaltd_instance_heartbeats`

Store one atomic runtime snapshot per process rather than one row per app. With five replicas and a 15-second interval, this produces about 20 writes per minute regardless of the number of registry apps.

Primary key: `instance_id`.

```json
{
  "instance_id": "8dfcdc5b-cea7-4869-a2e8-5a51d29e8996",
  "source_version": "4f71afddf31d2c452ecd248779a04c905a7b9988",
  "started_at": "2026-07-30T13:48:41Z",
  "heartbeat_at": "2026-07-30T13:52:15Z",
  "apps": {
    "g-issues": {
      "state": "running",
      "desired_version": "0.0.0-snapshot.gd15d64d",
      "running_version": "0.0.0-snapshot.gd15d64d",
      "observed_at": "2026-07-30T13:52:15Z",
      "last_error": ""
    },
    "deal-hub": {
      "state": "error",
      "desired_version": "0.0.0-snapshot.g117b0b54",
      "observed_at": "2026-07-30T13:52:15Z",
      "last_error": "workflow target app is not configured"
    }
  }
}
```

`apps` is one JSON value so all app observations share one timestamp and one transaction. A missing configured registry app in the map is interpreted as `unknown`, never as healthy.

App states:

| State | Meaning |
| --- | --- |
| `running` | Provider registry, running-version map, and `active-version` marker agree on `running_version`. |
| `starting` | The desired package is being materialized or provider startup is in progress. |
| `not_running` | The app is configured but no provider is registered as running. |
| `error` | Reconciliation or runtime-state validation failed; `last_error` contains a diagnostic message. |
| `unknown` | The process cannot safely determine current app state. |

`desired_version` is diagnostic. It does not prove that the process is serving that version. Only `running_version` with `state: running` is affirmative runtime evidence.

### Writer

Each process starts one heartbeat loop after startup-provider initialization:

1. Write immediately.
2. Write every **15 seconds**.
3. Enumerate deploy-configured registry-only apps.
4. Read each app's provider registry, running-version map, and `active-version` marker under the existing lifecycle synchronization.
5. Write the full process snapshot in one IndexedDB operation.

Heartbeat collection must not start, stop, materialize, or relabel an app. Observation failure is represented as `unknown` or `error`.

The process uses its existing process-unique instance ID and `SOURCE_VERSION`. A process restart gets a new instance ID. Graceful shutdown may make a final best-effort delete or expiration write, but correctness never depends on it.

### Freshness

Default heartbeat interval: **15 seconds**.

Default freshness lease: **45 seconds**.

A row is live when:

```text
heartbeat.source_version == current_source_version
heartbeat.heartbeat_at >= now - heartbeat_ttl
```

Readers use UTC process time, consistent with existing rollout timestamps. The three-interval lease tolerates a delayed write while bounding stale membership. Stale rows remain available for short-term diagnostics but do not participate in current health or rollout completion.

Prune heartbeat rows after **24 hours**. Pruning is storage hygiene, not membership logic.

## Expected Fleet Capacity

Freshness alone is insufficient. If one of five replicas disappears, the four remaining heartbeats must not make the fleet look healthy.

Extend `gestaltd_source_version_state`:

```json
{
  "id": "gestaltd",
  "current_source_version": "4f71afddf31d2c452ecd248779a04c905a7b9988",
  "minimum_healthy_instances": 5,
  "updated_at": "2026-07-30T13:52:00Z"
}
```

Toolshed deployment orchestration supplies the Cloud Run minimum instance count when activating a candidate:

```text
POST /activate
  ?source_version={SOURCE_VERSION}
  &minimum_healthy_instances=5
```

The candidate still rejects a source-version mismatch. Activation atomically updates the current source version and minimum count and retargets active app rollouts. Deployment retries refresh the same values.

Production fleet health is `unknown` when the minimum count is absent or zero. Local development may default it to one.

Autoscaling above the minimum is supported. Every fresh replica must report a healthy app observation; the minimum is only the lower bound on capacity.

## Current Fleet State

For each registry app:

1. Load the desired version.
2. Load current source-version state and minimum healthy instances.
3. Select fresh heartbeats for the current source version.
4. Project the app observation from every fresh heartbeat.

States:

| State | Condition |
| --- | --- |
| `healthy` | Live count is at least the minimum, and every live replica reports `running` at the desired version. |
| `converging` | A rollout is active and the fleet is not yet healthy, but its deadline has not passed. |
| `degraded` | At least the minimum replicas are live, but one or more report an error, unknown state, no running version, or a version mismatch. |
| `unknown` | Source/minimum state is unavailable, or fewer than the minimum replicas have fresh heartbeats. |

The projection includes:

```json
{
  "state": "healthy",
  "sourceVersion": "4f71afddf31d2c452ecd248779a04c905a7b9988",
  "desiredVersion": "0.0.0-snapshot.gd15d64d",
  "minimumHealthyInstances": 5,
  "liveInstances": 5,
  "runningDesiredVersion": 5,
  "mismatched": 0,
  "errors": 0,
  "heartbeatTtlSeconds": 45,
  "evaluatedAt": "2026-07-30T13:52:20Z"
}
```

Current fleet state is independent of `app_rollouts.state`. A historical rollout may be `failed` while current fleet state is `healthy`.

## Replacement-Aware Rollouts

### Remove Frozen Enrollment

New rollouts start in `restarting`; they do not freeze process identities.

`app_rollouts` continues to record:

- app and desired version
- target source version
- deadline
- terminal timestamps

Add:

- `minimum_healthy_instances`
- `healthy_since`

The minimum count is snapshotted from current source-version state at admission. Activation retargeting refreshes the source version, minimum count, deadline, and health observation epoch.

### Completion

The heartbeat evaluator may set `healthy_since` when:

- the rollout's target source version is still current
- at least `minimum_healthy_instances` fresh target-source heartbeats exist
- every fresh target-source heartbeat reports the rollout version as running
- no fresh heartbeat reports an app error, unknown state, missing app, or version mismatch

If the condition becomes false, clear `healthy_since`.

Mark the rollout `complete` only after the condition remains true for a **60-second stability window**. The stability window must be longer than the heartbeat freshness lease so a replica that dies immediately after `healthy_since` is set expires before completion can succeed. Terminal transitions remain fenced by app, version, target source version, and activation epoch so concurrent evaluators cannot complete a stale rollout.

Mark the rollout `failed` when its deadline passes without stable health. Include a structured failure summary:

- expected and live instance counts
- mismatched/error instance counts
- stale instances that were recently live
- the most recent per-instance app errors

### Replacement Behavior

Replica identity is diagnostic, not a required rollout slot:

1. Replica A heartbeats and begins convergence.
2. Replica A terminates.
3. A's heartbeat expires after 45 seconds.
4. Replacement B heartbeats on the same target source version.
5. B runs the desired app version.
6. If the live fleet meets the minimum and every live replica agrees, the rollout may complete after the stability window.

A fresh, unhealthy heartbeat cannot be hidden by scaling above the minimum. Every fresh target-source replica must agree.

## Recovery After Failure

Do not rewrite `app_version_rollout_outcomes`. A failed attempt remains failed in revision history.

Add `app_version_recovery_observations`, keyed by change-request ID:

```json
{
  "id": "89804ee7-b11e-40ec-9b48-7ea67179581a",
  "app": "g-issues",
  "version": "0.0.0-snapshot.gd15d64d",
  "recovered_at": "2026-07-30T13:52:15Z",
  "source_version": "4f71afddf31d2c452ecd248779a04c905a7b9988",
  "live_instances": 5,
  "minimum_healthy_instances": 5
}
```

Record recovery once when:

- the latest rollout outcome for the current desired-version change request is `failed`
- that version is still desired
- current fleet state satisfies the same 60-second healthy stability window

Recovery does not change rollout state, append a change request, reset retention, or trigger auto-deploy coalescing. It is observability, not a second admission.

## APIs and Admin UI

### Embedded Admin

Extend registry-app list/detail responses with `fleetState`. Keep `rollout` unchanged as historical execution state.

App detail lists fresh replicas first and stale recent replicas separately:

| Instance  | Source     | Heartbeat | Running version | Runtime state  |
| --------- | ---------- | --------- | --------------- | -------------- |
| `8dfcdc…` | `4f71afd…` | 4s ago    | `gd15d64d…`     | Running        |
| `3bcb66…` | `117b0b5…` | stale     | —               | Process exited |

### App Admin

Extend:

- `GET /api/v1/apps/{app}/admin/registry`
- `GET /api/v1/apps/{app}/admin/registry/history`

The app-admin header shows current fleet state separately from the rollout stepper:

```text
Current fleet: Healthy · 5/5 running gd15d64d… on 4f71afd…
Last rollout: Failed after 15m · Recovered Jul 30 at 09:52
```

Presentation rules:

- Never label a failed rollout `complete`.
- When fleet state is healthy after failure, show **Recovered after failed rollout**.
- When fewer than the expected replicas heartbeat, show **Fleet state unknown**, not healthy.
- Show heartbeat age and current source version so operators can distinguish stale observations from live state.
- Keep rollout timing and recovery timing as separate fields in revision history.

### Proposed Response Shape

```json
{
  "desiredVersion": "0.0.0-snapshot.gd15d64d",
  "rollout": {
    "state": "failed",
    "failedAt": "2026-07-29T20:00:19Z"
  },
  "fleetState": {
    "state": "healthy",
    "sourceVersion": "4f71afddf31d2c452ecd248779a04c905a7b9988",
    "minimumHealthyInstances": 5,
    "liveInstances": 5,
    "runningDesiredVersion": 5,
    "evaluatedAt": "2026-07-30T13:52:20Z"
  },
  "recovery": {
    "recoveredAt": "2026-07-30T13:52:15Z",
    "sourceVersion": "4f71afddf31d2c452ecd248779a04c905a7b9988"
  }
}
```

## Failure and Retry Behavior

- Heartbeat write failure logs a warning and retries on the next interval. It does not stop providers.
- Runtime-state inspection failure reports `unknown`; it does not guess from desired version or materialization history.
- IndexedDB unavailability makes fleet state `unknown` and prevents rollout completion.
- A stale heartbeat never counts toward health, even if it reported the desired version.
- A heartbeat from a superseded source version remains diagnostic but does not affect current health or target rollout completion.
- A source-version activation retargets active rollouts and resets `healthy_since`.
- If deployment rollback changes traffic, orchestration must activate the restored source version and minimum count. Otherwise fleet state correctly remains `unknown` for the stale current-source pointer.
- Multiple replicas may evaluate fleet state. Store updates and terminal transitions are idempotent and epoch-fenced.

## Migration

1. Add heartbeat, recovery-observation, and source minimum-count schemas and services without changing rollout behavior.
2. Start heartbeat writers and expose read-only `fleetState`.
3. Update Toolshed activation to send `minimum_healthy_instances`.
4. Enable recovery observation persistence and UI presentation.
5. Switch new rollouts to heartbeat-based completion.
6. Stop creating `enrolling` rollouts after all production revisions support heartbeats.

Active legacy rollouts retain enrollment semantics until terminal or until a deployment activation explicitly retargets them into the heartbeat evaluator. Historical `app_instance_materializations` and rollout outcomes are preserved.

During mixed-version deployment, absence of heartbeat support yields `unknown`, never a false `healthy`.

## Implementation

Planned as changelog milestone **27 — Runtime Heartbeats and Fleet State**.

### gestalt

**PR 1 — Design doc**

Land this document, link it from [readme.md](../readme.md), and add milestone `27` to [changelog.md](../project/changelog.md).

**PR 2 — Heartbeat and source-capacity state**

- Add `gestaltd_instance_heartbeats` and `app_version_recovery_observations` stores and services in `internal/coredata/`.
- Extend `gestaltd_source_version_state` with `minimum_healthy_instances`.
- Extend `POST /activate` with `minimum_healthy_instances`; preserve source-version mismatch, retry, and retargeting behavior.
- Add heartbeat interval, freshness lease, stability window, retention, and rollout-mode config. Default rollout mode remains `enrollment` during migration.
- Tests: coredata store tests, source-version activation tests, config validation.

**PR 3 — Runtime heartbeat writer and fleet projection**

Stack on PR 2.

- Add one heartbeat loop after startup-provider initialization.
- Observe every registry app from the provider registry, running-version map, and `active-version` marker under existing lifecycle synchronization.
- Write one atomic process snapshot per interval and prune rows older than retention.
- Add the read-only fleet-state projector with `healthy`, `converging`, `degraded`, and `unknown`.
- Tests: heartbeat timing, runtime-state invariants, source filtering, freshness, minimum capacity, mismatches, and write failures.

**PR 4 — Fleet observability API and embedded admin UI**

Stack on PR 3. This PR remains read-only and does not change rollout completion.

- Extend registry-app list/detail APIs in `internal/server/handlers_admin_app_rollout.go` with `fleetState` and fresh/stale replica observations.
- Add current fleet health, expected/live counts, heartbeat age, and source version to the embedded admin UI under `services/ui/adminui/ui/src/features/registry/`.
- Keep `rollout` and `fleetState` as separate response objects and visual states.
- Tests: handler projections and embedded admin UI tests.

**PR 5 — Recovery observations and app-admin API**

Stack on PR 3; it may be reviewed in parallel with PR 4.

- Persist one recovery observation after a failed desired-version rollout later satisfies the healthy stability window.
- Extend `GET /api/v1/apps/{app}/admin/registry` and `GET …/history` with `fleetState` and `recovery`.
- Do not mutate `app_version_rollout_outcomes`, append a change request, or change auto-deploy state.
- Tests: recovery stability, deduplication, newer desired-version fencing, and app-admin response shapes.

**PR 6 — Replacement-aware rollout evaluator**

Stack on PRs 2–3. Land after PR 5 so recovery and history semantics are available before rollout behavior changes.

- Add `minimum_healthy_instances` and `healthy_since` to active rollout state.
- Implement heartbeat-based completion and structured deadline failures in `internal/appregistry/poller.go` and `internal/coredata/app_rollouts.go`.
- Allow replacement replicas to satisfy expired process identities while requiring every fresh target-source replica to agree.
- Fence transitions by app, version, target source version, and activation epoch.
- Keep the behavior behind `server.appRegistry.rolloutMode: heartbeat`; default remains `enrollment`.
- Tests: replacement, stale/fresh membership, stability reset, deadline failure, activation retargeting, and concurrent evaluators.

### gestalt-providers

**PR 7 — App-admin fleet health and recovery UI**

Land after gestalt PRs 1–5 merge.

- Show current fleet health independently from the rollout stepper on `/apps/{app}/admin`.
- Render **Recovered after failed rollout**, expected/live counts, heartbeat freshness, and source version.
- Keep Revision history rollout outcome and recovery timing distinct.
- Tests: `e2e/app-admin-mock.spec.ts`.

### toolshed

**PR 8 — Deploy heartbeat observability**

Land after gestalt PRs 2–6 and gestalt-providers PR 7 merge.

- Update activation and rollback orchestration to send the Cloud Run minimum instance count and reactivate the restored source version.
- Bump `GESTALTD_PINNED_SHA` and the `apps.home` snapshot.
- Deploy with `server.appRegistry.rolloutMode: enrollment` so heartbeat writes, fleet projection, recovery visibility, and both admin UIs can be verified without changing rollout completion.

**PR 9 — Enable heartbeat rollouts**

Land after PR 8 has run for at least two heartbeat freshness windows and all expected production replicas report healthy heartbeats.

- Set `server.appRegistry.rolloutMode: heartbeat`.
- Do not combine this switch with an app snapshot bump or unrelated Toolshed change.
- Roll back by restoring `rolloutMode: enrollment`; preserve heartbeat and recovery data.

### gestalt (docs)

**PR 10 — Fold design into main docs**

Land after PRs 2–9.

- Fold operational detail into [lifecycle.md](../operations/lifecycle.md), [admin.md](../operations/admin.md), [config.md](../architecture/config.md), [indexeddb.md](../architecture/indexeddb.md), and [tests.md](../project/tests.md).
- Update [changelog.md](../project/changelog.md) with merged PR links.
- Keep this one-pager as the canonical design reference in [one-pagers/](./).

### Stacking

```text
main
 ├── gestalt PR 1 — design doc
 ├── gestalt PR 2 — heartbeat and source-capacity state
 │    └── gestalt PR 3 — heartbeat writer and fleet projection
 │         ├── gestalt PR 4 — fleet observability API and embedded admin UI
 │         ├── gestalt PR 5 — recovery observations and app-admin API
 │         └── gestalt PR 6 — replacement-aware rollout evaluator
 ├── gestalt-providers PR 7 — app-admin fleet health and recovery UI
 ├── toolshed PR 8 — deploy heartbeat observability
 ├── toolshed PR 9 — enable heartbeat rollouts
 └── gestalt PR 10 — fold design into main docs
```

| PR | Repo | Base branch | Depends on |
| --- | --- | --- | --- |
| 1 | gestalt | `main` | — |
| 2 | gestalt | `main` | — |
| 3 | gestalt | PR 2 branch | PR 2 |
| 4 | gestalt | PR 3 branch | PR 3 |
| 5 | gestalt | PR 3 branch | PR 3 |
| 6 | gestalt | PR 3 branch | PRs 2–3; land after PR 5 |
| 7 | gestalt-providers | `main` | gestalt PRs 1–5 merged |
| 8 | toolshed | `main` | gestalt PRs 2–6 and gestalt-providers PR 7 merged |
| 9 | toolshed | `main` | PR 8 deployed and verified |
| 10 | gestalt | `main` | PRs 2–9 merged |

### Process

1. **gestalt design and foundations (PRs 1–3)** — Land PR 1, then implement the PR 2 → PR 3 stack. Babysit until CI passes and Bugbot is clean on each. Present all three PRs together and get explicit approval on each.
2. **gestalt observability and recovery (PRs 4–5)** — Open PRs 4 and 5 from the PR 3 branch. They may be reviewed in parallel. Babysit until CI passes and Bugbot is clean; merge PR 4 and PR 5 after PR 3.
3. **gestalt rollout semantics (PR 6)** — Rebase on merged PRs 2–5, verify both enrollment and heartbeat modes, and land with heartbeat mode still disabled by default.
4. **gestalt-providers UI (PR 7)** — Pin the merged gestalt version, add app-admin fleet and recovery presentation, babysit CI and Bugbot, and get approval.
5. **toolshed observability deploy (PR 8)** — Merge PR 7, wait for the `apps.home` registry snapshot, then open PR 8 with gestaltd/UI pins and activation orchestration. Keep enrollment mode active.
6. **production verification** — After PR 8 deploys, verify current-source heartbeats for every expected replica, stale-row expiry after a controlled revision replacement, fleet-state API/UI output, and rollback reactivation. Observe for at least two freshness windows.
7. **heartbeat rollout enablement (PR 9)** — Confirm no app rollout is active, enable heartbeat mode in an isolated Toolshed PR, merge, and exercise a `g-issues` rollout. Verify that a replacement replica can satisfy a departed identity and that current fleet health remains distinct from rollout history.
8. **documentation fold (PR 10)** — Fold the shipped behavior into the main app-registry docs, add merged PR links to changelog milestone 27, babysit CI and Bugbot, and merge.

## Tests

### Heartbeats and Fleet Projection

- Immediate heartbeat after startup-provider initialization.
- Periodic heartbeat contains an atomic observation for every configured registry app.
- `running` requires provider registry, running-version map, and marker agreement.
- Missing or stale heartbeat rows do not count as live.
- Current-source filtering excludes old Cloud Run revisions.
- Fewer than the minimum live instances returns `unknown`.
- One mismatched or errored fresh replica returns `degraded`.
- Additional healthy autoscaled replicas participate without changing the minimum.

### Rollout Completion

- Replacement replica satisfies capacity after a departed heartbeat expires.
- A fresh unhealthy replica blocks completion even above the minimum.
- Healthy state must persist for the stability window.
- Health regression clears `healthy_since`.
- Deadline without stable health records a structured failure.
- Activation retargeting fences stale evaluators and resets stability.
- Concurrent evaluators produce one terminal transition and one outcome row.

### Recovery

- A failed desired version records recovery after stable fleet health.
- Recovery does not overwrite the failed rollout outcome.
- Recovery is not recorded after a newer desired version is admitted.
- Duplicate evaluations produce one recovery row.

### Admin

- APIs expose rollout outcome and current fleet state independently.
- Failed plus healthy renders **Recovered after failed rollout**.
- Insufficient fresh replicas never render healthy.
- Stale replicas remain diagnostic and are labeled stale.

## Operational Defaults

| Setting                              | Default                           |
| ------------------------------------ | --------------------------------- |
| Heartbeat interval                   | 15 seconds                        |
| Heartbeat freshness lease            | 45 seconds                        |
| Healthy stability window             | 60 seconds                        |
| Heartbeat row retention              | 24 hours                          |
| Production minimum healthy instances | Supplied by deployment activation |
| Local minimum healthy instances      | 1                                 |

Make the timing values configurable under `server.appRegistry` for tests and non-production deployments. Validate that the freshness lease is greater than the heartbeat interval and that the stability window is greater than the freshness lease.

## Out of Scope

- Using heartbeats to select or admit a desired app version.
- Automatic rollback to the previous app version.
- Treating Cloud Run request health as proof that every registry app is running.
- Per-user or per-request app version routing.
- Replacing artifact materialization progress and diagnostics.
- Cross-region disaster-recovery fleet aggregation.
- Exactly-once heartbeat writes.

## Related Docs

<pre>
├── <a href="../operations/lifecycle.md">lifecycle.md</a> — current enrollment, polling, and runtime invariants
├── <a href="../operations/admin.md">admin.md</a> — embedded and app-admin presentation
├── <a href="../architecture/indexeddb.md">indexeddb.md</a> — rollout, source-version, and materialization stores
├── <a href="../project/tests.md">tests.md</a> — rollout coordination and admin tests
└── <a href="./revision-history-rollout.md">revision-history-rollout.md</a> — immutable rollout outcomes in revision history
</pre>
