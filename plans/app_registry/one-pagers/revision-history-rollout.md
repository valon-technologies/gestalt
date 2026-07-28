# Revision History Rollout Visibility

Show in-flight and completed rollout timing on the **Revision history** tab on `/apps/{app}/admin`.

## Overview

Fleet admission appends a change request immediately, but the version is not fleet-available until rollout reaches `complete`. Today the Revision history tab lists that transition with only `deployedAt` and no rollout state, so operators cannot see that a deployment is still converging or how long it took to become available.

This addendum extends the Revision history table with rollout status and duration labels that mirror the **Publishing** / **Published in** pattern on **Published snapshots**. See [pending-publish.md](../operations/pending-publish.md#publish-duration).

Scope is `/apps/{app}/admin` only — not the embedded fleet `/admin` UI.

## Goals

- Show **Rolling out** on the matching revision row while rollout state is `enrolling` or `restarting`.
- Tick rollout duration live with `useLiveNow` between registry polls, the same way **Publishing** rows advance on Published snapshots.
- After rollout is terminal, show a static duration on that row: **Available in** on `complete`, **Failed after** on `failed`.
- Persist terminal rollout timing so historical rows keep their duration after `app_rollouts` is replaced by a later admission.
- Reuse existing **3s** registry polling while rollout is active; do not add a separate history poll interval.

## Rollout Duration

Durations are computed at read/UI time from immutable timestamps. Field definitions follow the publish-duration model in [pending-publish.md](../operations/pending-publish.md#publish-duration).

| Row status | Start | End | Status label |
| --- | --- | --- | --- |
| **Rolling out** | `deployedAt` (change-request timestamp) | `liveNow` (client clock, 1s) | `Rolling out` + `for 2m 14s` |
| **Available** | `deployedAt` | `rolloutCompletedAt` | `Available in 3m 08s` |
| **Failed** | `deployedAt` | `rolloutFailedAt` | `Failed after 12m 41s` |

`deployedAt` already exists on every revision-history row. Terminal end timestamps come from a rollout-outcome sidecar written once when the poller marks the rollout `complete` or `failed`. While rollout is active, join live state from `app_rollouts` on `(app, version)`; do not wait for the sidecar.

The app-admin history API includes `rolloutForSeconds` on in-flight rows and `rolloutDurationSeconds` on terminal rows when timestamps exist. The app admin UI prefers `deployedAt` + a client-side `liveNow` clock for in-flight **Rolling out** labels so durations advance between registry polls; terminal rows use the static API fields.

## Persistence

`app_version_change_requests` stays append-only. Do not patch accepted change requests after admission.

Add `app_version_rollout_outcomes`, keyed by change-request `id`:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "app": "g-issues",
  "version": "0.0.0-snapshot.gdef456",
  "completed_at": "2026-07-24T20:45:08Z"
}
```

or, on failure:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "app": "g-issues",
  "version": "0.0.0-snapshot.gdef456",
  "failed_at": "2026-07-24T20:54:41Z"
}
```

Write the row exactly once in the poller when `MarkCompleteForRollout` or `MarkFailedForRollout` succeeds. Resolve the change request by `(app, version)` using the latest request for that `to_version` at transition time. Ignore duplicate writes for the same `id`.

## API

Extend `GET /api/v1/apps/{app}/admin/registry/history` revision objects:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "version": "0.0.0-snapshot.gdef456",
  "previousVersion": "0.0.0-snapshot.gabc123",
  "deployedAt": "2026-07-24T20:42:00Z",
  "deployedBy": "user:alice",
  "rolloutState": "restarting",
  "rolloutForSeconds": 134,
  "rolloutDurationSeconds": null,
  "rolloutCompletedAt": null,
  "rolloutFailedAt": null
}
```

Rules:

- Omit rollout fields when no rollout exists for the revision's version and no outcome sidecar is stored.
- `rolloutState` is `enrolling`, `restarting`, `complete`, or `failed`.
- For active rollouts, project `rolloutState` from `app_rollouts` when `rollout.version == revision.version`.
- For terminal rows, prefer the outcome sidecar. Fall back to `app_rollouts` when the rollout record is still terminal and versions match.
- `rolloutForSeconds` is set only while state is `enrolling` or `restarting`.
- `rolloutDurationSeconds`, `rolloutCompletedAt`, and `rolloutFailedAt` are set only for terminal states.
- Legacy revisions without a stored outcome omit terminal duration fields.

No new routes. `GET /api/v1/apps/{app}/admin/registry` is unchanged; the UI may still read `registry.rollout` for live decoration while the history tab is open.

## App Admin UI

Add a **Status** column to the Revision history table.

| Deployed at | Transition | Status | Deployed by |
| --- | --- | --- | --- |
| 2 minutes ago | `gabc123 → gdef456` | **Rolling out** · for 2m 14s | alice@valon.com |
| Jul 24 16:42 | `gdef456 → gabc123` | Available in 3m 08s | alice@valon.com |
| Jul 21 12:00 | First deployment → `gabc123` | Available in 1m 52s | bob@valon.com |

During an active rollout:

```text
Revision history

