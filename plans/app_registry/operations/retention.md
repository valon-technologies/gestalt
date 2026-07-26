# Published Version Retention

Delete unused published app registry versions while preserving audit metadata for every version that entered the fleet deploy chain.

## Problem

Published versions and artifacts accumulate in GCS indefinitely. Never-deployed snapshots can be cleaned up quickly. Versions in the deploy chain need two different lifetimes: their audit metadata is permanent, while their ability to be selected again expires after a bounded recovery window.

## Goals

- Configurable retention on each registry binding (`unusedRetention`, `deployedRetention`).
- Mutable `retention.json` per app tracking deployment and lock state.
- **Unused retention** (default `72h`) — delete published versions that were never deployed within the window after publication.
- **Permanent deploy history** — permanently retain every deployed version's change requests and metadata, including upgrades and downgrades.
- **Historical redeploy window** (default `720h`, or 30 days) — after a version stops being desired, allow it to be selected again until its fixed `deployableUntil` deadline. After the deadline it is permanently locked.
- Keep artifacts while a version is desired or historically redeployable. Locked historical versions may have artifacts pruned, while their index summary, version metadata, retention row, and change requests remain.
- `gestaltd app registry retention prune` to delete eligible versions from GCS.
- Scheduled prune from the **deploy reader** side (not publish CI).
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
├── retention.json
├── pending.json
├── failed.json
├── versions/{version}.json
└── artifacts/{version}/…
```

`retention.json` is a mutable catalog. Shape and fields: [models.md — RetentionIndex](../architecture/models.md#retentionindex). Updates use the same optimistic-concurrency pattern as `index.json` (`if-generation-match`).

## Who Writes and Who Deletes

| Action | Owner | What |
| --- | --- | --- |
| Publish | **Publisher** (`gestaltd app registry publish`) | Upsert `retention.json` with `publishedAt` and `everDeployed = false` |
| Fleet selection | **Reader** (`POST …/admin/registry/version`; low-level `POST …/add` and `POST …/upgrade` for initial admission) | Set `everDeployed = true`, set `firstDeployedAt` when absent, and clear the incoming version's historical deadline while desired |
| Desired version changes | **Reader** | On the outgoing version, set `lastDeactivatedAt = now` and `deployableUntil = now + deployedRetention`; on the incoming version, clear its active deadline |
| Lock historical version | **Scheduled prune job** (`gestaltd app registry retention prune`) | Once `deployableUntil` passes, set sticky `lockedAt`; future config changes do not unlock it |
| Delete unused version | **Scheduled prune job** (`gestaltd app registry retention prune`) | Remove a never-deployed expired version from `index.json`, `retention.json`, `versions/{version}.json`, and `artifacts/{version}/` |
| Prune locked artifact | **Scheduled prune job** (`gestaltd app registry retention prune`) | Remove `artifacts/{version}/` only; retain index summary, version metadata, retention row, and change requests |

Admission checks the selected version while holding the app-scoped install lock. A historical version is eligible only when it is not locked and the request arrives before `deployableUntil`. Selecting it appends another change request and makes it desired; when it later stops being desired, it receives a new deadline based on the configured `deployedRetention`.

Prune acquires the same app-scoped install lock used by version selection and holds it while evaluating and mutating that app's registry objects. It cross-checks `retention.json` against `app_version_change_requests` before fully deleting any version. If either source says the version was deployed, only locked artifacts may be removed. Prune never modifies `pending.json`, `failed.json`, version metadata for deployed versions, or change requests.

```bash
gestaltd app registry retention prune \
  --bucket gs://… \
  --app g-issues \
  [--dry-run]
```

Run on a schedule from the deploy reader. Toolshed runs [app-registry-retention-prune.yml](https://github.com/valon-technologies/toolshed/blob/main/.github/workflows/app-registry-retention-prune.yml) daily for every registry-only app in `valon-tools/deploy/config.yaml`, invoking `gestaltd app registry retention prune --bucket … --app …` with the pinned gestaltd release. Do not run from `gestaltd serve` request handlers or from publish CI.

## Retention States

| State | Deployable | Cleanup |
| --- | --- | --- |
| Never deployed, younger than `unusedRetention` | yes | none |
| Never deployed, window expired | no | delete index entry, metadata, artifact, and retention row |
| Current desired version | already active | retain all objects; no historical deadline runs |
| Historical, before `deployableUntil` | yes | retain all objects |
| Historical, deadline expired | no, permanently locked | retain deploy history and metadata; artifact may be deleted |
| Active rollout target | yes | retain all objects regardless of other clocks |

The Revision history tab always shows deployed transitions, including locked versions. Deployment controls live on Published snapshots and expose **Deploy** only for unexpired never-deployed or still-redeployable versions. See [admin.md](./admin.md#revision-history-tab) and [lifecycle.md](./lifecycle.md#revision-history).

## Shipped In

- [gestalt#2937](https://github.com/valon-technologies/gestalt/pull/2937) — `RetentionIndex`, config validation, and reader-side transition writes
- [gestalt#2938](https://github.com/valon-technologies/gestalt/pull/2938) — `gestaltd app registry retention prune`
- [toolshed#3786](https://github.com/valon-technologies/toolshed/pull/3786) — daily scheduled prune for registry-only apps
- [gestalt#2939](https://github.com/valon-technologies/gestalt/pull/2939) — revision history API and deployment-state projection
- [gestalt#2941](https://github.com/valon-technologies/gestalt/pull/2941) — `deployedBy` email resolution
- [gestalt-providers#1163](https://github.com/valon-technologies/gestalt-providers/pull/1163) — Revision history tab on app admin

---

## Appendix

### Related Changelogs

<pre>
├── <a href="../project/changelog.md#changelog-17">17 — Version Retention and Cleanup</a>
└── <a href="../project/changelog.md#changelog-18">18 — Revision History and Redeploy Windows</a>
</pre>

### Related Docs

<pre>
├── <a href="../architecture/config.md">config.md</a> — retention fields on appRegistries
├── <a href="../architecture/models.md#retentionindex">models.md</a> — RetentionIndex
├── <a href="./lifecycle.md">lifecycle.md</a> — app-admin version selection and low-level fleet admission
└── <a href="./pending-publish.md">pending-publish.md</a> — pending.json / failed.json pruning
</pre>
