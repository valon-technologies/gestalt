# Responsive App Admin Polling

Plan for changelog step **20**. Improves `/apps/{app}/admin` responsiveness during publish and rollout without changing GCS write paths, install behavior, or the app-admin API.

**Status:** planned (not shipped)

## Problem

Today the app admin UI polls `GET /api/v1/apps/{app}/admin/registry` every **12 seconds** while publish or rollout is active.

Two UX gaps follow:

1. **Slow updates** — a pending row or publish completion can take up to ~12s to appear or flip status.
2. **Frozen duration labels** — `Publishing · for 4m` only updates on poll. `publishingForSeconds` is computed server-side at request time, so the label jumps in 12-second steps even though `startedAt` is already on the row.

Scope is `/apps/{app}/admin` only. The embedded fleet `/admin` registry UI is out of scope unless we later reuse the same polling helpers.

## Goals

| Goal | Target |
| --- | --- |
| Pending row appears after CI `pending set` | ≤ **3s** after page load (p95) |
| Publish completion reflected in snapshots table | ≤ **3s** after `pending clear` + `index.json` update |
| Publish-duration label | Updates every **1s** locally while a row is **Publishing** |

## Non-Goals

- New app-admin API routes (keep `GET …/admin/registry` for all polls)
- Push notifications (SSE, WebSocket, GCS Pub/Sub)
- Server-side caching of registry documents
- Faster catalog poller (replica convergence remains ~1 minute)
- Embedded `/admin/registry` list auto-refresh (still one-shot today)
- Polling solely because `failedVersions` is non-empty (unchanged)

---

## Design

All polls use the existing endpoint:

```
GET /api/v1/apps/{app}/admin/registry
```

Separate **data freshness** (poll cadence) from **display freshness** (local 1s clock for duration labels).

### Poll mode

Per open tab only — closing the tab stops all polling. There is no server-side idle state. The UI derives mode from the latest registry response plus one local value (`landingPollUntilMs`, 5 minutes after page load).

| What you see | Mode | Background refresh |
| --- | --- | --- |
| Page just opened; waiting to see if a publish starts | **Landing** | `GET …/admin/registry` every **3s** (first 5 minutes) |
| A publish, rollout, or deploy lock is in progress | **Active** | `GET …/admin/registry` every **3s** |
| Quiet page — nothing moving, been open > 5 minutes | **Quiet** | None — table is static until you refresh or click Deploy |
| Tab closed | — | Nothing runs |

**Landing** ends after 5 minutes unless an active signal keeps polling beyond that window.

Whenever the UI is polling (landing or active), use the same interval — **3s**. Landing and active differ only in *why* polling continues past the first response, not in cadence.

**Active signals** (from the registry response — not a mode field on the app):

- A row is **Publishing** (`pendingVersions` non-empty)
- Rollout banner shows **Enrolling** or **Restarting**
- Deploy buttons are disabled (`selectionDisabled`)

Implement `computePollMode(registry, landingPollUntilMs)` → `landing` | `active` | `quiet` in `gestalt-providers/app/default/src/features/registry/polling.ts`.

Constants: `POLL_INTERVAL_MS = 3_000` whenever `shouldPollAppAdminRegistry` is true. Keep `APP_ADMIN_BOOTSTRAP_POLL_MS` as the landing-window duration (internal constant name).

Replace the chained `setTimeout` in `AppAdminPageClient.tsx` with `setInterval` (or React Query `refetchInterval`) keyed on poll mode. Pause polling when `document.visibilityState === "hidden"`; catch up once when visible again.

### Continuous duration labels (local clock)

Server fields name *when* something started; the UI computes *how long* from `startedAt` + client `now`. Ignore `publishingForSeconds` for display once the live clock is wired (field may remain on the API).

**Hook:** `useLiveNow({ enabled, intervalMs = 1_000 })`

- `enabled` when the snapshots table has at least one `pending` row.
- Clears when no pending rows or tab is hidden.

Pass `liveNow` into `snapshotStatusTimer` and `snapshotLastUpdatedLabel` in `snapshot-rows.ts` (both already accept an optional `now`).

| Row | Label | Source |
| --- | --- | --- |
| **Publishing** | `for 4m 12s` | `durationSecondsBetween(pending.startedAt, liveNow)` |
| **Available** (published) | `Published in 4m 32s` | static |
| **Failed** | `Failed after 35m` | static |
| **Last update** (pending) | `4 minutes ago` | `formatRegistryTimeAgo(pending.updatedAt, liveNow)` |

**Do not** add a 1s poll to gestaltd. The continuous look is entirely client-side.

---

## Implementation Checklist

### gestalt-providers

- [x] `APP_ADMIN_POLL_INTERVAL_MS = 3_000` in `polling.ts`; wired through `useAppAdminRegistryQuery` (`refetchInterval`)
- [x] `useLiveNow` in `hooks/use-live-now.ts`
- [x] Pass `liveNow` into `AppAdminSnapshotsTable` → `snapshotStatusTimer` / `snapshotLastUpdatedLabel`
- [x] Pending duration from `startedAt` + `liveNow` (not stale `publishingForSeconds` from last poll)
- [x] E2E: pending visibility timeout **15s → 6s**
- [ ] Pause registry polling when tab is hidden (optional follow-up)

### docs (on ship)

- [ ] [pending-publish.md](./pending-publish.md) — polling section (3s while landing or active)
- [ ] [admin.md](./admin.md) — app admin timing
- [ ] [changelog.md](../project/changelog.md) — step `20`
- [ ] [tests.md](../project/tests.md) — UI polling tests

---

## Tests

### UI unit

| Test | Asserts |
| --- | --- |
| `snapshotStatusTimer` with advancing `now` | `for 59s` → `for 1m` → `for 1m 1s` without new API data (add when app-default has a unit test runner) |

### E2E (`app-admin-mock.spec.ts`)

| Test | Asserts |
| --- | --- |
| `polls for pending publish without manual refresh` | Pending row within **6s** |
| `publishing timer ticks between polls` | Optional: frozen pending `startedAt`, label advances over 2s |
---

## Future Work (out of scope)

- Lightweight `GET …/admin/registry/status` to reduce GCS reads per poll
- Short TTL registry cache in gestaltd
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
