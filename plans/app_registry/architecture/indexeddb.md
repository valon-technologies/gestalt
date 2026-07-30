# App Registry IndexedDB Install State

Reference for the host IndexedDB stores used for app registry installation, per-replica convergence, runtime heartbeats, and recovery observations.

The install HTTP API and installer append only to `app_version_change_requests`. Admin list/get endpoints project fleet-known versions from those requests.

Implementation:

- Schemas — `gestaltd/internal/coredata/schemas.go`
- Change request service — `gestaltd/internal/coredata/app_version_change_requests.go`
- Projections — `gestaltd/internal/coredata/app_version_change_requests_projection.go`
- Domain types — `gestaltd/core/types.go` (`AppInstallation`, `AppVersionChangeRequest`)
- Bootstrap wiring — `gestaltd/internal/bootstrap/bootstrap.go` → `coredata.NewWithOptions`

---

## Accepted Changes and Projections

| Layer | Store / API | Role |
| --- | --- | --- |
| **Stored records** | `app_version_change_requests` | Append-only accepted version changes per app (`from_version` → `to_version`) |
| **Materialized views** | `ListKnownVersionsByApp`, `ListAllKnownVersions` | Computed in Go from change requests |

**Fleet-known versions** — one projected entry per `(app, to_version)` pair from the latest change request for that pair. Failed validation rejects the install HTTP request; no row is written.

`AppInstallation` in `core/types.go` is the projected shape returned by admin HTTP and install handlers, not a direct IndexedDB row.

The ordered change requests are also the permanent **revision history** for an app. They are never removed by registry retention. The history API returns the raw transition sequence rather than the deduplicated fleet-known projection, so each accepted `from_version` → `to_version` change remains auditable, including repeated versions in upgrade and downgrade chains such as `v1 → v2 → v1`.

IndexedDB stores accepted version changes, rollout execution, each replica's historical progress, and one current runtime snapshot per process. It does not store a single authoritative "current fleet version": fleet state is derived from the desired change request, activated source/capacity, and fresh heartbeat rows.

Gestalt derives the desired version from the latest change-request timestamp. If two requests have the same timestamp, it chooses the lexicographically greatest version string so every replica selects the same version. Bootstrap and polling both compare the requests using this rule; they do not assume that the first or last record returned by IndexedDB is the newest.

Each replica materializes and retains only this desired version. Older fleet-known versions remain catalog history; their per-replica rows may be acknowledged and marked restarted when the replica reconciles past them without ever receiving `materialized_at`.

Each Gestalt process separately tracks the version its provider is currently serving. It uses an in-process running-version map and a local `active-version` marker in the artifacts directory, and updates both with provider start and stop. `app_instance_materializations` is not used for current runtime state because it contains historical rollout progress for multiple versions and can become stale after a process exits or crashes.

---

## When Stores Are Created

`CreateObjectStore` runs **during `gestaltd` bootstrap**, not before it and not lazily on first install.

Rough order inside `bootstrap.Run` (see `gestaltd/internal/bootstrap/bootstrap.go`):

1. Load and validate deploy config.
2. Build workflow and agent runtime placeholders.
3. Resolve the selected **main-db** IndexedDB provider (`cfg.SelectedIndexedDBProvider()`).
4. Open a connection to that provider (`buildIndexedDB`).
5. Call **`coredata.NewWithOptions(ctx, store, …)`** — this is where host control-plane stores are created.
6. Continue bootstrap: build provider graph, sync lockfile artifacts, write `app_shas`, construct the HTTP server, etc.

So store creation is an **early bootstrap step**, immediately after the main IndexedDB handle is available and **before** the runtime provider graph is fully built and before `gestaltd serve` accepts traffic.

### What `coredata.NewWithOptions` Does

When `SkipSchemaBootstrap` is false (default for a **local** main-db provider), `coredata` idempotently calls `CreateObjectStore` for each host store.

App registry stores:

