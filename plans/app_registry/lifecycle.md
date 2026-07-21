# App Registry Replica Lifecycle

How each `gestaltd` replica observes fleet install state, materializes app artifacts locally, and serves registry-installed app versions.

Install is change-request-only; per-replica convergence (ack → download → restart → mount) is planned via polling below. List/get install endpoints expose **projected known versions** from change requests.

Related docs:

- [plan.md](./plan.md) — install flow, catalog model, rollout steps
- [validation.md](./validation.md) — install-time validation
- [admin.md](./admin.md) — admin UI and rollout read APIs
- [indexeddb.md](./indexeddb.md) — `app_version_change_requests`, `app_instance_materializations`, install locks
- [config.md](./config.md) — `appRegistries` deploy reader config
- [models.md](./models.md) — index and published version JSON stored in GCS
- [service.md](./service.md) — Go publish/read helpers behind the registry bucket
- [tests.md](./tests.md) — convergence unit tests

Implementation:

- Registry list handlers — `gestaltd/internal/server/handlers_admin_app_registry.go`
- Install handlers — `gestaltd/internal/server/handlers_admin_app_install.go`
- Registry fetcher — `gestaltd/internal/appregistry/reader.go`
- Registry installer — `gestaltd/internal/appregistry/installer.go`
- Catalog poller — `gestaltd/internal/appregistry/poller.go`
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

