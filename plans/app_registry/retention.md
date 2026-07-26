# App Registry Version Retention

Automatic cleanup of published app registry versions that are no longer needed,
with configurable **default retention** windows.

Related docs:

- [plan.md](./plan.md) — registry goals and implementation path
- [models.md](./models.md) — `index.json` and published version documents
- [config.md](./config.md) — deploy reader config and registry bindings
- [lifecycle.md](./lifecycle.md) — fleet-known versions, rollouts, and desired version
- [indexeddb.md](./indexeddb.md) — `app_version_change_requests` projection
- [admin.md](./admin.md) — app admin snapshots UI
- [pending-publish.md](./pending-publish.md) — pending/failed catalog pruning (separate concern)
- [service.md](./service.md) — Go publish/read helpers
- [tests.md](./tests.md) — test plan additions (future)

## Problem

Published versions and their artifacts accumulate indefinitely in GCS. Each CI
publish adds immutable `versions/{version}.json` entries and platform archives
under `artifacts/{version}/`.

Most snapshots are never selected for fleet install. Even versions that were
deployed once are often superseded quickly. Without retention, operators must
manually curate the bucket or accept unbounded storage and a noisy admin
snapshots table.

## Goals

Operators should be able to configure a **default retention policy** per registry
(or per app) so gestaltd can delete eligible versions safely.

The initial policy has two configurable windows:

| Rule | Default | Meaning |
|------|---------|---------|
| **Unused retention** | 3 days | Delete a version that has **never been deployed** and has had **no qualifying use** for this long. |
| **Deployed retention** | 7 days | Delete a version that **has been deployed before** when it has had **no qualifying use** for this long. |

**Qualifying use** resets the retention clock for that version. Deploying a
version counts as use and also marks it as *ever deployed*.

Retention must never delete versions that are still required for safe fleet
operation (current desired version, active rollout target, in-flight publish).

## Definitions

### Published version

A version listed in `apps/{app}/index.json` with a corresponding
`versions/{version}.json` and artifact objects. Pending and failed catalog
entries are out of scope for this document; see [pending-publish.md](./pending-publish.md).

### Used (qualifying use)

A version is **used** at time `T` when any of the following occur:

1. **Fleet admission** — a change request is appended with `to_version` equal to
   this version (`POST …/add` or `POST …/upgrade`). This includes the first
   install and every re-select of the same version.
2. **Desired version** — the version becomes `LatestKnownVersion` for the app
   (including when a rollout completes and the version remains desired).
