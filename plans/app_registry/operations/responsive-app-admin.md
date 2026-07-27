# Responsive App Admin Polling

Plan for changelog step **20**. Improves `/apps/{app}/admin` responsiveness during publish and rollout without changing GCS write paths or install behavior.

**Status:** planned (not shipped)

## Problem

Today the app admin UI polls `GET /api/v1/apps/{app}/admin/registry` every **12 seconds** while publish or rollout is active. Each request makes gestaltd fetch four GCS documents (`index.json`, `pending.json`, `failed.json`, `retention.json`) plus IndexedDB reads for fleet state.

Two UX gaps follow:

1. **Slow publish visibility** — a CI-recorded pending row can take up to ~12s to appear after page load, and publish completion can take another ~12s to flip from **Publishing** to **Available**.
2. **Frozen duration labels** — `Publishing · for 4m` only updates when the next poll returns. `publishingForSeconds` is computed server-side at request time, so the label jumps in 12-second steps even though `startedAt` is already on the row.

Scope is `/apps/{app}/admin` only. The embedded fleet `/admin/registry` UI is out of scope for this milestone unless we later reuse the same polling helpers.

## Goals

| Goal | Target |
| --- | --- |
| Pending row appears after CI `pending set` | ≤ **3s** after page load (p95) |
| Publish completion reflected in snapshots table | ≤ **3s** after `pending clear` + `index.json` update |
| Publish-duration label | Updates every **1s** locally while a row is **Publishing** |
| GCS reads per active poll | **1–2** objects instead of **4** |

## Non-Goals

- Push notifications (SSE, WebSocket, GCS Pub/Sub)
- Server-side caching of registry documents
- Faster catalog poller (replica convergence remains ~1 minute)
- Embedded `/admin/registry` list auto-refresh (still one-shot today)
- Polling solely because `failedVersions` is non-empty (unchanged)

---

## Design

### 1. Lightweight status endpoint

Add a read-only app-admin route that returns only the mutable publish catalogs and fleet control state gestaltd already derives for the full registry response.

```
GET /api/v1/apps/{app}/admin/registry/status
```

**Auth:** same middleware as `GET …/admin/registry` (`app` plugin route + `admin` on `app/{app}`).

**GCS fetches:**

| Document | Fetch |
| --- | --- |
| `pending.json` | yes |
| `failed.json` | yes |
| `index.json` | no |
| `retention.json` | no |

**IndexedDB reads:**

| Store | Fields |
| --- | --- |
| `app_version_change_requests` | `desiredVersion` (via `LatestKnownVersion`) |
| `app_rollouts` | active rollout `version` + `state` |
| — | `selectionDisabled` / `disabledReason` (same rules as full handler) |

**Response shape:**

```json
{
  "pendingVersions": [ /* same row shape as full registry */ ],
  "failedVersions": [ /* same row shape as full registry */ ],
  "desiredVersion": "0.0.0-snapshot.gabc123",
  "rollout": { "version": "…", "state": "enrolling" },
  "selectionDisabled": false,
  "disabledReason": ""
}
```

Reuse the existing `appAdminPendingVersion` / `appAdminFailedVersion` structs and admin-catalog merge helpers from `handlers_app_admin_registry.go`. Do not duplicate merge precedence logic in the UI.

### UI lifecycle

The page holds one **registry snapshot**. The server returns facts; the UI derives polling mode and escalation — there is no `pollingMode` on the app.

| Layer | Endpoint | When |
| --- | --- | --- |
| **Full** | `GET …/admin/registry` | Page load, deploy click, **escalation** |
| **Status** | `GET …/admin/registry/status` | **Active** poll mode (3s) |
| **Local clock** | — | Pending rows only (1s; uses `startedAt`) |

#### Poll mode

Per open tab only — closing the tab stops all polling. There is no server-side idle state.

| What you see | Mode | Background refresh |
| --- | --- | --- |
| Page just opened; waiting to see if a publish starts | **Landing** | Full snapshot every **12s** (first 5 minutes) |
| A publish, rollout, or deploy lock is in progress | **Active** | Lightweight status every **3s**; **Publishing · for …** ticks every **1s** locally |
| Quiet page — nothing moving, been open > 5 minutes | **Quiet** | None — table is static until you refresh or click Deploy |
| Tab closed | — | Nothing runs |

**Landing** ends after 5 minutes or as soon as an active signal appears (whichever comes first).

**Active signals** (from the API — not a mode field on the app):

- A row is **Publishing** (`pendingVersions` non-empty)
- Rollout banner shows **Enrolling** or **Restarting**
- Deploy buttons are disabled (`selectionDisabled`)