```text
app_version_change_requests      ← accepted version changes (append-only)
app_version_install_locks        ← install admission lock per app
gestaltd_source_version_state    ← Toolshed source version selected for rollout accounting
app_rollouts                     ← current rollout per app
app_version_rollout_outcomes     ← terminal rollout timing per change request
app_instance_materializations    ← per-replica acknowledgement of fleet-known versions
app_auto_deploy_settings         ← per-app auto-deploy policy
gestaltd_instance_heartbeats     ← current per-process runtime snapshots
app_version_recovery_observations ← stable recovery facts for failed revisions
```

Other host stores created at bootstrap include `users`, `managed_subjects`, `app_shas`, etc.

If a store already exists with a matching schema, creation is a no-op. A second bootstrap on the same database does not fail.

When `SkipSchemaBootstrap` is true (main-db is **delegated** to a remote gestaltd that already owns schema), `CreateObjectStore` is skipped on this instance. The remote owner must have created the stores already.

### What Bootstrap Does _Not_ Do on Startup

Bootstrap does **not** write change requests. After deployment, the store is empty until an install request or test appends data.

Access install services after bootstrap via `Result.Services` / `prepared.Services`:

```text
Services.AppVersionChangeRequests   ← install prototype
Services.GestaltdSourceVersionState ← current Toolshed source version and activation time
Services.AppRollouts                ← rollout state
Services.AppVersionRolloutOutcomes  ← terminal rollout timing per change request
Services.AutoDeploySettings         ← per-app auto-deploy policy
Services.GestaltdInstanceHeartbeats ← per-process runtime snapshots
Services.AppVersionRecoveryObservations ← immutable failed-rollout recovery facts
Services.DB                         ← underlying main-db handle
```

---

## Store: `app_version_change_requests` (Accepted Version Changes)

Append-only fleet requests to move one app from `from_version` to `to_version`. `from_version` is required on every row and is set server-side — callers never send it. `POST …/add` writes `from_version: "registry:first-install"` when the app has no fleet-known versions. `POST …/upgrade` sets `from_version` to the latest fleet-known `to_version` (`LatestKnownVersion`). See [lifecycle.md](../operations/lifecycle.md#post-adminapiv1app-registriesregistryappsappadd).

```text
app_version_change_requests
  - id
  - app                            # app name, e.g. g-issues
  - from_version                   # required; server-written audit field (not a runnable version on add)
  - to_version
  - actor
  - timestamp
  - from_version_deployable_until # audit copy of outgoing expiresAt at transition; omitted on first install
  - metadata_json                  # install contract snapshot
```

Primary key: `id` (UUID).

`app` is the **app name** (for example `g-issues`), matching `app_shas.id`.

**Required transition fields** — `from_version_deployable_until` is required when `from_version` is runnable and omitted for the first-install sentinel:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "app": "g-issues",
  "from_version": "0.0.0-snapshot.gdeadbeef",
  "to_version": "0.0.0-snapshot.gabc123",
  "timestamp": "2026-07-10T02:20:00Z",
  "from_version_deployable_until": "2026-08-09T02:20:00Z"
}
```

`id` and `timestamp` are generated by `AppendRequest` when omitted.

**Add** — first fleet-known version (`from_version` is server-written, not caller-supplied):

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440001",
  "app": "g-issues",
  "from_version": "registry:first-install",
  "to_version": "0.0.0-snapshot.gabc123",
  "timestamp": "2026-07-10T02:20:00Z"
}
```

`registry:first-install` is an audit sentinel only. Projection helpers must not treat it as a runnable version.

`from_version_deployable_until` captures the configured `deployedRetention` deadline when the outgoing version stops being desired. It is immutable with the transition, so later config changes cannot shorten, extend, or reopen that historical version's window. The latest transition whose `from_version` matches a non-current version defines that version's redeploy deadline.

**Optional fields**:

