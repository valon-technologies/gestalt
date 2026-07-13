# App Registry Admin API

Reference for the admin HTTP API under `/admin/api/v1` for configured app registries:

- [gestalt#2716](https://github.com/valon-technologies/gestalt/pull/2716) — plan step 4: list configured registry versions
- [gestalt#2730](https://github.com/valon-technologies/gestalt/pull/2730) — plan step 6: install a registry app via catalog + local materialization

Gestalt exposes configured `appRegistries` through list routes that read each registry's public HTTPS root and fetch `apps/{app}/index.json`. Install is catalog-only; every replica materializes via a background controller — see [lifecycle.md](./lifecycle.md). List/get install endpoints expose **projected known versions** from `version_added` records. Full published version JSON (`apps/{app}/versions/{version}.json`) is returned only to the install flow, not inlined by the list routes.

Related docs:

- [config.md](./config.md) — `appRegistries` deploy reader config
- [models.md](./models.md) — index and published version JSON stored in GCS
- [service.md](./service.md) — Go publish/read helpers behind the registry bucket
- [plan.md](./plan.md) — install flow and follow-up steps

Implementation:

- Registry list handlers — `gestaltd/internal/server/handlers_admin_app_registry.go`
- Install handlers — `gestaltd/internal/server/handlers_admin_app_install.go`
- Registry fetcher — `gestaltd/internal/appregistry/reader.go`
- Registry installer — `gestaltd/internal/appregistry/installer.go`
- Catalog projections — `gestaltd/internal/coredata/app_version_catalog_projection.go`

---

## How to invoke

Call a running `gestaltd` instance. Routes live under `/admin/api/v1` on the same listener as the other admin API (for example `/admin/api/v1/runtime/providers`).

Set a base URL for the examples below:

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

Install one published version on the handling instance:

```bash
curl -sS -X POST "$GESTALTD_URL/admin/api/v1/app-registries/toolshed/apps/g-issues/install" \
  -H 'Content-Type: application/json' \
  -d '{"version":"0.0.0-snapshot.gabc123","actor":"user:alice"}' | jq .
```

List known installed versions (fleet-wide):

```bash
curl -sS "$GESTALTD_URL/admin/api/v1/app-installations" | jq .
```

`toolshed` is the registry name from deploy `config.yaml` (`appRegistries.toolshed`). `g-issues` is the published app name.

---

## Prerequisites

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

- IndexedDB `app_version_catalog` service configured on the server (`AppVersionCatalogService`)
- IndexedDB `app_version_install_locks` service configured on the server (`AppVersionInstallLockService`)
- A configured artifacts directory (`cfg.Server.ArtifactsDir`, propagated from the CLI `--artifacts-dir` at load time)

The `app_installations` IndexedDB store was removed. List/get install endpoints project known versions from `app_version_catalog` only.

---

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/admin/api/v1/app-registries` | List registries from config |
| `GET` | `/admin/api/v1/app-registries/{registry}/apps/{app}/versions` | List published versions for one app |
| `POST` | `/admin/api/v1/app-registries/{registry}/apps/{app}/install` | Validate version and record known version in catalog (catalog-only; local materialization via background controller) |
| `GET` | `/admin/api/v1/app-installations` | List all **known versions** across apps |
| `GET` | `/admin/api/v1/app-installations/{app}` | List **known versions** for one app |

List routes are read-only (`GET` only). Install uses `POST` on a separate route group with a longer request timeout (10 minutes).

### `GET /admin/api/v1/app-registries`

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

**Runtime behavior**

1. Read the in-memory `appRegistries` map.
2. For each configured registry, derive `kind` and `publicUrl`.
3. Return the sorted list as JSON.

No network call to GCS.

### `GET /admin/api/v1/app-registries/{registry}/apps/{app}/versions`

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
    },
    {
      "version": "0.0.0-snapshot.gdef456",
      "metadata": "apps/g-issues/versions/0.0.0-snapshot.gdef456.json",
      "platforms": ["linux/amd64"],
      "publishedAt": "2026-07-09T12:00:00Z"
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

**Runtime behavior**

1. Resolve `{registry}` against the in-memory map; return `404` if unknown.
2. Validate `{app}` with `ValidateRepositoryName`; return `400` if invalid.
3. Require `kind: gcs` and compute `publicUrl` from the registry config.
4. `RegistryReader.FetchAppIndex` performs `GET {publicUrl}/apps/{app}/index.json`.
5. `VersionsFromIndex` maps the fetched index to `VersionSummary` rows and sorts by `publishedAt` descending (ties broken by version string, descending).
6. Return the in-memory summary as JSON. Nothing is cached or persisted after the response.

### `POST /admin/api/v1/app-registries/{registry}/apps/{app}/install`

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
| `version` | string | yes | Published version to install |
| `actor` | string | no | Actor recorded on catalog records |

**Response `200`**

```json
{
  "registry": "toolshed",
  "app": "g-issues",
  "materializedPath": "/var/gestalt/artifacts/registry-installed/g-issues/0.0.0-snapshot.gabc123",
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
| `materializedPath` | string | Omitted — local materialization is per-replica via the background controller (see [lifecycle.md](./lifecycle.md)) |
| `installation` | object | Known version after a successful install (from the `version_added` record, or existing catalog entry on re-install) |

Re-installing a version that is already in the catalog returns **400 Bad Request** (`app version is already installed`).

**Runtime behavior**

Synchronous on the handling instance. The HTTP response is sent after the catalog write finishes or fails.

1. **Claim fleet install lock** — insert a row into `app_version_install_locks` for `(app, version)` with a unique holder id for this install attempt. If another install attempt already holds a non-expired lock for this pair, return **409 Conflict** immediately. Locks expire after 15 minutes so crashed instances do not block installs forever; expired locks can be taken over.
2. **Reject known versions** — if `(app, version)` is already in `app_version_catalog`, return **400 Bad Request** without fetching the registry.
3. Fetch the published version document (`versions/{version}.json`) to validate the install candidate. **No artifact download** on the handling instance.
4. Append `version_added` to `app_version_catalog`. On failure before step 4, append `install_failed` for audit and return an error.
5. **Release install lock** — delete the lock row when this instance still holds it (always runs on success or failure via defer).

Every replica (including the handler) materializes locally on the background catalog controller (startup + 1 minute tick). See [lifecycle.md](./lifecycle.md).

Different `(app, version)` pairs use independent locks and can install in parallel across the fleet. The same version cannot be installed concurrently on multiple instances.

**Does not start any process.** The app is not bound into the runtime provider graph.

### `GET /admin/api/v1/app-installations`

Returns all **known versions** projected from `version_added` catalog records. See [indexeddb.md](./indexeddb.md).

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

**Runtime behavior**

`ListAllKnownVersions` projects one entry per `(app, version)` from `app_version_catalog`. No registry network calls.

### `GET /admin/api/v1/app-installations/{app}`

Returns **known versions** for one app.

**Response `200`** — array of objects with the same shape as one element of the fleet list response above.

**Response `404`** when the app has no `version_added` records yet.

**Runtime behavior**

`ListKnownVersionsByApp` projects known versions for `{app}`. No registry network calls.

---

## Errors

Errors use the standard gestaltd admin API error envelope (`error` field).

| Status | When |
|--------|------|
| `400` | Missing path param; invalid `app` name; invalid JSON body; missing `version`; unsupported registry `kind` (non-`gcs`); app version already installed |
| `404` | Unknown `registry` name; published version not found; no known versions for `{app}`; no `appRegistries` configured |
| `409` | Another instance is already installing this `(app, version)` (install lock held and not expired) |
| `502` | Published version fetch failed; failed to append `version_added` record; upstream fetch of `apps/{app}/index.json` failed (network, non-2xx other than 404, invalid JSON) |
| `500` | Artifacts directory not configured; registry `publicUrl` could not be derived from config; unexpected catalog projection failure |
| `503` | Version catalog service or installer not configured |

Example:

```json
{
  "error": "app registry not found"
}
```

---

## Startup behavior

When `gestaltd serve` starts:

1. Load and validate deploy config, including `appRegistries` (`validateAppRegistries` checks registry names, `kind: gcs`, and that `publicUrl` can be derived from `gcs.bucket`).
2. Pass the parsed `appRegistries` map through bootstrap into `server.New`, which clones it onto the running `Server` as an in-memory map.

Bootstrap does not fetch registry indexes or prefetch version metadata. The source of truth for registry configuration remains the config file on disk; gestaltd does not persist `appRegistries` elsewhere.

Install is HTTP-triggered for fleet declaration. On startup, gestaltd does not bind installed apps into the provider graph. Start a background catalog controller on every replica — one `ConvergeOnce` at startup, then every 1 minute — to read `app_version_catalog` and materialize missing versions locally. See [lifecycle.md](./lifecycle.md).

The admin UI and these routes share the `/admin/api/v1` prefix. In deployments that split public and management listeners, call the management base URL. Protect that listener according to your environment (private networking, reverse proxy, or gestaltd admin authorization policy for the admin UI surface).
