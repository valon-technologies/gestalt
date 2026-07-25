# Pending Publish Visibility

Show in-flight app registry publishes on `/apps/{app}/admin` before `index.json`
is updated at the end of CI.

Related docs:

- [plan.md](./plan.md) — registry goals and implementation path
- [models.md](./models.md) — registry JSON documents, including [PendingIndex](./models.md#pendingindex) and [FailedIndex](./models.md#failedindex)
- [admin.md](./admin.md) — app admin UI capabilities
- [service.md](./service.md) — Go publish/read helpers
- [lifecycle.md](./lifecycle.md) — app-admin HTTP API

---

## Problem

Today the **Published snapshots** table only reflects versions listed in
`apps/{app}/index.json`. That file is updated as the **last** step of
`gestaltd app registry publish`, after packaging and artifact upload complete.

During CI (`publish-app-registry.yml`), operators see nothing on the admin page
while a publish is running — even though the version string and workflow run are
already known. There is no registry signal between “workflow started” and
“index updated”.

---

## Goals

Operators on `/apps/{app}/admin` should be able to answer:

| Question | Admin surface |
|----------|---------------|
| Is a new snapshot being published right now? | Row with status **Publishing** |
| Which commit / PR triggered it? | Pending entry provenance (same fields as published rows where available) |
| Where is the CI run? | Link to `publication.workflowRunUrl` |
| When did publish start? | `startedAt` on the pending entry |
| How long has publish been running? | Elapsed time from `startedAt` on **Publishing** rows |
| How long did publish take? | Total publish time on published and **Failed** rows |
| Did a recent publish fail? | Row with status **Failed** and `failedAt` |
| Why did it fail? | `reason` on the failed entry (`workflow_failed` or `stale`) |

---

## Design summary

Add mutable `pending.json` and `failed.json` catalogs in GCS per app.

- **Start** — CI calls `gestaltd app registry pending set` when the publish
  workflow starts.
- **Success** — `gestaltd app registry pending clear` removes the pending entry.
- **Failure** — `gestaltd app registry pending fail` removes the pending entry
  and records `failedAt` in `failed.json`.
- **Stale** — `PrunePendingIndex` on the next `pending set` moves entries whose
  `startedAt` is older than **30 minutes** to `failed.json` with `reason=stale`.

`gestaltd` reads both catalogs alongside `index.json` and exposes them through
the app-admin registry API. The UI merges pending, failed, and published entries
in the snapshots table.

Pending and failed versions are **not** installable. Install-time validation
continues to read only `versions/{version}.json` via the published index
contract.

---

## Publish duration

Show how long a publish has been running and how long it took to complete.
Durations are derived from timestamps at read/UI time — not stored as separate
GCS fields. Field definitions: [`publishStartedAt`](./models.md#publishedversion).

| Row status | Start timestamp | End timestamp | UI label |
|------------|-----------------|---------------|----------|
| **Publishing** | `startedAt` | now | Publishing for 4m |
| **Available** / **Deployed** | `publishStartedAt` | `publishedAt` | Published in 4m 32s |
| **Failed** | `startedAt` | `failedAt` | Failed after 35m |

`gestaltd app registry publish` reads `pending.json` when present and copies
`PendingVersion.startedAt` into `publishStartedAt` on `versions/{version}.json`
and `index.json`. CI publishes always run `pending set` first, so new registry
entries include `publishStartedAt`. The field is **not required in the JSON
schema** so readers tolerate legacy entries and manual publishes that skipped
`pending set`; the UI shows `publishedAt` only when the start timestamp is absent.

The app-admin API may include `publishingForSeconds` on pending rows and
`publishDurationSeconds` on published and failed rows. Omit these fields when the
start timestamp is missing.

---

## GCS layout

Extend the per-app registry tree:

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

`pending.json` and `failed.json` are mutable catalogs (like `index.json`), not
immutable release objects. Use the same optimistic-concurrency update pattern as
index writes: read generation, merge, upload with `if-generation-match`.

---

## PendingIndex

`pending.json` document shape, fields, and example:
[models.md — PendingIndex](./models.md#pendingindex).

### Phases

| Phase | Meaning |
|-------|---------|
| `publishing` | Publish in progress — from workflow start through artifact upload and `index.json` update. |

Record `phase=publishing` at workflow start.

On workflow success, `gestaltd app registry pending clear` removes the pending
entry. No failed record is written.

On workflow failure, `gestaltd app registry pending fail` removes the pending
entry and upserts `failed.json` with `failedAt` and `reason=workflow_failed`.
The workflow run URL remains the failure audit trail.

---

## FailedIndex

`failed.json` document shape, fields, and example:
[models.md — FailedIndex](./models.md#failedindex).

### Failure reasons

| `reason` | Meaning |
|----------|---------|
| `workflow_failed` | CI called `gestaltd app registry pending fail` after packaging or publish failed |
| `stale` | Pending `startedAt` was older than 30 minutes when `PrunePendingIndex` ran |

---

## Self-healing

`gestaltd serve` reads `pending.json` and `failed.json` over public HTTP and
cannot write back to GCS. Prune helpers run on the **`gestaltd app registry
pending` write path** (CI), which already holds bucket credentials.

`gestaltd app registry pending set` always runs prune helpers before upserting.

### `PrunePendingIndex`

For each `pending.{version}` entry:

| Condition | Action |
|-----------|--------|
| `startedAt` older than **30 minutes** | Move to `failed.json` with `failedAt=now`, `reason=stale` |
| Same `version` already listed in `index.json` | Remove from pending only (publish succeeded; pending cleanup missed) |
| Otherwise | Keep |

Use `startedAt`, not `updatedAt`, for the stale threshold. `updatedAt` is only
refreshed when the same version re-runs `pending set`.

### `PruneFailedIndex`

For each `failed.{version}` entry:

| Condition | Action |
|-----------|--------|
| `failedAt` older than **30 days** | Remove |
| Same `version` already listed in `index.json` | Remove |
| Otherwise | Keep |

The 30-minute pending threshold applies on `gestaltd app registry pending set`.
Failed entries are retained for 30 days so operators can see recent failures on
the admin page.

`pending set` also **removes `failed.{version}`** for the version being upserted
before prune helpers run, so a workflow retry does not leave the same version in
both catalogs.

`PrunePendingIndex` and `PruneFailedIndex` use the same optimistic-concurrency
loop as other catalog writes: download generation, merge, upload with
`if-generation-match`.

Every app publish workflow self-heals stuck pending entries on the next merge to
`main` for that app.

Prune is idempotent and safe to run concurrently with an in-flight publish:
generation-match retries apply.

---

## Publish lifecycle

```text
CI: publish-app-registry.yml
│
├─1. Resolve PACKAGE_VERSION + PROVIDER_REF
│
├─2. gestaltd app registry pending set
│      (same --bucket/--app/--version/--ref/--workflow-run-url flags as step 4)
│      → remove failed.{version} for this version (workflow retry)
│      → PrunePendingIndex (stale → failed.json; already-published → drop)
│      → PruneFailedIndex (drop entries older than 30 days or already published)
│      → upsert pending.json for this version (phase=publishing)
│
├─3. Package artifacts (may take tens of minutes)
│
├─4. gestaltd app registry publish --dist-dir … (artifacts + versions/{version}.json + index.json)
│
└─5. gestaltd app registry pending clear|fail  (always())
       → success: pending clear
       → failure: pending fail
```

Today, `publish-app-registry.yml` runs `gestaltd app publish --dist-dir …` only.
This plan adds `gestaltd app registry pending` for steps 2 and 5, migrates step
4 to `gestaltd app registry publish`, and records failures in `failed.json`.

On success, step 4 uploads artifacts and updates `index.json` (unchanged order
— index last). Step 5 runs `gestaltd app registry pending clear`.

On failure, step 5 runs `gestaltd app registry pending fail`, recording
`failedAt` in `failed.json`. If step 5 itself fails, the next
`gestaltd app registry pending set` for any app version moves a stale pending
entry to `failed.json` with `reason=stale`.

### CLI

`gestaltd app registry` gains:

```text
gestaltd app registry pending set|clear|fail
gestaltd app registry publish
```

`gestaltd app registry publish` replaces `gestaltd app publish` for immutable
uploads only. `gestaltd app registry pending` owns the mutable `pending.json` and
`failed.json` catalogs. Keep `gestaltd app publish` as a deprecated alias for
`gestaltd app registry publish` during migration, then remove it.

**`gestaltd app registry pending`** — write or update `pending.json` and
`failed.json`. `pending set` runs `PrunePendingIndex` and `PruneFailedIndex`
before upserting.

```bash
# Workflow start — record pending publish (prunes stuck entries first)
gestaltd app registry pending set \
  --bucket gs://… \
  --app traffic-cop \
  --version 0.0.0-snapshot.g… \
  --ref abc123… \
  --workflow-run-url … \
  [--trigger-pr-number … --trigger-pr-url …]

# Workflow end — success (idempotent)
gestaltd app registry pending clear \
  --bucket gs://… \
  --app traffic-cop \
  --version 0.0.0-snapshot.g…

# Workflow end — failure (idempotent)
gestaltd app registry pending fail \
  --bucket gs://… \
  --app traffic-cop \
  --version 0.0.0-snapshot.g…
```

`pending set` writes `phase=publishing`. It is idempotent for the same
`(app, version)`: refresh `updatedAt` and merge `publication` on re-run.
`pending clear` is idempotent when the version is already absent from pending.
`pending fail` is idempotent when the version is already absent from pending; an
existing `failed.json` entry is left unchanged.

**`gestaltd app registry publish`** — upload artifacts and version metadata,
update `index.json` only. Does not write `pending.json` or `failed.json`. Reads
`pending.json` when present to copy `startedAt` into `publishStartedAt` on the
uploaded version metadata and index entry. Same flags as today's
`gestaltd app publish`; requires `--dist-dir`.

```bash
gestaltd app registry publish \
  --bucket gs://… \
  --app traffic-cop \
  --version 0.0.0-snapshot.g… \
  --ref abc123… \
  --dist-dir "${RUNNER_TEMP}/app-dist" \
  --workflow-run-url … \
  [publication flags]
```

CI runs `gestaltd app registry pending set` at workflow start,
`gestaltd app registry publish` after packaging, and
`gestaltd app registry pending clear` or `gestaltd app registry pending fail` in
`always()` at workflow end depending on outcome. Manual publishes can call
`gestaltd app registry pending set` first and `gestaltd app registry pending
clear` or `gestaltd app registry pending fail` afterward when they want admin
UI visibility.

Implementation extends `gestaltd/internal/daemon/app_publish.go` and reuses the
index upload helper (generation-match retries).

---

## Read path

### `RegistryReader`

Add:

```go
func (r *RegistryReader) FetchPendingIndex(ctx context.Context, publicRoot, appName string) (*PendingIndex, error)
func (r *RegistryReader) FetchFailedIndex(ctx context.Context, publicRoot, appName string) (*FailedIndex, error)
```

- HTTP GET `apps/{app}/pending.json` and `apps/{app}/failed.json`
- Missing object → empty catalog (same as missing `index.json`)
- Decode and validate `schemaVersion`

### App-admin API

Extend `GET /api/v1/apps/{app}/admin/registry` response:

```json
{
  "app": "traffic-cop",
  "registry": "toolshed",
  "desiredVersion": "0.0.0-snapshot.g…",
  "knownVersions": [ … ],
  "publishedVersions": [ … ],
  "pendingVersions": [
    {
      "version": "0.0.0-snapshot.g…",
      "startedAt": "2026-07-24T19:00:00Z",
      "updatedAt": "2026-07-24T19:04:12Z",
      "phase": "publishing",
      "sourceRef": "abc123…",
      "sourceUrl": "https://github.com/…/commit/abc123…",
      "publication": { … }
    }
  ],
  "failedVersions": [
    {
      "version": "0.0.0-snapshot.g…",
      "startedAt": "2026-07-24T18:00:00Z",
      "failedAt": "2026-07-24T18:35:00Z",
      "reason": "stale",
      "sourceRef": "abc123…",
      "sourceUrl": "https://github.com/…/commit/abc123…",
      "publication": { … }
    }
  ],
  "rollout": { … },
  "selectionDisabled": false
}
```

Merge rules:

- Each version appears in **at most one** of `pendingVersions`, `failedVersions`,
  and `publishedVersions` in the API response and snapshots table.
- **Precedence** (when a race or missed cleanup leaves duplicates in GCS):
  **published** > **pending** > **failed**. Keep the highest-precedence row and
  omit the others. Example: a version in both `pending.json` and `failed.json`
  during a workflow retry surfaces as **Publishing**, not **Failed**.
- `pendingVersions` sorted by `startedAt` descending (newest first).
- `failedVersions` sorted by `failedAt` descending (newest first).
- Published rows may include `publishStartedAt`. Pending, published, and failed
  rows may include computed duration fields. See [Publish duration](#publish-duration).

Install and upgrade handlers ignore `pending.json` and `failed.json` entirely.

---

## UI changes (`/apps/{app}/admin`)

Update the snapshots table to render pending and failed entries alongside
published entries (single newest-first list with a status column). Apply merge
precedence (**published** > **pending** > **failed**) before choosing each row's
status — see [Merge rules](#app-admin-api) above.

| Status | Condition |
|--------|-----------|
| **Publishing** | Entry from `pendingVersions` |
| **Failed** | Entry from `failedVersions` |
| **Deployed** | `version === desiredVersion` (unchanged) |
| **Rolling out** | Active rollout for this version (unchanged) |
| **Available** | Published, not desired (unchanged) |

Pending entries:

- Show PR / commit provenance when present (same columns as published)
- **Published** column shows elapsed publish time from `startedAt`
- **Action** column: Deploy disabled; optional “View workflow” link using
  `publication.workflowRunUrl`

Failed entries:

- Show PR / commit provenance when present
- **Published** column shows `failedAt`, total time before failure, and a reason
  label (`workflow_failed` → “Workflow failed”, `stale` → “Timed out”)
- **Action** column: Deploy disabled; “View workflow” when
  `publication.workflowRunUrl` is present

Published entries without `publishStartedAt` show `publishedAt` only. See
[Publish duration](#publish-duration) for timing labels on all row types.

Polling: extend `AppAdminPageClient` to poll every **12s** while
`pendingVersions.length > 0` or fleet rollout is non-terminal. **Do not poll
solely because `failedVersions` is non-empty** — failed rows are retained for
30 days for visibility but are not in-flight; load them on page fetch and after
deploy actions. Stop polling when no pending versions remain and rollout is
terminal.

---

## CI changes (`publish-app-registry.yml`)

In `toolshed` (and any repo using the shared workflow):

1. After `Resolve package inputs` (when `PACKAGE_VERSION` is known), authenticate
   to GCP and run `gestaltd app registry pending set` with the same publication
   flags used in the publish step.
2. Migrate the existing publish step to
   `gestaltd app registry publish --dist-dir …` (does not touch `pending.json`
   or `failed.json`).
3. Add an `always()` step that runs `gestaltd app registry pending clear` on
   success or `gestaltd app registry pending fail` on failure.

Install `gestaltd` before step 1 (today it is installed only in the publish job;
move or duplicate install earlier so pending can be recorded at workflow start).

---

## Safety and invariants

| Invariant | Enforcement |
|-----------|-------------|
| Pending and failed versions are not installable | `Installer` / `InstallValidator` only read `versions/{version}.json` reached via published index entries |
| Pending and failed catalogs do not affect fleet state | No IndexedDB writes |
| Published catalog remains immutable per version | `versions/{version}.json` and artifacts still uploaded with `if-generation-match=0` |
| Public read stays HTTP GET | `pending.json` and `failed.json` per app, no bucket listing |
| Concurrent publishes for one app | Rare; `pending` map holds multiple versions. CI concurrency is per `(app, sha)`; different SHAs produce different version strings |
| Stuck pending entries become failed | `PrunePendingIndex` on `pending set`; 30-minute `startedAt` threshold writes `failedAt` with `reason=stale` |
| Workflow retries clear prior failures | `pending set` removes `failed.{version}` for the version being upserted |
| Duplicate version across catalogs | API and UI apply **published** > **pending** > **failed** precedence; see [Merge rules](#app-admin-api) |
| Old failed entries are pruned | `PruneFailedIndex` on `pending set`; 30-day `failedAt` threshold |

---

## Implementation path

Four PRs. Merge in order below. CI can land after PR 1 and before the UI —
`pending.json` is written and cleared in production before `/apps/{app}/admin`
shows publishing rows.

```text
PR 1 (gestalt) ──► PR 2 (gestalt) ──► PR 4 (gestalt)
       │
       └──────────► PR 3 (toolshed)
```

### PR 1 — Models and write path (`gestalt`)

Add types and CLI first so CI can call the new commands.

- Add `PendingIndex` / `PendingVersion` and `FailedIndex` / `FailedVersion` to
  `gestaltd/internal/appregistry/` ([models.md](./models.md) documents shapes).
- Add `publishStartedAt` to `IndexVersion` and `PublishedVersion`. Set it in
  `gestaltd app registry publish` by reading `pending.json` at upload time (do
  not mutate pending). See [Publish duration](#publish-duration).
- Add `gestaltd app registry pending set|clear|fail` and
  `gestaltd app registry publish` (move logic from `gestaltd app publish`).
- `pending set` removes `failed.{version}` for the version being upserted, then
  calls `PrunePendingIndex` and `PruneFailedIndex` before upsert; stale pending
  (`startedAt` > 30 minutes) → `failed.json`. `gestaltd app registry publish` does
  not touch `pending.json` or `failed.json`.
- Deprecate `gestaltd app publish` (alias → `gestaltd app registry publish`).
- Tests: pending write/prune/fail/clear, stale → failed, deprecated alias. Add a
  [tests.md](./tests.md) section for the write path.

### PR 2 — Read path and app-admin API (`gestalt`)

Depends on **PR 1**. Exposes pending and failed catalog state to the admin page.

- `FetchPendingIndex` and `FetchFailedIndex` in `gestaltd/internal/appregistry/`.
- Extend `getAppAdminRegistry` with `pendingVersions[]` and `failedVersions[]`.
- Apply merge precedence (**published** > **pending** > **failed**) when building
  the response. Expose `publishStartedAt` and computed duration fields per
  [Publish duration](#publish-duration).
- Tests: fetch helpers via `registrytest` HTTP fixture; handler returns pending
  and failed entries alongside published. Extend [tests.md](./tests.md).

### PR 3 — CI (`toolshed`)

Depends on **PR 1** merged and `gestaltd` available to the workflow (released
binary or workflow install from `main`). Does **not** wait on PR 2 or PR 4 —
operators get correct GCS state before the UI exists.

- `gestaltd app registry pending set` at workflow start (after version + GCP
  auth).
- Migrate publish step to `gestaltd app registry publish`.
- `gestaltd app registry pending clear` on success or
  `gestaltd app registry pending fail` on failure in `always()` at workflow end.
- Install `gestaltd` early enough for step 1 (see [CI changes](#ci-changes-publish-app-registryyml)
  above).

### PR 4 — UI (`gestalt`)

Depends on **PR 2**. Completes admin visibility.

- **Publishing** and **Failed** rows in the `gestalt-providers` app admin
  snapshots table. Timing labels per [Publish duration](#publish-duration).
- Poll every ~12s while any pending version exists or rollout is non-terminal.
  Apply merge precedence (**published** > **pending** > **failed**) when building
  the snapshots table.
- Manual / browser smoke per [tests.md](./tests.md) when implementing.

---

## Decisions

**Scope:** Pending publish visibility is **app-scoped only** — `/apps/{app}/admin`.
The embedded fleet `/admin` registry list does not show a publishing indicator.

**CLI:** `gestaltd app registry pending` (`set`, `clear`, `fail`) owns
`pending.json` and `failed.json`. `gestaltd app registry publish` uploads
immutable artifacts and `index.json` (replaces `gestaltd app publish`); it may
read `pending.json` to copy `publishStartedAt` but does not mutate pending or
failed catalogs. CI calls `pending clear` on success and `pending fail` on
failure.

**Failed retention:** `failed.json` entries are kept for **30 days**, then pruned
on `pending set`. Stale pending (`startedAt` older than 30 minutes) records
`failedAt` with `reason=stale`.

**Single phase:** Pending entries use `phase=publishing` from workflow start.

**Workflow start:** `publish-app-registry.yml` should call
`gestaltd app registry pending set` as early as possible (once `PACKAGE_VERSION`,
GCP auth, and `gestaltd` are available), not only when artifacts are ready.

**Publish duration:** `publishStartedAt` on published registry objects; elapsed
and total time derived at read/UI time. See [Publish duration](#publish-duration).

**Catalog merge precedence:** When the same version appears in more than one GCS
catalog, the app-admin API and snapshots table keep **published** over
**pending** over **failed**. See [Merge rules](#app-admin-api).