```json
{
  "actor": "user:alice",
  "metadata_json": {
    "registry": "toolshed",
    "source_ref": "abc123def456abc123def456abc123def456abcd",
    "provider_release_url": "https://storage.googleapis.com/.../versions/0.0.0-snapshot.gabc123.json",
    "artifact_checksums": {
      "linux/amd64": "deadbeef…"
    },
    "publication": {
      "workflow_run_url": "https://github.com/valon-technologies/valon-tools/actions/runs/123456789",
      "trigger_pull_request": {
        "number": 3251,
        "url": "https://github.com/valon-technologies/valon-tools/pull/3251"
      }
    },
    "installed_at": "2026-07-10T02:20:00Z"
  }
}
```

`metadata_json` carries the immutable install contract and publication provenance snapshot used to project `AppInstallation` and revision-history responses. Legacy rows may omit `publication`.

The low-level `POST …/add` and `POST …/upgrade` routes reject an already-known `to_version`. App-admin version selection may append the same `to_version` again when that historical version is still before `expiresAt`. After `expiresAt`, selection returns **400** and no row is appended.

### Indexes

| Index | Key path | Use |
| --- | --- | --- |
| `by_app` | `app` | List all requests for one app (unordered). |
| `by_app_timestamp` | `app`, `timestamp` | Time-ordered request history per app. |
| `by_app_to_version` | `app`, `to_version` | Lookup whether a version is already requested. |

### Service API

`AppVersionChangeRequestService` (`gestaltd/internal/coredata/app_version_change_requests.go`):

| Method | Description |
| --- | --- |
| `AppendRequest(ctx, request)` | Insert one change request (`Add`; fails if `id` collides). |
| `ListRequestsByApp(ctx, app)` | Requests for an app in `timestamp` order; backs the permanent revision-history API. |
| `HasKnownVersion(ctx, app, version)` | Whether a change request exists for `(app, to_version)`. |
| `ListKnownVersionsByApp(ctx, app)` | Projected fleet-known versions for one app. |
| `ListAllKnownVersions(ctx)` | Projected fleet-known versions across all apps. |

Projection helpers live in `app_version_change_requests_projection.go`.

---

## Store: `app_version_install_locks` (Install Admission Lock)

Short-lived lock rows that serialize install admission for one app across the fleet. The version remains diagnostic metadata, but the lock key is app-scoped so two versions of the same app cannot be admitted concurrently.

Primary key: `id` = `app`.

```json
{
  "id": "g-issues",
  "app": "g-issues",
  "version": "0.0.0-snapshot.gabc123",
  "holder": "550e8400-e29b-41d4-a716-446655440000",
  "acquired_at": "2026-07-10T14:19:50Z",
  "expires_at": "2026-07-10T14:34:50Z"
}
```

- `holder` — opaque id for one install attempt (UUID generated per `POST …/add` or `POST …/upgrade`)
- `expires_at` — default TTL 15 minutes; stale locks can be taken over after expiry

### Service API

`AppVersionInstallLockService` (`gestaltd/internal/coredata/app_version_install_locks.go`):

| Method | Description |
| --- | --- |
| `Acquire(ctx, app, version, holder, ttl)` | Claim the app-scoped lock; returns `ErrAppVersionInstallLockHeld` if another holder owns a non-expired lock |
| `Release(ctx, app, version, holder)` | Drop lock when this holder still owns it |

Used by `POST …/add` and `POST …/upgrade` while each checks rollout admission and appends the change request; released on success or failure.

---

## Store: `gestaltd_source_version_state` (Activated Source and Capacity)

Tracks the Toolshed `SOURCE_VERSION` used for current fleet reads and rollout membership, plus the Cloud Run minimum capacity required for health. Toolshed deployment orchestration is the only writer; a `gestaltd` process must not elect its own source version current.

Primary key: `id` = `gestaltd`.

```json
{
  "id": "gestaltd",
  "current_source_version": "574fe7704ed67fc15d44f76698755bb94ad33d43",
  "minimum_healthy_instances": 5,
  "updated_at": "2026-07-27T22:30:00Z"
}
```

The source version is the Toolshed commit SHA injected into each process as `SOURCE_VERSION`. `minimum_healthy_instances` comes from the activated Cloud Run revision's minimum scale. `updated_at` records the last source/minimum change or explicit deployment retry.

