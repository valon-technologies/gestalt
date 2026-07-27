# App Admin Registry Polling

Faster registry refresh and live publish-duration labels on `/apps/{app}/admin` during CI publish and fleet rollout.

## Overview

Today `gestalt-providers` polls `GET /api/v1/apps/{app}/admin/registry` every **12 seconds** while publish or rollout is active. Each request makes `gestaltd serve` fetch `index.json`, `pending.json`, `failed.json`, and `retention.json` from GCS, plus IndexedDB reads for fleet state.

Two operator-visible gaps follow:

1. **Slow table updates** — a CI-recorded **Publishing** row or a completed publish can take up to ~12s to appear or change status.
2. **Frozen duration labels** — `Publishing · for 4m` only updates on poll. `publishingForSeconds` is computed server-side at request time, so the label jumps in 12-second steps even though `startedAt` is already on the row.

Scope is `/apps/{app}/admin` only — the embedded fleet `/admin` registry UI does not show publishing state and is out of scope unless it later reuses the same helpers.

Planned changelog step: **20**. See [changelog.md](../project/changelog.md).

## Goals

- Poll `GET /api/v1/apps/{app}/admin/registry` every **3s** while the bootstrap window is open or publish/rollout is active (today: **12s**).
- Show a **Publishing** row within **3s** (p95) after CI `pending set` when an operator already has the page open.
- Reflect publish completion in **Published snapshots** within **3s** after `pending clear` and `index.json` update.
- Tick **Publishing** duration labels every **1s** client-side from `startedAt`, without additional `gestaltd` requests.

## Non-Goals

- New app-admin API routes
- Push notifications (SSE, WebSocket, GCS Pub/Sub)
- Server-side caching of registry documents
- Faster catalog poller (replica convergence remains ~1 minute)
- Embedded `/admin/registry` list auto-refresh (still one-shot today)
- Polling solely because `failedVersions` is non-empty (unchanged; see [pending-publish.md](./pending-publish.md))

---

## App-Admin API

No API changes. All polls continue to use:

`GET /api/v1/apps/{app}/admin/registry`

