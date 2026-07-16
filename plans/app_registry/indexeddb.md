# App Registry IndexedDB Install State

Reference for the host IndexedDB stores added in plan **step 5** ([gestalt#2718](https://github.com/valon-technologies/gestalt/pull/2718)) and the **catalog** install model in step 6 ([gestalt#2730](https://github.com/valon-technologies/gestalt/pull/2730)).

Step 5 adds store schemas, bootstrap (`CreateObjectStore`), and Go services. Step 6 adds the install HTTP API and installer, which **append only to `app_version_change_requests`**. Admin list/get endpoints **project known versions** from those requests.

Related docs:

- [plan.md](./plan.md) — install flow, multi-instance convergence, planned `app_instance_materializations`
- [lifecycle.md](./lifecycle.md) — replica startup, background controller, admin HTTP API
- [models.md](./models.md) — GCS registry entry JSON that change requests reference

Implementation:

- Schemas — `gestaltd/internal/coredata/schemas.go`
- Change request service — `gestaltd/internal/coredata/app_version_change_requests.go`
- Projections — `gestaltd/internal/coredata/app_version_change_requests_projection.go`
- Domain types — `gestaltd/core/types.go` (`AppInstallation`, `AppVersionChangeRequest`)
- Bootstrap wiring — `gestaltd/internal/bootstrap/bootstrap.go` → `coredata.NewWithOptions`

---

## Accepted changes and projections

| Layer | Store / API | Role |
|-------|-------------|------|
| **Stored records** | `app_version_change_requests` | Append-only accepted version changes per app (`from_version` → `to_version`) |
| **Materialized views** | `ListKnownVersionsByApp`, `ListAllKnownVersions` | Computed in Go from change requests |

**Known versions** — one projected entry per `(app, to_version)` pair from the latest change request for that pair. Failed validation rejects the install HTTP request; no row is written.

`AppInstallation` in `core/types.go` is the projected shape returned by admin HTTP and install handlers, not a direct IndexedDB row.

There is **no fleet head**, **no promotion**, and **no rollback** in the step 6 catalog model. Activation and rollback are planned for later steps.

---

## When stores are created

`CreateObjectStore` runs **during `gestaltd` bootstrap**, not before it and not lazily on first install.

Rough order inside `bootstrap.Run` (see `gestaltd/internal/bootstrap/bootstrap.go`):

1. Load and validate deploy config.
2. Build workflow and agent runtime placeholders.
3. Resolve the selected **main-db** IndexedDB provider (`cfg.SelectedIndexedDBProvider()`).
4. Open a connection to that provider (`buildIndexedDB`).
5. Call **`coredata.NewWithOptions(ctx, store, …)`** — this is where host control-plane stores are created.
6. Continue bootstrap: build provider graph, sync lockfile artifacts, write `app_shas`, construct the HTTP server, etc.

So store creation is an **early bootstrap step**, immediately after the main IndexedDB handle is available and **before** the runtime provider graph is fully built and before `gestaltd serve` accepts traffic.

### What `coredata.NewWithOptions` does

When `SkipSchemaBootstrap` is false (default for a **local** main-db provider), `coredata` idempotently calls `CreateObjectStore` for each host store.

App registry stores:

```text
app_version_change_requests      ← accepted version changes (append-only)
app_version_install_locks        ← install admission lock per app
app_rollouts                     ← current rollout per app
app_instance_materializations    ← per-replica ack of fleet-known versions
```

Other host stores created at bootstrap include `users`, `managed_subjects`, `app_shas`, etc.

If a store already exists with a matching schema, creation is a no-op. A second bootstrap on the same database does not fail.

When `SkipSchemaBootstrap` is true (main-db is **delegated** to a remote gestaltd that already owns schema), `CreateObjectStore` is skipped on this instance. The remote owner must have created the stores already.

### What bootstrap does *not* do on startup

Bootstrap does **not** write change requests. After deploy the store is empty until an install request (or tests) append data.

Access install services after bootstrap via `Result.Services` / `prepared.Services`:

```text
Services.AppVersionChangeRequests   ← install prototype
Services.AppRollouts                ← rollout state
Services.DB                         ← underlying main-db handle
```

---

## Store: `app_version_change_requests` (accepted version changes)

Append-only fleet requests to move one app from `from_version` to `to_version`. `from_version` is required on every row. The installer resolves it from the latest fleet-known `to_version`, or falls back to the app's pinned version in `config.yaml` / `gestalt.lock.json` when the app has not been installed via the registry yet.

```text
app_version_change_requests
  - id
  - app                            # app name, e.g. g-issues
  - from_version                   # required; fleet-known version or config pin
  - to_version
  - actor
  - timestamp
  - metadata_json                  # install contract snapshot
```

Primary key: `id` (UUID).

`app` is the **app name** (for example `g-issues`), matching `app_shas.id`.

**Required fields** — must be present on every row:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "app": "g-issues",
  "from_version": "0.0.0-snapshot.gdeadbeef",
  "to_version": "0.0.0-snapshot.gabc123",
  "timestamp": "2026-07-10T02:20:00Z"
}
```

`id` and `timestamp` are generated by `AppendRequest` when omitted.

**Optional fields**:

```json
{
  "actor": "user:alice",
  "metadata_json": {
    "registry": "toolshed",
    "materialized_path": "/var/gestalt/artifacts/registry-installed/g-issues/0.0.0-snapshot.gabc123",
    "source_ref": "abc123def456abc123def456abc123def456abcd",
    "provider_release_url": "https://storage.googleapis.com/.../versions/0.0.0-snapshot.gabc123.json",
    "artifact_checksums": {
      "linux/amd64": "deadbeef…"
    },
    "installed_at": "2026-07-10T02:20:00Z"
  }
}
```

`metadata_json` carries the install contract snapshot used to project `AppInstallation` for HTTP responses.

Re-installing an already-known `to_version` returns **400** from `POST …/install` (`HasKnownVersion` guard); no duplicate change request is appended.

### Indexes

| Index | Key path | Use |
|-------|----------|-----|
| `by_app` | `app` | List all requests for one app (unordered). |
| `by_app_timestamp` | `app`, `timestamp` | Time-ordered request history per app. |
| `by_app_to_version` | `app`, `to_version` | Lookup whether a version is already requested. |

### Service API

`AppVersionChangeRequestService` (`gestaltd/internal/coredata/app_version_change_requests.go`):

| Method | Description |
|--------|-------------|
| `AppendRequest(ctx, request)` | Insert one change request (`Add`; fails if `id` collides). |
| `ListRequestsByApp(ctx, app)` | Requests for an app in `timestamp` order. |
| `HasKnownVersion(ctx, app, version)` | Whether a change request exists for `(app, to_version)`. |
| `ListKnownVersionsByApp(ctx, app)` | Projected known versions for one app. |
| `ListAllKnownVersions(ctx)` | Projected known versions across all apps. |

Projection helpers live in `app_version_change_requests_projection.go`.

---

## Store: `app_version_install_locks` (install admission lock)

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

- `holder` — opaque id for one install attempt (UUID generated per `POST …/install`)
- `expires_at` — default TTL 15 minutes; stale locks can be taken over after expiry

### Service API

`AppVersionInstallLockService` (`gestaltd/internal/coredata/app_version_install_locks.go`):

| Method | Description |
|--------|-------------|
| `Acquire(ctx, app, version, holder, ttl)` | Claim the app-scoped lock; returns `ErrAppVersionInstallLockHeld` if another holder owns a non-expired lock |
| `Release(ctx, app, version, holder)` | Drop lock when this holder still owns it |

Used by `POST …/install` while it checks rollout admission and appends the change request; released on success or failure.

---

## Store: `app_rollouts` (current rollout per app)

Tracks the current fleet rollout for each app. `app_version_change_requests` records accepted version changes; `app_rollouts` records their fleet-wide execution and outcome. The app-scoped primary key allows only one active rollout per app.

Primary key: `id` = `app`.

```json
{
  "id": "g-issues",
  "app": "g-issues",
  "version": "0.0.0-snapshot.gabc123",
  "state": "enrolling",
  "created_at": "2026-07-13T21:00:00Z",
  "enrollment_ends_at": "2026-07-13T21:02:00Z",
  "deadline": "2026-07-13T21:15:00Z"
}
```

States:

- `enrolling` — replicas may join the rollout by acknowledging it.
- `restarting` — the acknowledged cohort is frozen and is converging.
- `complete` — every enrolled replica recorded `restarted_at`.
- `failed` — the cohort did not converge before the deadline.

A terminal record may be replaced when the next version is admitted. A non-terminal record causes `POST …/install` for the same app to return **409 Conflict**. Terminal transitions record `completed_at` or `failed_at`.

### Service API

`AppRolloutService` (`gestaltd/internal/coredata/app_rollouts.go`):

| Method | Description |
|--------|-------------|
| `Get(ctx, app)` | Load the current rollout for one app. |
| `Create(ctx, rollout)` | Create a rollout when the current record is absent or terminal. |
| `ListActive(ctx)` | List rollouts in `enrolling` or `restarting`. |
| `MarkRestarting(ctx, app, version)` | Freeze the acknowledged cohort after enrollment. |
| `MarkComplete(ctx, app, version, completedAt)` | Record successful cohort convergence. |
| `MarkFailed(ctx, app, version, failedAt)` | Record that the rollout missed its deadline. |

---

## Store: `app_instance_materializations` (per-replica convergence)

Records one replica's acknowledgement and restart progress for a fleet-known `(app, version)`.

Primary key: `id` (UUID). Uniqueness for `(instance_id, app, version)` is enforced by the `by_instance_app_version` index.

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "instance_id": "cloud-run-revision-pod",
  "app": "g-issues",
  "version": "0.0.0-snapshot.gabc123",
  "acknowledged_at": "2026-07-13T21:00:00Z",
  "stopped_at": "2026-07-13T21:00:05Z",
  "restarted_at": "2026-07-13T21:01:05Z"
}
```

`instance_id` defaults to the process hostname (`os.Hostname()`).

### Service API

`AppInstanceMaterializationService` (`gestaltd/internal/coredata/app_instance_materializations.go`):

| Method | Description |
|--------|-------------|
| `HasAcknowledged(ctx, instanceID, app, version)` | Whether this replica already acked the pair. |
| `Get(ctx, instanceID, app, version)` | Load the per-replica materialization row. |
| `Acknowledge(ctx, materialization)` | Insert ack row; idempotent if already present. |
| `ListByAppVersion(ctx, app, version)` | List the replicas that acknowledged one rollout. |
| `MarkStopped(ctx, instanceID, app, version, stoppedAt)` | Record when the app provider was stopped for this fleet version. |
| `MarkRestarted(ctx, instanceID, app, version, restartedAt)` | Record when the app provider restart cycle completed for this fleet version. |

Written by the background catalog poller (`gestaltd/internal/appregistry/poller.go`).
