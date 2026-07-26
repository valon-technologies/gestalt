# Published Version Retention

Delete published app registry versions that are no longer needed, using
configurable retention windows on `appRegistries`.

Related docs:

- [config.md](./config.md) — `retention` fields on `appRegistries`
- [models.md](./models.md) — [RetentionIndex](./models.md#retentionindex)
- [lifecycle.md](./lifecycle.md) — fleet admission (`POST …/add`, `POST …/upgrade`)
- [pending-publish.md](./pending-publish.md) — `pending.json` / `failed.json` pruning

## Problem

Published versions and artifacts accumulate in GCS indefinitely. Most snapshots
are never fleet-known; many that were desired once are superseded quickly.

## Goals

- Configurable retention on each registry binding (`unusedRetention`,
  `deployedRetention`).
- Mutable `retention.json` per app tracking `lastUsedAt`, `everDeployed`, and
  optional `pinned`.
- **Unused retention** (default `72h`) — delete published versions that were
  never fleet-known and idle longer than the window.
- **Deployed retention** (default `168h`) — delete previously fleet-known versions
  idle longer than the window since last fleet use. Timer resets on reuse.
- `gestaltd app registry retention prune` to delete eligible versions from GCS.
- Scheduled prune from the **deploy reader** side (not publish CI).
- Optional: retention metadata on the app-admin registry API and `/apps/{app}/admin`.

Fleet use is fleet admission, becoming **desired version**, or an operator pin.
Publishing alone does not count.

## Config

On `appRegistries.{name}`:

```yaml
retention:
  unusedRetention: 72h
  deployedRetention: 168h
```

Defaults: `72h` / `168h` when omitted. See [config.md](./config.md).

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
[models.md — RetentionIndex](./models.md#retentionindex). Updates use the same
optimistic-concurrency pattern as `index.json` (`if-generation-match`).

## Who writes and who deletes

| Action | Owner | What |
|--------|-------|------|
| Publish | **Publisher** (`gestaltd app registry publish`) | Upsert `retention.json`: `lastUsedAt = publishedAt`, `everDeployed = false` |
| Fleet admission | **Reader** (`POST …/add`, `POST …/upgrade`) | Bump `lastUsedAt`; on first admission set `everDeployed = true`, `firstDeployedAt = now` |
| Delete | **Reader** (`gestaltd app registry retention prune`) | Remove eligible versions from `index.json`, `retention.json`, `versions/{version}.json`, and `artifacts/{version}/` |

Prune loads deploy config (`appRegistries.*.retention`) and fleet state
(`desiredVersion`, active rollouts) before deleting. It does not touch
`pending.json` or `failed.json`.

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
| `version === desiredVersion` | |
| Active rollout target (`enrolling` or `restarting`) | |
| Entry in `pending.json` | |
| `retention.pinned: true` | |
| Last published version when `knownVersions` is empty | |

## Implementation path

1. **PR 1 — Models and write path** (`gestalt`) — `RetentionIndex`, config validation, publish and install handler updates.
2. **PR 2 — Prune command** (`gestalt`) — `gestaltd app registry retention prune` with `--dry-run` and fleet guards.
3. **PR 3 — Scheduler** (deploy / ops) — scheduled `retention prune` per registry-only app.
4. **PR 4 — Admin API/UI** (optional) — retention fields on app-admin registry API and `/apps/{app}/admin`.
