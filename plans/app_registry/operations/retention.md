# Published Version Retention

Delete unused published app registry versions while preserving every version
that entered the fleet deploy chain.

Related docs:

- [config.md](../architecture/config.md) — `retention` fields on `appRegistries`
- [models.md](../architecture/models.md) — [RetentionIndex](../architecture/models.md#retentionindex)
- [lifecycle.md](./lifecycle.md) — fleet admission (`POST …/add`, `POST …/upgrade`)
- [pending-publish.md](./pending-publish.md) — `pending.json` / `failed.json` pruning

## Problem

Published versions and artifacts accumulate in GCS indefinitely. Most snapshots
are never fleet-known and can be cleaned up. Versions that were deployed are a
permanent audit trail: operators must be able to inspect the complete sequence
of accepted fleet changes even after those versions are no longer runnable.

## Goals

- Configurable retention on each registry binding (`unusedRetention`).
- Mutable `retention.json` per app tracking `lastUsedAt` and `everDeployed`.
- **Unused retention** (default `72h`) — delete published versions that were
  never fleet-known and idle longer than the window.
- **Permanent deploy history** — retain every version that appears as
  `to_version` in `app_version_change_requests`, including its index entry,
  version metadata, and artifacts.
- `gestaltd app registry retention prune` to delete eligible versions from GCS.
- Scheduled prune from the **deploy reader** side (not publish CI).
- A read-only **Revision history** tab on `/apps/{app}/admin` backed by the
  append-only deploy chain.

Fleet admission permanently changes a version from unused to deployed.
Publishing alone does not count. `everDeployed` is sticky and never resets.

## Config

On `appRegistries.{name}`:

```yaml
retention:
  unusedRetention: 72h
```

The default is `72h` when omitted. There is no deployed-version retention
window: deployed versions are permanent. See [config.md](../architecture/config.md).

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
| Publish | **Publisher** (`gestaltd app registry publish`) | Upsert `retention.json`: `lastUsedAt = publishedAt`, `everDeployed = false` |
| Fleet admission | **Reader** (`POST …/add`, `POST …/upgrade`) | Set `everDeployed = true` permanently and record `firstDeployedAt` |
| Delete | **Reader** (`gestaltd app registry retention prune`) | Remove only never-deployed eligible versions from `index.json`, `retention.json`, `versions/{version}.json`, and `artifacts/{version}/` |

Prune loads deploy config (`appRegistries.*.retention`) and fleet state before
deleting. It cross-checks `everDeployed` against
`app_version_change_requests`: if either source says a version was deployed,
the version is protected. This fail-safe prevents a missing or stale
`retention.json` update from deleting deploy history. Prune does not touch
`pending.json`, `failed.json`, or change requests.

```bash
gestaltd app registry retention prune \
  --bucket gs://… \
  --app g-issues \
  [--dry-run]
```

Run on a schedule from the deploy reader (for example a daily `gestaltd` cron).
Do not run from `gestaltd serve` request handlers.

## Protected versions

Do not delete while any guard holds:

| Guard | |
|-------|---|
| Version appears as `to_version` in `app_version_change_requests` | Permanent; preserves the full deploy chain |
| `retention.json` has `everDeployed: true` | Permanent; protects history if fleet lookup is incomplete |
| `version === desiredVersion` | |
| Active rollout target (`enrolling` or `restarting`) | |
| Entry in `pending.json` | |
| Last published version when `knownVersions` is empty | |

Protected historical versions are visible but not deployable. Version
selection rejects any version already present in the deploy chain; the
Revision history tab is an audit surface, not a rollback mechanism. See
[admin.md](./admin.md#revision-history-tab) and
[lifecycle.md](./lifecycle.md#revision-history).

## Implementation path

1. **PR 1 — Models and write path** (`gestalt`) — `RetentionIndex`, config
   validation, and permanent `everDeployed` updates during admission.
2. **PR 2 — Prune command** (`gestalt`) — `gestaltd app registry retention
   prune` with `--dry-run`, never-deployed eligibility, and change-request
   cross-checks.
3. **PR 3 — Scheduler** (deploy / ops) — scheduled `retention prune` per
   registry-only app.
4. **PR 4 — Revision history API/UI** (`gestalt`, `gestalt-providers`) —
   paginated deploy-chain API and read-only app-admin tab.