Response shapes and merge rules: [pending-publish.md — App-Admin API](./pending-publish.md#app-admin-api). Wireframes and status labels: [admin.md — App Admin UI](./admin.md#app-admin-ui-appsappadmin).

---

## UI (`/apps/{app}/admin`)

Implemented in `gestalt-providers` (`polling.ts`, `lib/queries/app-admin.ts`, `app-admin-snapshots-table.tsx`, `snapshot-rows.ts`).

Separate **registry refresh** (poll cadence) from **duration display** (local 1s clock).

### Polling

Polling is per browser tab. Closing the tab stops all refresh. `gestaltd` does not track a server-side poll mode.

The UI already derives whether to poll from `shouldPollAppAdminRegistry` in `polling.ts` plus `APP_ADMIN_BOOTSTRAP_POLL_MS` (**5 minutes** after page load). This plan changes only the interval and duration-label behavior.

| When | Registry refresh |
| --- | --- |
| **Bootstrap window** — first **5 minutes** after page load, no active signals yet | `GET /api/v1/apps/{app}/admin/registry` every **3s** |
| **Active publish or rollout** — see signals below | every **3s** |
| **Idle** — past bootstrap window and no active signals | none until manual refresh or deploy |
| Tab closed | none |

**Bootstrap window** ends after **5 minutes** unless an active signal keeps polling beyond that window.

**Active signals** (from the registry response — not a field on the app):

- `pendingVersions.length > 0`
- `rollout.state` is `enrolling` or `restarting` (non-terminal rollout)
- `selectionDisabled` is `true` (deploy actions locked while rollout is active)

Planned constants:

- `APP_ADMIN_POLL_INTERVAL_MS = 3_000` whenever `shouldPollAppAdminRegistry` returns true (today: `12_000` in `lib/queries/app-admin.ts`)
- keep `APP_ADMIN_BOOTSTRAP_POLL_MS` for the bootstrap-window duration

Wire `APP_ADMIN_POLL_INTERVAL_MS` through `useAppAdminRegistryQuery` (`refetchInterval`). Optional follow-up: pause polling when `document.visibilityState === "hidden"` and catch up once when visible again.

Do not poll solely because `failedVersions` is non-empty — failed rows load on the initial page fetch.

### Publish duration labels

Durations are computed at read/UI time — not stored as separate GCS fields. Baseline rules: [pending-publish.md — Publish Duration](./pending-publish.md#publish-duration).

This milestone adds a client-side clock so in-flight labels advance between registry polls:

| Row status | Label | Source after this change |
| --- | --- | --- |
| **Publishing** | `Publishing` + `for 4m 12s` | `durationSecondsBetween(pending.startedAt, liveNow)` |
| **Publishing** last update | `4 minutes ago` | `formatRegistryTimeAgo(pending.updatedAt, liveNow)` |
| **Available** / **Deployed** / etc. | `Published in 4m 32s` | unchanged — static from `publishDurationSeconds` or `publishStartedAt` → `publishedAt` |
| **Failed** | `Failed after 35m` | unchanged — static from `failedAt` − `startedAt` |

Add `useLiveNow` in `hooks/use-live-now.ts`:

- `enabled` when **Published snapshots** has at least one `pending` row
- tick every **1s**; pause when the tab is hidden
- pass `liveNow` into `snapshotStatusTimer` and `snapshotLastUpdatedLabel` (both already accept an optional `now` in `snapshot-rows.ts`)

For **Publishing** rows, prefer `startedAt` + `liveNow` over `publishingForSeconds` from the last poll. Do not add a 1s poll to `gestaltd`.

---

## Implementation PRs

This plan PR ([gestalt#2949](https://github.com/valon-technologies/gestalt/pull/2949)) is docs only. Ship step **20** with the PRs below.

| # | Repo | Scope | Touches |
| --- | --- | --- | --- |
| **1** | `gestalt-providers` | 3s registry polling | `polling.ts`, `lib/queries/app-admin.ts`, `e2e/app-admin-mock.spec.ts` |
| **2** | `gestalt-providers` | Live **Publishing** duration labels | `hooks/use-live-now.ts`, `app-admin-snapshots-table.tsx`, `snapshot-rows.ts` |
| **3** | `toolshed` | Deploy bump | `valon-tools/deploy/config.yaml` — `apps.home.source.git.ref` to the merged `gestalt-providers` commit |
| **4** | `gestalt` | App registry docs for step **20** | `pending-publish.md`, `admin.md`, `changelog.md`, `tests.md`, `responsive-app-admin.md` (Shipped In) |

**Merge order:** **1** and **2** may land in either order or as one `gestalt-providers` PR if the diff stays small. Merge **3** after **1** and **2** so production serves the new default app UI. Merge **4** last so shipped docs match the deployed behavior.

No `gestaltd` changes — the app-admin API and GCS layout are unchanged.

### PR 1 — `gestalt-providers` (registry polling)

- Add `APP_ADMIN_POLL_INTERVAL_MS = 3_000` in `polling.ts`
- Replace the local `12_000` constant in `lib/queries/app-admin.ts` with the shared export
- `useAppAdminRegistryQuery` `refetchInterval` unchanged except interval value
- E2E: `polls for pending publish without manual refresh` timeout **15s → 6s**

### PR 2 — `gestalt-providers` (live duration labels)

- Add `useLiveNow` in `hooks/use-live-now.ts`
- `app-admin-snapshots-table.tsx` — enable while any `pending` row exists; pass `liveNow` to formatters
- `snapshot-rows.ts` — **Publishing** duration from `startedAt` + `liveNow` (not `publishingForSeconds`)
- Optional E2E: duration label advances between frozen registry responses

### PR 3 — `toolshed` (deploy bump)

- Bump `apps.home.source.git.ref` in `valon-tools/deploy/config.yaml` to the `gestalt-providers` commit that includes **1** and **2**
- `apps.home` is the default `/apps` shell that serves `/apps/{app}/admin` for registry-only apps such as `g-issues`
- No workflow or registry publish changes — UI-only deploy pin

### PR 4 — `gestalt` (documentation)

- [pending-publish.md](./pending-publish.md) — polling bullets (**3s** bootstrap and active)
- [admin.md](./admin.md) — refresh-until-terminal wording
- [changelog.md](../project/changelog.md) — step **20** entry with links to PRs **1**–**4**
- [tests.md](../project/tests.md) — UI polling and live-clock coverage
- [responsive-app-admin.md](./responsive-app-admin.md) — add **Shipped In**; remove planned-only wording

Optional follow-up (not required for step **20**):

| Repo | Scope |
| --- | --- |
| `gestalt-providers` | Pause registry polling when `document.visibilityState === "hidden"` |

---

## Implementation

### `gestalt-providers`

See [Implementation PRs](#implementation-prs) **1** and **2**.

### Docs (on ship)

See [Implementation PRs](#implementation-prs) **4**.

---

## Tests

Run app-admin E2E from `gestalt-providers/app/default`:

```bash
cd gestalt-providers/app/default
npm run test:e2e -- e2e/app-admin-mock.spec.ts
```

| Area | Asserts |
| --- | --- |
| `app-admin-mock.spec.ts` | **Publishing** row within **6s** without manual refresh |
| `snapshot-rows.ts` | `for 59s` → `for 1m` → `for 1m 1s` with advancing `now` (add when app-default has a unit test runner) |
| `app-admin-mock.spec.ts` (optional) | frozen pending `startedAt`; duration label advances over 2s between registry polls |

---

## Future Work

- Lightweight `GET /api/v1/apps/{app}/admin/registry/status` to reduce GCS reads per poll
- Short TTL registry cache in `gestaltd`
- SSE push on `pending set` or rollout transitions
- Embedded `/admin/registry` adaptive polling (align with [admin.md](./admin.md#apps-list-adminregistry))

---

## Appendix

### Related Changelogs

<pre>
├── <a href="../project/changelog.md#changelog-16">16 — Pending and Failed Publish Visibility</a>
└── <a href="../project/changelog.md">changelog.md</a> — step 20 (on ship)
</pre>

### Related Docs

<pre>
├── <a href="../readme.md">readme.md</a> — registry architecture and future work
├── <a href="../project/changelog.md">changelog.md</a> — implementation milestones and pull requests
├── <a href="./pending-publish.md">pending-publish.md</a> — publish catalogs and current 12s polling
├── <a href="./admin.md">admin.md</a> — app admin UI capabilities
├── <a href="./lifecycle.md">lifecycle.md</a> — app-admin HTTP API
└── <a href="../project/tests.md">tests.md</a> — test index (extend on ship)
</pre>