Toolshed calls the candidate's tagged `POST /activate?source_version={SOURCE_VERSION}&minimum_healthy_instances={MIN_SCALE}` endpoint before shifting traffic. The handler rejects a source-version mismatch and invalid capacity. Activation atomically updates both values and retargets active app rollouts with a fresh evaluation epoch. A repeated activation for the same values is idempotent.

If deployment fails after activation, rollback reactivates the restored revision and its minimum after traffic restoration. A workflow retry includes `retry=true`, updates `updated_at`, refreshes active rollout epochs, and may reopen rollouts for that source that failed after its previous activation. Neither operation appends another change request.

---

## Store: `app_rollouts` (Current Rollout per App)

Tracks the current fleet rollout for each app. `app_version_change_requests` records accepted version changes; `app_rollouts` records their fleet-wide execution and outcome. The app-scoped primary key allows only one active rollout per app.

Primary key: `id` = `app`.

```json
{
  "id": "g-issues",
  "app": "g-issues",
  "version": "0.0.0-snapshot.gabc123",
  "state": "restarting",
  "mode": "heartbeat",
  "target_source_version": "61885becf49a25a4a8c0063a4d9dd9643b28c2a6",
  "minimum_healthy_instances": 5,
  "created_at": "2026-07-13T21:00:00Z",
  "enrollment_ends_at": "2026-07-13T21:02:00Z",
  "deadline": "2026-07-13T21:15:00Z",
  "healthy_since": "2026-07-13T21:03:00Z",
  "heartbeat_evaluated_at": "2026-07-13T21:03:15Z"
}
```

States:

- `enrolling` — enrollment-mode replicas may join by acknowledging before the window closes.
- `restarting` — providers are converging. In heartbeat mode this is the initial state and live membership is not frozen.
- `complete` — the stored mode's completion condition remained satisfied.
- `failed` — the stored mode did not satisfy completion before the deadline.

A terminal record may be replaced when the next version is admitted. A non-terminal record causes `POST …/add` or `POST …/upgrade` for the same app to return **409 Conflict**. Terminal transitions record `completed_at` or `failed_at`.

`mode` is `enrollment` or `heartbeat`. Heartbeat rollouts snapshot `minimum_healthy_instances`; `healthy_since` is set while every fresh target-source heartbeat reports the rollout version and cleared on any regression. `heartbeat_evaluated_at` and `failure_summary` preserve the latest evaluation and structured deadline diagnostics. Activation retargeting resets these fields, the deadline, and the evaluation epoch.

### Service API

`AppRolloutService` (`gestaltd/internal/coredata/app_rollouts.go`):

| Method | Description |
| --- | --- |
| `Get(ctx, app)` | Load the current rollout for one app. |
| `Create(ctx, rollout)` | Create a rollout when the current record is absent or terminal. |
| `ListActive(ctx)` | List rollouts in `enrolling` or `restarting`. |
| `MarkRestarting(ctx, app, version)` | Freeze the acknowledged cohort after enrollment. |
| `MarkComplete(ctx, app, version, completedAt)` | Record successful cohort convergence. |
| `MarkFailed(ctx, app, version, failedAt)` | Record that the rollout missed its deadline. |
| `EvaluateHeartbeatRollout(ctx, rollout, evaluation)` | Transactionally advance/reset stability or complete/fail a heartbeat rollout, fenced by the rollout epoch. |

**Admin exposure:** `Get` and `ListActive` back `GET /admin/api/v1/app-rollouts` and rollout summaries on `GET /admin/api/v1/registry-apps`. See [lifecycle.md](../operations/lifecycle.md#admin-observability-api).

---

## Store: `app_version_rollout_outcomes` (Terminal Rollout Timing)

Persists when each admitted version finished or failed its fleet rollout. `app_version_change_requests` stays append-only; this sidecar supplies terminal `completed_at` or `failed_at` for revision-history rows after `app_rollouts` is replaced by a later admission.

Primary key: `id` = change-request `id`.

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "app": "g-issues",
  "version": "0.0.0-snapshot.gdef456",
  "completed_at": "2026-07-24T20:45:08Z"
}
```

or, on failure:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "app": "g-issues",
  "version": "0.0.0-snapshot.gdef456",
  "failed_at": "2026-07-24T20:54:41Z"
}
```