Deployed at  | Transition                    | Status                    | Deployed by
------------ | ----------------------------- | ------------------------- | ---------------
2 minutes ago| gabc123 → gdef456 (upgrade)   | Rolling out · for 2m 14s  | alice@valon.com
Jul 24 16:42 | gdef456 → gabc123 (downgrade) | Available in 3m 08s       | alice@valon.com
```

The newest row whose `version` matches `registry.rollout.version` shows **Rolling out** while rollout is `enrolling` or `restarting`. Match the rollout phase stepper and Published snapshots polling: enable `useLiveNow` while the history table has at least one in-flight rollout row (pause when the tab is hidden).

Automatic admissions still show `system:auto-deploy` in **Deployed by**; rollout status is independent of actor.

## Edge Cases

| Scenario | Behavior |
| --- | --- |
| Rollout retargeted to a new source version | Same desired version and change request; duration continues from original `deployedAt`. |
| Rollout `failed` | Show **Failed after** on the matching revision row. Fleet may still run the previous desired version. |
| Next version admitted before operator opens history | Prior revision keeps terminal duration from the outcome sidecar. |
| Legacy rows before this addendum | Omit rollout status and duration; `deployedAt` only. |
| History tab open during rollout | Parent registry query already polls every **3s**; history query does not need its own interval. |
| Concurrent manual deploy while history is visible | Install lock serializes; only one active rollout row at a time. |

## Out of Scope

- Per-replica convergence detail on the history tab (embedded `/admin` app detail).
- Editing or retrying rollouts from Revision history.
- Showing auto-deploy **Queued for deploy** on this tab (remains on Published snapshots).
- Notifications on rollout `failed`.
- Replacing the rollout phase stepper or selected snapshot row affordances.

## Implementation

Planned as an addendum to the app registry docs, not a new changelog milestone.

### gestalt

**PR 1 — Design doc**

Land this document and link it from [readme.md](../readme.md).

**PR 2 — Rollout outcomes and history enrichment**

- Add `app_version_rollout_outcomes` store and service in `internal/coredata/`.
- Write outcome rows from the rollout poller on successful `MarkCompleteForRollout` / `MarkFailedForRollout`.
- Extend `GET …/admin/registry/history` with rollout status and duration fields.
- Tests: `app_version_rollout_outcomes_test.go`; extend `handlers_app_admin_registry_test.go`.

### gestalt-providers

**PR 3 — Revision history UI**

Land after gestalt PR 2 merges.

- Add **Status** column, `revisionRolloutStatusTimer`, and `useLiveNow` integration in `app-admin-history-table.tsx`.
- Merge live rollout state from the parent registry query when decorating the newest matching row.
- Tests: `e2e/app-admin-mock.spec.ts`.

### toolshed

**PR 4 — Deploy**

Land after gestalt PR 2 and gestalt-providers PR 3. Bump `GESTALTD_PINNED_SHA` and the `apps.home` snapshot so `/apps/{app}/admin` ships the history status column.

### gestalt (docs)

**PR 5 — Fold design into main docs**

Land after PRs 2–4. Fold operational detail from this document into [admin.md](../operations/admin.md), [lifecycle.md](../operations/lifecycle.md), and [indexeddb.md](../architecture/indexeddb.md). Keep this one-pager as the canonical design reference in [one-pagers/](./).

### Stacking

```text
main
 ├── gestalt PR 1 — design doc
 ├── gestalt PR 2 — rollout outcomes and history enrichment
 ├── gestalt-providers PR 3 — revision history UI
 ├── toolshed PR 4 — deploy
 └── gestalt PR 5 — fold design into main docs
```

| PR | Repo | Base branch | Depends on |
| --- | --- | --- | --- |
| 1 | gestalt | `main` | — |
| 2 | gestalt | `main` | — |
| 3 | gestalt-providers | `main` | PR 2 merged |
| 4 | toolshed | `main` | PRs 2–3 merged |
| 5 | gestalt | `main` | PRs 2–4 merged |

### Process

1. **gestalt (PRs 1–2)** — Land PR 1 on `main`, then PR 2. Babysit until CI passes and Bugbot is clean on each. Present both PRs together and get explicit approval on each.
2. **gestalt merge and gestalt-providers (PR 3)** — Merge gestalt PRs in order (1 → 2). Pin the merged gestalt version in gestalt-providers. Open PR 3. Babysit until CI passes and Bugbot is clean. Get approval.
3. **gestalt-providers merge and toolshed deploy (PR 4)** — Merge PR 3. Wait for the `apps.home` registry snapshot to publish. Open toolshed PR 4 (`GESTALTD_PINNED_SHA` and `apps.home` snapshot bump). Babysit until CI passes and Bugbot is clean. Get approval.
4. **toolshed merge and verify** — Merge PR 4. Wait for deploy. Test rollout status on the Revision history tab on `g-issues`.
5. **gestalt docs (PR 5)** — Fold this document into the main app registry docs. Open PR 5. Babysit until CI passes and Bugbot is clean. Get approval.
6. **gestalt docs merge** — Merge PR 5.

---

## Appendix

### Related Docs

<pre>
├── <a href="../readme.md">readme.md</a> — registry architecture
├── <a href="../project/changelog.md">changelog.md</a> — implementation milestones
├── <a href="../operations/admin.md">admin.md</a> — Revision history tab wireframes
├── <a href="../operations/lifecycle.md">lifecycle.md</a> — rollout admission and completion
├── <a href="../operations/pending-publish.md">pending-publish.md</a> — publish-duration pattern this addendum mirrors
└── <a href="../architecture/indexeddb.md">indexeddb.md</a> — app_rollouts and change requests
</pre>
