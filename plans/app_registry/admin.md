# App Registry Admin Observability

Operator-facing visibility for registry-managed apps: fleet-accepted versions, rollout progress, and per-replica convergence.

Related docs:

- [plan.md](./plan.md) — implementation path
- [lifecycle.md](./lifecycle.md) — replica startup, background controller, existing admin HTTP API
- [indexeddb.md](./indexeddb.md) — `app_rollouts`, `app_instance_materializations`, change-request projections
- [tests.md](./tests.md#admin-observability-tests) — planned HTTP and UI tests

Implementation (planned):

- Read handlers — `gestaltd/internal/server/handlers_admin_app_rollout.go` (new)
- Admin UI — `gestaltd/services/ui/adminui/` (extend embedded `/admin` shell)
- IndexedDB services — existing `AppRolloutService`, `AppInstanceMaterializationService`, `AppVersionChangeRequestService`

---

## Goals

Operators installing registry apps should answer these questions without reading pod logs or querying IndexedDB directly:

| Question | Primary source today | Planned admin surface |
|----------|----------------------|------------------------|
| Which apps are registry-managed? | Deploy `apps.*.source.registry` | Registry apps list |
| What version did the fleet accept? | `app_version_change_requests` projection | Known version on app detail |
| Is the rollout still in progress? | `app_rollouts` | Rollout status badge |
| Which replicas have converged? | `app_instance_materializations` | Replica convergence table |
| What is each replica running right now? | In-process map + local `active-version` file (not centralized) | Phase 1: inferred; phase 2: heartbeat |

---

## Current state

### Admin HTTP API

| Endpoint | Exposes |
|----------|---------|
| `GET /admin/api/v1/app-registries` | Configured registry names |
| `GET /admin/api/v1/app-registries/{registry}/apps/{app}/versions` | **Published** versions in GCS (not fleet state) |
| `GET /admin/api/v1/app-installations` | Fleet-**known** versions (change-request projection) |
| `GET /admin/api/v1/app-installations/{app}` | Known versions for one app |
| `POST …/add`, `POST …/upgrade` | Write fleet acceptance |

`app_rollouts` and `app_instance_materializations` are written by the catalog poller but have **no read HTTP routes** yet. The multi-replica E2E plan in [tests.md](./tests.md#multi-replica-materialization-ack-e2e) still refers to polling IndexedDB directly.

### Admin UI

The embedded UI at `/admin` ([`gestaltd/services/ui/adminui/`](../../gestaltd/services/ui/adminui/)) is a single-page Prometheus metrics viewer. It does not call the app-registry admin APIs.

---

## Terminology

Use the same names as [lifecycle.md](./lifecycle.md#runtime-version-invariants):

- **Fleet-known (desired)** — latest `(app, version)` from `app_version_change_requests` via `LatestKnownVersion`.
- **Rollout** — fleet-wide execution record in `app_rollouts` (`enrolling` → `restarting` → `complete` | `failed`).
- **Converged (per replica)** — this replica recorded `restarted_at` for the rollout's `(app, version)` in `app_instance_materializations`.
- **Running (per replica)** — this process registered the provider for a specific version. Not stored in IndexedDB today.

`restarted_at` means the replica reconciled through that catalog change. It does **not** guarantee the provider is still running that version after a crash or a later silent drift. The UI must label this distinction clearly.

---

## Planned admin HTTP API

All routes live under `/admin/api/v1`, reuse existing admin auth (`gestaltAdmin`), and are read-only except where install actions are triggered from the UI.

### `GET /admin/api/v1/registry-apps`

List deploy-configured apps whose `source.registry` is set, merged with fleet state.

**Response `200`**

```json
[
  {
    "app": "g-issues",
    "registry": "toolshed",
    "desiredVersion": "0.0.0-snapshot.gcd9d741cc35728476426afce6c069e198799a8be",
    "rollout": {
      "version": "0.0.0-snapshot.gcd9d741cc35728476426afce6c069e198799a8be",
      "state": "complete",
      "createdAt": "2026-07-21T17:06:12Z",
      "enrollmentEndsAt": "2026-07-21T17:08:12Z",
      "deadline": "2026-07-21T17:21:12Z",
      "completedAt": "2026-07-21T17:09:06Z"
    },
    "cohort": {
      "acknowledged": 3,
      "materialized": 3,
      "restarted": 3,
      "failed": 0
    }
  }
]
```

| Field | Description |
|-------|-------------|
| `app` | App name from deploy `apps` |
| `registry` | `source.registry` binding |
| `desiredVersion` | `LatestKnownVersion`; omitted when the fleet catalog is empty |
| `rollout` | Current or most recent rollout for this app; omitted when none |
| `cohort` | Counts over `app_instance_materializations` rows for `rollout.version` that acknowledged before `enrollmentEndsAt` |

Apps with no fleet-known version still appear when configured with `source.registry` so operators can see "not installed yet."

Implementation reads deploy `AppDefs`, `app_version_change_requests`, `app_rollouts`, and aggregates materialization rows. No GCS fetch.

---

### `GET /admin/api/v1/registry-apps/{app}`

Detail view for one registry-managed app. Same fields as one list element, plus:

```json
{
  "app": "g-issues",
  "registry": "toolshed",
  "desiredVersion": "0.0.0-snapshot.gcd9d741…",
  "knownVersions": [
    {
      "version": "0.0.0-snapshot.gcd9d741…",
      "installedAt": "2026-07-21T17:06:12Z",
      "installedBy": "user:michael.wang@valon.com"
    }
  ],
  "rollout": { },
  "latestPublished": {
    "version": "0.0.0-snapshot.gcd9d741…",
    "publishedAt": "2026-07-21T15:36:47Z"
  }
}
```

`latestPublished` is optional. The handler may call the existing registry index fetch (`GET …/app-registries/{registry}/apps/{app}/versions`) and return the newest `publishedAt` entry so operators can compare **published** vs **accepted** vs **converged**.

**Response `404`** when `{app}` is not a registry-managed app in deploy config.

---

### `GET /admin/api/v1/app-rollouts`

List rollout records. Default: active rollouts (`enrolling`, `restarting`) plus terminal rollouts from the last 24 hours. Support `?app={app}` and `?state={state}` filters.

**Response `200`**

```json
[
  {
    "app": "g-issues",
    "version": "0.0.0-snapshot.gcd9d741…",
    "state": "restarting",
    "createdAt": "2026-07-21T17:06:12Z",
    "enrollmentEndsAt": "2026-07-21T17:08:12Z",
    "deadline": "2026-07-21T17:21:12Z"
  }
]
```

Backed by `AppRolloutService.ListActive` and a new `ListRecentTerminal` (or `Get` + history index if added later).

---

### `GET /admin/api/v1/app-rollouts/{app}/materializations`

Per-replica convergence rows for one app rollout.

**Query parameters**

| Parameter | Description |
|-----------|-------------|
| `version` | Published version. Defaults to the current rollout's `version`, else `desiredVersion`. |

**Response `200`**

```json
{
  "app": "g-issues",
  "version": "0.0.0-snapshot.gcd9d741…",
  "rolloutState": "complete",
  "materializations": [
    {
      "instanceId": "gestaltd-8d9487869-ncnq6",
      "acknowledgedAt": "2026-07-21T17:08:12Z",
      "materializedAt": "2026-07-21T17:08:57Z",
      "stoppedAt": "2026-07-21T17:08:57Z",
      "restartedAt": "2026-07-21T17:08:58Z",
      "attemptCount": 0,
      "lastErrorAt": null,
      "lastErrorMessage": "",
      "inCohort": true,
      "converged": true
    }
  ]
}
```

| Field | Description |
|-------|-------------|
| `inCohort` | `acknowledged_at < rollout.enrollment_ends_at` |
| `converged` | `restarted_at` set and not after `rollout.deadline` while rollout is active; when terminal, `restarted_at` present |

Backed by `AppInstanceMaterializationService.ListByAppVersion`. This replaces direct IndexedDB polling in tests and runbooks.

---

## Admin UI

Extend the embedded `/admin` shell. Keep the existing metrics page; add an **App Registry** section.

### Navigation

```text
/admin
├── Metrics          (existing)
└── App Registry
    ├── Apps list
    └── App detail: {app}
```

Use the same session auth as other admin routes. Fetch JSON from the admin API with `credentials: "include"` (session cookie) or document bearer-token use for CLI-only workflows.

### Apps list (`/admin/registry`)

Table columns:

| App | Registry | Desired version | Rollout | Cohort |
|-----|----------|-----------------|---------|--------|
| g-issues | toolshed | `0.0.0-snapshot.g…` | complete | 3/3 restarted |

Empty catalog: show configured registry apps with desired version "—" and a primary action **Install** (opens version picker fed by `…/versions`).

### App detail (`/admin/registry/{app}`)

Sections:

1. **Summary** — registry binding, desired version, latest published version (if fetched), install metadata (`installedBy`, `installedAt`).
2. **Rollout** — state badge, timestamps, enrollment deadline, failure reason when `failed`.
3. **Replicas** — materializations table (`instanceId`, ack / materialized / stopped / restarted, `attemptCount`, last error). Sort by `instanceId`.
4. **Actions** — **Upgrade** when a newer published version exists; disabled while rollout is `enrolling` or `restarting`.

Auto-refresh every 10–15s while rollout is non-terminal.

### Wireframe (app detail)

```text
┌─────────────────────────────────────────────────────────────┐
│ g-issues                                    rollout: complete │
│ registry: toolshed                                          │
├─────────────────────────────────────────────────────────────┤
│ Desired:  0.0.0-snapshot.gcd9d741…   installed 2026-07-21 …  │
│ Published latest: 0.0.0-snapshot.gcd9d741… (same)           │
├─────────────────────────────────────────────────────────────┤
│ Replicas                                                    │
│ instance_id              mat.   restart   attempts  error   │
│ gestaltd-…-ncnq6         ✓     ✓         0                   │
│ gestaltd-…-hdnx2         ✓     ✓         0                   │
│ gestaltd-…-smmq7         ✓     ✓         0                   │
├─────────────────────────────────────────────────────────────┤
│ [ Upgrade to … ]  (disabled when rollout active)            │
└─────────────────────────────────────────────────────────────┘
```

---

## Out of Scope: Runtime Heartbeats

To answer "what is each replica running **right now**" reliably:

1. Add optional store `app_registry_runtime_state` (or extend materialization rows with `observed_version` + `observed_at`).
2. On each catalog poll pass (and after bootstrap `StartApp`), upsert `{ instance_id, app, observed_version, observed_at }` when the in-process running-version map matches the materialized package.
3. Expose via `GET …/runtime` or inline on materialization rows as `observedVersion` / `observedAt`.
4. Stale `observed_at` (for example older than 3× poll interval) renders as **stale** in the UI.

Phase 1 ships without this store. Convergence columns are sufficient for rollout debugging; heartbeats close the gap for live runtime audits.

---

## Errors

Reuse the standard admin error envelope. New cases:

| Status | When |
|--------|------|
| `404` | `{app}` is not registry-managed in deploy config; no rollout when explicitly requested |
| `503` | `AppRollouts` or `AppInstanceMaterializations` service unavailable |

---

## Out of scope

- Install-time validation ([validation.md](./validation.md))
- Dedicated rollback route (revert via `upgrade` to an older published version)
- Mutating rollouts from the UI (cancel, force-complete)
- Publishing to GCS from the UI
- Replacing `kubectl logs` for provider crash diagnostics
