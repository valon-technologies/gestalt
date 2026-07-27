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

The page keeps one **registry snapshot** in memory (published rows, pending, rollout, deploy buttons). Two APIs feed it:

| API | Purpose | Poll cadence |
| --- | --- | --- |
| **Full** `GET …/admin/registry` | Complete snapshot — what is published, deployable, and locked | On load, after deploy, and on **escalation** |
| **Status** `GET …/admin/registry/status` | In-flight state only — pending, failed, rollout, desired version | Every **3s** while something is moving |
| **Local clock** | Smooth duration labels (`Publishing · for 4m 12s`) | Every **1s** while a pending row exists (no server call) |

**Mental model:** load the full snapshot once, watch cheaply with status while publish or rollout is active, and reload full when status tells you the table needs new published data or deploy rules.

```text
open page ──► full (once)
                 │
     ┌───────────┴────────────┐
     │                        │
  idle (<5 min)            active
  full every 12s           status every 3s
  (bootstrap watch)        + 1s local timer if publishing
     │                        │
     └───────────┬────────────┘
                 │
         escalation? ──yes──► full (once) ──► resume or stop polling
                 │
                no
                 │
            keep current mode
```

| Phase | What happens |
| --- | --- |
| **Land** | Full fetch; render table; start 5-minute bootstrap watch |
| **Bootstrap** (first 5 minutes, nothing active) | Full every **12s** — catches a CI pending row that appears after you opened the page |
| **Active publish** (`pendingVersions.length > 0`) | Status every **3s**; local clock ticks **Publishing · for …** every **1s** |
| **Active rollout** or deploy locked (`selectionDisabled`) | Status every **3s**; rollout banner and disabled Deploy buttons stay current |
| **After deploy click** | Full once (fresh rollout + lock state), then status every **3s** |
| **Idle** (no pending, no rollout, past bootstrap) | Stop polling |

#### Escalation

**Escalation** is a one-time full registry fetch triggered when a status poll detects that the snapshots table needs data the status endpoint does not carry — published version metadata, deployment state (`Available` / `Redeployable` / `Locked`), `deployableUntil`, and deploy-button eligibility.

Status polling answers “is something still in flight?” Full registry answers “what does the table show now?”

Escalate (call full, replace snapshot) when status reports any of:

| Trigger | Why full is required |
| --- | --- |
| A `pendingVersions` entry disappears | Publish finished or was pruned — build the **Available** or remove the **Publishing** row |
| A new `failedVersions` entry appears | Surface the **Failed** row with full provenance |
| Rollout reaches `complete` or `failed` | Re-enable Deploy and refresh rollout badges |
| `desiredVersion` changes | Update which row is **Deployed** |

After escalation, return to the current polling mode: status every 3s if still active, bootstrap full every 12s if within the window, otherwise stop.

### 2. Adaptive polling intervals

Replace the single `POLL_INTERVAL_MS = 12_000` with two constants:

| Constant | Value | Used when |
| --- | ---: | --- |
| `POLL_INTERVAL_ACTIVE_MS` | `3_000` | `shouldPollAppAdminRegistry` is true **and** at least one active trigger is present (pending, rollout, or `selectionDisabled`) |
| `POLL_INTERVAL_IDLE_MS` | `12_000` | bootstrap window only (no pending / rollout / selection lock) |

Update `shouldPollAppAdminRegistry` in `gestalt-providers/app/default/src/features/registry/polling.ts`:

```ts
export function appAdminPollIntervalMs(
  registry: AppAdminRegistryResponse,
  bootstrapPollUntilMs: number,
  now = Date.now(),
): number | null {
  if (!shouldPollAppAdminRegistry(registry, bootstrapPollUntilMs, now)) return null;
  const active =
    (registry.pendingVersions?.length ?? 0) > 0 ||
    registry.selectionDisabled ||
    (registry.rollout && isActiveRegistryRollout(registry.rollout.state));
  return active ? POLL_INTERVAL_ACTIVE_MS : POLL_INTERVAL_IDLE_MS;
}
```

**Polling implementation:** replace the chained `setTimeout` in `AppAdminPageClient.tsx` (effect re-schedules only after `registry` changes) with a single `setInterval` (or React Query `refetchInterval`) keyed on the computed interval. Clear the timer on unmount and when polling stops.

**Status vs full during active poll:**

```ts
const endpoint =
  (registry.pendingVersions?.length ?? 0) > 0 ||
  registry.rollout ||
  registry.selectionDisabled
    ? getAppAdminRegistryStatus
    : getAppAdminRegistry; // bootstrap-only path
```

During bootstrap with no active signals, keep fetching the full registry at 12s so a not-yet-visible pending row still resolves without requiring the operator to refresh.

Pause polling while `document.visibilityState === "hidden"`; run one catch-up fetch when the tab becomes visible again.

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

## UI State Machine

```text
[load] ──full──► registry state
                    │
        ┌───────────┴───────────┐
        │ shouldPoll?           │
        └───────────┬───────────┘
                    │ yes
        ┌───────────▼───────────┐
        │ pick interval         │
        │  3s active / 12s boot │
        └───────────┬───────────┘
                    │
        ┌───────────▼───────────┐
        │ pick endpoint         │
        │  status vs full       │
        └───────────┬───────────┘
                    │
        ┌───────────▼───────────┐
        │ merge or escalate     │
        └───────────┬───────────┘
                    │
        ┌───────────▼───────────┐
        │ live clock (1s)       │
        │ if pending rows       │
        └───────────────────────┘
```

**Merge helper** (`mergeAppAdminRegistryStatus`):

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

Compare previous and next status to detect escalation triggers before merging.

---

## Implementation Checklist

### gestalt

- [ ] `GET /api/v1/apps/{app}/admin/registry/status` handler in `handlers_app_admin_registry.go`
- [ ] Mount route in `mountAppAdminRegistryRoutes`
- [ ] Unit tests: empty pending, pending rows with `publishingForSeconds`, failed merge precedence, rollout + `selectionDisabled` flags, 403/404 parity with full handler
- [ ] Lifecycle doc: request/response shapes and GCS read set

### gestalt-providers

- [ ] `getAppAdminRegistryStatus` in `lib/api.ts` + `AppAdminRegistryStatusResponse` type
- [ ] `appAdminPollIntervalMs`, `mergeAppAdminRegistryStatus`, escalation detector in `polling.ts`
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
| `appAdminPollIntervalMs` | `3000` with pending; `12000` during bootstrap only; `null` when idle |
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
