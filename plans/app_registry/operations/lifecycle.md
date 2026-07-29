# App Registry Replica Lifecycle

How each `gestaltd` replica observes fleet install state, materializes app artifacts locally, and serves registry-installed app versions.

Install is change-request-only; per-replica convergence (acknowledge → download → restart → mount) is handled by the polling process described below. List/get install endpoints expose **projected fleet-known versions** from change requests.

Implementation:

- Registry list handlers — `gestaltd/internal/server/handlers_admin_app_registry.go`
- Install handlers — `gestaltd/internal/server/handlers_admin_app_install.go`
- Registry fetcher — `gestaltd/internal/appregistry/reader.go`
- Registry installer — `gestaltd/internal/appregistry/installer.go`
- Catalog poller — `gestaltd/internal/appregistry/poller.go`
- Auto-deploy controller — `gestaltd/internal/appregistry/autodeploy/controller.go`
- Artifact materializer — `gestaltd/internal/appregistry/materializer.go`
- Registry mount resolver — `gestaltd/internal/appregistry/mount.go`
- App provider restarter — `gestaltd/internal/bootstrap/app_provider_restart.go`
- Change request projections — `gestaltd/internal/coredata/app_version_change_requests_projection.go`
- Rollout state — `gestaltd/internal/coredata/app_rollouts.go`

## Startup

`coredata.NewWithOptions` — idempotently create host stores, including `app_version_change_requests`, `app_version_install_locks`, and `app_rollouts`. Bootstrap does not write change requests or rollouts; the stores start empty until an install.

When `gestaltd serve` starts:

1. Load and validate deploy config, including `appRegistries` (`validateAppRegistries` checks registry names, `kind: gcs`, and that `publicUrl` can be derived from `gcs.bucket`).
2. Pass the parsed `appRegistries` map through bootstrap into `server.New`, which clones it onto the running `Server` as an in-memory map.

Bootstrap does not fetch registry indexes or prefetch version metadata. The source of truth for registry configuration remains the config file on disk; gestaltd does not persist `appRegistries` elsewhere.

