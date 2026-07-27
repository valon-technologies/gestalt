# Published Version Retention

Delete unused published app registry versions while preserving audit metadata for every version that entered the fleet deploy chain.

## Problem

Published versions and artifacts accumulate in GCS indefinitely. Never-deployed snapshots can be cleaned up quickly. Versions in the deploy chain need two different lifetimes: their audit metadata is permanent, while their ability to be selected again expires after a bounded recovery window.

## Goals

- Configurable retention on each registry binding (`unusedRetention`, `deployedRetention`).
- **Unused retention** (default `72h`) — delete published versions that were never deployed within the window after publication.
- **Permanent deploy history** — permanently retain every deployed version's change requests and metadata, including upgrades and downgrades.
- **Historical redeploy window** (default `720h`, or 30 days) — after a version stops being desired, allow it to be selected again until its fixed `deployableUntil` deadline. After the deadline it is permanently locked.
- Keep artifacts while a version is desired or historically redeployable. Locked historical versions may have artifacts pruned, while their index summary, version metadata, and change requests remain.
- `gestaltd app registry retention prune` to delete eligible versions from GCS, one app at a time.
- Scheduled prune from the **gestaltd fleet** with fleet IndexedDB access (not publish CI or bucket-only jobs).
- A read-only **Revision history** tab on `/apps/{app}/admin` backed by the append-only deploy chain.

Fleet admission permanently changes a version from unused to deployed. Publishing alone does not count. The deploy chain may contain the same version more than once, for example `v1 → v2 → v1`; each accepted transition is a separate revision-history event.

## Config

On `appRegistries.{name}`:

```yaml
retention:
  unusedRetention: 72h
  deployedRetention: 720h
```

Defaults are `72h` and `720h` when omitted. The configured deployed duration is captured as a fixed deadline for each deactivation interval. Redeploying and later deactivating a version creates a new deadline; config changes do not alter an already captured deadline or unlock an expired version. See [config.md](../architecture/config.md).

## Schema and Storage

```text
apps/{app}/
├── index.json
├── pending.json
├── failed.json
├── versions/{version}.json
└── artifacts/{version}/…
```

Retention policy reads from two existing sources:

| Source | Location | Used for |
| --- | --- | --- |
| **Published catalog** | `index.json` | `publishedAt` for never-deployed versions |
| **Deploy chain** | `app_version_change_requests` | fleet deploy history and `from_version_deployable_until` |

There is no separate `retention.json` overlay. Legacy `retention.json` objects in GCS are ignored.

## Who Writes and Who Deletes

| Action | Owner | What |
| --- | --- | --- |
| Publish | **Publisher** (`gestaltd app registry publish`) | Upsert `index.json` with `publishedAt` for the new version |
| Fleet selection | **Reader** (`POST …/admin/registry/version`; low-level `POST …/add` and `POST …/upgrade`) | Append a change request with `from_version_deployable_until` for the outgoing version |
| Delete unused version | **Scheduled prune job** (`gestaltd app registry retention prune`) | Remove a never-deployed expired version from `index.json`, `versions/{version}.json`, and `artifacts/{version}/` |
| Prune locked artifact | **Scheduled prune job** (`gestaltd app registry retention prune`) | Remove `artifacts/{version}/` only; retain index summary, version metadata, and change requests |

Admission checks the selected version while holding the app-scoped install lock. A historical version is eligible only when the request arrives before `deployableUntil`. The reader resolves that deadline from `from_version_deployable_until` on the append-only change-request chain. See [indexeddb.md](../architecture/indexeddb.md). Selecting it appends another change request and makes it desired; when it later stops being desired, it receives a new deadline based on the configured `deployedRetention`.

Prune runs on the gestaltd fleet with `--config`, reads the deploy chain from IndexedDB, and mutates GCS. It acquires the same app-scoped install lock used by version selection and holds it while evaluating and mutating that app's `index.json`. Prune never modifies `pending.json`, `failed.json`, version metadata for deployed versions, or change requests.

```bash
gestaltd app registry retention prune \
  --config /path/to/deploy/config.yaml \
  --app g-issues \
  [--dry-run]
```

`--app` is required. Run one invocation per registry-only app. Schedule from the gestaltd fleet (for example a host cron job or Kubernetes CronJob with deploy config and IndexedDB access). Do not run from `gestaltd serve` request handlers or from publish CI.

### Concurrency

Prune is safe under concurrent publish, selection, and other prune attempts for the same app:

1. **Install lock** — prune claims the app-scoped install lock before reading the deploy chain or mutating GCS, the same lock used by version selection.
2. **Optimistic concurrency on `index.json`** — writes use `if-generation-match` and retry when another writer updates the catalog first.
3. **Deploy-chain cross-check** — a version present in `app_version_change_requests` is never fully deleted, even when `index.json` is stale.

## Retention States

| State | Deployable | Cleanup |
| --- | --- | --- |
| Never deployed, younger than `unusedRetention` | yes | none |
| Never deployed, window expired | no | delete index entry, metadata, and artifact |
| Current desired version | already active | retain all objects; no historical deadline runs |
| Historical, before `deployableUntil` | yes | retain all objects |
| Historical, deadline expired | no, permanently locked | retain deploy history and metadata; artifact may be deleted |
| Active rollout target | yes | retain all objects regardless of other clocks |

The Revision history tab always shows deployed transitions, including locked versions. Deployment controls live on Published snapshots and expose **Deploy** only for unexpired never-deployed or still-redeployable versions. See [admin.md](./admin.md#revision-history-tab) and [lifecycle.md](./lifecycle.md#revision-history).

## Shipped In

- [gestalt#2937](https://github.com/valon-technologies/gestalt/pull/2937) — retention config validation (original `retention.json` model; superseded by fleet prune)
- [gestalt#2938](https://github.com/valon-technologies/gestalt/pull/2938) — `gestaltd app registry retention prune`
- [toolshed#3786](https://github.com/valon-technologies/toolshed/pull/3786) — daily scheduled prune for registry-only apps (bucket-only; superseded by fleet prune)
- [gestalt#2939](https://github.com/valon-technologies/gestalt/pull/2939) — revision history API and deployment-state projection
- [gestalt#2941](https://github.com/valon-technologies/gestalt/pull/2941) — `deployedBy` email resolution
- [gestalt-providers#1163](https://github.com/valon-technologies/gestalt-providers/pull/1163) — Revision history tab on app admin

---

## Appendix

### Related Changelogs

<pre>
├── <a href="../project/changelog.md#changelog-17">17 — Version Retention and Cleanup</a>
├── <a href="../project/changelog.md#changelog-18">18 — Revision History and Redeploy Windows</a>
└── <a href="../project/changelog.md#changelog-22">22 — Fleet Retention Prune</a>
</pre>

### Related Docs

<pre>
├── <a href="../architecture/config.md">config.md</a> — retention fields on appRegistries
├── <a href="../architecture/indexeddb.md">indexeddb.md</a> — deploy chain and redeploy deadlines
├── <a href="./lifecycle.md">lifecycle.md</a> — app-admin version selection and low-level fleet admission
└── <a href="./pending-publish.md">pending-publish.md</a> — pending.json / failed.json pruning
</pre>
