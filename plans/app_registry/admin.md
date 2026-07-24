# App Registry Administration

Operator-facing visibility for registry-only apps and app-scoped fleet version selection.

Related docs:

- [plan.md](./plan.md) — implementation path
- [lifecycle.md](./lifecycle.md) — HTTP APIs, admission checks, and rollout behavior
- [indexeddb.md](./indexeddb.md) — `app_rollouts`, `app_instance_materializations`, change-request projections
- [pending-publish.md](./pending-publish.md) — in-flight publish visibility on app admin
- [tests.md](./tests.md#admin-observability-tests) — observability HTTP and UI tests; [app version selection tests](./tests.md#app-version-selection-tests)

---

## Goals

Operators installing registry apps should answer these questions without reading pod logs or querying IndexedDB directly:

| Question | Admin surface |
|----------|---------------|
| Which apps are registry-only? | Embedded admin apps list |
| What is the desired fleet-known version? | Desired version on app detail |
| Is the rollout still in progress? | Rollout status badge |
| Which replicas have converged? | Replica convergence table |

App admins additionally select the fleet-wide desired version and inspect what
published each candidate version.

---

## Terminology

Use the same names as [lifecycle.md](./lifecycle.md#runtime-version-invariants):

- **Fleet-known** — an accepted `(app, version)` projected from `app_version_change_requests`.
- **Desired version** — latest fleet-known version for an app (`LatestKnownVersion`).
- **Rollout** — fleet-wide execution record in `app_rollouts` (`enrolling` → `restarting` → `complete` | `failed`).
- **Converged** — the poller recorded `restarted_at` for the replica and version. Rollout accounting, not proof the provider is still running that version.

Label **converged** as rollout progress, not current runtime state.

---

## Embedded admin UI (`/admin`)

Read-only fleet observability for registry-only apps. Requires global
`gestaltAdmin`. API shapes: [lifecycle.md](./lifecycle.md#admin-observability-api).

The embedded shell at `/admin` keeps the Prometheus metrics viewer and adds an
**App Registry** section. It does not install, upgrade, publish, or mutate
rollouts.

### Navigation

```text
/admin
├── Metrics          (existing)
└── App Registry
    ├── Apps list
    └── App detail: {app}
```

### Apps list (`/admin/registry`)

| App | Registry | Desired version | Rollout | Cohort |
|-----|----------|-----------------|---------|--------|
| g-issues | toolshed | `0.0.0-snapshot.g…` | complete | 3/3 restarted |

Show configured registry-only apps even when the fleet catalog is empty (desired
version "—", status "not installed").

Auto-refresh every 10–15s while any listed rollout is non-terminal.

### App detail (`/admin/registry/{app}`)

1. **Summary** — registry binding, desired version, latest published version (if available), install metadata (`installedBy`, `installedAt`).
2. **Rollout** — state badge, timestamps, enrollment deadline, failure reason when `failed`.
3. **Replicas** — per-replica rollout progress (`instanceId`, ack / materialized / stopped / restarted, `attemptCount`, last error). Sort by `instanceId`.

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

## App admin UI (`/apps/{app}/admin`)

Fleet version selection for one registry-only app. Requires `admin` on
`app/{app}`. API shapes and admission checks:
[lifecycle.md](./lifecycle.md#app-admin-version-selection).

Implemented in `gestalt-providers` (default `/apps` UI), not the embedded
`/admin` shell.

### Capabilities

- **Manage app** on the `/apps` catalog when the caller can administer that app.
- Select the fleet-wide desired version: first install, upgrade, or revert to an older published version.
- Show per-version `publishedAt`, linked source commit, triggering PR or commit, and publishing workflow run.
- Legacy published versions without workflow metadata still link the commit and show **not recorded** for workflow/PR fields.
- Disable the selector while a rollout is `enrolling` or `restarting`; refresh until terminal.
- Render access denied on **403** without leaking registry metadata.
- Show in-flight publishes with status **Publishing** and recent failures with
  status **Failed** while CI is running or after a failed attempt. See
  [pending-publish.md](./pending-publish.md).

Selection is fleet-wide. It is not per-user or per-replica.

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
│ Commit def456 · PR #3251 · workflow run                     │
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

After a successful selection, show the new rollout state and keep the selector
disabled until that rollout reaches `complete` or `failed`.

---

## Out of scope

- Per-replica observed running version (runtime heartbeats)
- Installing or upgrading from the embedded `/admin` UI
- Publishing indicator on the embedded `/admin` registry list (app-scoped
  `/apps/{app}/admin` only; see [pending-publish.md](./pending-publish.md))
- Canceling or force-completing a rollout from either UI
- Publishing versions from either UI
- Granting or editing app authorization relationships
- Selecting a version for only one user or one replica
- Replacing `kubectl logs` for provider crash diagnostics