Apps with a deploy-time source pin (`source.git`, `source.path`, etc.) bind into the provider graph during the normal startup provider build. Registry-only apps (`source.registry`) are different — see [config.md](../architecture/config.md#registry-only-app-source). They have no resolved manifest or baked artifact at deploy time, so bootstrap excludes them from that build loop.

At `StartAppProviders`, each registry-only app:

1. Read fleet-known versions via `ListKnownVersionsByApp`. When the projection is empty, skip the app — nothing is running until the first `POST …/add`.
2. Take the desired version selected by `LatestKnownVersion`.
3. Materialize the registry artifact to `{artifactsDir}/registry-installed/{app}/{version}` when the tree is missing or incomplete.
4. Start the provider through `StartApp` with the registry-mounted binary — the same mount path used by catalog-driven restarts.

The catalog poller still handles first install and upgrades on replicas that were skipped at boot or join after a rollout.

### Runtime Version Invariants

The lifecycle uses four distinct states. They must not be treated as interchangeable:

- **Fleet-known** — an accepted `(app, version)` projected from `app_version_change_requests`.
- **Materialized** — a complete, validated package exists at `{artifactsDir}/registry-installed/{app}/{version}` on this replica.
- **Running** — this replica successfully built and registered the provider from that exact materialized package.
- **Converged** — the poller recorded `restarted_at` for the replica and version, meaning the replica reconciled through that catalog change. This is rollout accounting, not proof that the provider ran that exact version or is currently running it.

This document uses these names for local runtime state:

- **Provider registry** — the in-process collection of providers available for operation invocation.
- **Running-version map** — the in-process `app → version` record for registered registry-app providers.
- **`active-version` marker** — the local file that selects the materialized static bundle for an app.
- **Rollout-progress row** — an `app_instance_materializations` record; it describes previously recorded rollout work, not current provider state.

Bootstrap and the poller must both use `LatestKnownVersion` to select the same desired installation. See [indexeddb.md](../architecture/indexeddb.md#accepted-changes-and-projections) for the version-ordering rule.

Each replica materializes and retains only that latest desired version. Older fleet-known versions remain visible in catalog history, but a replica that advances past them does not download their artifacts. After the desired version starts successfully, Gestalt removes superseded registry-installed package directories for that app.

`app_rollouts` and `app_instance_materializations` are not boot inputs. Bootstrap reads only deploy config and the fleet-known version projection. In particular, stale or missing convergence rows must not prevent a registry-only app from starting.

### Bootstrap Before Polling

Bootstrap finishes its registry-app startup attempts before the catalog poller begins:

1. Bootstrap materializes and starts the desired version without updating `app_rollouts` or `app_instance_materializations`; the poller owns those rollout-accounting writes.
2. After the exact package is validated and its provider starts successfully, bootstrap records the app and version in this process's running-version map and local `active-version` marker. Static and runtime handlers may then serve that version. If provider startup fails, neither the running-version map nor the `active-version` marker may identify the requested version as running.
3. After bootstrap has attempted every registry-only app, it marks startup-provider initialization complete. An individual registry app failure does not prevent this transition or block core server startup.
4. The poller then starts and runs its first reconciliation pass immediately.
5. The poller compares the desired version from `app_version_change_requests` with this process's provider registry and running-version map:
   - **Match** — the desired version is already running. The poller validates that version's local package and records its `materialized_at`, then sets `restarted_at` on every pending row without downloading superseded versions or restarting the app.
   - **Missing or different** — if another version is running, the poller stops it; then it starts the desired version and updates the rollout-progress rows. A historical `stopped_at` in `app_instance_materializations` does not prove that the provider is still absent, because those rows record previous rollout writes rather than current process state.
6. An empty fleet-known projection leaves the app stopped and clears any stale running-version map entry and `active-version` marker.

A replica does not acknowledge a rollout while it is bootstrapping. If rollout enrollment closes before that replica starts polling, the replica is not part of the rollout cohort. Its first poll still reads the persisted change request and converges locally without reopening the terminal rollout.

The selected installation's registry must match deploy `source.registry` before materialization or start.

## Polling

After startup-provider initialization completes, every replica starts one background catalog controller: one reconcile pass immediately, then every **1 minute** on a single loop goroutine.

The controller is **pull-based** and **local**: each replica reads `app_version_change_requests` (`ListAllKnownVersions`) and reconciles itself against fleet install state. No replica fans out install RPCs to peers.

### Rollout Admission and Completion

Only one rollout per app may be active across the fleet. `POST …/add` and `POST …/upgrade` hold the app-scoped install lock while they reject an existing `enrolling` or `restarting` rollout, validate the candidate, create the new rollout, and append its change request. Different apps may roll out concurrently.

Rollout membership is scoped to one Toolshed source version. `SOURCE_VERSION` is the commit SHA already injected into every Cloud Run process. Keep `instance_id` process-unique; using one source version as the instance ID would collapse five replicas into one row and falsely report convergence after one process restarts.

Replica membership is discovered rather than configured:

1. App admission snapshots the current `SOURCE_VERSION` into `app_rollouts.target_source_version`.
2. The rollout remains `enrolling` for a bounded window of at least two poll intervals.
3. Replicas with that source version join by writing `acknowledged_at` before `enrollment_ends_at` and download the rollout artifact during this window, recording `materialized_at` while the current provider keeps running.
4. Replicas from another source version still reconcile the durable desired version, but they are not members of the target cohort and do not affect its outcome.
5. After enrollment, the target cohort is frozen and the rollout becomes `restarting`.
6. The rollout completes when the target cohort is non-empty and every member records `restarted_at`.
7. The rollout fails if a target-cohort replica does not restart before the deadline, or if no target replica acknowledges before the deadline.

When the catalog poller transitions a rollout to `complete` or `failed`, it writes one `app_version_rollout_outcomes` row keyed by the latest change request for `(app, version)`. Duplicate writes for the same change-request `id` are ignored. The poller records an outcome only when the transition succeeds; no-op transitions on an already-terminal rollout do not write. Terminal durations on the revision-history API prefer this sidecar over the live `app_rollouts` record.

This prevents a Cloud Run deployment from combining five old and five candidate processes into a ten-member cohort. When Cloud Run terminates the old deployment, those rows remain visible for diagnostics but do not block the candidate deployment from completing at `5/5 restarted`.

Never declare success because any source-version cohort completed. The old source version could reach `5/5` while the promoted source version fails. Success is tied to `target_source_version`.

#### Target Source Version Selection

The replica handling the version-selection HTTP request does not choose the target from its own environment. During Cloud Run traffic migration, an old revision can still finish an in-flight request and would incorrectly target the source version being removed.

Toolshed deployment orchestration owns shared source-version state:

1. After the candidate passes readiness, call `POST /activate?source_version={SOURCE_VERSION}` on its tagged management URL before shifting traffic. The candidate rejects the request unless the expected source version matches its local value. Activation atomically records that `SOURCE_VERSION` as current and retargets active app rollouts with a fresh enrollment epoch.
2. App admission reads the current source-version record while holding the app install lock and copies it to the rollout.
3. Shift 100% traffic to the candidate.

A repeated activation for the same source version is idempotent. If deployment fails after activation, rollback restores Cloud Run and Temporal traffic but does not restore the previous source-version record. Until the deployment is retried, a new app rollout may target the unavailable candidate and fail. Retrying the failed deployment calls `POST /activate?source_version={SOURCE_VERSION}&retry=true`; this refreshes active rollout epochs and reopens rollouts for that source version that failed since its previous activation before traffic is shifted again.

If a new source version becomes current while an app rollout is active, retarget the rollout and open a fresh enrollment window. Rollout transitions are fenced by target source version and enrollment epoch so a poller that sampled the old rollout cannot complete, fail, or advance the new one. The desired app version and change request do not change. Materialization rows from the superseded source version remain diagnostic and have `inCohort: false`.

Production rollout coordination requires `SOURCE_VERSION`. Local development may use a process-local value.

Replicas that do not acknowledge before enrollment closes are not cohort members and do not block completion. A late target-source-version replica still converges locally but does not reopen a terminal rollout.

Each pass:

1. Acknowledge each new `(app, version)` in `app_instance_materializations`, including the process `SOURCE_VERSION`.
2. Group pending versions by app and select one desired version with `LatestKnownVersion`.
3. Download and extract only the desired registry artifact to the canonical `{artifactsDir}/registry-installed/{app}/{desiredVersion}` path, recording `materialized_at` on that version's row while the provider is still running. During an active rollout's enrollment window, replicas materialize but do not stop or restart until enrollment closes. Superseded pending rows remain without `materialized_at`. Re-validate the desired path on every pass; if the tree is missing or corrupt, re-download it before `StopApp`.
4. After enrollment closes, stop and restart each restartable app once, recording `stopped_at` and `restarted_at` on every pending row. These timestamps mean the replica reconciled past those catalog changes; they do not mean every superseded version ran. `StartApp` receives the desired version and builds the provider from that materialized package instead of the deploy-time pin.
5. After the desired version is active, remove older registry-installed package directories for that app.
6. Mark non-restartable apps converged without a local stop/start.

A provider is **restartable** when this replica builds it locally from the configured pin: `server.remote` is unset or the provider has `local: true`, and the provider is not running in dev mode. Remote and dev-mode providers are non-restartable.

`StopApp` holds the per-app lifecycle lease until `StartApp` completes, preventing overlapping builds or replacements. On each replica, materialization is serialized per app: two versions of the same app cannot materialize concurrently, but different apps may.

`StartApp(app, version)` is strict for registry-only apps:

1. The materialized package must exist and pass package validation. Registry-only apps never fall back to an unresolved deploy-time provider entry.
2. If a provider is already registered and the local running-version map says it is serving the requested version, `StartApp` may return success without rebuilding it. If the recorded version is missing or different, Gestalt must not relabel the existing provider as the requested version; it must stop the existing provider and start the requested version.
3. If provider build, registration, or activation fails, Gestalt must clean up any partial state from that start attempt. The provider must not remain registered as the requested version, and static or runtime handlers must not serve it.
4. Stopping or removing a provider clears its running-version map entry and `active-version` marker, including when the provider was already absent.

Failure and retry behavior:

1. Reconciliation operations are idempotent: materializing an already valid package, stopping an absent provider, starting the already-running desired version, and repeating a rollout-progress write must succeed without duplicating work.
2. When an app reconciliation fails, the poller calls `RecordFailure` on the desired version's `app_instance_materializations` row. This atomically increments `attempt_count` and stores `last_error_at` and `last_error_message`. The poller then releases that app's lifecycle lease and continues reconciling other apps. If the failure itself cannot be written to IndexedDB, the poller logs that write error.
3. While `attempt_count` is below `server.appRegistry.maxReconcileAttempts`, the next poll retries that app from the beginning. It inspects the current provider registry, running-version map, `active-version` marker, and rollout-progress rows rather than relying on in-memory progress from the failed attempt. If stopping had begun, the app may remain unavailable until retry succeeds.
4. When `attempt_count` reaches the configured maximum, the poller stops retrying materialization and provider lifecycle work for that desired version on this replica. The limit does not apply to bootstrap: after a process restart, bootstrap still attempts the desired version selected by `LatestKnownVersion` without consulting `attempt_count`. If bootstrap succeeds, the poller may record that observed convergence despite its retry limit because no additional materialization or provider lifecycle attempt is required. The poller may likewise record convergence for a non-restartable app.
5. A newly accepted desired version gets a new row and a fresh attempt count. Increasing the configured maximum also permits retry when the stored count is below the new value.
6. Updates to the local `active-version` marker are atomic. A failed replacement leaves the previous valid marker in place.
7. If Gestalt cannot determine or clean up the local provider state safely, it marks the process unhealthy and terminates so the process supervisor can restart it.

### Runtime Behavior of Configured App Surfaces

A registry-only app does not have to expose a static UI, HTTP bindings, MCP, or any other particular surface. The following rules apply only to surfaces enabled for that app in the Gestalt YAML deploy configuration.

Gestalt constructs its HTTP server before it downloads or starts a registry package. Server construction must therefore succeed without a package manifest, operation catalog, static root, or running provider.

- **Static UI** — Gestalt creates the mount declared in YAML, but returns **503 Service Unavailable** until the provider registry, running-version map, and `active-version` marker agree on one version. It then serves the static bundle from that version's materialized package. If the version changes while a request is being resolved, Gestalt returns **503** rather than combining one version's UI with another version's provider. Theme files declared in YAML are resolved independently of the package.
- **HTTP bindings** — Gestalt mounts routes declared in YAML when it constructs the server, without requiring the package's operation catalog. Requests may be unavailable until the provider starts. A package cannot add routes to the already-constructed server, so every required HTTP binding must be declared in YAML.
- **MCP and operation invocation** — these resolve against the provider registry. They become available after `StartApp` registers the provider and unavailable when the provider is removed.

Runtime handlers determine availability from the local provider registry, running-version map, and `active-version` marker. They never use `app_instance_materializations`, whose rows record rollout progress rather than current runtime state.

## Runtime

The admin HTTP API is served under `/admin/api/v1` on the same listener as the other admin API (for example `/admin/api/v1/runtime/providers`). In deployments that split public and management listeners, call the management base URL.

### How to Invoke

```bash
# Local dev (default public port)
export GESTALTD_URL=http://localhost:8080

# Split management listener (production-style)
# export GESTALTD_URL=https://gestalt-management.example.com
```

List configured registries:

```bash
curl -sS "$GESTALTD_URL/admin/api/v1/app-registries" | jq .
```

List published versions for one app:

```bash
curl -sS "$GESTALTD_URL/admin/api/v1/app-registries/toolshed/apps/g-issues/versions" | jq .
```

Add the app to the fleet-known projection:

```bash
curl -sS -X POST "$GESTALTD_URL/admin/api/v1/app-registries/toolshed/apps/g-issues/add" \
  -H 'Content-Type: application/json' \
  -d '{"version":"0.0.0-snapshot.gabc123","actor":"user:alice"}' | jq .
```

Upgrade to a newer published version:

```bash
curl -sS -X POST "$GESTALTD_URL/admin/api/v1/app-registries/toolshed/apps/g-issues/upgrade" \
  -H 'Content-Type: application/json' \
  -d '{"version":"0.0.0-snapshot.gdef456","actor":"user:alice"}' | jq .
```

List fleet-known versions:

```bash
curl -sS "$GESTALTD_URL/admin/api/v1/app-installations" | jq .
```

`toolshed` is the registry name from deploy `config.yaml` (`appRegistries.toolshed`). `g-issues` is the published app name.

### Prerequisites

`appRegistries` must be present in deploy config. Example ([toolshed#3251](https://github.com/valon-technologies/toolshed/pull/3251)):

```yaml
appRegistries:
  toolshed:
    kind: gcs
    gcs:
      bucket: gs://gitlab-peach-street-gestalt-app-registry
```

Gestalt derives `publicUrl` — `https://storage.googleapis.com/{bucket}` — from `gcs.bucket`. The `gs://` storage form is used internally at publish time but is not returned by the admin list API.

The versions endpoint fetches `GET {publicUrl}/apps/{app}/index.json` over HTTP. The bucket must be readable at that public URL (or reachable from the gestaltd process network).

Install routes additionally require:

- IndexedDB `app_version_change_requests` service configured on the server (`AppVersionChangeRequestsService`)
- IndexedDB `app_version_install_locks` service configured on the server (`AppVersionInstallLockService`)

List/get install endpoints project fleet-known versions from `app_version_change_requests` only.

### Endpoints

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/admin/api/v1/app-registries` | List registries from config |
| `GET` | `/admin/api/v1/app-registries/{registry}/apps/{app}/versions` | List published versions for one app |
| `POST` | `/admin/api/v1/app-registries/{registry}/apps/{app}/add` | Record the first fleet-known version for an app |
| `POST` | `/admin/api/v1/app-registries/{registry}/apps/{app}/upgrade` | Record a new fleet-known version when the app is already in the catalog |
| `GET` | `/admin/api/v1/app-installations` | List all **fleet-known versions** across apps |
| `GET` | `/admin/api/v1/app-installations/{app}` | List **fleet-known versions** for one app |

List routes are read-only (`GET` only). `add` and `upgrade` use `POST` on a separate route group with a longer request timeout (10 minutes).

#### `GET /admin/api/v1/app-registries`

Returns the named registries configured in `appRegistries`, sorted by name.

**Response `200`**

```json
[
  {
    "name": "toolshed",
    "kind": "gcs",
    "publicUrl": "https://storage.googleapis.com/gitlab-peach-street-gestalt-app-registry"
  }
]
```

| Field       | Type   | Description                              |
| ----------- | ------ | ---------------------------------------- |
| `name`      | string | Registry key from `appRegistries`        |
| `kind`      | string | Registry backend (`gcs` today)           |
| `publicUrl` | string | HTTPS root used to fetch index documents |

When no registries are configured, the response is an empty array `[]` (not `null`).

1. `listAdminAppRegistries` reads `s.appRegistries` (a clone of `appRegistries` from deploy config, set at `server.New`).
2. If `appRegistries` is empty or unset, respond `200` with `[]`.
3. Otherwise sort registry names and, for each entry, build `{ name, kind, publicUrl }`.
4. Respond `200` with the JSON array.

No GCS fetch. No IndexedDB read or write.

#### `GET /admin/api/v1/app-registries/{registry}/apps/{app}/versions`

Fetches `apps/{app}/index.json` from the configured registry's `publicUrl` and returns version summaries for `{app}`.

**Path parameters**

| Parameter | Description |
| --- | --- |
| `registry` | Name of a configured registry (`appRegistries` key) |
| `app` | Published app name (same rules as `gestaltd app registry publish --app`) |

App names must match `providerregistry.ValidateRepositoryName`: lowercase letters, digits, dots, underscores, and hyphens only. Invalid names (including path traversal such as `..`) return `400`.

**Response `200`**

```json
{
  "registry": "toolshed",
  "app": "g-issues",
  "versions": [
    {
      "version": "0.0.0-snapshot.gabc123",
      "metadata": "apps/g-issues/versions/0.0.0-snapshot.gabc123.json",
      "platforms": ["linux/amd64", "darwin/arm64"],
      "publishedAt": "2026-07-10T02:21:54Z"
    }
  ]
}
```

| Field | Type | Description |
| --- | --- | --- |
| `registry` | string | Registry name from the path |
| `app` | string | App name from the path |
| `versions` | array | Version summaries, newest `publishedAt` first |
| `versions[].version` | string | Published version identifier |
| `versions[].metadata` | string | Relative path to the full published version JSON in the bucket |
| `versions[].platforms` | string array | Platforms published for this version (omitted when empty) |
| `versions[].publishedAt` | RFC 3339 timestamp | Publish time (UTC) |

When the app has no index or no versions yet, `versions` is `[]` (not `null`). A missing `apps/{app}/index.json` object is treated as an empty catalog.

`metadata` points at the immutable published version document described in [models.md](../architecture/models.md). These routes do not inline published version fields (artifacts, interface, dependencies).

1. `listAdminAppRegistryAppVersions` reads `{registry}` and `{app}` from the URL.
2. Validate `app` (`providerregistry.ValidateRepositoryName`). Look up `{registry}` in `s.appRegistries`; reject unknown registries and non-`gcs` kinds.
3. `RegistryReader.FetchAppIndex` — HTTP `GET` `apps/{app}/index.json` from the configured registry (live fetch on every request).
4. Respond `200` with `{ registry, app, versions }`.

No IndexedDB read or write. Lists **published** versions in the registry bucket, not fleet-known versions from `app_version_change_requests`.

#### `POST /admin/api/v1/app-registries/{registry}/apps/{app}/add`

Record the **first** fleet-known version for an app. Use when `ListKnownVersionsByApp` is empty.

**Path parameters**

| Parameter  | Description                                         |
| ---------- | --------------------------------------------------- |
| `registry` | Name of a configured registry (`appRegistries` key) |
| `app`      | Published app name                                  |

**Request body**

```json
{
  "version": "0.0.0-snapshot.gabc123",
  "actor": "user:alice"
}
```

| Field     | Type   | Required | Description                       |
| --------- | ------ | -------- | --------------------------------- |
| `version` | string | yes      | Published version to add          |
| `actor`   | string | no       | Actor recorded on catalog records |

**Response `200`** — same shape as upgrade below.

Returns **409 Conflict** when the app already has fleet-known versions (use `upgrade` instead).

#### `POST /admin/api/v1/app-registries/{registry}/apps/{app}/upgrade`

Record a new fleet-known version when the app is already in the catalog.

**Path parameters**

| Parameter  | Description                                         |
| ---------- | --------------------------------------------------- |
| `registry` | Name of a configured registry (`appRegistries` key) |
| `app`      | Published app name                                  |

**Request body**

```json
{
  "version": "0.0.0-snapshot.gdef456",
  "actor": "user:alice"
}
```

| Field     | Type   | Required | Description                       |
| --------- | ------ | -------- | --------------------------------- |
| `version` | string | yes      | Published version to upgrade to   |
| `actor`   | string | no       | Actor recorded on catalog records |

**Response `200`**

```json
{
  "registry": "toolshed",
  "app": "g-issues",
  "installation": {
    "app": "g-issues",
    "version": "0.0.0-snapshot.gabc123",
    "sourceRef": "abc123def456abc123def456abc123def456abcd",
    "registry": "toolshed",
    "providerReleaseUrl": "https://storage.googleapis.com/gitlab-peach-street-gestalt-app-registry/apps/g-issues/versions/0.0.0-snapshot.gabc123.json",
    "artifactChecksums": {
      "linux/amd64": "deadbeef…"
    },
    "installedAt": "2026-07-10T14:19:58Z",
    "updatedAt": "2026-07-10T14:19:58Z"
  }
}
```

| Field | Type | Description |
| --- | --- | --- |
| `registry` | string | Registry name from the path |
| `app` | string | App name from the path |
| `installation` | object | Known version after a successful add or upgrade (from the `change request` record) |

Re-installing a version that is already in the catalog returns **400 Bad Request** (`app version is already installed`). Returns **400** when the app has no fleet-known versions yet (use `add` instead).

Synchronous on the handling instance. The HTTP response is sent after the catalog write finishes or fails.

1. Handler reads `{registry}` and `{app}` from the URL and `{ version, actor }` from the JSON body.
2. Validate path params and look up `{registry}` in `s.appRegistries`.
3. `Installer.Add` or `Installer.Upgrade` on the handling instance:
   1. Claim the app-scoped fleet install lock in `app_version_install_locks`; the version is diagnostic metadata (`409` if another holder holds a non-expired lock).
   2. **`add`** — reject when `ListKnownVersionsByApp` is non-empty (`409`). **`upgrade`** — reject when the projection is empty (`400`).
   3. If `(app, version)` is already known in `app_version_change_requests`, return `400`.
   4. `RegistryReader.FetchEntry` — HTTP `GET` the published version document from the configured registry (validate the version exists; **no artifact download**).
   5. **`InstallValidator.Validate`** — reject incompatible or unsatisfied candidates (**400**); see [validation.md](../architecture/validation.md).
   6. `Rollouts.Create` — start fleet rollout (`409` when another rollout is active).
   7. Append `change request` to `app_version_change_requests`. Set `from_version` server-side: `registry:first-install` on `add`, `LatestKnownVersion` on `upgrade`. Callers never send `from_version`. Mark rollout `failed` if append fails.
   8. Release the install lock (always, via defer). On failure before the change request is appended, return an HTTP error; no change request is written.
4. Respond `200` with `{ registry, app, installation }`.

Per-replica convergence via the background catalog controller (see Polling). IndexedDB write on the handling instance for add/upgrade.

#### `GET /admin/api/v1/app-installations`

Returns all **fleet-known versions** projected from change requests. See [indexeddb.md](../architecture/indexeddb.md).

**Response `200`**

```json
[
  {
    "app": "g-issues",
    "version": "0.0.0-snapshot.gabc123",
    "sourceRef": "abc123def456abc123def456abc123def456abcd",
    "registry": "toolshed",
    "providerReleaseUrl": "https://storage.googleapis.com/gitlab-peach-street-gestalt-app-registry/apps/g-issues/versions/0.0.0-snapshot.gabc123.json",
    "artifactChecksums": {
      "linux/amd64": "deadbeef…"
    },
    "installedAt": "2026-07-10T14:19:58Z",
    "updatedAt": "2026-07-10T14:19:58Z"
  }
]
```

When no fleet-known versions exist, the response is `[]` (not `null`).

1. `listAdminAppInstallations` requires `AppVersionChangeRequests` on the server; otherwise respond `503`.
2. `ChangeRequests.ListAllKnownVersions` — read `app_version_change_requests` and project fleet-known `(app, to_version)` pairs.
3. Map each projection to `{ app, version, sourceRef, registry, providerReleaseUrl, artifactChecksums, installedBy, installedAt, updatedAt }`.
4. Respond `200` with the JSON array (empty when no fleet-known versions exist).

IndexedDB read only. No GCS fetch.

#### `GET /admin/api/v1/app-installations/{app}`

Returns **fleet-known versions** for one app.

**Response `200`** — array of objects with the same shape as one element of the fleet list response above.

**Response `404`** when the app has no `change request` records yet.

1. `getAdminAppInstallation` reads `{app}` from the URL and validates the app name.
2. Requires `AppVersionChangeRequests` on the server; otherwise respond `503`.
3. `ChangeRequests.ListKnownVersionsByApp` — read `app_version_change_requests` and project fleet-known versions for that app.
4. If no fleet-known versions exist, respond `404`.
5. Otherwise map results to the same installation object shape and respond `200` with a JSON array.

IndexedDB read only. No GCS fetch.

### Admin Observability API

Read-only routes under `/admin/api/v1` with `gestaltAdmin` auth. UI wireframes: [admin.md](./admin.md#embedded-admin-ui-admin).

These routes expose IndexedDB rollout state that the catalog poller already writes. They do not change install or convergence behavior.

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/admin/api/v1/registry-apps` | Registry-only apps from deploy config, merged with desired version and rollout summary |
| `GET` | `/admin/api/v1/registry-apps/{app}` | One registry-only app: fleet-known versions, rollout, optional latest published registry version |
| `GET` | `/admin/api/v1/app-rollouts` | List active and recent terminal rollouts |
| `GET` | `/admin/api/v1/app-rollouts/{app}/materializations` | Per-replica rollout-progress rows for one `(app, version)` |

#### `GET /admin/api/v1/registry-apps`

List deploy-configured registry-only apps (`source.registry`), merged with fleet state.

**Response `200`**

```json
[
  {
    "app": "g-issues",
    "registry": "toolshed",
    "desiredVersion": "0.0.0-snapshot.gcd9d741cc35728476426afce6c069e198799a8be",
    "rollout": {
      "version": "0.0.0-snapshot.gcd9d741cc35728476426afce6c069e198799a8be",
      "state": "complete",
      "targetSourceVersion": "61885becf49a25a4a8c0063a4d9dd9643b28c2a6",
      "createdAt": "2026-07-21T17:06:12Z",
      "enrollmentEndsAt": "2026-07-21T17:08:12Z",
      "deadline": "2026-07-21T17:21:12Z",
      "completedAt": "2026-07-21T17:09:06Z"
    },
    "cohort": {
      "acknowledged": 3,
      "materialized": 3,
      "restarted": 3,
      "failed": 0
    }
  }
]
```

| Field | Type | Description |
| --- | --- | --- |
| `app` | string | App name from deploy `apps` |
| `registry` | string | `source.registry` binding |
| `desiredVersion` | string | `LatestKnownVersion`; omitted when the fleet catalog is empty |
| `rollout` | object | Current or most recent rollout for this app; omitted when none |
| `rollout.targetSourceVersion` | string | Toolshed `SOURCE_VERSION` selected by deployment orchestration |
| `cohort` | object | Counts over target-source-version materialization rows that acknowledged before `enrollmentEndsAt` |

Apps with no fleet-known version still appear when configured with `source.registry`. IndexedDB read only. No GCS fetch.

#### `GET /admin/api/v1/registry-apps/{app}`

Detail view for one registry-only app. Same fields as one list element, plus:

```json
{
  "app": "g-issues",
  "registry": "toolshed",
  "desiredVersion": "0.0.0-snapshot.gcd9d741…",
  "knownVersions": [
    {
      "version": "0.0.0-snapshot.gcd9d741…",
      "installedAt": "2026-07-21T17:06:12Z",
      "installedBy": "user:michael.wang@valon.com"
    }
  ],
  "rollout": {},
  "latestPublished": {
    "version": "0.0.0-snapshot.gcd9d741…",
    "publishedAt": "2026-07-21T15:36:47Z"
  }
}
```

`knownVersions` lists fleet-known `(app, version)` pairs projected from `app_version_change_requests`.

`latestPublished` is optional. The handler may fetch the registry index and return the newest `publishedAt` entry so operators can compare **published** vs **fleet-known** vs **converged**.

**Response `404`** when `{app}` is not a registry-only app in deploy config.

#### `GET /admin/api/v1/app-rollouts`

List rollout records. Default: active rollouts (`enrolling`, `restarting`) plus terminal rollouts from the last 24 hours. Support `?app={app}` and `?state={state}` filters.

**Response `200`**

```json
[
  {
    "app": "g-issues",
    "version": "0.0.0-snapshot.gcd9d741…",
    "state": "restarting",
    "createdAt": "2026-07-21T17:06:12Z",
    "enrollmentEndsAt": "2026-07-21T17:08:12Z",
    "deadline": "2026-07-21T17:21:12Z"
  }
]
```

#### `GET /admin/api/v1/app-rollouts/{app}/materializations`

Per-replica rollout-progress rows for one app rollout.

**Query parameters**

| Parameter | Type | Description |
| --- | --- | --- |
| `version` | string | Fleet-known version. Defaults to the current rollout's `version`, else `desiredVersion`. |

**Response `200`**

```json
{
  "app": "g-issues",
  "version": "0.0.0-snapshot.gcd9d741…",
  "rolloutState": "complete",
  "materializations": [
    {
      "instanceId": "gestaltd-8d9487869-ncnq6",
      "sourceVersion": "61885becf49a25a4a8c0063a4d9dd9643b28c2a6",
      "acknowledgedAt": "2026-07-21T17:08:12Z",
      "materializedAt": "2026-07-21T17:08:57Z",
      "stoppedAt": "2026-07-21T17:08:57Z",
      "restartedAt": "2026-07-21T17:08:58Z",
      "attemptCount": 0,
      "lastErrorAt": null,
      "lastErrorMessage": "",
      "inCohort": true,
      "converged": true
    }
  ]
}
```

| Field | Type | Description |
| --- | --- | --- |
| `sourceVersion` | string | Toolshed `SOURCE_VERSION` for this process |
| `inCohort` | boolean | `sourceVersion` matches `targetSourceVersion` and acknowledgement is within the current enrollment epoch |
| `converged` | boolean | `restarted_at` set and not after `rollout.deadline` while rollout is active; when terminal, `restarted_at` present |

The embedded `/admin` UI includes a read-only **App Registry** section that consumes these endpoints.

### App-Admin Version Selection

App-scoped routes on the authenticated public API. UI capabilities: [admin.md](./admin.md#app-admin-ui-appsappadmin).

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/v1/apps/{app}/admin/registry` | Load pending, failed, published, and fleet-known versions, the desired version, rollout admission state, and auto-deploy settings |
| `GET` | `/api/v1/apps/{app}/admin/registry/history` | Load the permanent deploy chain for the Revision history tab |
| `POST` | `/api/v1/apps/{app}/admin/registry/version` | Select the fleet-wide desired version |
| `PUT` | `/api/v1/apps/{app}/admin/registry/auto-deploy` | Enable or disable automatic admission of new published snapshots |

#### Authorization

Only an authenticated **user** with explicit `admin` on `app/{app}` may call these routes. Fail closed:

- **401** when unauthenticated
- **403** when authenticated but not an app admin (including global `gestaltAdmin` without app admin)
- **503** when authorization is unavailable

Do not reuse the mounted-UI fallback when no authorization provider exists.

`GET /api/v1/apps` exposes optional `managementPath` (for example `/apps/g-issues/admin`) only when the app is registry-only, the caller is a user, and the caller has `admin` on `app/{app}`.

#### `GET /api/v1/apps/{app}/admin/registry`

**Response `200`**

```json
{
  "app": "g-issues",
  "registry": "toolshed",
  "desiredVersion": "0.0.0-snapshot.gabc123",
  "knownVersions": [
    {
      "version": "0.0.0-snapshot.gabc123",
      "installedAt": "2026-07-22T14:00:00Z",
      "installedBy": "user:alice"
    }
  ],
  "pendingVersions": [],
  "failedVersions": [],
  "publishedVersions": [
    {
      "version": "0.0.0-snapshot.gdef456",
      "publishedAt": "2026-07-22T15:00:00Z",
      "platforms": ["linux/amd64"],
      "sourceRef": "def456def456def456def456def456def456def4",
      "sourceUrl": "https://github.com/valon-technologies/valon-tools/commit/def456def456def456def456def456def456def4",
      "deploymentState": "available",
      "publication": {
        "workflowRunUrl": "https://github.com/valon-technologies/valon-tools/actions/runs/123456789",
        "triggerPullRequest": {
          "number": 3251,
          "url": "https://github.com/valon-technologies/valon-tools/pull/3251"
        }
      }
    }
  ],
  "rollout": {
    "version": "0.0.0-snapshot.gabc123",
    "state": "complete"
  },
  "autoDeploy": {
    "enabled": false,
    "pendingVersion": "0.0.0-snapshot.gdef456",
    "lastError": null
  },
  "selectionDisabled": false
}
```

When a rollout is active, `selectionDisabled` is `true` and `disabledReason` is `"rollout in progress"`.

Rules:

- `{app}` must be deploy-configured with `source.registry`.
- `knownVersions` comes from the change-request projection; `desiredVersion` is `LatestKnownVersion` and is omitted before first install.
- `publishedVersions` comes from the registry index, newest `publishedAt` first. Each entry includes `publishedAt`, `sourceRef`, `sourceUrl`, and `publication` when recorded. Legacy versions may omit `publication`.
- `publishedVersions[].deploymentState` is `available` for never-deployed versions before `expiresAt`, `expired` for never-deployed versions after `expiresAt` but before pruning, `desired` for the current version, `redeployable` for historical versions before `expiresAt`, or `locked` after `expiresAt`.
- `deployableUntil` is returned for historical versions (mapped from `expiresAt`). The UI offers **Deploy** only for `available` and `redeployable`.
- `pendingVersions` and `failedVersions` come from `pending.json` and `failed.json`. See [pending-publish.md](./pending-publish.md#read-path).
- When the same version appears in more than one catalog, apply [merge rules](./pending-publish.md#app-admin-api).
- `selectionDisabled` is true only while rollout state is `enrolling` or `restarting`.

#### Revision History

<a id="revision-history"></a>

`GET /api/v1/apps/{app}/admin/registry/history?limit=50&cursor=…` returns the append-only `app_version_change_requests` sequence in reverse chronological order.

**Response `200`**

```json
{
  "app": "g-issues",
  "revisions": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "version": "0.0.0-snapshot.gdef456",
      "previousVersion": "0.0.0-snapshot.gabc123",
      "deployedAt": "2026-07-24T20:42:00Z",
      "deployedBy": "user:alice",
      "sourceRef": "def456def456def456def456def456def456def4",
      "sourceUrl": "https://github.com/valon-technologies/valon-tools/commit/def456def456def456def456def456def456def4",
      "publication": {
        "workflowRunUrl": "https://github.com/valon-technologies/valon-tools/actions/runs/123456789",
        "triggerPullRequest": {
          "number": 3251,
          "url": "https://github.com/valon-technologies/valon-tools/pull/3251"
        }
      },
      "deploymentState": "desired",
      "current": true,
      "rolloutState": "restarting",
      "rolloutForSeconds": 134,
      "rolloutDurationSeconds": null,
      "rolloutCompletedAt": null,
      "rolloutFailedAt": null
    }
  ],
  "nextCursor": "2026-07-24T20:42:00Z:550e8400-e29b-41d4-a716-446655440000"
}
```

Rules:

- The endpoint reads raw change requests, not `knownVersions`, so repeated projections cannot collapse accepted transitions.
- Sort by `(timestamp, id)` descending. The opaque cursor contains that pair; default `limit` is 50 and maximum is 100.
- The first deployment omits `previousVersion` when stored `from_version` is `registry:first-install`.
- `current` is true only for the latest request that produced `LatestKnownVersion`. Earlier requests remain historical even when they selected the same version.
- `deploymentState` is the selected version's present-day eligibility: `desired`, `redeployable`, or `locked`. Repeated entries for the same version share the same value. Historical entries include `deployableUntil` (from `expiresAt`) when applicable.
- `deployedAt`, `deployedBy`, source fields, and publication provenance come from the change request and its immutable `metadata_json` install-contract snapshot. New admissions snapshot publication provenance; legacy rows may omit it.
- Omit rollout fields when no rollout exists for the revision and no outcome sidecar is stored.
- `rolloutState` is `enrolling`, `restarting`, `complete`, or `failed`.
- For active rollouts, project `rolloutState` from `app_rollouts` only on the current revision (`id` matches the latest change request). Older same-version rows do not inherit the live rollout.
- For terminal rows, prefer the `app_version_rollout_outcomes` sidecar. Fall back to `app_rollouts` when the rollout record is still terminal and versions match.
- `rolloutForSeconds` is set only while state is `enrolling` or `restarting`.
- `rolloutDurationSeconds`, `rolloutCompletedAt`, and `rolloutFailedAt` are set only for terminal states.
- Legacy revisions without a stored outcome omit terminal duration fields.
- An app with no change requests returns `revisions: []`. The route has the same app-admin authorization and fail-closed behavior as the registry-state route.
- The endpoint performs no writes and exposes no deploy action. Eligible historical versions are selected from Published snapshots.

#### `POST /api/v1/apps/{app}/admin/registry/version`

**Request**

```json
{
  "version": "0.0.0-snapshot.gdef456"
}
```

The request accepts no `actor`, `registry`, or `fromVersion`; unknown fields return **400**. The server derives `actor` from the authenticated user, `registry` from deploy config, and `fromVersion` from `LatestKnownVersion`.

**Response `200`**

```json
{
  "app": "g-issues",
  "registry": "toolshed",
  "fromVersion": "0.0.0-snapshot.gabc123",
  "desiredVersion": "0.0.0-snapshot.gdef456",
  "rollout": {
    "version": "0.0.0-snapshot.gdef456",
    "state": "enrolling"
  }
}
```

A request is accepted only when all checks pass:

1. Authenticate the caller and authorize `admin` on `app/{app}` (**401** / **403**; **503** when authorization is unavailable).
2. Validate `{app}` is deploy-configured with `source.registry` (**404** when not registry-only).
3. Claim the app-scoped install lock.
4. Read the current rollout while holding the lock.
5. Reject when rollout state is `enrolling` or `restarting` with **409**. No registry fetch, install-time validation, rollout creation, or change-request append occurs on this path. Terminal `complete` or `failed` rollouts do not block a new selection.
6. Reject when the selected version equals the current desired version with **400** and no writes.
7. Resolve deployment state from `retention.json`:
   - accept a never-deployed published version only before `expiresAt`
   - accept a historical version only before `expiresAt`, or when `expiresAt` is omitted on a previously deployed version
   - reject an expired never-deployed version or an expired historical version with **400** and no writes
8. Fetch the published version from the configured registry and run install-time validation ([validation.md](../architecture/validation.md)).
9. Create the rollout and append the change request (`add` on first selection; `upgrade` on later selections, including a downgrade or repeated version). The change request may record `from_version_deployable_until` for audit; admission does not read it.
10. Mirror the transition into `retention.json` (clear `expiresAt` on the version that becomes desired; set `expiresAt = now + deployedRetention` on the version that stops being desired).
11. Release the install lock.

The rollout check in step 5 is authoritative. Concurrent selection requests must recheck under the install lock so only one admission succeeds.

#### Historical Redeployment and Locking

When `v1 → v2` is accepted, `v1.expiresAt` is set to `now + deployedRetention` (default 30 days). `v2` is desired, so `expiresAt` is cleared.

Selecting `v1` before `expiresAt` appends a new `v2 → v1` request. The chain retains both transitions. When `v1` later stops being desired, it receives a new `expiresAt`. Once `expiresAt` passes, the version is permanently locked and cannot be selected again, though every history event and its metadata remains visible.

For a repeated version, reset per-replica materialization rows whose `acknowledged_at` predates the new rollout's `created_at`. Count cohort membership and convergence only from timestamps at or after the current rollout's `created_at`.

The server enforces the deadline while holding the app-scoped install lock. UI suppression is not the security boundary.

#### Auto-Deploy Published Snapshots

App admins opt a registry-only app into automatic fleet admission on `/apps/{app}/admin`. When enabled, Gestalt admits the newest published snapshot as the fleet-wide desired version without a manual **Deploy** click. Auto-deploy uses the same admission path as `POST /api/v1/apps/{app}/admin/registry/version`: install lock, install-time validation, rollout creation, change-request append, and retention mirror. Per-replica convergence is unchanged.

Scope is `/apps/{app}/admin` only — not the embedded fleet `/admin` UI. Only an authenticated user with `admin` on `app/{app}` may enable or disable the policy. Auto-deploy does not bypass install-time validation, retention locks, or rollout admission checks.

Admission triggers on publish completion — when `index.json` includes the version (after `gestaltd app registry pending clear`). Pending and failed publishes do not trigger auto-deploy. See [pending-publish.md](./pending-publish.md). At most one rollout per app remains unchanged: while rollout state is `enrolling` or `restarting`, new publishes update the pending version but do not start another admission. When the rollout reaches `complete`, Gestalt admits the pending version if it differs from the current desired version; intermediate publishes are skipped. On rollout `failed`, Gestalt disables auto-deploy, clears pending state, and records `lastError` until an app admin deploys manually and re-enables the policy. Automatic admissions appear in revision history with actor `system:auto-deploy`.

Fleet policy is stored in IndexedDB (`app_auto_deploy_settings`). See [indexeddb.md](../architecture/indexeddb.md#store-app_auto_deploy_settings-auto-deploy-policy).

##### Publish Detection

For enabled apps, a background auto-deploy controller in `gestaltd serve` polls `apps/{app}/index.json` over HTTP from the registry `publicUrl`. Each poll is one GET per enabled app. Admission still writes to IndexedDB and fetches `versions/{version}.json` only when coalescing starts an install.

Default poll interval is **1 minute**, aligned with the catalog poller. Override with `server.appRegistry.autoDeployPollInterval` when set. Coalescing correctness does not depend on the interval. If `gestaltd serve` is unavailable at publish time, the next poll still converges on the latest version.

Each replica may poll independently today. Duplicate reads are acceptable at this rate; designate a single evaluator later if the enabled-app count grows.

The controller stores the last `ETag` per app in process memory. On each poll:

1. Send `If-None-Match: {etag}` when a prior `ETag` exists.
2. On **304 Not Modified**, skip publish detection, then continue coalescing any persisted pending version. This recovers if a rollout-terminal wakeup was missed.
3. On **200**, parse the index, update the stored `ETag`, and compare the newest published version against `last_seen_version`.

GCS returns `ETag` on every `index.json` response. A missing `ETag` on the first response falls back to an unconditional GET on the next poll.

##### Coalescing

Per enabled app, Gestalt tracks a pending version: the newest published snapshot that should become desired once admission is possible.

1. If auto-deploy is disabled, stop. Disabling clears `pending_version`.
2. On publish detection or enable — set `pending_version` to the newest published version.
3. On a new rollout `failed` transition — disable auto-deploy, clear `pending_version` and `last_seen_version`, record `last_error` and `last_failed_rollout_at`; stop. The timestamp prevents the retained terminal rollout row from immediately disabling a later app-admin re-enable.
4. If rollout state is `enrolling` or `restarting`, stop. `pending_version` is already updated.
5. If `pending_version` is empty, stop.
6. If `pending_version` equals the desired version, clear `pending_version` and stop.
7. Capture `pending_version` and attempt admission for that version (same as manual version selection). Run this step after publish detection and on periodic or rollout-terminal reconciliation, including when the registry returns **304**.
   - **Success** — clear `pending_version` only if it still equals the captured version. Preserve a newer target written concurrently by another replica.
   - **Validation failure (400)** — clear `pending_version` and set `last_error` only if it still equals the captured version. Do not retry until the next publish or a manual deploy.
   - **Active rollout (409)** — keep `pending_version`; retry when the rollout reaches `complete`.

| Action | Behavior |
| --- | --- |
| Disable | Clear the pending version and stop future automatic admissions. An in-flight rollout continues; the current desired version is unchanged. |
| Enable | Run coalescing once. When no rollout is active and the newest published version is not desired, admit it. A successful enable notifies the background controller to reconcile immediately instead of waiting for the next poll. |

##### `PUT /api/v1/apps/{app}/admin/registry/auto-deploy`

**Request**

```json
{
  "enabled": true
}
```

**Response `200`**

```json
{
  "app": "g-issues",
  "autoDeploy": {
    "enabled": true,
    "pendingVersion": "0.0.0-snapshot.gdef456",
    "lastError": null
  }
}
```

`GET /api/v1/apps/{app}/admin/registry` projects `autoDeploy.enabled`, `autoDeploy.pendingVersion`, and `autoDeploy.lastError` alongside rollout and published-version state.

##### Edge Cases

| Scenario | Behavior |
| --- | --- |
| Install-time validation fails | Clear pending; expose `lastError`. No automatic retry. |
| Version expired or locked | Admission rejected (**400**); clear pending. |
| No fleet-known version yet | First publish triggers `add`. |
| Concurrent manual deploy | Install lock serializes; auto-deploy retries when the rollout reaches `complete`. |
| Rollout `failed` | Disable auto-deploy, clear pending, record `lastError`. App admin must deploy manually and re-enable auto-deploy to resume automatic admissions. |

### Errors

Errors use the standard gestaltd admin API error envelope (`error` field).

| Status | When |
| --- | --- |
| `400` | Missing path param; invalid `app` name; invalid JSON body; missing `version`; unsupported registry `kind` (non-`gcs`); app version already installed; `upgrade` called when the app has no fleet-known versions; **install-time validation failed** |
| `404` | Unknown `registry` name; published version not found; no fleet-known versions for `{app}`; no `appRegistries` configured |
| `409` | Another instance holds the install lock for this app; `add` called when the app already has fleet-known versions |
| `502` | Published version fetch failed; registry fetch failed during install validation; registry named in a fleet-known installation is missing from gestaltd config; failed to append `change request` record; upstream fetch of `apps/{app}/index.json` failed (network, non-2xx other than 404, invalid JSON) |
| `500` | Registry `publicUrl` could not be derived from config; unexpected catalog projection failure |
| `503` | Version catalog service or installer not configured |

#### App-Admin Errors

App-admin routes use the same `{ "error": "…" }` envelope.

| Status | When |
| --- | --- |
| `400` | Selected version is current, expired, or permanently locked; unknown request fields; install-time validation failure |
| `401` | Missing or invalid authentication |
| `403` | Authenticated user lacks `admin` on `app/{app}` |
| `404` | App is not registry-only; published version does not exist |
| `409` | Rollout is active; concurrent selection lost admission |
| `502` | Registry index or version metadata fetch failed |
| `503` | Authorization or registry installation services are unavailable |

Example:

```json
{
  "error": "app registry not found"
}
```

---

## Appendix

### Related Changelogs

<pre>
├── <a href="../project/changelog.md#changelog-06">06 — Registry Installation Prototype</a>
├── <a href="../project/changelog.md#changelog-07">07 — Catalog-Only Admission</a>
├── <a href="../project/changelog.md#changelog-08">08 — Per-Replica Catalog Polling</a>
├── <a href="../project/changelog.md#changelog-09">09 — Coordinated Provider Restarts</a>
├── <a href="../project/changelog.md#changelog-10">10 — Materialize Before Restart</a>
├── <a href="../project/changelog.md#changelog-11">11 — Mount the Registry-Installed Package</a>
├── <a href="../project/changelog.md#changelog-12">12 — Complete Registry-Only Lifecycle</a>
├── <a href="../project/changelog.md#changelog-14">14 — Fleet Admin Observability</a>
├── <a href="../project/changelog.md#changelog-15">15 — App-Scoped Version Selection</a>
├── <a href="../project/changelog.md#changelog-17">17 — Version Retention and Cleanup</a>
├── <a href="../project/changelog.md#changelog-18">18 — Revision History and Redeploy Windows</a>
└── <a href="../project/changelog.md#changelog-25">25 — Auto-Deploy Published Snapshots</a>
</pre>

### Related Docs

<pre>
├── <a href="../readme.md">readme.md</a> — registry architecture and future work
├── <a href="../project/changelog.md">changelog.md</a> — implementation milestones and pull requests
├── <a href="../architecture/validation.md">validation.md</a> — install-time validation
├── <a href="./admin.md">admin.md</a> — admin UI capabilities
├── <a href="../architecture/indexeddb.md">indexeddb.md</a> — app_version_change_requests, app_instance_materializations, install locks
├── <a href="../architecture/config.md">config.md</a> — appRegistries deploy reader config
├── <a href="../architecture/models.md">models.md</a> — index and published version JSON stored in GCS
└── <a href="../project/tests.md">tests.md</a> — convergence unit tests
</pre>
