# Published Version Retention

Delete unused published app registry versions while preserving every version
that entered the fleet deploy chain.

Related docs:

- [config.md](../architecture/config.md) — `retention` fields on `appRegistries`
- [models.md](../architecture/models.md) — [RetentionIndex](../architecture/models.md#retentionindex)
- [lifecycle.md](./lifecycle.md) — fleet admission (`POST …/add`, `POST …/upgrade`)
- [pending-publish.md](./pending-publish.md) — `pending.json` / `failed.json` pruning

## Problem

Published versions and artifacts accumulate in GCS indefinitely. Never-deployed
snapshots can be cleaned up quickly. Versions in the deploy chain need two
different lifetimes: their audit metadata is permanent, while their ability to
be selected again expires after a bounded recovery window.

## Goals

- Configurable retention on each registry binding (`unusedRetention`,
  `deployedRetention`).
- Mutable `retention.json` per app tracking deployment and lock state.
- **Unused retention** (default `72h`) — delete published versions that were
  never deployed within the window after publication.
- **Permanent deploy history** — retain every version that appears as
  `to_version` in `app_version_change_requests`, including upgrades and
  downgrades. Retain deployed version metadata permanently.
- **Historical redeploy window** (default `720h`, or 30 days) — after a version
  stops being desired, allow it to be selected again until its fixed
  `deployableUntil` deadline. After the deadline it is permanently locked.
- Keep artifacts while a version is desired or historically redeployable.
  Locked historical versions may have artifacts pruned, while their index
  summary, version metadata, retention row, and change requests remain.
- `gestaltd app registry retention prune` to delete eligible versions from GCS.
- Scheduled prune from the **deploy reader** side (not publish CI).
- A read-only **Revision history** tab on `/apps/{app}/admin` backed by the
  append-only deploy chain.

Fleet admission permanently changes a version from unused to deployed.
Publishing alone does not count. The deploy chain may contain the same version
more than once, for example `v1 → v2 → v1`; each accepted transition is a
separate revision-history event.

## Config

On `appRegistries.{name}`:

```yaml
retention:
  unusedRetention: 72h
  deployedRetention: 720h
```

Defaults are `72h` and `720h` when omitted. A configured deployed duration is
captured as a fixed deadline when a version stops being desired; changing
config later does not unlock an expired version. See
[config.md](../architecture/config.md).

## Schema and storage

```text
apps/{app}/
├── index.json
├── retention.json
├── pending.json
├── failed.json
├── versions/{version}.json
└── artifacts/{version}/…
```

`retention.json` is a mutable catalog. Shape and fields:
[models.md — RetentionIndex](../architecture/models.md#retentionindex). Updates use the same
optimistic-concurrency pattern as `index.json` (`if-generation-match`).

## Who writes and who deletes

| Action | Owner | What |
|--------|-------|------|
| Publish | **Publisher** (`gestaltd app registry publish`) | Upsert `retention.json` with `publishedAt` and `everDeployed = false` |
| First fleet admission | **Reader** (`POST …/add`, `POST …/upgrade`) | Set `everDeployed = true` and `firstDeployedAt`; clear any historical deadline while desired |
| Desired version changes | **Reader** | On the outgoing version, set `lastUsedAt = now` and `deployableUntil = now + deployedRetention`; on the incoming version, clear its active deadline |
| Lock historical version | **Reader / prune** | Once `deployableUntil` passes, set sticky `lockedAt`; future config changes do not unlock it |
| Delete unused version | **Reader / prune** | Remove a never-deployed expired version from `index.json`, `retention.json`, `versions/{version}.json`, and `artifacts/{version}/` |
| Prune locked artifact | **Reader / prune** | Remove `artifacts/{version}/` only; retain index summary, version metadata, retention row, and change requests |

Admission checks the selected version while holding the app-scoped install
lock. A historical version is eligible only when it is not locked and the
request arrives before `deployableUntil`. Selecting it appends another change
request and makes it desired; when it later stops being desired, it receives a
new 30-day deadline.

Prune cross-checks `retention.json` against `app_version_change_requests` before
fully deleting any version. If either source says the version was deployed,
only locked artifacts may be removed. Prune never modifies `pending.json`,
`failed.json`, version metadata for deployed versions, or change requests.

```bash
gestaltd app registry retention prune \
  --bucket gs://… \
  --app g-issues \
  [--dry-run]
```

Run on a schedule from the deploy reader (for example a daily `gestaltd` cron).
Do not run from `gestaltd serve` request handlers.

## Retention states

| State | Deployable | Cleanup |
|-------|------------|---------|
| Never deployed, younger than `unusedRetention` | yes | none |
| Never deployed, window expired | no | delete index entry, metadata, artifact, and retention row |
| Current desired version | already active | retain all objects; no historical deadline runs |
| Historical, before `deployableUntil` | yes | retain all objects |
| Historical, deadline expired | no, permanently locked | retain deploy history and metadata; artifact may be deleted |
| Active rollout target | yes | retain all objects regardless of other clocks |

The Revision history tab always shows deployed transitions, including locked
versions. Deployment controls live on Published snapshots and expose
**Deploy** only for never-deployed or still-redeployable versions. See
[admin.md](./admin.md#revision-history-tab) and
[lifecycle.md](./lifecycle.md#revision-history).

## Implementation path

1. **PR 1 — Models and write path** (`gestalt`) — `RetentionIndex`, config
   validation, desired-version transition updates, and fixed redeploy deadlines.
2. **PR 2 — Prune command** (`gestalt`) — `gestaltd app registry retention
   prune` with `--dry-run`, unused deletion, historical locking/artifact
   cleanup, and change-request cross-checks.
3. **PR 3 — Scheduler** (deploy / ops) — scheduled `retention prune` per
   registry-only app.
4. **PR 4 — Revision history API/UI** (`gestalt`, `gestalt-providers`) —
   paginated deploy-chain API and read-only app-admin tab.
