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
`gestaltd app publish`, after packaging and artifact upload complete.

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
| `packaging` | `gestaltd provider package` / artifact build in progress. Pending record created at workflow start. |
| `publishing` | Artifacts built; `gestaltd app publish` upload + index update in progress. Set immediately before the publish step. |

On failure, **remove** the pending
entry in a workflow `always()` cleanup step. The workflow run URL remains the
failure audit trail.

### Self-healing

`gestaltd serve` reads `pending.json` over public HTTP and cannot write back to
GCS. Self-healing runs on the **publish write path** (CI and
`gestaltd app registry pending` subcommands), which already hold bucket
credentials.

A shared `PrunePendingIndex` helper removes stuck entries from `pending.json`
before any pending mutation. Prune criteria for each `pending.{version}` entry:

| Condition | Action |
|-----------|--------|
| `updatedAt` older than **30 minutes** | Remove — publish likely failed or cleanup never ran |
| Same `version` already listed in `index.json` | Remove — publish succeeded but `pending end` did not run |
| Otherwise | Keep |

The 30-minute threshold is the default for `pending begin` and `pending prune`.
No `stale` field on the app-admin API — stuck rows are removed in GCS instead
of surfaced as a separate UI state.

Prune uses the same optimistic-concurrency loop as `begin` / `end`: download
`pending.json` generation, drop matching entries, upload with
`if-generation-match`.

**When prune runs:**

1. **`pending begin`** — always prune before adding the new entry. Every app
   publish workflow self-heals stuck pending rows older than 30 minutes, so
   orphaned entries are cleared on the next merge to `main` for that app.
2. **`pending prune`** (new subcommand) — explicit sweep for one app or all
   enrolled apps. Used by a scheduled CI workflow and for manual operator cleanup.

```bash
# Prune one app (typical scheduled / manual use)
gestaltd app registry pending prune \
  --bucket gs://… \
  --app traffic-cop \
  [--max-age 30m]

# Prune every app that has a pending.json object (optional scheduled job)
gestaltd app registry pending prune \
  --bucket gs://… \
  --all-apps \
  [--max-age 30m]
```

`--all-apps` lists `apps/*/pending.json` via the GCS API (publisher credentials
only; not used by `gestaltd serve`). For each object, load the sibling
`index.json`, apply the prune rules, and upload only when the pending map changed.

**Scheduled janitor (recommended):** add a workflow in `toolshed` that runs
`pending prune --all-apps` on a cadence shorter than the 30-minute threshold
(e.g. every 15 minutes) so stuck entries are removed even when an app has not
published recently.

Prune is idempotent and safe to run concurrently with an in-flight publish:
generation-match retries apply. An active publish refreshes `updatedAt` on each
`begin` / `update`, so a healthy in-flight entry is not removed. Workflows with
packaging longer than 30 minutes should call `pending update` before the
threshold elapses to refresh `updatedAt` (the step before `gestaltd app publish`
already does this when entering the `publishing` phase).

---

## Publish lifecycle

```text
CI: publish-app-registry.yml
│
├─1. Resolve PACKAGE_VERSION + PROVIDER_REF
│
├─2. gestaltd app registry pending begin
│      → upsert pending.json (phase=packaging)
│
├─3. Package artifacts (may take tens of minutes)
│
├─4. gestaltd app registry pending update --phase publishing
│
├─5. gestaltd app publish (artifacts + versions/{version}.json + index.json)
│
└─6. gestaltd app registry pending end  (also in failure cleanup)
       → remove version from pending.json
```

On **success**, step 6 runs after step 5. The admin UI should briefly show the
version as published (index) rather than pending. Order within step 5 is
unchanged: `index.json` is still updated last, but step 6 clears pending
immediately after so operators never depend on pending lingering post-success.

On **failure**, an `always()` workflow step runs step 6 even when packaging or
publish failed, so pending does not stick unless the cleanup step itself fails.

### CLI surface (proposal)

Add a subcommand group rather than overloading `gestaltd app publish`:

```bash
gestaltd app registry pending begin \
  --bucket gs://… \
  --app traffic-cop \
  --version 0.0.0-snapshot.g… \
  --ref abc123… \
  --workflow-run-url … \
  [--trigger-pr-number … --trigger-pr-url …]

gestaltd app registry pending update \
  --bucket gs://… \
  --app traffic-cop \
  --version 0.0.0-snapshot.g… \
  --phase publishing

gestaltd app registry pending end \
  --bucket gs://… \
  --app traffic-cop \
  --version 0.0.0-snapshot.g…

gestaltd app registry pending prune \
  --bucket gs://… \
  --app traffic-cop \
  [--all-apps] \
  [--max-age 30m]
```

`begin` prunes stuck entries before upserting (see [Self-healing](#self-healing)).
`begin` is idempotent for the same `(app, version)`: refresh `updatedAt` and
merge `publication` if re-run. `end` is idempotent if the version is already
absent.

Implementation reuses the index upload helper (generation-match retries) in
`gestaltd/internal/daemon/app_publish.go`.

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
      "phase": "packaging",
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
   to GCP and run `gestaltd app registry pending begin` with workflow/PR metadata.
2. Immediately before `gestaltd app publish`, run `pending update --phase publishing`.
3. Add a final job step (or `always()` step on the publish job) that runs
   `gestaltd app registry pending end` for `PACKAGE_VERSION`.

The `begin` step needs the gestaltd binary — reuse the existing install step
ordering (install gestaltd before pending begin).

**Janitor:** add a scheduled workflow (e.g. every 15 minutes) that runs
`gestaltd app registry pending prune --all-apps` so stuck entries older than
30 minutes are removed even when an app has not published recently.

---

## Safety and invariants

| Invariant | Enforcement |
|-----------|-------------|
| Pending versions are not installable | `Installer` / `InstallValidator` only read `versions/{version}.json` reached via published index entries |
| Pending does not affect fleet state | No IndexedDB writes |
| Published catalog remains immutable per version | `versions/{version}.json` and artifacts still uploaded with `if-generation-match=0` |
| Public read stays HTTP GET | Single `pending.json` per app, no bucket listing |
| Concurrent publishes for one app | Rare; `pending` map holds multiple versions. CI concurrency is per `(app, sha)`; different SHAs produce different version strings |
| Stuck pending rows are removed | `PrunePendingIndex` on `pending begin` and `pending prune`; 30-minute `updatedAt` threshold + already-published checks |

---

## Implementation path

1. **Models** — Add `PendingIndex` / `PendingVersion` to
   `gestaltd/internal/appregistry/`; document in [models.md](./models.md).
2. **Write path** — `gestaltd app registry pending begin|update|end|prune` with
   generation-match retries (mirror index upsert). `begin` and `prune` call
   `PrunePendingIndex`.
3. **Read path** — `FetchPendingIndex`; unit tests with `registrytest` HTTP
   fixture.
4. **App-admin API** — extend `getAppAdminRegistry` with `pendingVersions`.
5. **UI** — pending rows + polling in `gestalt-providers` app admin table.
6. **CI** — wire `publish-app-registry.yml` begin/update/end steps; add scheduled
   `pending prune --all-apps` janitor workflow.
7. **Tests** — see [tests.md](./tests.md) (add section when implementing).

Suggested order: 1 → 2 → 3 → 4 → 6 (CI can ship begin/end before UI) → 5.

---

## Open questions

1. **Phase granularity** — Is `packaging` / `publishing` enough, or do we want
   per-platform sub-phases in v1?
2. **Manual publish** — Should `gestaltd app publish` call `pending begin/end`
   automatically when `--workflow-run-url` is set, so local/manual publishes
   also appear in the admin UI?
3. **Embedded `/admin`** — Should the fleet observability app list show a
   “publishing” indicator, or only the app-scoped `/apps/{app}/admin` page?
4. **Janitor cadence** — Is every 15 minutes sufficient for `pending prune
   --all-apps`, or should it run more frequently?

---

## Future work

- Failed publish retention: optional `apps/{app}/failed/{version}.json` with
  `failedAt` and `reason` instead of only relying on GitHub Actions history.
- Global pending index across apps for a registry-wide dashboard.
- Webhook or Pub/Sub on `index.json` change to push updates to the UI instead
  of polling.