#### Escalation

**Escalation** is not a poll mode. It is a one-time **full** fetch during **active** status polling when the status response changes in a way status alone cannot reflect in the table (published rows, deploy buttons, **Deployed** badge).

Compare the new status response to the previous one. Escalate if any of:

| Diff | Example |
| --- | --- |
| Pending version removed | Publish completed |
| New failed version | Workflow failed or stale prune |
| Rollout became terminal | `enrolling`/`restarting` → `complete`/`failed` |
| `desiredVersion` changed | Deploy landed |

After escalation, recompute poll mode from the updated snapshot.

### 2. Polling implementation

Constants: `POLL_INTERVAL_ACTIVE_MS = 3_000`, `POLL_INTERVAL_LANDING_MS = 12_000` (rename from `POLL_INTERVAL_IDLE_MS`).

Implement `computePollMode` (`landing` | `active` | `quiet`) and `shouldEscalateToFullRegistry` in `gestalt-providers/app/default/src/features/registry/polling.ts`. Keep `APP_ADMIN_BOOTSTRAP_POLL_MS` as the landing-window duration (internal name; user-facing docs say **landing**).

Pause polling when `document.visibilityState === "hidden"`; catch up once when visible again.

### 3. Continuous duration labels (local clock)

Separate **data freshness** (3s / 12s polls) from **display freshness** (1s local tick).

**Principle:** server fields name *when* something started; the UI computes *how long ago* from `startedAt` + client `now`. Stop sending `publishingForSeconds` on status responses once the UI owns the live clock (keep on full registry for backward compatibility during rollout).

**Hook:** `useLiveNow({ enabled, intervalMs = 1_000 })`

- `enabled` when the snapshots table has at least one `pending` row, or optionally when any `formatRegistryTimeAgo` label is shown and the page is visible.
- Returns `now` (ms) updated every second via `setInterval`.
- Clears interval when `enabled` is false or the tab is hidden.
- Uses `document.visibilityState` to avoid background timers.

**Wire into existing formatters** — `snapshotStatusTimer` and `snapshotLastUpdatedLabel` already accept an optional `now` argument in `snapshot-rows.ts`. Pass `liveNow` from the table (or a parent provider) instead of defaulting to `Date.now()` only at render time without a tick.

```ts
// app-admin-snapshots-table.tsx
const liveNow = useLiveNow({
  enabled: rows.some((row) => row.kind === "pending"),
});

const statusTimer = snapshotStatusTimer(row, liveNow);
const lastUpdated = snapshotLastUpdatedLabel(row, liveNow);
```

**Display rules:**

| Row | Label | Source |
| --- | --- | --- |
| **Publishing** | `for 4m 12s` | `durationSecondsBetween(pending.startedAt, liveNow)` |
| **Available** (published) | `Published in 4m 32s` | static (`publishDurationSeconds` or `publishStartedAt` → `publishedAt`) |
| **Failed** | `Failed after 35m` | static |
| **Last update** (pending) | `4 minutes ago` | `formatRegistryTimeAgo(pending.updatedAt, liveNow)` — ticks during publish |

Published and failed duration sublabels stay static; only in-flight publish labels and pending last-update relatives need the live clock.

**Do not** add a 1s poll to gestaltd. The continuous look is entirely client-side.

---

## API Summary

| Route | GCS | IndexedDB | Purpose |
| --- | --- | --- | --- |
| `GET …/admin/registry` | index, pending, failed, retention | desired, rollout | Full snapshots + deploy actions |
| `GET …/admin/registry/status` | pending, failed | desired, rollout, selection | Active polling |
| `GET …/admin/registry/history` | index, retention | change requests | Unchanged (lazy tab load) |
| `POST …/admin/registry/version` | index (validation) | write | Unchanged |

