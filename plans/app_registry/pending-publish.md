# Pending Publish Visibility

Show in-flight app registry publishes on `/apps/{app}/admin` before `index.json`
is updated at the end of CI.

Related docs:

- [plan.md](./plan.md) — registry goals and implementation path
- [models.md](./models.md) — registry JSON documents, including [PendingIndex](./models.md#pendingindex)
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

| Question | Surface |
|----------|---------|
| Is a new snapshot being published right now? | Row with status **Publishing** |
| Which commit / PR triggered it? | Pending row provenance (same fields as published rows where available) |
| Where is the CI run? | Link to `publication.workflowRunUrl` |
| When did publish start? | `startedAt` on the pending row |
| Did a recent publish fail? | Row with status **Failed** and `failedAt` |
| Why did it fail? | `reason` on the failed row (`workflow_failed` or `stale`) |

---

## Design summary

Add mutable **pending** and **failed** catalog documents in GCS per app. CI
writes pending when a publish starts. On success, `pending clear` removes the
row. On workflow failure, `pending fail` records `failedAt` in `failed.json`.
Stale pending rows (30 minutes without `updatedAt` refresh) are moved to
`failed.json` with `reason=stale` on the next `pending set`. `gestaltd` reads
both files alongside `index.json` and exposes rows through the app-admin
registry API. The UI merges pending, failed, and published rows in the snapshots
table.

Pending versions are **not** installable. Install-time validation continues to
read only `versions/{version}.json` via the published index contract.

---

## GCS layout

Extend the per-app registry tree:

```text
apps/{app}/
├── index.json
├── pending.json          # in-flight publishes
├── failed.json           # recent failed publishes
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

Record pending with `phase=publishing` at workflow start.

On workflow **success**, `gestaltd app registry pending clear` removes the
pending row (no failed record).

On workflow **failure**, `gestaltd app registry pending fail` removes the pending
row and upserts `failed.json` with `failedAt` and `reason=workflow_failed`. The
workflow run URL remains the failure audit trail.

## FailedIndex

`failed.json` document shape, fields, and example:
[models.md — FailedIndex](./models.md#failedindex).

### Failure reasons

| `reason` | Meaning |
|----------|---------|
| `workflow_failed` | CI called `pending fail` after packaging or publish failed |
| `stale` | Pending `updatedAt` was older than 30 minutes when `PrunePendingIndex` ran |

### Self-healing

`gestaltd serve` reads `pending.json` and `failed.json` over public HTTP and
cannot write back to GCS. Self-healing runs on the **`gestaltd app registry
pending` write path** (CI), which already holds bucket credentials.

`gestaltd app registry pending set` always runs reconciliation before upserting:

**`PrunePendingIndex`** — for each `pending.{version}` entry:

| Condition | Action |
|-----------|--------|
| `updatedAt` older than **30 minutes** | Move to `failed.json` with `failedAt=now`, `reason=stale` |
| Same `version` already listed in `index.json` | Remove from pending only (publish succeeded; pending cleanup missed) |
| Otherwise | Keep |

**`PruneFailedIndex`** — for each `failed.{version}` entry:

| Condition | Action |
|-----------|--------|
| `failedAt` older than **30 days** | Remove |
| Same `version` already listed in `index.json` | Remove |
| Otherwise | Keep |

The 30-minute pending threshold applies on `gestaltd app registry pending set`.
Failed rows are retained for 30 days so operators can see recent failures on the
admin page.

`PrunePendingIndex` and `PruneFailedIndex` use the same optimistic-concurrency
loop as other catalog writes: download generation, merge, upload with
`if-generation-match`.

Every app publish workflow self-heals stuck pending rows on the next merge to
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
│      → PrunePendingIndex (stale → failed.json; already-published → drop)
│      → PruneFailedIndex (drop entries older than 30 days or already published)
│      → upsert pending.json for this version (phase=publishing)
│
├─3. Package artifacts (may take tens of minutes)
│
├─4. gestaltd app registry publish --dist-dir … (artifacts + versions/{version}.json + index.json)
│
└─5. gestaltd app registry pending clear|fail  (always())
       → success: pending clear (remove pending row)
       → failure: pending fail (remove pending row, upsert failed.json)
```

Today, `publish-app-registry.yml` runs `gestaltd app publish --dist-dir …` only.
This proposal adds `gestaltd app registry pending` for steps 2 and 5, migrates
step 4 to `gestaltd app registry publish`, and records failures in
`failed.json`.

On **success**, step 4 uploads artifacts and updates `index.json` (unchanged
order — index last). Step 5 runs `pending clear`.

On **failure**, step 5 runs `pending fail`, recording `failedAt` in
`failed.json`. If step 5 itself fails, the next `pending set` for any version
moves a stale pending row to `failed.json` with `reason=stale`.

### CLI surface

Introduce two commands under `gestaltd app registry`:

```text
gestaltd app registry pending set|clear|fail
gestaltd app registry publish
```

`gestaltd app registry publish` replaces `gestaltd app publish` for immutable
uploads only. `gestaltd app registry pending` owns the mutable `pending.json` and
`failed.json` lifecycle. Keep `gestaltd app publish` as a deprecated alias for
`registry publish` during migration, then remove it.

**Pending** — write or update `pending.json`; reconcile stale pending and old
failed rows via `PrunePendingIndex` / `PruneFailedIndex` on `set` only.

```bash
# Workflow start — record pending publish (prunes stuck entries first)
gestaltd app registry pending set \
  --bucket gs://… \
  --app traffic-cop \
  --version 0.0.0-snapshot.g… \
  --ref abc123… \
  --workflow-run-url … \
  [--trigger-pr-number … --trigger-pr-url …]

# Workflow end — success: remove pending row (idempotent)
gestaltd app registry pending clear \
  --bucket gs://… \
  --app traffic-cop \
  --version 0.0.0-snapshot.g…

# Workflow end — failure: record failedAt and remove pending row (idempotent)
gestaltd app registry pending fail \
  --bucket gs://… \
  --app traffic-cop \
  --version 0.0.0-snapshot.g…
```

`pending set` writes `phase=publishing`. It is idempotent for the same
`(app, version)`: refresh `updatedAt` and merge `publication` if re-run.
`pending clear` is idempotent if the version is already absent from pending.
`pending fail` is idempotent if the version is already absent from pending (keeps
an existing `failed.json` entry unchanged).

**Publish** — upload artifacts and version metadata, update `index.json` only.
Does not read or write `pending.json`. Same flags as today's `gestaltd app
publish`; requires `--dist-dir`.

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

CI runs `pending set` at workflow start, `registry publish` after packaging, and
`pending clear` or `pending fail` in `always()` at workflow end depending on
outcome. Manual publishes can call `pending set` first and `pending clear` or
`pending fail` after when they want admin UI visibility.

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

- HTTP GET `apps/{app}/pending.json` / `apps/{app}/failed.json`
- Missing object → empty index (same as missing `index.json`)
- Decode + validate `schemaVersion`

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

- A version MUST NOT appear in more than one of `pendingVersions`,
  `failedVersions`, and `publishedVersions`. If multiple exist (race or missed
  cleanup), prefer **published** and omit pending and failed.
- `pendingVersions` sorted by `startedAt` descending (newest first).
- `failedVersions` sorted by `failedAt` descending (newest first).

Install and upgrade handlers ignore `pending.json` and `failed.json` entirely.

---

## UI changes (`/apps/{app}/admin`)

Update the snapshots table to render **pending** and **failed** rows alongside
published rows (single newest-first list with a status column).

| Status | Condition |
|--------|-----------|
| **Publishing** | Row from `pendingVersions` |
| **Failed** | Row from `failedVersions` |
| **Deployed** | `version === desiredVersion` (unchanged) |
| **Rolling out** | Active rollout for this version (unchanged) |
| **Available** | Published, not desired (unchanged) |

Pending rows:

- Show PR / commit provenance when present (same columns as published)
- **Published** column shows “In progress” or time since `startedAt`
- **Action** column: Deploy disabled; optional “View workflow” link using
  `publication.workflowRunUrl`
- No deploy button for pending versions

Failed rows:

- Show PR / commit provenance when present
- **Published** column shows `failedAt` (and optional reason label:
  `workflow_failed` → “Workflow failed”, `stale` → “Timed out”)
- **Action** column: Deploy disabled; “View workflow” when
  `publication.workflowRunUrl` is present
- No deploy button for failed versions

Polling: extend `AppAdminPageClient` to poll every **12s** while
`pendingVersions.length > 0` or `failedVersions.length > 0`, not only during
fleet rollout. Stop polling when idle.

---

## CI changes (`publish-app-registry.yml`)

In `toolshed` (and any repo using the shared workflow):

1. After `Resolve package inputs` (when `PACKAGE_VERSION` is known), authenticate
   to GCP and run `gestaltd app registry pending set` with the same publication
   flags used in the publish step.
2. Keep the existing publish step, migrated to
   `gestaltd app registry publish --dist-dir …` (does not touch `pending.json`).
3. Add an `always()` step that runs `gestaltd app registry pending clear` on
   success or `gestaltd app registry pending fail` on failure.

Install `gestaltd` before step 1 (today it is installed only in the publish job;
move or duplicate install earlier so pending can be recorded at workflow start).

---

## Safety and invariants

| Invariant | Enforcement |
|-----------|-------------|
| Pending versions are not installable | `Installer` / `InstallValidator` only read `versions/{version}.json` reached via published index entries |
| Pending does not affect fleet state | No IndexedDB writes |
| Published catalog remains immutable per version | `versions/{version}.json` and artifacts still uploaded with `if-generation-match=0` |
| Public read stays HTTP GET | `pending.json` and `failed.json` per app, no bucket listing |
| Concurrent publishes for one app | Rare; `pending` map holds multiple versions. CI concurrency is per `(app, sha)`; different SHAs produce different version strings |
| Stuck pending rows become failed | `PrunePendingIndex` on `pending set`; 30-minute `updatedAt` threshold writes `failedAt` with `reason=stale` |
| Old failed rows are pruned | `PruneFailedIndex` on `pending set`; 30-day `failedAt` threshold |

---

## Implementation path

**Four PRs.** Merge in order below. CI can land after PR 1 and before the UI —
`pending.json` is written and cleared in production before `/apps/{app}/admin`
shows publishing rows.

```text
PR 1 (gestalt) ──► PR 2 (gestalt) ──► PR 4 (gestalt)
       │
       └──────────► PR 3 (toolshed)
```

### PR 1 — Models and write path (`gestalt`)

Foundation for everything else. Ship types and CLI first so CI can call the new
commands.

- Add `PendingIndex` / `PendingVersion` and `FailedIndex` / `FailedVersion` to
  `gestaltd/internal/appregistry/` ([models.md](./models.md) documents shapes).
- Add `gestaltd app registry pending set|clear|fail` and
  `gestaltd app registry publish` (move logic from `gestaltd app publish`).
- `pending set` calls `PrunePendingIndex` and `PruneFailedIndex` before
  upsert; stale pending → `failed.json`. `registry publish` does not touch
  `pending.json` or `failed.json`.
- Deprecate `gestaltd app publish` (alias → `registry publish`).
- Tests: pending write/prune/fail/clear, stale → failed, deprecated alias. Add a
  [tests.md](./tests.md) section for the write path.

### PR 2 — Read path and app-admin API (`gestalt`)

Depends on **PR 1**. Exposes pending state to the admin page.

- `FetchPendingIndex` and `FetchFailedIndex` in `gestaltd/internal/appregistry/`.
- Extend `getAppAdminRegistry` with `pendingVersions[]` and `failedVersions[]`.
- Tests: fetch helpers via `registrytest` HTTP fixture; handler returns pending
  and failed alongside published. Extend [tests.md](./tests.md).

### PR 3 — CI (`toolshed`)

Depends on **PR 1** merged and `gestaltd` available to the workflow (released
binary or workflow install from `main`). Does **not** wait on PR 2 or PR 4 —
operators get correct GCS state before the UI exists.

- `gestaltd app registry pending set` at workflow start (after version + GCP
  auth).
- Migrate publish step to `gestaltd app registry publish`.
- `gestaltd app registry pending clear` on success or `pending fail` on failure
  in `always()` at workflow end.
- Install `gestaltd` early enough for step 1 (see [CI changes](#ci-changes-publish-app-registryyml)
  above).

### PR 4 — UI (`gestalt`)

Depends on **PR 2**. Completes operator visibility.

- Pending rows (**Publishing**) and failed rows (**Failed**) in the
  `gestalt-providers` app admin snapshots table.
- Poll every ~12s while any pending or failed version exists; prefer published
  row when the same version appears in multiple lists.
- Manual / browser smoke per [tests.md](./tests.md) when implementing.

---

## Decisions

**Scope:** Pending publish visibility is **app-scoped only** — `/apps/{app}/admin`.
The embedded fleet `/admin` registry list does not show a publishing indicator.

**CLI surface:** Two commands under `gestaltd app registry`:

- `pending set|clear|fail` — mutable `pending.json` and `failed.json` lifecycle
  (CI calls `clear` on success, `fail` on failure)
- `publish` — immutable artifacts + `index.json` only (replaces `gestaltd app publish`)

**Failed retention:** `failed.json` entries are kept for **30 days**, then pruned
on `pending set`. Stale pending (30 minutes without `updatedAt` refresh) records
`failedAt` with `reason=stale`.

**Single phase:** Pending rows use `phase=publishing` from workflow start.

**Workflow start:** `publish-app-registry.yml` should call
`gestaltd app registry pending set` as early as possible (once `PACKAGE_VERSION`,
GCP auth, and `gestaltd` are available), not only when artifacts are ready.

---

## Future work

- Global pending index across apps for a registry-wide dashboard.
- Webhook or Pub/Sub on `index.json` change to push updates to the UI instead
  of polling.
