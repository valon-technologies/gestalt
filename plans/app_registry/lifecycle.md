# App Registry Replica Lifecycle

How each `gestaltd` replica observes fleet install state, materializes app artifacts locally, and serves registry-installed app versions.

Install is change-request-only; per-replica convergence (ack → download → restart → mount) is planned via polling below. List/get install endpoints expose **projected known versions** from change requests.

Related docs:

- [plan.md](./plan.md) — install flow, catalog model, rollout steps
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
- **Converged** — the poller recorded `restarted_at` for the replica and version. This is rollout accounting, not proof by itself that a provider is currently running.

Bootstrap and the poller must both use `LatestKnownVersion` to select the same desired installation. See [indexeddb.md](./indexeddb.md#accepted-changes-and-projections) for the version-ordering rule.

`app_rollouts` and `app_instance_materializations` are not boot inputs. Bootstrap reads only deploy config and the fleet-known version projection. In particular, stale or missing convergence rows must not prevent a known registry app from starting.

### Bootstrap before polling

Bootstrap finishes its registry-app startup attempts before the catalog poller begins:

1. Bootstrap materializes and starts the desired fleet-known version without updating `app_rollouts` or `app_instance_materializations`; the poller owns those rollout-accounting writes.
2. After the exact package is validated and its provider starts successfully, bootstrap records the app and version in this process's running-version map and local `active-version` marker. Static and runtime handlers may then serve that version. If provider startup fails, neither local state may identify the requested version as running.
3. After bootstrap has attempted every registry-only app, it marks startup-provider initialization complete. An individual registry app failure does not prevent this transition or block core server startup.
4. The poller then starts and runs its first reconciliation pass immediately.
5. If the desired version is already running, the poller sets `restarted_at` on each pending `app_instance_materializations` row without stopping and restarting the app.
6. To determine whether a provider is currently running and which version it serves, the poller uses this process's provider registry and running-version map. `app_instance_materializations` only says which rollout-progress writes were recorded previously. Therefore, a historical `stopped_at` does not prove that the provider is still absent. If local runtime state shows a version other than the desired version, the poller stops that provider, starts the desired version, and then updates the rollout-progress row.
7. An empty fleet-known projection leaves the app stopped and clears stale local activation state.

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
2. Group pending versions by app.
3. Download and extract each pending registry artifact to the canonical `{artifactsDir}/registry-installed/{app}/{version}` path, recording `materialized_at` while the provider is still running. Re-validate that path on every pass; if the tree is missing or corrupt, re-download before `StopApp`.
4. Stop and restart each restartable app once, recording `stopped_at` and `restarted_at` on every pending row. `StartApp` receives the driver fleet version and, when a complete registry install exists on disk, builds the provider from that materialized package instead of the deploy-time pin.
5. Mark non-restartable apps converged without a local stop/start.

A provider is **restartable** when this replica builds it locally from the configured pin: `server.remote` is unset or the provider has `local: true`, and the provider is not running in dev mode. Remote and dev-mode providers are non-restartable.

`StopApp` holds the per-app lifecycle lease until `StartApp` completes, preventing overlapping builds or replacements. Materialization of the same `(app, version)` is serialized; unrelated app versions may materialize concurrently.

`StartApp(app, version)` is strict for registry-only apps:

1. The materialized package must exist and pass package validation. Registry-only apps never fall back to an unresolved deploy-time provider entry.
2. If a provider is already registered, idempotent success is allowed only when its recorded running version equals `version`. Empty or different running-version state requires a real stop/start and must not be overwritten optimistically.
3. Provider build, registration, and activation act as one observable transition. On failure, the provider must not remain registered as the requested version and static/runtime surfaces must not advertise it.
4. Stopping or removing a provider clears its running-version and active-static state, including the already-absent-provider path.

Failure and retry behavior:

1. If `Close` fails, the provider stays absent because it may be partially closed. Later polls retain the error; shutdown releases the lease.
2. If writing `stopped_at` fails after stop, the next poll retries the write without stopping the absent provider again.
3. If writing `restarted_at` fails after start, the next poll retries the write without rebuilding the running provider.
4. A version discovered while the app is stopped joins the current cycle and inherits its original `stopped_at`.
5. Replacing local active-version state preserves the prior valid value if the replacement cannot be committed. Temporary files and backups may be cleaned only after replacement or restoration succeeds.

### Runtime surfaces for registry-only app slots

The HTTP server is constructed before registry packages are available. Registry-only app slots therefore must not require a deploy-time resolved manifest, static root, provider catalog, or running provider merely to construct the server.

- **Static UI** — create the configured mount lazily. A request is served only when the app has a running version and the corresponding materialized package resolves a static bundle. The handler must reject a version change observed during resolution rather than mixing frontend and provider generations. Not-yet-running, stopping, or changing versions return **503 Service Unavailable**, not a permanent **404**. Deploy-config theme files are resolved independently of the package and remain available to the mount.
- **HTTP bindings** — bindings declared in deploy config are mounted before the provider starts and do not require startup-time provider-catalog validation. Invocation may return unavailable until the provider is running. Package-only bindings cannot mutate the already-built HTTP router; registry-only apps that need mounted HTTP routes must declare those bindings in deploy config.
- **MCP and generic operation invocation** — resolve against the currently registered provider. They become available after successful `StartApp` and unavailable immediately when the provider is removed.

No surface may infer the running version from `app_instance_materializations`. Static serving uses local active/running state tied to provider lifecycle; convergence rows remain accounting records.

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
   5. Append `change request` to `app_version_change_requests`. Set `from_version` server-side: `registry:first-install` on `add`, `LatestKnownVersion` on `upgrade`. Callers never send `from_version`.
   6. Release the install lock (always, via defer). On failure before step 5, return an HTTP error; no change request is written.
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

### Errors

Errors use the standard gestaltd admin API error envelope (`error` field).

| Status | When |
|--------|------|
| `400` | Missing path param; invalid `app` name; invalid JSON body; missing `version`; unsupported registry `kind` (non-`gcs`); app version already installed; `upgrade` called when the app has no fleet-known versions |
| `404` | Unknown `registry` name; published version not found; no known versions for `{app}`; no `appRegistries` configured |
| `409` | Another instance is already installing this `(app, version)` (install lock held and not expired); `add` called when the app already has fleet-known versions |
| `502` | Published version fetch failed; failed to append `change request` record; upstream fetch of `apps/{app}/index.json` failed (network, non-2xx other than 404, invalid JSON) |
| `500` | Registry `publicUrl` could not be derived from config; unexpected catalog projection failure |
| `503` | Version catalog service or installer not configured |

Example:

```json
{
  "error": "app registry not found"
}
```