The catalog poller writes the row exactly once when `MarkCompleteForRollout` or `MarkFailedForRollout` succeeds, resolving the change request by `(app, version)` using the latest request for that `to_version` at transition time.

### Service API

`AppVersionRolloutOutcomeService` (`gestaltd/internal/coredata/app_version_rollout_outcomes.go`):

| Method | Description |
| --- | --- |
| `Get(ctx, changeRequestID)` | Load one outcome by change-request `id`. |
| `GetMany(ctx, changeRequestIDs)` | Batch-load outcomes for history enrichment. |
| `RecordComplete(ctx, changeRequestID, app, version, completedAt)` | Persist a successful rollout end time. |
| `RecordFailed(ctx, changeRequestID, app, version, failedAt)` | Persist a failed rollout end time. |

**Admin exposure:** `GetMany` backs rollout duration fields on `GET /api/v1/apps/{app}/admin/registry/history`. See [lifecycle.md](../operations/lifecycle.md#revision-history).

---

## Store: `gestaltd_instance_heartbeats` (Per-Replica Runtime State)

One row per process contains an atomic observation of every deploy-configured registry-only app. Primary key `id` and unique index `by_instance` both use the process-unique `instance_id`; source and timestamp indexes support fresh-fleet reads.

```json
{
  "id": "8dfcdc5b-cea7-4869-a2e8-5a51d29e8996",
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
    }
  }
}
```

The `apps` JSON value makes one heartbeat a coherent process snapshot. A configured app missing from the runtime snapshot is written as `unknown`; `desired_version` is diagnostic, while only `state: running` plus an exact `running_version` match is affirmative runtime evidence.

Observation states are `running` (all runtime markers agree), `starting`, `not_running`, `error`, and `unknown`. `last_error` carries the latest diagnostic for an error/unknown observation. Collection is passive and never changes provider lifecycle state.

Rows older than the configured retention are pruned for storage hygiene. Membership uses the shorter freshness lease and current source filter, so stale rows may remain available for diagnosis without affecting health.

### Service API

| Method | Description |
| --- | --- |
| `Upsert(ctx, heartbeat)` | Replace one process's full runtime snapshot. |
| `List(ctx)` | List all retained rows for fresh/stale admin detail. |
| `ListFreshBySourceVersion(ctx, source, cutoff)` | Load current candidates for fleet projection and rollout/recovery evaluation. |
| `PruneBefore(ctx, cutoff)` | Delete rows older than retention. |

---

## Store: `app_version_recovery_observations` (Post-Failure Recovery)

Persists one recovery fact keyed by change-request `id` when the still-desired version has a failed rollout outcome and later remains healthy for the configured stability window.

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "app": "g-issues",
  "version": "0.0.0-snapshot.gdef456",
  "recovered_at": "2026-07-30T13:52:15Z",
  "source_version": "4f71afddf31d2c452ecd248779a04c905a7b9988",
  "live_instances": 5,
  "minimum_healthy_instances": 5
}
```

`RecordIfCurrentFailed` fences the write against the current desired revision and its failed immutable outcome. Concurrent replica observers are idempotent. A recovery row does not mutate `app_version_rollout_outcomes`, `app_rollouts`, retention, or auto-deploy state.

`Get`, `GetMany`, and `RecordIfCurrentFailed` back app-admin current state and revision-history recovery fields.

---

## Legacy RelationalDB Schema Compatibility

RelationalDB requires exact object-store schema metadata equality at `CreateObjectStore`, but stores each complete record in `record_blob`. Runtime Heartbeats therefore keeps the deployed schema declarations for the existing `gestaltd_source_version_state` and `app_rollouts` stores unchanged.

The new source-capacity and heartbeat-rollout fields are non-key, non-indexed record fields. Services read and write them in the full record blob even though they are intentionally absent from the declared column metadata. This permits old and new gestaltd revisions to bootstrap against the same stores during a rolling deployment. The two genuinely new stores, `gestaltd_instance_heartbeats` and `app_version_recovery_observations`, are still created normally.

Do not add the non-indexed Runtime Heartbeats fields to those two existing schema declarations without an explicit mixed-version migration design. [gestalt#3015](https://github.com/valon-technologies/gestalt/pull/3015) added regression coverage for initial and repeated bootstrap plus field round trips under the legacy metadata.

---

## Store: `app_auto_deploy_settings` (Auto-Deploy Policy)

Per-app fleet policy for automatic admission of newly published snapshots. Writable only through the app-admin auto-deploy API; the background auto-deploy controller reads and updates progress fields during coalescing.

Primary key: `app` (app name).

```json
{
  "app": "g-issues",
  "enabled": true,
  "pending_version": "0.0.0-snapshot.gdef456",
  "last_seen_version": "0.0.0-snapshot.gabc123",
  "last_error": "",
  "last_failed_rollout_at": null
}
```

| Field | Purpose |
| --- | --- |
| `enabled` | Per-app toggle; writable only via app-admin API |
| `pending_version` | Newest published version waiting for admission |
| `last_seen_version` | Deduplicate publish detection across polls |
| `last_error` | Last failure message; set on validation failure or rollout `failed` |
| `last_failed_rollout_at` | Deduplicate handling of a persisted failed rollout across disable and re-enable |

The store is created idempotently during bootstrap. When `SkipSchemaBootstrap` is true, `EnsureStore` runs before reads and writes so existing deployments that started before the store was added still converge. See [lifecycle.md](../operations/lifecycle.md#auto-deploy-published-snapshots).

### Service API

`AutoDeploySettingsService` (`gestaltd/internal/coredata/app_auto_deploy_settings.go`):

| Method | Description |
| --- | --- |
| `Get(ctx, app)` | Load settings for one app. Returns `ErrNotFound` when no row exists. |
| `ListEnabled(ctx)` | List apps with `enabled: true`. |
| `Update(ctx, app, fn)` | Read-modify-write one app's settings. Missing rows start disabled with empty progress fields. |
| `EnsureStore(ctx)` | Idempotently create the object store when schema bootstrap is skipped. |

`Get` and `Update` back `GET` and `PUT /api/v1/apps/{app}/admin/registry/auto-deploy`. The registry-state route projects `enabled`, `pendingVersion`, and `lastError`.

---

## Store: `app_instance_materializations` (Per-Replica Convergence)

Records one replica's acknowledgement and catalog-convergence progress for a fleet-known `(app, version)`.

Primary key: `id` (UUID). Uniqueness for `(instance_id, app, version)` is enforced by the `by_instance_app_version` index.

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "instance_id": "cloud-run-revision-pod",
  "source_version": "61885becf49a25a4a8c0063a4d9dd9643b28c2a6",
  "app": "g-issues",
  "version": "0.0.0-snapshot.gabc123",
  "acknowledged_at": "2026-07-13T21:00:00Z",
  "materialized_at": "2026-07-13T21:00:02Z",
  "stopped_at": "2026-07-13T21:00:05Z",
  "restarted_at": "2026-07-13T21:01:05Z",
  "attempt_count": 1,
  "last_error_at": "2026-07-13T21:00:30Z",
  "last_error_message": "start provider: executable exited before registration"
}
```

