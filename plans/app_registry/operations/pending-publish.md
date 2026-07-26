# Pending Publish Visibility

In-flight and recently failed app registry publishes on `/apps/{app}/admin`
before `index.json` is updated at the end of CI.

Related docs:

- [readme.md](../readme.md) — registry architecture and future work
- [changelog.md](../project/changelog.md) — implementation milestones and pull requests
- [models.md](../architecture/models.md) — [PendingIndex](../architecture/models.md#pendingindex) and [FailedIndex](../architecture/models.md#failedindex)
- [admin.md](./admin.md) — app admin UI capabilities
- [lifecycle.md](./lifecycle.md) — app-admin HTTP API
- [tests.md](../project/tests.md#pending-catalog-write-path) — write-path and admin tests

## Overview

`gestaltd app registry publish` updates `index.json` last, after packaging and
artifact upload. Without pending catalogs, operators saw nothing on
`/apps/{app}/admin` during a long CI run.

Mutable `pending.json` and `failed.json` per app record publish state in GCS.
`gestaltd serve` reads both catalogs over HTTP and exposes them on
`GET /api/v1/apps/{app}/admin/registry`. The `gestalt-providers` app admin UI
merges pending, failed, and published rows in **Published snapshots**.

Scope is `/apps/{app}/admin` only — the embedded fleet `/admin` registry UI does
not show publishing state.

| Event | Command |
|-------|---------|
| Publish starts | `gestaltd app registry pending set` |
| Publish succeeds | `gestaltd app registry pending clear` |
| Publish fails | `gestaltd app registry pending fail` |
| Pending stuck > 30 minutes | `PrunePendingIndex` on next `pending set` → `failed.json` with `reason=stale` |

Pending and failed versions are **not** installable. Install handlers read only
published `versions/{version}.json` entries via `index.json`.

## GCS layout

```text
apps/{app}/
├── index.json
├── pending.json
├── failed.json
├── versions/
│   └── {version}.json
└── artifacts/
    └── {version}/
        └── …
```

`pending.json` and `failed.json` are mutable catalogs. Updates use optimistic
concurrency: read generation, merge, upload with `if-generation-match`.

Document shapes: [PendingIndex](../architecture/models.md#pendingindex),
[FailedIndex](../architecture/models.md#failedindex).

### Phases and failure reasons

| `phase` | Meaning |
|---------|---------|
| `publishing` | Publish in progress |

| `reason` | Meaning |
|----------|---------|
| `workflow_failed` | CI called `gestaltd app registry pending fail` |
| `stale` | `PrunePendingIndex` moved a pending entry older than **30 minutes** |

## Publish duration

Durations are computed at read/UI time — not stored as separate GCS fields.
Field definitions: [`publishStartedAt`](../architecture/models.md#publishedversion).

| Row status | Start | End | Status label |
|------------|-------|-----|--------------|
| **Publishing** | `startedAt` | now | `Publishing` + `for 4m` |
| **Available** / **Deployed** / **Previously deployed** | `publishStartedAt` | `publishedAt` | `Published in 4m 32s` |
| **Failed** | `startedAt` | `failedAt` | `Failed after 35m` |

`gestaltd app registry publish` reads `pending.json` and copies
`PendingVersion.startedAt` into `publishStartedAt` on `versions/{version}.json`
and `index.json`. The field is optional in the JSON schema so legacy entries and
manual publishes that skipped `pending set` still decode.

The app-admin API includes `publishingForSeconds` on pending rows and
`publishDurationSeconds` on published and failed rows when start timestamps exist.

## Self-healing

`gestaltd serve` cannot write GCS. Prune helpers run on the
**`gestaltd app registry pending` write path** (CI).

`gestaltd app registry pending set` always:

1. Removes `failed.{version}` for the version being upserted (workflow retry).
2. Runs `PrunePendingIndex` (skips the target version; stale → `failed.json`;
   already-published → drop from pending).
3. Runs `PruneFailedIndex` (drop entries older than **30 days** or already
   published).
4. Upserts `pending.{version}` with `phase=publishing`.

`pending clear` and `pending fail` are idempotent when the version is absent from
pending. `pending fail` does not overwrite an existing `failed.json` entry.

## CI (`publish-app-registry.yml`)

Implemented in `toolshed` as two jobs:

```text
metadata
├── record-pending          (ubuntu; runs in parallel with darwin-python)
│     → pending set         (early — pending row visible during packaging)
└── publish                 (needs record-pending)
      → pending set         (refresh publication metadata)
      → package artifacts
      → gestaltd app registry publish
      → pending clear       (on success)
      → pending fail        (on failure)
```

`record-pending` lets `/apps/{app}/admin` show **Publishing** while
`darwin-python` and the main `publish` job still run. The publish job calls
`pending set` again before packaging so `updatedAt` and publication metadata stay
current.

Publication flags (`--workflow-run-url`, `--trigger-pr-number`,
`--trigger-pr-url`, `--trigger-pr-title`, or commit trigger fields) are built by
`.github/scripts/write_app_registry_publication_args.sh` and passed to both
`pending set` and `gestaltd app registry publish`.

`gestaltd app registry publish` does not touch `pending.json` or `failed.json`.
`gestaltd app publish` remains a deprecated alias.

### CLI

```bash
gestaltd app registry pending set \
  --bucket gs://… \
  --app g-issues \
  --version 0.0.0-snapshot.g… \
  --ref abc123… \
  --workflow-run-url … \
  [--trigger-pr-number … --trigger-pr-url … --trigger-pr-title …]

gestaltd app registry pending clear \
  --bucket gs://… \
  --app g-issues \
  --version 0.0.0-snapshot.g…

gestaltd app registry pending fail \
  --bucket gs://… \
  --app g-issues \
  --version 0.0.0-snapshot.g…

gestaltd app registry publish \
  --bucket gs://… \
  --app g-issues \
  --version 0.0.0-snapshot.g… \
  --ref abc123… \
  --dist-dir "${RUNNER_TEMP}/app-dist" \
  [publication flags]
```

## Read path

### `RegistryReader`

```go
func (r *RegistryReader) FetchPendingIndex(ctx context.Context, publicRoot, appName string) (*PendingIndex, error)
func (r *RegistryReader) FetchFailedIndex(ctx context.Context, publicRoot, appName string) (*FailedIndex, error)
```

HTTP GET `apps/{app}/pending.json` and `apps/{app}/failed.json`. Missing object
→ empty catalog.

### App-admin API

`GET /api/v1/apps/{app}/admin/registry` includes `pendingVersions[]` and
`failedVersions[]` alongside `publishedVersions[]`.

Merge rules:

- Each version appears in at most one of `pendingVersions`, `failedVersions`, and
  `publishedVersions`.
- Precedence: **published** > **pending** > **failed**.
- `pendingVersions` sorted by `startedAt` descending.
- `failedVersions` sorted by `failedAt` descending.

Install and upgrade handlers ignore `pending.json` and `failed.json`.

## UI (`/apps/{app}/admin`)

Wireframes and columns: [admin.md](./admin.md#app-admin-ui-appsappadmin).

| Status | Condition |
|--------|-----------|
| **Publishing** | Entry from `pendingVersions` |
| **Failed** | Entry from `failedVersions` |
| **Deployed** | `version === desiredVersion` |
| **Rolling out** | Active rollout for this version |
| **Previously deployed** | Published, not desired, and `previouslyDeployed` |
| **Available** | Published, not desired, and never deployed |

Only **Available** rows expose **Deploy**. Revision history is read-only and
appears on its own tab; see [admin.md](./admin.md#revision-history-tab).

Polling (`gestalt-providers`):

- Every **12s** while `pendingVersions.length > 0`, rollout is non-terminal, or
  selection is disabled.
- Every **12s** for **5 minutes** after page load (bootstrap window) so a
  CI-recorded pending row appears without manual refresh.
- Do not poll solely because `failedVersions` is non-empty — failed rows load on
  page fetch.

## Safety and invariants

| Invariant | Enforcement |
|-----------|-------------|
| Pending and failed versions are not installable | Install validation reads only published index entries |
| Pending and failed catalogs do not affect fleet state | No IndexedDB writes |
| Published contract per version stays immutable | `versions/{version}.json` and artifacts uploaded with `if-generation-match=0` |
| Stuck pending becomes failed | `PrunePendingIndex` on `pending set`; 30-minute `startedAt` threshold |
| Workflow retries clear prior failures | `pending set` removes `failed.{version}` for the version being upserted |
| Duplicate version across catalogs | API and UI apply **published** > **pending** > **failed** |

Implementation:

- Pending/failed types and prune — `gestaltd/internal/appregistry/pending.go`
- `gestaltd app registry pending` CLI — `gestaltd/internal/daemon/app_registry_pending.go`
- `gestaltd app registry publish` CLI — `gestaltd/internal/daemon/app_registry_publish.go`, `gestaltd/internal/daemon/app_publish.go`
- App-admin registry API — `gestaltd/internal/server/handlers_app_admin_registry.go`
- Registry fetch — `gestaltd/internal/appregistry/reader.go`
- App admin snapshots UI — `gestalt-providers/app/default` (`polling.ts`, `app-admin-snapshots-table.tsx`)

## Shipped in

- [gestalt#2932](https://github.com/valon-technologies/gestalt/pull/2932) — models, `pending` CLI, `publishStartedAt`
- [gestalt#2931](https://github.com/valon-technologies/gestalt/pull/2931) — read path and app-admin API
- [toolshed#3775](https://github.com/valon-technologies/toolshed/pull/3775) — `record-pending` job and CI lifecycle
- [gestalt-providers#1159](https://github.com/valon-technologies/gestalt-providers/pull/1159)–[#1161](https://github.com/valon-technologies/gestalt-providers/pull/1161) — app admin UI (pending/failed rows, polling, status column)