Document the status route in [lifecycle.md](./lifecycle.md#app-admin-version-selection) alongside the existing app-admin registry handlers.

---

## Client helpers

**`mergeAppAdminRegistryStatus`** — apply a status response onto the current full snapshot (pending, failed, rollout, selection fields only). Run `shouldEscalateToFullRegistry` before merge; on escalation, fetch full instead.

```ts
function mergeAppAdminRegistryStatus(
  current: AppAdminRegistryResponse,
  status: AppAdminRegistryStatusResponse,
): AppAdminRegistryResponse {
  return {
    ...current,
    pendingVersions: status.pendingVersions,
    failedVersions: status.failedVersions,
    desiredVersion: status.desiredVersion,
    rollout: status.rollout,
    selectionDisabled: status.selectionDisabled,
    disabledReason: status.disabledReason,
  };
}
```

---

## Implementation Checklist

### gestalt

- [ ] `GET /api/v1/apps/{app}/admin/registry/status` handler in `handlers_app_admin_registry.go`
- [ ] Mount route in `mountAppAdminRegistryRoutes`
- [ ] Unit tests: empty pending, pending rows with `publishingForSeconds`, failed merge precedence, rollout + `selectionDisabled` flags, 403/404 parity with full handler
- [ ] Lifecycle doc: request/response shapes and GCS read set

### gestalt-providers

- [ ] `getAppAdminRegistryStatus` in `lib/api.ts` + `AppAdminRegistryStatusResponse` type
- [ ] `computePollMode`, `shouldEscalateToFullRegistry`, `mergeAppAdminRegistryStatus` in `polling.ts`
- [ ] `useLiveNow` hook (e.g. `hooks/use-live-now.ts`)
- [ ] Refactor `AppAdminPageClient.tsx`: interval polling, status/full selection, escalation, visibility pause
- [ ] Pass `liveNow` into `AppAdminSnapshotsTable` → `snapshotStatusTimer` / `snapshotLastUpdatedLabel`
- [ ] Update `e2e/app-admin-mock.spec.ts`: pending visibility timeout **15s → 5s**; add live-clock unit test for `snapshotStatusTimer`

### docs (on ship)

- [ ] [pending-publish.md](./pending-publish.md) — polling section
- [ ] [admin.md](./admin.md) — app admin capabilities / timing
- [ ] [changelog.md](../project/changelog.md) — step `20`
- [ ] [tests.md](../project/tests.md) — status endpoint + UI tests

---

## Tests

### Server (`handlers_app_admin_registry_test.go`)

| Test | Asserts |
| --- | --- |
| `TestGetAppAdminRegistryStatus/pending_only` | Returns pending rows; does not call `FetchAppIndex` / `FetchRetentionIndex` (mock reader records fetch paths) |
| `TestGetAppAdminRegistryStatus/merge_precedence` | Published version in index suppresses pending/failed of same version when handler is tested with fixture that has both |
| `TestGetAppAdminRegistryStatus/rollout_and_selection` | IndexedDB rollout and `selectionDisabled` match full handler |
| `TestGetAppAdminRegistryStatus/forbidden` | **403** without registry metadata leak |

### UI unit

| Test | Asserts |
| --- | --- |
| `computePollMode` | `active` with pending/rollout; `landing` within 5 min only; `quiet` otherwise |
| `shouldEscalateToFullRegistry` | pending cleared, new failed, terminal rollout, desired version change |
| `snapshotStatusTimer` with advancing `now` | `for 59s` → `for 1m` → `for 1m 1s` without new API data |

### E2E (`app-admin-mock.spec.ts`)

| Test | Asserts |
| --- | --- |
| `polls status endpoint during publish` | Route mock distinguishes `…/registry/status` from full `…/registry`; pending row within **5s** |
| `escalates to full registry when publish completes` | After pending cleared in status mock, full endpoint called and **Available** row appears |
| `publishing timer ticks between polls` | With frozen API, label changes over 2s (unit-level Playwright or component test) |

---

## Rollout / Safety

| Invariant | Preserved how |
| --- | --- |
| Pending/failed not installable | Install handlers unchanged |
| Published > pending > failed | Same server-side merge on status endpoint |
| No GCS writes from `gestaltd serve` | Status is read-only |
| Failed rows on first load | Initial full fetch still includes `failedVersions`; status polls do not start solely for failed |

Deploy gestalt (new route) before or with gestalt-providers (UI). Old UI against new gestalt: works (ignores unknown route). New UI against old gestalt: status calls return **404** — ship together or gate UI on route probe.

---

## Future Work (out of scope)

- IndexedDB-only status variant during rollout (zero GCS) when no pending/failed catalogs are relevant
- Short TTL registry cache in gestaltd to allow sub-second polling without GCS amplification
- SSE push on `pending set` / rollout transitions
- Embedded `/admin/registry` adaptive polling (align with [admin.md](./admin.md#apps-list-adminregistry))

---

## Related Docs

<pre>
├── <a href="../project/changelog.md">changelog.md</a> — step 20 (on ship)
├── <a href="./pending-publish.md">pending-publish.md</a> — publish catalogs and current 12s polling
├── <a href="./admin.md">admin.md</a> — app admin UI wireframes
├── <a href="./lifecycle.md">lifecycle.md</a> — app-admin HTTP APIs
└── <a href="../project/tests.md">tests.md</a> — test index (extend on ship)
</pre>