`instance_id` defaults to the process hostname (`os.Hostname()`).

`source_version` groups replicas for rollout accounting and copies `SOURCE_VERSION` from the process environment.

Keep `instance_id` process-unique. Replacing it with `source_version` would collapse every replica running one Toolshed version into one row and falsely report convergence after a single process restarts.

`materialized_at` is set only when that version was the replica's desired version and its package was validated locally. A superseded row can have `restarted_at` without `materialized_at`: this means the replica reconciled past that catalog change while running a newer desired version, not that the superseded version ran.

For example, suppose a replica misses `v1` and next polls after `v2` has become the desired version:

| Version | `materialized_at` | `restarted_at` | Meaning |
| --- | --- | --- | --- |
| `v1` | unset | set | `v1` was superseded and intentionally skipped. The replica reconciled past this row, so the poller must not reconsider it. |
| `v2` | set | set | `v2` was downloaded, validated, and activated. |

In this table, `restarted_at` on `v1` means "reconciled through this catalog version," not "this exact version ran."

`attempt_count` counts failed reconciliation attempts for this replica, app, and version. `last_error_at` and `last_error_message` retain the most recent failure for diagnosis, including after a later attempt succeeds. They do not determine whether the version is currently running.

### Service API

`AppInstanceMaterializationService` (`gestaltd/internal/coredata/app_instance_materializations.go`):

