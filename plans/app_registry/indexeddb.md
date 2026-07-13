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

## Source of truth vs projections

| Layer | Store / API | Role |
|-------|-------------|------|
| **Source of truth** | `app_version_change_requests` | Append-only fleet change requests per app (`from_version` → `to_version`) |
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
app_version_change_requests    ← source of truth (append-only)
app_version_install_locks      ← fleet install lock per (app, version)
```

Other host stores created at bootstrap include `users`, `managed_subjects`, `app_shas`, etc.

If a store already exists with a matching schema, creation is a no-op. A second bootstrap on the same database does not fail.

When `SkipSchemaBootstrap` is true (main-db is **delegated** to a remote gestaltd that already owns schema), `CreateObjectStore` is skipped on this instance. The remote owner must have created the stores already.

### What bootstrap does *not* do on startup

Bootstrap does **not** write change requests. After deploy the store is empty until an install request (or tests) append data.

Access install services after bootstrap via `Result.Services` / `prepared.Services`:

```text
Services.AppVersionChangeRequests   ← install prototype
Services.DB                         ← underlying main-db handle
```

---

## Store: `app_version_change_requests` (source of truth)

Append-only fleet requests to move one app from `from_version` to `to_version`. First install uses an empty `from_version`.

```text
app_version_change_requests
  - id
  - app                            # app name, e.g. g-issues
  - from_version                   # empty on first install
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
  "from_version": "",
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

## Store: `app_version_install_locks` (fleet install lock)

Short-lived lock rows that ensure only one `gestaltd` instance installs a given `(app, version)` at a time.

Primary key: `id` = `app` + `\x00` + `version`.

```json
{
  "id": "g-issues\u00000.0.0-snapshot.gabc123",
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
| `Acquire(ctx, app, version, holder, ttl)` | Claim lock; returns `ErrAppVersionInstallLockHeld` if another holder owns a non-expired lock |
| `Release(ctx, app, version, holder)` | Drop lock when this holder still owns it |

Used by `POST …/install` before download; released on success or failure.
