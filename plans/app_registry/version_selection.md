# App Registry Version Selection

App-resource administrators should be able to select the fleet-wide desired
version of a registry-only app without receiving access to the global Gestalt
admin surface.

Related docs:

- [plan.md](./plan.md) — implementation path
- [lifecycle.md](./lifecycle.md) — install admission and replica convergence
- [admin.md](./admin.md) — read-only global rollout observability
- [validation.md](./validation.md) — validation before fleet accept
- [tests.md](./tests.md#app-version-selection-tests) — authorization, API, and UI tests

---

## Scope

Add a separate app-management page at:

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

---

## Authorization

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

---

## App-management API

Routes live under the authenticated public API rather than
`/admin/api/v1`. They are available on the public Gestalt listener that serves
the default `/apps` UI.

### `GET /api/v1/apps/{app}/admin/registry`

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

### `POST /api/v1/apps/{app}/admin/registry/version`

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

### Re-selecting an older known version

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

---

## Rollout guard

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

---

## UI

### Apps catalog

An app-admin sees a **Manage app** link on the existing registry-app card.
Other users see the existing card without management controls.

### App admin page

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

---

## Errors

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

---

## Out of scope

- Selecting a version for only one user or one replica
- Canceling or force-completing a rollout
- Publishing versions from the UI
- Granting or editing app authorization relationships
- Adding mutation controls to `/admin/registry`
