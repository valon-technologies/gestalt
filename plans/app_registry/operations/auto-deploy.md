# Auto-Deploy Published Snapshots

App-admin-controlled automatic fleet admission when a new registry snapshot is published.

## Overview

App admins opt a registry-only app into **auto-deploy** on `/apps/{app}/admin`. When enabled, Gestalt admits the newest published snapshot as the fleet-wide desired version without a manual **Deploy** click.

Auto-deploy uses the same admission path as `POST /api/v1/apps/{app}/admin/registry/version`: install lock, install-time validation, rollout creation, change-request append, and retention mirror. Per-replica convergence is unchanged. See [lifecycle.md](./lifecycle.md#app-admin-version-selection).

Scope is `/apps/{app}/admin` only — not the embedded fleet `/admin` UI.

## Goals

- App admins toggle auto-deploy per app (`admin` on `app/{app}`).
- Trigger on publish completion — when `index.json` includes the version (after `gestaltd app registry pending clear`). Pending and failed publishes do not trigger auto-deploy.
- At most one rollout per app (unchanged). While rollout state is `enrolling` or `restarting`, new publishes update the **pending target** but do not start another admission.
- When the rollout reaches `complete`, admit the pending target if it differs from the current desired version. Intermediate publishes are skipped.
- On rollout `failed`, disable auto-deploy and require manual intervention before any further automatic admissions.
- Record auto-deploy admissions in revision history with actor `system:auto-deploy`.
- Poll `apps/{app}/index.json` every **1 minute** (default; configurable via `server.appRegistry.autoDeployPollInterval`). Use conditional GETs with `If-None-Match` to skip unchanged indexes.

## Publish Detection

For auto-deploy-enabled apps, a background watcher in `gestaltd serve` polls `apps/{app}/index.json` over HTTP from the registry `publicUrl` (public GCS). Each poll is one GET per enabled app. Admission still writes to IndexedDB and fetches `versions/{version}.json` only when coalescing starts an install.

Default poll interval: **1 minute**, aligned with the catalog poller. Configurable via `server.appRegistry.autoDeployPollInterval` when set. Coalescing correctness does not depend on the interval. If `gestaltd serve` is unavailable at publish time, the next poll still converges on the latest version.

Each replica may poll independently today. Duplicate reads are acceptable at this rate; designate a single evaluator later if the enabled-app count grows.

The watcher stores the last `ETag` per app in process memory. On each poll:

1. Send `If-None-Match: {etag}` when a prior `ETag` exists.
2. On **304 Not Modified**, stop — no new publish to process.
3. On **200**, parse the index, update the stored `ETag`, and compare the newest published version against `auto_deploy_last_seen_version`.

GCS returns `ETag` on every `index.json` response. A missing `ETag` on the first response falls back to an unconditional GET on the next poll.

## Coalescing

Per auto-deploy-enabled app, Gestalt tracks a **pending target**: the newest published version that should become desired once admission is possible.

1. On publish detection or enable — set `pendingTarget` to the newest published version.
2. On rollout `failed` — disable auto-deploy, clear `pendingTarget`, record `lastError`; stop.
3. On rollout `complete` — set `pendingTarget` to the newest published version.
4. If rollout state is `enrolling` or `restarting`, stop. `pendingTarget` is already updated.
5. If `pendingTarget == desiredVersion`, clear `pendingTarget` and stop.
6. Otherwise attempt admission for `pendingTarget` (same as manual version selection).
   - **Success** — clear `pendingTarget`.
   - **Validation failure (400)** — clear `pendingTarget`; expose `lastError` on registry GET. Do not retry until the next publish or a manual deploy.
   - **Active rollout (409)** — keep `pendingTarget`; retry when the rollout reaches `complete`.

### Toggle

| Action | Behavior |
| --- | --- |
| Disable | Stop future automatic admissions. An in-flight rollout continues; the current desired version is unchanged. |
| Enable | Run coalescing once. When no rollout is active and the newest published version is not desired, admit it. |

## State

Stored in IndexedDB (fleet policy, not GCS publish metadata):

| Field | Purpose |
| --- | --- |
| `auto_deploy_enabled` | Per-app toggle; writable only via app-admin API |
| `auto_deploy_pending_version` | Newest published version waiting for admission |
| `auto_deploy_last_seen_version` | Deduplicate publish detection across polls |

The app-scoped install lock serializes concurrent manual and automatic admissions.

## API

Extend `GET /api/v1/apps/{app}/admin/registry`:

```json
{
  "autoDeploy": {
    "enabled": true,
    "pendingVersion": "0.0.0-snapshot.gdef456",
    "lastError": null
  }
}
```

| Method | Path | Description |
| --- | --- | --- |
| `PUT` | `/api/v1/apps/{app}/admin/registry/auto-deploy` | `{ "enabled": true \| false }` |

Authorization matches version selection: `admin` on `app/{app}`. Auto-deploy does not bypass install-time validation, retention locks, or rollout admission checks.

## App Admin UI

- Toggle: **Automatically deploy new snapshots**. Turn off automatically on rollout `failed`; show `lastError` until an app admin re-enables auto-deploy or deploys manually.
- During an active rollout, show **Queued for deploy** on the snapshot row for `autoDeploy.pendingVersion` (distinct from **Deploying...** on the admitted version).
- Revision history shows `system:auto-deploy` for automatic admissions.

## Edge Cases

| Scenario | Behavior |
| --- | --- |
| Install-time validation fails | Clear pending; expose `lastError`. No automatic retry. |
| Version expired or locked | Admission rejected (400); clear pending. |
| No fleet-known version yet | First publish triggers `add`. |
| Concurrent manual deploy | Install lock serializes; auto-deploy retries when the rollout reaches `complete`. |
| Rollout `failed` | Disable auto-deploy, clear pending, record `lastError`. App admin must deploy manually and re-enable auto-deploy to resume automatic admissions. |

## Out of Scope

- Auto-deploy for pending or failed publishes.
- Per-replica or per-user deployment.
- Cancelling an in-flight rollout to jump to a newer version.
- Changing rollout enrollment or restart semantics.
- Automatic rollback on rollout failure.
- Notifications on validation failure.

## Implementation

Planned as changelog milestone **25 — Auto-Deploy Published Snapshots**.

### gestalt

**PR 1 — Design doc**

Land this document, link it from [readme.md](../readme.md), and add milestone `25` to [changelog.md](../project/changelog.md).

**PR 2 — State and conditional registry reads**

IndexedDB persistence, registry conditional GET support, and poll-interval config. No HTTP routes or background worker yet.

- Add `app_auto_deploy_settings` store and `AutoDeploySettingsService` in `internal/coredata/`.
- Extend `RegistryReader` with conditional `index.json` fetch (`If-None-Match`; **304** / **200**).
- Add `server.appRegistry.autoDeployPollInterval` (default `1m`).
- Tests: `reader_test.go`; `app_auto_deploy_settings_test.go`.

**PR 3 — App-admin API**

- `PUT /api/v1/apps/{app}/admin/registry/auto-deploy`
- Extend `GET …/registry` with `autoDeploy: { enabled, pendingVersion, lastError }`
- Tests: `handlers_app_admin_registry_test.go`

**PR 4 — Watcher and coalescing**

- Add `internal/appregistry/autodeploy/` controller.
- Implement [Coalescing](#coalescing). Actor `system:auto-deploy`. Hook rollout terminal transitions.
- Tests: `autodeploy_test.go`

**PR 6 — Fold design into main docs**

Land after PRs 2–5. Merge this document into [lifecycle.md](./lifecycle.md), [admin.md](./admin.md), [config.md](../architecture/config.md), [indexeddb.md](../architecture/indexeddb.md), and [tests.md](../project/tests.md). Update [changelog.md](../project/changelog.md) with merged PR links. Remove this file.

### gestalt-providers

**PR 5 — App admin UI**

- Toggle, **Queued for deploy**, `lastError` affordance, `system:auto-deploy` in revision history.
- Tests: `e2e/app-admin-mock.spec.ts`
- Deploy: bump host app snapshot in `toolshed` when required.

---

## Appendix

### Related Docs

<pre>
├── <a href="../project/changelog.md">changelog.md</a> — implementation milestones
├── <a href="../project/tests.md">tests.md</a> — behavioral test coverage
├── <a href="./lifecycle.md">lifecycle.md</a> — version selection and rollout admission
├── <a href="./admin.md">admin.md</a> — app-admin UI capabilities
├── <a href="./pending-publish.md">pending-publish.md</a> — publish completes when index.json is updated
└── <a href="./retention.md">retention.md</a> — expiry rules apply to auto-deploy
</pre>