Apps with a deploy-time source pin (`source.git`, `source.path`, etc.) bind into the provider graph during the normal startup provider build. Registry-only apps (`source.registry`) are different — see [config.md](./config.md#registry-only-app-source). They have no resolved manifest or baked artifact at deploy time, so bootstrap excludes them from that build loop.

At `StartAppProviders`, each registry-only app:

1. Read fleet-known versions via `ListKnownVersionsByApp`. When the projection is empty, skip the app — nothing is running until the first `POST …/add`.
2. Take the latest fleet-known version (`LatestKnownVersion`).
3. Materialize the registry artifact to `{artifactsDir}/registry-installed/{app}/{version}` when the tree is missing or incomplete.
4. Start the provider through `StartApp` with the registry-mounted binary — the same mount path used by catalog-driven restarts.

The catalog poller still handles first install and upgrades on replicas that were skipped at boot or join after a rollout.

### Runtime version invariants

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

Bootstrap and the poller must both use `LatestKnownVersion` to select the same desired installation. See [indexeddb.md](./indexeddb.md#accepted-changes-and-projections) for the version-ordering rule.

Each replica materializes and retains only that latest desired version. Older fleet-known versions remain visible in catalog history, but a replica that advances past them does not download their artifacts. After the desired version starts successfully, Gestalt removes superseded registry-installed package directories for that app.

`app_rollouts` and `app_instance_materializations` are not boot inputs. Bootstrap reads only deploy config and the fleet-known version projection. In particular, stale or missing convergence rows must not prevent a known registry app from starting.

### Bootstrap before polling

Bootstrap finishes its registry-app startup attempts before the catalog poller begins:

1. Bootstrap materializes and starts the desired fleet-known version without updating `app_rollouts` or `app_instance_materializations`; the poller owns those rollout-accounting writes.
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

### Rollout admission and completion

Only one rollout per app may be active across the fleet. `POST …/add` and `POST …/upgrade` hold the app-scoped install lock while they reject an existing `enrolling` or `restarting` rollout, validate the candidate, create the new rollout, and append its change request. Different apps may roll out concurrently.

Replica membership is discovered rather than configured:

1. The rollout remains `enrolling` for a bounded window of at least two poll intervals.
2. Replicas join by writing `acknowledged_at` before `enrollment_ends_at`.
3. After enrollment, the cohort is frozen and the rollout becomes `restarting`.
4. The rollout completes when every cohort member records `restarted_at`.
5. The rollout fails if an acknowledged replica does not restart before the deadline.

Replicas that do not acknowledge before enrollment closes are not cohort members and do not block completion. A late replica still converges locally but does not reopen a terminal rollout.

Each pass:

1. Acknowledge each new `(app, version)` in `app_instance_materializations`.
2. Group pending versions by app and select one desired version with `LatestKnownVersion`.
3. Download and extract only the desired registry artifact to the canonical `{artifactsDir}/registry-installed/{app}/{desiredVersion}` path, recording `materialized_at` on that version's row while the provider is still running. Superseded pending rows remain without `materialized_at`. Re-validate the desired path on every pass; if the tree is missing or corrupt, re-download it before `StopApp`.
4. Stop and restart each restartable app once, recording `stopped_at` and `restarted_at` on every pending row. These timestamps mean the replica reconciled past those catalog changes; they do not mean every superseded version ran. `StartApp` receives the desired version and builds the provider from that materialized package instead of the deploy-time pin.
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
4. When `attempt_count` reaches the configured maximum, the poller stops retrying materialization and provider lifecycle work for that desired version on this replica. The limit does not apply to bootstrap: after a process restart, bootstrap still attempts the latest fleet-known version without consulting `attempt_count`. If bootstrap succeeds, the poller may record that observed convergence despite its retry limit because no additional materialization or provider lifecycle attempt is required. The poller may likewise record convergence for a non-restartable app.
5. A newly accepted desired version gets a new row and a fresh attempt count. Increasing the configured maximum also permits retry when the stored count is below the new value.
6. Updates to the local `active-version` marker are atomic. A failed replacement leaves the previous valid marker in place.
7. If Gestalt cannot determine or clean up the local provider state safely, it marks the process unhealthy and terminates so the process supervisor can restart it.

### Runtime behavior of configured app surfaces

A registry-only app does not have to expose a static UI, HTTP bindings, MCP, or any other particular surface. The following rules apply only to surfaces enabled for that app in the Gestalt YAML deploy configuration.

Gestalt constructs its HTTP server before it downloads or starts a registry package. Server construction must therefore succeed without a package manifest, operation catalog, static root, or running provider.

- **Static UI** — Gestalt creates the mount declared in YAML, but returns **503 Service Unavailable** until the provider registry, running-version map, and `active-version` marker agree on one version. It then serves the static bundle from that version's materialized package. If the version changes while a request is being resolved, Gestalt returns **503** rather than combining one version's UI with another version's provider. Theme files declared in YAML are resolved independently of the package.
- **HTTP bindings** — Gestalt mounts routes declared in YAML when it constructs the server, without requiring the package's operation catalog. Requests may be unavailable until the provider starts. A package cannot add routes to the already-constructed server, so every required HTTP binding must be declared in YAML.
- **MCP and operation invocation** — these resolve against the provider registry. They become available after `StartApp` registers the provider and unavailable when the provider is removed.

Runtime handlers determine availability from the local provider registry, running-version map, and `active-version` marker. They never use `app_instance_materializations`, whose rows record rollout progress rather than current runtime state.

## Runtime

Admin HTTP under `/admin/api/v1` on the same listener as the other admin API (for example `/admin/api/v1/runtime/providers`). In deployments that split public and management listeners, call the management base URL.

### How to invoke

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

Add the app to the fleet catalog (first known version):

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

List known installed versions (fleet-wide):

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

List/get install endpoints project known versions from `app_version_change_requests` only.

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/admin/api/v1/app-registries` | List registries from config |
| `GET` | `/admin/api/v1/app-registries/{registry}/apps/{app}/versions` | List published versions for one app |
| `POST` | `/admin/api/v1/app-registries/{registry}/apps/{app}/add` | Record the first fleet-known version for an app |
| `POST` | `/admin/api/v1/app-registries/{registry}/apps/{app}/upgrade` | Record a new fleet-known version when the app is already in the catalog |
| `GET` | `/admin/api/v1/app-installations` | List all **known versions** across apps |
| `GET` | `/admin/api/v1/app-installations/{app}` | List **known versions** for one app |

Planned read routes for rollout observability (step 14): see [Admin observability API](#admin-observability-api) and [admin.md](./admin.md).

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

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Registry key from `appRegistries` |
| `kind` | string | Registry backend (`gcs` today) |
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
|-----------|-------------|
| `registry` | Name of a configured registry (`appRegistries` key) |
| `app` | Published app name (same rules as `gestaltd app publish --app`) |

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
|-------|------|-------------|
| `registry` | string | Registry name from the path |
| `app` | string | App name from the path |
| `versions` | array | Version summaries, newest `publishedAt` first |
| `versions[].version` | string | Published version identifier |
| `versions[].metadata` | string | Relative path to the full published version JSON in the bucket |
| `versions[].platforms` | string array | Platforms published for this version (omitted when empty) |
| `versions[].publishedAt` | RFC 3339 timestamp | Publish time (UTC) |

When the app has no index or no versions yet, `versions` is `[]` (not `null`). A missing `apps/{app}/index.json` object is treated as an empty catalog.

`metadata` points at the immutable published version document described in [models.md](./models.md). These routes do not inline published version fields (artifacts, interface, dependencies).

1. `listAdminAppRegistryAppVersions` reads `{registry}` and `{app}` from the URL.
2. Validate `app` (`providerregistry.ValidateRepositoryName`). Look up `{registry}` in `s.appRegistries`; reject unknown registries and non-`gcs` kinds.
3. `RegistryReader.FetchAppIndex` — HTTP `GET` `apps/{app}/index.json` from the configured registry (live fetch on every request).
4. Respond `200` with `{ registry, app, versions }`.

No IndexedDB read or write. Lists **published** versions in the registry bucket, not fleet-installed versions from `app_version_change_requests`.

#### `POST /admin/api/v1/app-registries/{registry}/apps/{app}/add`

Record the **first** fleet-known version for an app. Use when `ListKnownVersionsByApp` is empty.

**Path parameters**

| Parameter | Description |
|-----------|-------------|
| `registry` | Name of a configured registry (`appRegistries` key) |
| `app` | Published app name |

**Request body**

```json
{
  "version": "0.0.0-snapshot.gabc123",
  "actor": "user:alice"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `version` | string | yes | Published version to add |
| `actor` | string | no | Actor recorded on catalog records |

**Response `200`** — same shape as upgrade below.

Returns **409 Conflict** when the app already has fleet-known versions (use `upgrade` instead).

#### `POST /admin/api/v1/app-registries/{registry}/apps/{app}/upgrade`

Record a new fleet-known version when the app is already in the catalog.

**Path parameters**

| Parameter | Description |
|-----------|-------------|
| `registry` | Name of a configured registry (`appRegistries` key) |
| `app` | Published app name |

**Request body**

```json
{
  "version": "0.0.0-snapshot.gdef456",
  "actor": "user:alice"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `version` | string | yes | Published version to upgrade to |
| `actor` | string | no | Actor recorded on catalog records |

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
|-------|------|-------------|
| `registry` | string | Registry name from the path |
| `app` | string | App name from the path |
| `installation` | object | Known version after a successful add or upgrade (from the `change request` record) |

Re-installing a version that is already in the catalog returns **400 Bad Request** (`app version is already installed`). Returns **400** when the app has no fleet-known versions yet (use `add` instead).

Synchronous on the handling instance. The HTTP response is sent after the catalog write finishes or fails.

1. Handler reads `{registry}` and `{app}` from the URL and `{ version, actor }` from the JSON body.
2. Validate path params and look up `{registry}` in `s.appRegistries`.
3. `Installer.Add` or `Installer.Upgrade` on the handling instance:
   1. Claim a fleet install lock in `app_version_install_locks` for `(app, version)` (`409` if another holder holds a non-expired lock).
   2. **`add`** — reject when `ListKnownVersionsByApp` is non-empty (`409`). **`upgrade`** — reject when the projection is empty (`400`).
   3. If `(app, version)` is already known in `app_version_change_requests`, return `400`.
   4. `RegistryReader.FetchEntry` — HTTP `GET` the published version document from the configured registry (validate the version exists; **no artifact download**).
   5. **`InstallValidator.Validate`** — reject incompatible or unsatisfied candidates (**400**); see [validation.md](./validation.md).
   6. `Rollouts.Create` — start fleet rollout (`409` when another rollout is active).
   7. Append `change request` to `app_version_change_requests`. Set `from_version` server-side: `registry:first-install` on `add`, `LatestKnownVersion` on `upgrade`. Callers never send `from_version`. Mark rollout `failed` if append fails.
   8. Release the install lock (always, via defer). On failure before the change request is appended, return an HTTP error; no change request is written.
4. Respond `200` with `{ registry, app, installation }`.

Per-replica convergence via the background catalog controller (see Polling). IndexedDB write on the handling instance for add/upgrade.

#### `GET /admin/api/v1/app-installations`

Returns all **known versions** projected from `change request` catalog records. See [indexeddb.md](./indexeddb.md).

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

When no versions are known yet, the response is `[]` (not `null`).

1. `listAdminAppInstallations` requires `AppVersionChangeRequests` on the server; otherwise respond `503`.
2. `ChangeRequests.ListAllKnownVersions` — read `app_version_change_requests` and project known `(app, to_version)` pairs.
3. Map each projection to `{ app, version, sourceRef, registry, providerReleaseUrl, artifactChecksums, installedBy, installedAt, updatedAt }`.
4. Respond `200` with the JSON array (empty if nothing installed fleet-wide).

IndexedDB read only. No GCS fetch.

#### `GET /admin/api/v1/app-installations/{app}`

Returns **known versions** for one app.

**Response `200`** — array of objects with the same shape as one element of the fleet list response above.

**Response `404`** when the app has no `change request` records yet.

1. `getAdminAppInstallation` reads `{app}` from the URL and validates the app name.
2. Requires `AppVersionChangeRequests` on the server; otherwise respond `503`.
3. `ChangeRequests.ListKnownVersionsByApp` — read `app_version_change_requests` and project known versions for that app.
4. If no known versions, respond `404`.
5. Otherwise map results to the same installation object shape and respond `200` with a JSON array.

IndexedDB read only. No GCS fetch.

IndexedDB read only. No GCS fetch.

### Admin observability API

**Status:** planned. Full shapes and UI wireframes: [admin.md](./admin.md).

These routes expose IndexedDB rollout state that the catalog poller already writes. They do not change install or convergence behavior.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/admin/api/v1/registry-apps` | Registry-managed apps from deploy config, merged with desired version and rollout summary |
| `GET` | `/admin/api/v1/registry-apps/{app}` | One app: known versions, rollout, optional latest published registry version |
| `GET` | `/admin/api/v1/app-rollouts` | List active and recent terminal rollouts |
| `GET` | `/admin/api/v1/app-rollouts/{app}/materializations` | Per-replica convergence rows for one `(app, version)` |

**Phase 2 (optional):** `GET /admin/api/v1/app-rollouts/{app}/runtime` — per-replica **observed** running version when heartbeat rows exist. Until then, materialization `restarted_at` is the best fleet-wide signal; it records rollout accounting, not live process state. See [admin.md](./admin.md#runtime-heartbeats-phase-2).

The embedded `/admin` UI gains an **App Registry** section that consumes these endpoints. Today `/admin` only shows Prometheus metrics.

### Errors

Errors use the standard gestaltd admin API error envelope (`error` field).

| Status | When |
|--------|------|
| `400` | Missing path param; invalid `app` name; invalid JSON body; missing `version`; unsupported registry `kind` (non-`gcs`); app version already installed; `upgrade` called when the app has no fleet-known versions; **install-time validation failed** |
| `404` | Unknown `registry` name; published version not found; no known versions for `{app}`; no `appRegistries` configured |
| `409` | Another instance is already installing this `(app, version)` (install lock held and not expired); `add` called when the app already has fleet-known versions |
| `502` | Published version fetch failed; registry fetch failed during install validation; registry named in a fleet-known installation is missing from gestaltd config; failed to append `change request` record; upstream fetch of `apps/{app}/index.json` failed (network, non-2xx other than 404, invalid JSON) |
| `500` | Registry `publicUrl` could not be derived from config; unexpected catalog projection failure |
| `503` | Version catalog service or installer not configured |

Example:

```json
{
  "error": "app registry not found"
}
```
