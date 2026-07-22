# App Registry Admin Observability

Operator-facing visibility for registry-only apps: fleet-known versions, rollout progress, and per-replica convergence.

Related docs:

- [plan.md](./plan.md) — implementation path
- [lifecycle.md](./lifecycle.md) — replica startup, background controller, existing admin HTTP API
- [indexeddb.md](./indexeddb.md) — `app_rollouts`, `app_instance_materializations`, change-request projections
- [tests.md](./tests.md#admin-observability-tests) — HTTP and UI tests
- [version_selection.md](./version_selection.md) — app-scoped version selection outside `/admin`

Implementation:

- Read handlers — `gestaltd/internal/server/handlers_admin_app_rollout.go`
- Admin UI — `gestaltd/services/ui/adminui/` (extend embedded `/admin` shell)
- IndexedDB services — `AppRolloutService`, `AppInstanceMaterializationService`, `AppVersionChangeRequestService`

---

## Goals

Operators installing registry apps should answer these questions without reading pod logs or querying IndexedDB directly:

| Question | Primary source today | Planned admin surface |
|----------|----------------------|------------------------|
| Which apps are registry-only? | Deploy `apps.*.source.registry` | Registry-only apps list |
| What is the desired fleet-known version? | `app_version_change_requests` projection (`LatestKnownVersion`) | Desired version on app detail |
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
| `GET /admin/api/v1/registry-apps` | Registry-only apps merged with desired version and rollout summary |
| `GET /admin/api/v1/registry-apps/{app}` | Registry-only app detail and known versions |
| `GET /admin/api/v1/app-rollouts` | Active and recent terminal rollouts |
| `GET /admin/api/v1/app-rollouts/{app}/materializations` | Per-replica rollout progress |
| `POST …/add`, `POST …/upgrade` | Append fleet-known versions (change requests) |

`app_rollouts` and `app_instance_materializations` are written by the catalog poller and exposed through read-only admin routes. The multi-replica E2E plan in [tests.md](./tests.md#planned-multi-replica-materialization-ack-e2e) can poll `GET …/materializations` without querying IndexedDB directly.

### Admin UI

The embedded UI at `/admin` ([`gestaltd/services/ui/adminui/`](../../gestaltd/services/ui/adminui/)) keeps the Prometheus metrics viewer and adds an App Registry section backed by the admin APIs.

The embedded admin UI remains observability-only. Fleet version selection is a
separate app-scoped capability at `/apps/{app}/admin`; see
[version_selection.md](./version_selection.md).

---

## Terminology

Use the same names as [lifecycle.md](./lifecycle.md#runtime-version-invariants):

- **Fleet-known** — an accepted `(app, version)` projected from `app_version_change_requests`.
- **Desired version** — latest fleet-known version for an app (`LatestKnownVersion`); JSON field `desiredVersion`.
- **Rollout** — fleet-wide execution record in `app_rollouts` (`enrolling` → `restarting` → `complete` | `failed`).
- **Converged** — the poller recorded `restarted_at` for the replica and version, meaning the replica reconciled through that catalog change. This is rollout accounting, not proof that the provider ran that exact version or is currently running it.
- **Running** — this replica successfully built and registered the provider from that exact materialized package. Not stored in IndexedDB today.

`restarted_at` on a rollout-progress row means the replica reconciled through that catalog change. It does **not** guarantee the provider is still running that version after a crash or a later silent drift. The UI must label this distinction clearly.

---

## Admin HTTP API

All routes live under `/admin/api/v1`, reuse existing admin auth (`gestaltAdmin`), and are read-only.

#### `GET /admin/api/v1/registry-apps`

List deploy-configured registry-only apps (`source.registry`), merged with fleet state.

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

| Field | Type | Description |
|-------|------|-------------|
| `app` | string | App name from deploy `apps` |
| `registry` | string | `source.registry` binding |
| `desiredVersion` | string | `LatestKnownVersion`; omitted when the fleet catalog is empty |
| `rollout` | object | Current or most recent rollout for this app; omitted when none |
| `cohort` | object | Counts over `app_instance_materializations` rows for `rollout.version` that acknowledged before `enrollmentEndsAt` |

Apps with no fleet-known version still appear when configured with `source.registry` so operators can see "not installed yet."

IndexedDB read only. No GCS fetch.

#### `GET /admin/api/v1/registry-apps/{app}`

Detail view for one registry-only app. Same fields as one list element, plus:

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

`knownVersions` lists fleet-known `(app, version)` pairs projected from `app_version_change_requests`.

`latestPublished` is optional. The handler may call the existing registry index fetch (`GET …/app-registries/{registry}/apps/{app}/versions`) and return the newest `publishedAt` entry so operators can compare **published** vs **fleet-known** vs **converged**.

**Response `404`** when `{app}` is not a registry-only app in deploy config.

#### `GET /admin/api/v1/app-rollouts`

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

Backed by `AppRolloutService.ListActiveAndRecentTerminal`, which reads one store snapshot so a state transition cannot duplicate an app across active and terminal results.

#### `GET /admin/api/v1/app-rollouts/{app}/materializations`

Per-replica rollout-progress rows for one app rollout.

**Query parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `version` | string | Fleet-known version. Defaults to the current rollout's `version`, else `desiredVersion`. |

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

| Field | Type | Description |
|-------|------|-------------|
| `inCohort` | boolean | `acknowledged_at < rollout.enrollment_ends_at` |
| `converged` | boolean | `restarted_at` set and not after `rollout.deadline` while rollout is active; when terminal, `restarted_at` present |

Backed by `AppInstanceMaterializationService.ListByAppVersion`. This replaces direct IndexedDB polling in tests and runbooks.

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

Empty fleet-known projection: show configured registry-only apps with desired version "—" and status "not installed."

### App detail (`/admin/registry/{app}`)

Sections:

1. **Summary** — registry binding, desired version, latest published version (if fetched), install metadata (`installedBy`, `installedAt`).
2. **Rollout** — state badge, timestamps, enrollment deadline, failure reason when `failed`.
3. **Replicas** — rollout-progress rows (`instanceId`, ack / materialized / stopped / restarted, `attemptCount`, last error). Sort by `instanceId`.

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
│ instanceId               mat.   restart   attempts  error   │
│ gestaltd-…-ncnq6         ✓     ✓         0                   │
│ gestaltd-…-hdnx2         ✓     ✓         0                   │
│ gestaltd-…-smmq7         ✓     ✓         0                   │
└─────────────────────────────────────────────────────────────┘
```

---

## Out of Scope: Runtime Heartbeats

To answer "what is each replica running **right now**" reliably:

1. Add optional store `app_registry_runtime_state` (or extend materialization rows with `observed_version` + `observed_at`).
2. On each catalog poll pass (and after bootstrap `StartApp`), upsert `{ instance_id, app, observed_version, observed_at }` when the in-process running-version map matches the materialized package.
3. Expose via `GET …/runtime` or inline on materialization rows as `observedVersion` / `observedAt`.
4. Stale `observed_at` (for example older than 3× poll interval) renders as **stale** in the UI.

Convergence columns (`restarted_at`, `materialized_at`) are enough to debug whether each replica completed a rollout. They are not enough when a replica later drifts from that recorded state — for example, the provider crashes after `restarted_at` was written, the process restarts on an older on-disk package, or a manual local change leaves the running binary out of sync with the fleet-known version. In those cases the rollout-progress row still looks converged even though the replica is no longer serving the desired version.

---

## Errors

Reuse the standard admin error envelope from [lifecycle.md](./lifecycle.md#errors). Observability routes add:

| Status | When |
|--------|------|
| `404` | `{app}` is not a registry-only app in deploy config; no rollout when explicitly requested |
| `503` | `AppRollouts` or `AppInstanceMaterializations` service unavailable |

---

## Out of scope

- Install-time validation ([validation.md](./validation.md))
- Dedicated rollback route (revert via `upgrade` to an older published version)
- Installing or upgrading apps from the embedded `/admin` UI
- Mutating rollouts from the UI (cancel, force-complete)
- Publishing to GCS from the UI
- Replacing `kubectl logs` for provider crash diagnostics
