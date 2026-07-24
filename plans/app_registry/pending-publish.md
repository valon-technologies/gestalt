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

---

## Design summary

Add a small **pending catalog** document in GCS per app. CI writes it when a
publish starts and removes the entry when the publish finishes (success or
failure). `gestaltd` reads it alongside `index.json` and exposes pending rows
through the existing app-admin registry API. The UI merges pending and
published rows in the snapshots table.

Pending versions are **not** installable. Install-time validation continues to
read only `versions/{version}.json` via the published index contract.

---

## GCS layout

Extend the per-app registry tree:

```text
apps/{app}/
├── index.json
├── pending.json          # new — in-flight publishes for this app
├── versions/
│   └── {version}.json
└── artifacts/
    └── {version}/
        └── …
```

`pending.json` is a mutable catalog (like `index.json`), not an immutable
release object. Use the same optimistic-concurrency update pattern as index
writes: read generation, merge, upload with `if-generation-match`.

---

## PendingIndex

`pending.json` document shape, fields, and example:
[models.md — PendingIndex](./models.md#pendingindex).

### Phases

| Phase | Meaning |
|-------|---------|
| `publishing` | Publish in progress — from workflow start through artifact upload and `index.json` update. |

Record pending with `phase=publishing` at workflow start. There is no separate
`packaging` phase; `gestaltd provider package` runs while the pending row already
shows **Publishing**.

On failure, **remove** the pending
entry in a workflow `always()` cleanup step. The workflow run URL remains the
failure audit trail.

### Self-healing

`gestaltd serve` reads `pending.json` over public HTTP and cannot write back to
GCS. Self-healing runs on the **`gestaltd app registry publish` write path** (CI and
manual publishes), which already hold bucket credentials.

A shared `PrunePendingIndex` helper removes stuck entries from `pending.json`
before any pending mutation. Prune criteria for each `pending.{version}` entry:

| Condition | Action |
|-----------|--------|
| `updatedAt` older than **30 minutes** | Remove — publish likely failed or cleanup never ran |
| Same `version` already listed in `index.json` | Remove — publish succeeded but pending was not cleared |
| Otherwise | Keep |

The 30-minute threshold applies on the first `gestaltd app registry publish
--pending publishing` call in a workflow.
No `stale` field on the app-admin API — stuck rows are removed in GCS instead
of surfaced as a separate UI state.

`PrunePendingIndex` uses the same optimistic-concurrency loop as pending
writes: download `pending.json` generation, drop matching entries, upload with
`if-generation-match`.

The first `gestaltd app registry publish --pending publishing` call in a
workflow always prunes before upserting. Every app publish workflow self-heals stuck pending rows older
than 30 minutes, so orphaned entries are cleared on the next merge to `main` for
that app.

Prune is idempotent and safe to run concurrently with an in-flight publish:
generation-match retries apply. An active publish refreshes `updatedAt` on each
`--pending publishing` call, so a healthy in-flight entry is not removed.
Workflows with packaging longer than 30 minutes should run
`gestaltd app registry publish --pending publishing` again before the full
`--dist-dir` step to refresh `updatedAt`.

---

## Publish lifecycle

```text
CI: publish-app-registry.yml
│
├─1. Resolve PACKAGE_VERSION + PROVIDER_REF
│
├─2. gestaltd app registry publish --pending publishing
│      (same --bucket/--app/--version/--ref/--workflow-run-url flags as step 4)
│      → PrunePendingIndex (drop entries older than 30m or already in index.json)
│      → upsert pending.json for this version (phase=publishing)
│
├─3. Package artifacts (may take tens of minutes)
│
├─4. gestaltd app registry publish --dist-dir … (artifacts + versions/{version}.json + index.json)
│      → clear this version from pending.json on success
│
└─5. gestaltd app registry publish --pending clear  (always() failure cleanup)
       → remove version from pending.json if step 4 did not run
```

Today, `publish-app-registry.yml` runs `gestaltd app publish --dist-dir …` only.
This proposal migrates to `gestaltd app registry publish` and adds steps 2 and 5
for pending visibility (step 2 at workflow start).

On **success**, step 4 clears pending after uploading. Order within step 4 is
unchanged: `index.json` is still updated last.

On **failure**, an `always()` workflow step runs step 5 even when packaging or
publish failed, so pending does not stick unless the cleanup step itself fails.

### CLI surface

Introduce `gestaltd app registry publish` to replace `gestaltd app publish`.
Move publish implementation under `gestaltd app registry`; keep `gestaltd app
publish` as a deprecated alias during migration, then remove it.

```text
gestaltd app registry publish [flags]
```

Flags are unchanged from today's `gestaltd app publish`, plus `--pending
publishing` and `--pending clear` for pending-only operations.

**Pending-only** (no `--dist-dir`): write or update `pending.json` only.

```bash
# Workflow start — record pending publish (prunes stuck entries first)
gestaltd app registry publish \
  --bucket gs://… \
  --app traffic-cop \
  --version 0.0.0-snapshot.g… \
  --ref abc123… \
  --workflow-run-url … \
  [--trigger-pr-number … --trigger-pr-url …] \
  --pending publishing

# Optional — refresh updatedAt if packaging exceeds 30 minutes
gestaltd app registry publish … --pending publishing

# Failure cleanup
gestaltd app registry publish … --pending clear
```

**Full publish:** when `--dist-dir` is set, upload artifacts and version
metadata, update `index.json`, then remove this version from `pending.json`.
Same behavior as today's `gestaltd app publish`.

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

Manual and CI publishes use the same command. A local run with
`--workflow-run-url` and `--pending publishing` at the start shows in the admin
UI the same way as CI.

`--pending publishing` is idempotent for the same `(app, version)`: refresh
`updatedAt` and merge `publication` if re-run. `--pending clear` is idempotent
if the version is already absent.

Implementation extends `gestaltd/internal/daemon/app_publish.go` and reuses the
index upload helper (generation-match retries).

---

## Read path

### `RegistryReader`

Add:

```go
func (r *RegistryReader) FetchPendingIndex(ctx context.Context, publicRoot, appName string) (*PendingIndex, error)
```

- HTTP GET `apps/{app}/pending.json`
- Missing object → empty pending index (same as missing `index.json`)
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
  "rollout": { … },
  "selectionDisabled": false
}
```

Merge rules:

- A version MUST NOT appear in both `pendingVersions` and `publishedVersions`.
  If both exist (race between index update and pending end), prefer **published**
  and omit pending.
- `pendingVersions` sorted by `startedAt` descending (newest first), matching
  published sort order.

Install and upgrade handlers ignore `pending.json` entirely.

---

## UI changes (`/apps/{app}/admin`)

Update the snapshots table to render **pending** rows above published rows (or
interleaved by `startedAt` / `publishedAt` — prefer single newest-first list
with a status column).

| Status | Condition |
|--------|-----------|
| **Publishing** | Row from `pendingVersions` |
| **Deployed** | `version === desiredVersion` (unchanged) |
| **Rolling out** | Active rollout for this version (unchanged) |
| **Available** | Published, not desired (unchanged) |

Pending rows:

- Show PR / commit provenance when present (same columns as published)
- **Published** column shows “In progress” or time since `startedAt`
- **Action** column: Deploy disabled; optional “View workflow” link using
  `publication.workflowRunUrl`
- No deploy button for pending versions

Polling: extend `AppAdminPageClient` to poll every **12s** while
`pendingVersions.length > 0`, not only during fleet rollout. Stop polling when
idle.

---

## CI changes (`publish-app-registry.yml`)

In `toolshed` (and any repo using the shared workflow):

1. After `Resolve package inputs` (when `PACKAGE_VERSION` is known), authenticate
   to GCP and run `gestaltd app registry publish --pending publishing` with the
   same publication flags used in the final publish step.
2. Keep the existing publish step, migrated to
   `gestaltd app registry publish --dist-dir …` (clears pending on success).
3. Add an `always()` step that runs `gestaltd app registry publish --pending clear`
   when the full publish step did not succeed.

Install `gestaltd` before step 1 (today it is installed only in the publish job;
move or duplicate install earlier so pending can be recorded at workflow start).

---

## Safety and invariants

| Invariant | Enforcement |
|-----------|-------------|
| Pending versions are not installable | `Installer` / `InstallValidator` only read `versions/{version}.json` reached via published index entries |
| Pending does not affect fleet state | No IndexedDB writes |
| Published catalog remains immutable per version | `versions/{version}.json` and artifacts still uploaded with `if-generation-match=0` |
| Public read stays HTTP GET | Single `pending.json` per app, no bucket listing |
| Concurrent publishes for one app | Rare; `pending` map holds multiple versions. CI concurrency is per `(app, sha)`; different SHAs produce different version strings |
| Stuck pending rows are removed | `PrunePendingIndex` on first `--pending publishing`; 30-minute `updatedAt` threshold + already-published checks |

---

## Implementation path

1. **Models** — Add `PendingIndex` / `PendingVersion` to
   `gestaltd/internal/appregistry/`; document in [models.md](./models.md).
2. **Write path** — add `gestaltd app registry publish` (move logic from
   `gestaltd app publish`); support `--pending publishing|clear`.
   Generation-match retries (mirror index upsert). First `--pending publishing`
   call calls `PrunePendingIndex`; full publish clears pending on success.
   Deprecate `gestaltd app publish`.
3. **Read path** — `FetchPendingIndex`; unit tests with `registrytest` HTTP
   fixture.
4. **App-admin API** — extend `getAppAdminRegistry` with `pendingVersions`.
5. **UI** — pending rows + polling in `gestalt-providers` app admin table.
6. **CI** — migrate `publish-app-registry.yml` from `gestaltd app publish` to
   `gestaltd app registry publish`; add `--pending publishing` at workflow start
   and `--pending clear` on failure.
7. **Tests** — see [tests.md](./tests.md) (add section when implementing).

Suggested order: 1 → 2 → 3 → 4 → 6 (CI can ship begin/end before UI) → 5.

---

## Open questions

1. **Embedded `/admin`** — Should the fleet observability app list show a
   “publishing” indicator, or only the app-scoped `/apps/{app}/admin` page?

## Decisions

**CLI surface:** `gestaltd app registry publish` replaces `gestaltd app publish`.
CI and manual publishes use the registry command. Pending lifecycle uses the
same command with `--pending publishing` or `--pending clear`.

**Single phase:** Pending rows use `phase=publishing` from workflow start. No
separate `packaging` phase.

**Workflow start:** `publish-app-registry.yml` should call
`gestaltd app registry publish --pending publishing` as early as possible (once
`PACKAGE_VERSION`, GCP auth, and `gestaltd` are available), not only at the end
when artifacts are ready.

---

## Future work

- Failed publish retention: optional `apps/{app}/failed/{version}.json` with
  `failedAt` and `reason` instead of only relying on GitHub Actions history.
- Global pending index across apps for a registry-wide dashboard.
- Webhook or Pub/Sub on `index.json` change to push updates to the UI instead
  of polling.