| Method | Description |
| --- | --- |
| `HasAcknowledged(ctx, instanceID, app, version)` | Whether this replica already acknowledged the pair. |
| `Get(ctx, instanceID, app, version)` | Load the per-replica materialization row. |
| `Acknowledge(ctx, materialization)` | Insert an acknowledgement row; idempotent if already present. |
| `MarkMaterialized(ctx, instanceID, app, version, materializedAt)` | Record when download and extraction completed at the canonical local path. |
| `ListByAppVersion(ctx, app, version)` | List the replicas that acknowledged one rollout. |
| `MarkStopped(ctx, instanceID, app, version, stoppedAt)` | Record when the app provider was stopped for this fleet version. |
| `MarkRestarted(ctx, instanceID, app, version, restartedAt)` | Record when the replica reconciled through this catalog version; a newer desired version may be the one that actually started. |
| `RecordFailure(ctx, instanceID, app, version, failedAt, message)` | Atomically increment `attempt_count` and replace the last-error fields. |

Written by the background catalog poller (`gestaltd/internal/appregistry/poller.go`).

`app_instance_materializations` rows are rollout-progress records; they do not decide which version a replica starts during boot. Bootstrap may start the desired version selected by `LatestKnownVersion` without waiting for the poller to create or update one of these rows. When the poller runs later, it checks the version that is actually running. If the replica is already running that desired version, the poller validates and records materialization for it, marks superseded pending rows converged without downloading them, and does not restart the app again.

`ListByAppVersion` backs `GET /admin/api/v1/app-rollouts/{app}/materializations`. See [lifecycle.md](../operations/lifecycle.md#admin-observability-api).

---

## Appendix

### Related Changelogs

<pre>
├── <a href="../project/changelog.md#changelog-05">05 — Installation State in IndexedDB</a>
├── <a href="../project/changelog.md#changelog-06">06 — Registry Installation Prototype</a>
├── <a href="../project/changelog.md#changelog-07">07 — Catalog-Only Admission</a>
├── <a href="../project/changelog.md#changelog-08">08 — Per-Replica Catalog Polling</a>
├── <a href="../project/changelog.md#changelog-09">09 — Coordinated Provider Restarts</a>
├── <a href="../project/changelog.md#changelog-15">15 — App-Scoped Version Selection</a>
├── <a href="../project/changelog.md#changelog-25">25 — Auto-Deploy Published Snapshots</a>
└── <a href="../project/changelog.md#changelog-27">27 — Runtime Heartbeats and Fleet State</a>
</pre>

### Related Docs

<pre>
├── <a href="../readme.md">readme.md</a> — registry architecture and future work
├── <a href="../project/changelog.md">changelog.md</a> — implementation milestones and pull requests
├── <a href="../operations/lifecycle.md">lifecycle.md</a> — replica startup, background controller, admin HTTP API
├── <a href="./validation.md">validation.md</a> — install-time validation
├── <a href="../operations/admin.md">admin.md</a> — admin UI capabilities
├── <a href="../one-pagers/runtime-heartbeats.md">runtime-heartbeats.md</a> — canonical design
└── <a href="./models.md">models.md</a> — GCS registry entry JSON that change requests reference
</pre>