3. **Operator retention touch** — an explicit CLI/API call extends retention for
   a pinned version (optional escape hatch; see [Protected versions](#protected-versions)).

Each qualifying use updates `lastUsedAt` for `(app, version)` in the retention
overlay (see [Retention overlay](#retention-overlay)).

Publishing alone does **not** count as use. A snapshot can exist in the registry
without ever being fleet-known.

### Deployed (ever deployed)

A version is **ever deployed** after the first qualifying use that admits it to
the fleet catalog (rule 1 above). The flag is sticky: once set, the version
follows **deployed retention** even if it is later superseded.

`firstDeployedAt` is recorded on first admission. `lastUsedAt` continues to
advance on every subsequent qualifying use.

### Retention clock

For each published `(app, version)`, retention evaluation uses:

- `lastUsedAt` — timestamp of the most recent qualifying use; if absent, fall
  back to `publishedAt` from the index entry.
- `everDeployed` — whether the version was fleet-admitted at least once.

At evaluation time `now`:

```text
if everDeployed:
    eligible when (now - lastUsedAt) >= deployedRetention
else:
    eligible when (now - lastUsedAt) >= unusedRetention
```

Defaults: `unusedRetention = 3 days`, `deployedRetention = 7 days`. Both are
configurable.

### Examples

| Version history | `everDeployed` | `lastUsedAt` | Policy | Eligible at `now` (defaults) |
|-----------------|----------------|--------------|--------|------------------------------|
| Published 4 days ago, never selected | false | `publishedAt` (4d ago) | unused (3d) | yes |
| Published yesterday, never selected | false | yesterday | unused (3d) | no |
| Deployed 10 days ago, never touched since | true | 10d ago | deployed (7d) | yes |
| Deployed 10 days ago, re-selected yesterday | true | yesterday | deployed (7d) | no |
| Published 5 days ago, deployed 5 days ago, idle since | true | 5d ago | deployed (7d) | no |
| Published 5 days ago, deployed 5 days ago, idle since | true | 8d ago | deployed (7d) | yes |

## Default retention policy

**Default retention** is the registry-wide baseline. Per-app overrides may
tighten or relax either window without changing the semantics above.

### Config

Retention is configured on the deploy reader registry binding
(`appRegistries.{name}`). It governs cleanup for apps published to that registry
bucket, not local replica artifact directories (those are already trimmed per
replica in [lifecycle.md](./lifecycle.md#startup)).

```yaml
apiVersion: gestaltd.config/v8

appRegistries:
  toolshed:
    kind: gcs
    gcs:
      bucket: gitlab-peach-street-gestalt-app-registry
    retention:
      default:
        unusedRetention: 72h    # 3 days
        deployedRetention: 168h   # 7 days
      apps:
        g-issues:
          unusedRetention: 48h    # optional per-app override
          deployedRetention: 336h # optional per-app override (14 days)
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `retention.default.unusedRetention` | duration | `72h` | Delete never-deployed versions after this idle period. |
| `retention.default.deployedRetention` | duration | `168h` | Delete ever-deployed versions after this idle period since last use. |
| `retention.apps.{app}` | object | — | Optional per-app overrides for either field. |

Durations use Go `time.ParseDuration` syntax (`72h`, `168h`, `30m`). Validation
rejects zero, negative, or `deployedRetention < unusedRetention` when both are
set at the same scope (deployed grace must be at least as long as unused).

### Precedence

For app `g-issues` on registry `toolshed`:

1. `retention.apps.g-issues.{field}` when set
2. else `retention.default.{field}`
3. else built-in default (`72h` / `168h`)

## Protected versions

Retention must **not** delete a version while any of the following hold:

| Guard | Reason |
|-------|--------|
| `version === desiredVersion` | Fleet is pinned to this version. |
| Active rollout with `version` as target (`enrolling` or `restarting`) | Install in flight. |
| Pending catalog entry for `version` | Publish not finished. See [pending-publish.md](./pending-publish.md). |
| `retention.pinned: true` in overlay | Operator pin (optional; bypass until unpinned). |
| Last published version for the app when `knownVersions` is empty | Prevent bricking a fresh app with no alternatives. |

Protected versions remain in `index.json` until the guard clears and the
retention clock elapses.

## Retention overlay

Usage timestamps are mutable fleet/registry metadata. They must not be written
into immutable `versions/{version}.json`.

Add a per-app overlay document:

```text
apps/{app}/
├── index.json
├── retention.json          # new
├── pending.json
├── failed.json
└── …
```

### `retention.json`

```json
{
  "schemaVersion": 1,
  "versions": {
    "0.0.0-snapshot.gabc123": {
      "lastUsedAt": "2026-07-22T14:00:00Z",
      "firstDeployedAt": "2026-07-22T14:00:00Z",
      "everDeployed": true,
      "pinned": false
    },
    "0.0.0-snapshot.gdef456": {
      "lastUsedAt": "2026-07-24T09:00:00Z",
      "everDeployed": false,
      "pinned": false
    }
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schemaVersion` | int | yes | Currently `1`. |
| `versions` | map | yes | Version string → `RetentionVersion`. |
| `versions.{v}.lastUsedAt` | RFC 3339 | yes | Last qualifying use; initialized to `publishedAt` on first index write if absent. |
| `versions.{v}.firstDeployedAt` | RFC 3339 | no | First fleet admission. |
| `versions.{v}.everDeployed` | bool | yes | Sticky flag set on first admission. |
| `versions.{v}.pinned` | bool | no | Operator pin; default `false`. |

`gestaltd app registry publish` appends the version to `index.json` and upserts
`retention.json` with `lastUsedAt = publishedAt`, `everDeployed = false`.

Fleet install handlers update the overlay when appending change requests:

- set `lastUsedAt = now`
- on first admission: `everDeployed = true`, `firstDeployedAt = now`

## Cleanup

### Command

```bash
gestaltd app registry retention prune \
  --bucket gs://gitlab-peach-street-gestalt-app-registry \
  --app g-issues \
  [--dry-run]
```

`prune` loads deploy config (for retention durations and registry name),
reads `index.json` and `retention.json`, consults fleet state
(`desiredVersion`, active rollouts, known versions), and deletes eligible
versions.

### Delete scope

For each eligible version, remove:

1. `apps/{app}/versions/{version}.json`
2. `apps/{app}/artifacts/{version}/` (all platform objects)
3. The `versions.{version}` entry from `index.json`
4. The `versions.{version}` entry from `retention.json`

Use the same optimistic concurrency pattern as `pending set` / index updates
(read generation, `if-generation-match` on upload).

### Scheduler

Run retention prune on a schedule per registry (for example daily via
toolshed/GitHub Actions or a gestaltd cron job). Prune is safe to re-run;
deleting an already-removed version is a no-op.

Recommended order per app:

1. `gestaltd app registry pending set` prune helpers (existing)
2. `gestaltd app registry retention prune` (this feature)

## Admin UI

`/apps/{app}/admin` continues to list published versions from `index.json`.
Deleted versions disappear on the next poll.

Optional follow-ups (not required for v1):

- Show `lastUsedAt` in the **Last update** column or a dedicated column when the
  API exposes retention metadata.
- Badge versions nearing expiry (`expires in 1d`).
- Pin/unpin control writing `retention.pinned`.

## Safety and invariants

| Invariant | Enforcement |
|-----------|-------------|
| Immutable published contract per version | `versions/{version}.json` is never mutated; retention only deletes whole versions |
| Fleet cannot install a deleted version | Install validation reads `index.json`; missing entry → 400 |
| Desired version is never deleted | Prune skips when `version === desiredVersion` |
| Active rollout target is never deleted | Prune skips non-terminal rollouts for that version |
| Pending publish is never deleted | Prune skips versions present in `pending.json` |
| Deployed grace ≥ unused grace | Config validation at load |
| Retention metadata survives publish | `retention.json` upsert on publish; overlay outlives individual prune passes |

## Implementation path

Suggested PR stack:

### PR 1 — Models and overlay write path (`gestalt`)

- Add `RetentionIndex` / `RetentionVersion` types in
  `gestaltd/internal/appregistry/`.
- Document shapes in [models.md](./models.md#retentionindex).
- `gestaltd app registry publish` initializes `retention.json` for the new version.
- Install handlers (`POST …/add`, `POST …/upgrade`) bump `lastUsedAt` and set
  `everDeployed` on first admission.
- Config validation for `appRegistries.*.retention` in
  `gestaltd/internal/config/`.
- Tests: overlay upsert, admission bumps, config validation.

### PR 2 — Prune command (`gestalt`)

- `gestaltd app registry retention prune` with `--dry-run`.
- Fleet guard queries (desired version, rollout state).
- GCS delete of version metadata, artifacts, and index/retention entries.
- Tests: eligibility matrix, protected versions, dry-run.

### PR 3 — Scheduler (`toolshed` or ops)

- Scheduled workflow (daily) calling `retention prune` per app in
  `.github/valon-tools-app-registry-apps.yaml`.
- Dry-run reporting in CI logs before enabling destructive mode.

### PR 4 — Admin API/UI (optional)

- Expose `lastUsedAt`, `everDeployed`, and computed `retainsUntil` on app-admin
  registry responses.
- UI column or tooltip for retention status.

## Relationship to other pruning

| Mechanism | Catalog | Threshold | Purpose |
|-----------|---------|-----------|---------|
| `PrunePendingIndex` | `pending.json` | 30 minutes `startedAt` | Stale in-flight publishes → `failed.json` |
| `PruneFailedIndex` | `failed.json` | 30 days `failedAt` | Old failed publish records |
| **Retention prune** | `index.json` + artifacts | configurable unused/deployed idle | Remove unused published versions |

Pending/failed pruning does not remove installable published versions.
Retention prune does not touch `pending.json` or `failed.json`.

## Open questions

1. **Cross-registry apps** — retention is per registry bucket; fleet state is
   global per deploy. Prune must resolve the registry name from deploy config
   for each app.
2. **Minimum catalog size** — whether to retain at least *N* newest published
   versions regardless of age (not in v1; can add `minVersions: 5` later).
3. **Audit log** — whether to write deletion events to structured logs or a
   `apps/{app}/retention-audit.jsonl` before object removal.
