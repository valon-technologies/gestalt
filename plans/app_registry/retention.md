# Published Version Retention

Delete published app registry versions that are no longer needed, using
configurable **default retention** windows on the deploy reader registry binding.

Related docs:

- [plan.md](./plan.md) — registry goals and implementation path
- [config.md](./config.md) — `appRegistries` and `retention` fields
- [models.md](./models.md) — registry JSON documents, including [RetentionIndex](./models.md#retentionindex)
- [lifecycle.md](./lifecycle.md) — fleet-known versions, rollouts, and `POST …/add` / `POST …/upgrade`
- [indexeddb.md](./indexeddb.md) — `app_version_change_requests` projection
- [admin.md](./admin.md) — **Published snapshots** table on `/apps/{app}/admin`
- [pending-publish.md](./pending-publish.md) — `pending.json` / `failed.json` pruning (separate concern)
- [service.md](./service.md) — Go publish/read helpers
- [tests.md](./tests.md) — retention tests (future)

## Problem

Each CI publish adds immutable `versions/{version}.json` entries and platform
archives under `artifacts/{version}/`. Nothing removes them today.

Most published snapshots are never fleet-known. Even versions that were desired
once are often superseded quickly. Without retention, operators must manually
curate the bucket or accept unbounded storage and a noisy **Published snapshots**
table on `/apps/{app}/admin`.

## Goals

Operators should configure a **default retention policy** per registry (with
optional per-app overrides) so `gestaltd` can delete eligible published versions
safely.

| Question | Answer |
|----------|--------|
| How long do we keep snapshots nobody installed? | **Unused retention** — default **3 days** (`unusedRetention: 72h`) |
| How long do we keep snapshots that were fleet-known before? | **Deployed retention** — default **7 days** (`deployedRetention: 168h`) since last fleet use |
| What resets the idle timer? | Fleet admission (`POST …/add`, `POST …/upgrade`), becoming **desired version**, or an operator pin |
| What must never be deleted? | **Desired version**, active rollout target, pending publish, operator pin, last published version when the fleet catalog is empty |

Publishing alone does **not** count as fleet use. A snapshot can sit in
`index.json` without ever becoming fleet-known.

## Design summary

Add mutable `retention.json` per app in GCS. Scope is registry bucket cleanup —
not local replica artifact directories (those are already trimmed per replica in
[lifecycle.md](./lifecycle.md#startup)).

| Actor | Responsibility |
|-------|----------------|
| **Publisher** (`gestaltd app registry publish`) | Append `index.json`, upload immutable version metadata and artifacts, upsert `retention.json` with `lastUsedAt = publishedAt` and `everDeployed = false` |
| **Reader** (`gestaltd serve` install handlers) | On fleet admission, bump `lastUsedAt` and set `everDeployed` on first admission |
| **Reader** (`gestaltd app registry retention prune`) | Evaluate policy, consult fleet state, delete eligible published versions from GCS |

Retention policy and prune decisions are **reader-owned**. The publisher only
seeds retention metadata at publish time. Prune must read deploy config
(`appRegistries.*.retention`) and fleet state (`desiredVersion`, active
rollouts) before deleting anything.

Two idle windows apply per published version:

| Window | Default | Applies when |
|--------|---------|--------------|
| **Unused retention** | `72h` | `everDeployed` is false |
| **Deployed retention** | `168h` | `everDeployed` is true |

At prune time, compare `now - lastUsedAt` against the applicable window.
`lastUsedAt` falls back to `publishedAt` from `index.json` when absent.

`gestaltd app registry retention prune` removes eligible versions from
`index.json`, `retention.json`, `versions/{version}.json`, and
`artifacts/{version}/`. It does **not** touch `pending.json` or `failed.json`.

## Retention policy

### Fleet use

A version is **used** when any of the following occur:

1. **Fleet admission** — a change request is appended with `to_version` equal to
   this version (`POST …/add` or `POST …/upgrade`). Includes first install and
   every re-select of the same version.
2. **Desired version** — the version becomes `LatestKnownVersion` for the app
   (including when a rollout completes and the version remains desired).
3. **Operator pin** — `retention.pinned: true` on the overlay entry (optional
   escape hatch; see [Protected versions](#protected-versions)).

Each fleet use updates `lastUsedAt` for `(app, version)` in `retention.json`.

### Previously fleet-known

`everDeployed` is set on first fleet admission and remains true for the lifetime
of the published version. `firstDeployedAt` records that first admission.
Previously fleet-known versions use **deployed retention** even after they are
no longer desired.

### Examples (defaults)

| Version history | `everDeployed` | `lastUsedAt` | Window | Eligible now? |
|-----------------|----------------|--------------|--------|---------------|
| Published 4 days ago, never fleet-known | false | `publishedAt` (4d ago) | unused (`72h`) | yes |
| Published yesterday, never fleet-known | false | yesterday | unused (`72h`) | no |
| Fleet-known 10 days ago, idle since | true | 10d ago | deployed (`168h`) | yes |
| Fleet-known 10 days ago, re-selected yesterday | true | yesterday | deployed (`168h`) | no |

## Config

Retention is configured on the deploy reader registry binding
(`appRegistries.{name}`). Field definitions and validation:
[config.md](./config.md).

```yaml
appRegistries:
  toolshed:
    kind: gcs
    gcs:
      bucket: gs://gestalt-app-registry
    retention:
      default:
        unusedRetention: 72h
        deployedRetention: 168h
      apps:
        g-issues:
          unusedRetention: 48h
          deployedRetention: 336h
```

Precedence for app `g-issues` on registry `toolshed`:

1. `retention.apps.g-issues.{field}` when set
2. else `retention.default.{field}`
3. else built-in default (`72h` / `168h`)

Durations use Go `time.ParseDuration` syntax. Validation rejects zero,
negative values, and `deployedRetention < unusedRetention` at the same scope.

## GCS layout

Extend the per-app registry tree:

```text
apps/{app}/
├── index.json
├── retention.json
├── pending.json
├── failed.json
├── versions/
│   └── {version}.json
└── artifacts/
    └── {version}/
        └── …
```

`retention.json` is a mutable catalog (like `index.json`), not an immutable
release object. Document shape, fields, and example:
[models.md — RetentionIndex](./models.md#retentionindex).

Use the same optimistic-concurrency update pattern as other catalog writes:
read generation, merge, upload with `if-generation-match`.

## Write path

### Publish (`gestaltd app registry publish`)

After uploading `versions/{version}.json` and artifacts, append the version to
`index.json` and upsert `retention.json`:

- `lastUsedAt = publishedAt`
- `everDeployed = false`

Publish does not evaluate retention or delete older versions.

### Fleet admission (`POST …/add`, `POST …/upgrade`)

After validation and `AppendRequest`, update `retention.json` for the admitted
version:

- `lastUsedAt = now`
- on first admission: `everDeployed = true`, `firstDeployedAt = now`

Install handlers do not delete registry objects. Deletion is confined to
`gestaltd app registry retention prune`.

## Prune path

### `gestaltd app registry retention prune`

```bash
gestaltd app registry retention prune \
  --bucket gs://… \
  --app g-issues \
  [--dry-run]
```

`prune` loads deploy config (retention durations), reads `index.json` and
`retention.json`, consults fleet state (`desiredVersion`, active rollouts,
known versions), and deletes eligible published versions.

For each eligible version, remove:

1. `apps/{app}/versions/{version}.json`
2. `apps/{app}/artifacts/{version}/` (all platform objects)
3. `versions.{version}` from `index.json`
4. `versions.{version}` from `retention.json`

Prune is idempotent and safe to re-run. Deleting an already-removed version is
a no-op.

### Scheduler

Run retention prune on a schedule from the **deploy reader** side — for example
a `gestaltd` cron job with deploy config and fleet API access. Prune requires
fleet guards; publish CI alone is not sufficient.

Do not run retention prune from `gestaltd serve` request handlers. Like
[pending-publish.md](./pending-publish.md#self-healing), catalog mutation runs
on an explicit CLI path with bucket credentials.

Recommended order per app when both run:

1. `gestaltd app registry pending set` prune helpers (existing)
2. `gestaltd app registry retention prune` (this feature)

## Protected versions

Retention must **not** delete a version while any guard holds:

| Guard | Reason |
|-------|--------|
| `version === desiredVersion` | Fleet is pinned to this version |
| Active rollout with `version` as target (`enrolling` or `restarting`) | Install in flight |
| Entry in `pending.json` for `version` | Publish not finished. See [pending-publish.md](./pending-publish.md). |
| `retention.pinned: true` | Operator pin until unpinned |
| Last published version when `knownVersions` is empty | Prevent bricking a fresh app with no alternatives |

Protected versions remain in `index.json` until the guard clears and the idle
window elapses.

## Relationship to pending/failed pruning

| Mechanism | Catalog | Threshold | Owner | Purpose |
|-----------|---------|-----------|-------|---------|
| `PrunePendingIndex` | `pending.json` | 30 minutes `startedAt` | Publisher (CI `pending set`) | Stale in-flight publishes → `failed.json` |
| `PruneFailedIndex` | `failed.json` | 30 days `failedAt` | Publisher (CI `pending set`) | Old failed publish records |
| **Retention prune** | `index.json` + artifacts | configurable idle windows | Reader (`retention prune`) | Remove unused published versions |

Pending/failed pruning does not remove installable published versions.
Retention prune does not touch `pending.json` or `failed.json`.

## Admin UI

`/apps/{app}/admin` continues to list published versions from `index.json`.
Deleted versions disappear on the next poll. Wireframes and columns:
[admin.md](./admin.md#app-admin-ui-appsappadmin).

Optional follow-ups (not required for v1):

- Expose `lastUsedAt`, `everDeployed`, and computed `retainsUntil` on the
  app-admin registry API.
- Show retention metadata in the **Last update** column or a dedicated column.
- Pin/unpin control writing `retention.pinned`.

## Safety and invariants

| Invariant | Enforcement |
|-----------|-------------|
| Published contract per version stays immutable until delete | `versions/{version}.json` is never mutated; retention deletes whole versions only |
| Fleet cannot install a deleted version | Install validation reads `index.json`; missing entry → 400 |
| Desired version is never deleted | Prune skips when `version === desiredVersion` |
| Active rollout target is never deleted | Prune skips non-terminal rollouts for that version |
| Pending publish is never deleted | Prune skips versions present in `pending.json` |
| Deployed grace ≥ unused grace | Config validation at load |
| Retention metadata survives publish | `retention.json` upsert on publish; overlay outlives individual prune passes |

Implementation:

- Retention types and prune — `gestaltd/internal/appregistry/retention.go`
- `gestaltd app registry retention` CLI — `gestaltd/internal/daemon/app_registry_retention.go`
- Overlay writes on publish — `gestaltd/internal/daemon/app_registry_publish.go`
- Overlay writes on admission — `gestaltd/internal/server/handlers_admin_app_install.go`
- Config validation — `gestaltd/internal/config/`

## Implementation path

Four PRs. Merge in order below.

```text
PR 1 (gestalt) ──► PR 2 (gestalt) ──► PR 4 (gestalt-providers, optional)
       │
       └──────────► PR 3 (deploy scheduler)
```

### PR 1 — Models and write path (`gestalt`)

- Add `RetentionIndex` / `RetentionVersion` to `gestaltd/internal/appregistry/`
  ([models.md](./models.md#retentionindex) documents shapes).
- `gestaltd app registry publish` initializes `retention.json` for the new version.
- Install handlers bump `lastUsedAt` and set `everDeployed` on first admission.
- Config validation for `appRegistries.*.retention` in `gestaltd/internal/config/`.
- Tests: overlay upsert, admission bumps, config validation. Extend
  [tests.md](./tests.md).

### PR 2 — Prune command (`gestalt`)

Depends on **PR 1**.

- `gestaltd app registry retention prune` with `--dry-run`.
- Fleet guard queries (desired version, rollout state).
- GCS delete of version metadata, artifacts, and index/retention entries.
- Tests: eligibility matrix, protected versions, dry-run.

### PR 3 — Scheduler (deploy / ops)

Depends on **PR 2**. Run from the reader side with deploy config and fleet access.

- Scheduled job (daily) calling `retention prune` per registry-only app.
- Dry-run reporting in logs before enabling destructive mode.

### PR 4 — Admin API/UI (optional)

Depends on **PR 2**.

- Expose retention metadata on `GET /api/v1/apps/{app}/admin/registry`.
- Optional UI column or tooltip on `/apps/{app}/admin`. See [admin.md](./admin.md).
