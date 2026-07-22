# App Registry Administration

Operator-facing visibility for registry-only apps and app-scoped fleet version selection.

Related docs:

- [plan.md](./plan.md) — implementation path
- [lifecycle.md](./lifecycle.md) — replica startup, background controller, existing admin HTTP API
- [indexeddb.md](./indexeddb.md) — `app_rollouts`, `app_instance_materializations`, change-request projections
- [validation.md](./validation.md) — validation before fleet accept
- [tests.md](./tests.md#admin-observability-tests) — observability, authorization, API, and UI tests

Implementation:

- Read handlers — `gestaltd/internal/server/handlers_admin_app_rollout.go`
- Admin UI — `gestaltd/services/ui/adminui/` (extend embedded `/admin` shell)
- App UI — `gestalt-providers/app/default/` (`/apps/{app}/admin`)
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
[App admin page](#app-admin-page).

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

## App admin page

App-resource administrators should be able to select the fleet-wide desired
version of a registry-only app without receiving access to the global Gestalt
admin surface.

The separate app-management page lives at:

```text
/apps/{app}/admin
```

The page selects a published version for one registry-only app. Selection is a
**fleet-wide** change: it updates the desired version projected from
`app_version_change_requests` and starts the normal multi-replica rollout. It
does not select a version for only the current user.

The selector supports:

- first install when the app has no fleet-known version
- upgrade to a newly published version
- revert to an older published version, including a version that was previously
  fleet-known

The embedded `/admin/registry` UI remains read-only. Step 15 does not add
install or upgrade controls to the global observability page.

### Cross-repository ownership

| Repository | Responsibility |
|------------|----------------|
| `gestalt` | App-scoped authorization, registry/version APIs, rollout admission, validation, and IndexedDB writes |
| `gestalt-providers` | `/apps/{app}/admin` route, version-selector UI, and management link from `/apps` |

The `/apps` UI is the root-mounted default app, not the embedded Gestalt admin
UI. Its implementation lives in `gestalt-providers`:

- `app/default/src/router.tsx`
- `app/default/src/pages/apps.tsx`
- `app/default/src/components/AppsCatalogPageClient.tsx`
- `app/default/src/components/IntegrationCard.tsx`
- `app/default/src/lib/api.ts`

### Authorization

Only an authenticated **user** with an explicit `admin` relationship on the
target app may load management data or select a version.

Authorization target:

```json
{
  "subject": "user:{user-id}",
  "relation": "admin",
  "resource": {
    "type": "app",
    "id": "g-issues"
  }
}
```

The API must:

1. Authenticate with the app's configured identity provider.
2. Resolve and canonicalize the caller to a user subject.
3. Reject service accounts, agents, workflows, and other non-user callers.
4. Query authorization relationships for `app/{app}`.
5. Require the exact `admin` role.

This check is fail-closed:

- **401** when the request is unauthenticated.
- **403** when the caller is authenticated but is not an app admin.
- **503** when authorization is not configured or the relationship check cannot
  be completed.

Do not reuse the mounted-UI fallback that allows access when no authorization
provider exists. Do not require `admin` on the global `gestaltAdmin` resource;
global admin access alone does not grant app version-management access.

`GET /api/v1/apps` should expose an optional `managementPath` only for a
registry-only app the caller can administer:

```json
{
  "name": "g-issues",
  "managementPath": "/apps/g-issues/admin"
}
```

The default UI renders **Manage app** only when `managementPath` is present.
Direct navigation still calls the protected management API and renders access
denied on **403**. Client-side route hiding is not the security boundary.

### App-management API

Routes live under the authenticated public API rather than
`/admin/api/v1`. They are available on the public Gestalt listener that serves
the default `/apps` UI.

#### `GET /api/v1/apps/{app}/admin/registry`

Return the data needed to render one selector.

**Response `200`**

```json
{
  "app": "g-issues",
  "registry": "toolshed",
  "desiredVersion": "0.0.0-snapshot.gabc123",
  "knownVersions": [
    {
      "version": "0.0.0-snapshot.gabc123",
      "installedAt": "2026-07-22T14:00:00Z",
      "installedBy": "user:alice"
    }
  ],
  "publishedVersions": [
    {
      "version": "0.0.0-snapshot.gdef456",
      "publishedAt": "2026-07-22T15:00:00Z",
      "platforms": ["linux/amd64"]
    },
    {
      "version": "0.0.0-snapshot.gabc123",
      "publishedAt": "2026-07-22T14:00:00Z",
      "platforms": ["linux/amd64"]
    }
  ],
  "rollout": {
    "version": "0.0.0-snapshot.gabc123",
    "state": "complete"
  },
  "selectionDisabled": false
}
```

When a rollout is active:

```json
{
  "selectionDisabled": true,
  "disabledReason": "rollout in progress"
}
```

Rules:

- `{app}` must be a deploy-configured registry-only app (`source.registry`).
- `knownVersions` comes from the change-request projection.
- `desiredVersion` is `LatestKnownVersion(knownVersions)` and is omitted before
  first install.
- `publishedVersions` comes from the configured registry index, newest
  `publishedAt` first.
- `selectionDisabled` is true only while rollout state is `enrolling` or
  `restarting`.
- A terminal `complete` or `failed` rollout does not disable selection.

#### `POST /api/v1/apps/{app}/admin/registry/version`

Select the fleet-wide desired version.

**Request**

```json
{
  "version": "0.0.0-snapshot.gdef456"
}
```

The request accepts no `actor`, `registry`, or `fromVersion`; unknown fields
return **400**. The server derives:

- `actor` from the canonical authenticated user subject
- `registry` from deploy `apps.{app}.source.registry`
- `fromVersion` from `LatestKnownVersion`

**Response `200`**

```json
{
  "app": "g-issues",
  "registry": "toolshed",
  "fromVersion": "0.0.0-snapshot.gabc123",
  "desiredVersion": "0.0.0-snapshot.gdef456",
  "rollout": {
    "version": "0.0.0-snapshot.gdef456",
    "state": "enrolling"
  }
}
```

Selection flow:

1. Authenticate and authorize `admin` on `app/{app}`.
2. Validate that `{app}` is registry-only and resolve its registry from deploy
   config.
3. Claim the existing app-scoped install lock.
4. Read the current rollout while holding the admission lock.
5. If rollout state is `enrolling` or `restarting`, return **409** before
   fetching registry metadata, validating the candidate, or writing IndexedDB.
6. Read the current desired version.
7. Reject selecting the current desired version with **400** and no writes.
8. Fetch and validate the selected published version using the existing
   install-time validator.
9. Create the rollout and append a change request using the canonical user
   subject as actor.
10. Release the install lock.

The first selection follows existing `add` semantics. Later selections follow
`upgrade` semantics.

#### Re-selecting an older known version

Reverting must work even when the selected version already appears in
`knownVersions`.

The existing duplicate-version rule must be narrowed:

- reject when selected version equals the **current desired version**
- allow a new change request whose `to_version` is an older, previously known
  version

The new request timestamp makes that version the latest desired selection while
the projection continues to return one entry per `(app, version)`.

Per-replica materialization rows are also keyed by `(instance, app, version)`.
Historical timestamps from the prior rollout of that version must not satisfy
the new rollout. On reconciliation:

- treat a row whose `acknowledged_at` predates `rollout.created_at` as stale
- reset its materialization, stop, restart, attempt, and error fields before
  acknowledging the new rollout
- count cohort membership and convergence only from timestamps at or after the
  current rollout's `created_at`

This forces each replica to validate/materialize and restart for the revert
instead of immediately completing from historical convergence records.

### Rollout guard

The UI guard is advisory:

- disable the selector and submit button while `selectionDisabled` is true
- display the active rollout version and state
- auto-refresh until the rollout becomes terminal

The server guard is authoritative. A rollout can start after the page loads, so
every selection request must recheck under the app-scoped install lock.
Concurrent requests may not both pass admission.

**Response `409`**

```json
{
  "error": "app rollout is active"
}
```

No registry fetch, validation, rollout creation, or change-request append occurs
for this rejection.

### UI

#### Apps catalog

An app-admin sees a **Manage app** link on the existing registry-app card.
Other users see the existing card without management controls.

#### App admin page

```text
┌─────────────────────────────────────────────────────────────┐
│ g-issues                                      App management │
│ registry: toolshed                                          │
├─────────────────────────────────────────────────────────────┤
│ Desired version                                             │
│ [ 0.0.0-snapshot.gabc123                         ▾ ]         │
│                                                             │
│ Published 2026-07-22 15:00 · linux/amd64                    │
│                                                             │
│                                      [ Select version ]      │
└─────────────────────────────────────────────────────────────┘
```

During an active rollout:

```text
│ Rollout enrolling: 0.0.0-snapshot.gdef456                  │
│ [ 0.0.0-snapshot.gabc123                         ▾ ] disabled│
│                                      [ Select version ] disabled
```

After a successful selection, show the new rollout state and keep selection
disabled until that rollout reaches `complete` or `failed`.

### Errors

| Status | When |
|--------|------|
| `400` | Invalid app/version; selected version is already desired; install-time validation failure |
| `401` | Missing or invalid authentication |
| `403` | Authenticated user lacks `admin` on `app/{app}` |
| `404` | App is not deploy-configured or is not registry-only; published version does not exist |
| `409` | Rollout is active; concurrent selection lost admission |
| `502` | Registry index or version metadata fetch failed |
| `503` | Authorization or registry installation services are unavailable |

Errors use the standard `{ "error": "…" }` envelope.

### Out of scope

- Selecting a version for only one user or one replica
- Canceling or force-completing a rollout
- Publishing versions from the UI
- Granting or editing app authorization relationships
- Adding mutation controls to `/admin/registry`

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
