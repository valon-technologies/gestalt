# App Registry Replica Lifecycle

How each `gestaltd` replica observes fleet install state, materializes app artifacts locally, and (eventually) serves dynamic apps. Complements [plan.md](./plan.md) step 7 and the multi-instance convergence section.

Related docs:

- [plan.md](./plan.md) — install flow, catalog model, rollout steps
- [indexeddb.md](./indexeddb.md) — `app_version_catalog`, install locks, planned `app_instance_materializations`
- [api.md](./api.md) — admin install HTTP API
- [tests.md](./tests.md) — convergence unit tests

## `gestaltd` startup

`coredata.NewWithOptions` — idempotently create host stores, including `app_version_catalog` and `app_version_install_locks`. Bootstrap does not write catalog records; the store starts empty until an install.

## Runtime

Admin HTTP under `/admin/api/v1`. See [api.md](./api.md) for request/response shapes.

### `GET /admin/api/v1/app-registries`

1. `listAdminAppRegistries` reads `s.appRegistries` (a clone of `appRegistries` from deploy config, set at `server.New`).
2. If `appRegistries` is empty or unset, respond `200` with `[]`.
3. Otherwise sort registry names and, for each entry, build `{ name, kind, publicUrl }`.
4. Respond `200` with the JSON array.

No GCS fetch. No IndexedDB read or write. This reflects **configured** registries only, not published or installed versions.

### `GET /admin/api/v1/app-registries/{registry}/apps/{app}/versions`

1. `listAdminAppRegistryAppVersions` reads `{registry}` and `{app}` from the URL.
2. Validate `app` (`providerregistry.ValidateRepositoryName`). Look up `{registry}` in `s.appRegistries`; reject unknown registries and non-`gcs` kinds.
3. `RegistryReader.FetchAppIndex` — HTTP `GET` `apps/{app}/index.json` from the configured registry (live fetch on every request).
4. Respond `200` with `{ registry, app, versions }`.

No IndexedDB read or write. Lists **published** versions in the registry bucket, not fleet-installed versions from `app_version_catalog`.

### `POST /admin/api/v1/app-registries/{registry}/apps/{app}/install`

1. `installAdminAppRegistryApp` reads `{registry}` and `{app}` from the URL and `{ version, actor }` from the JSON body.
2. Validate path params and look up `{registry}` in `s.appRegistries`.
3. `Installer.Install` on the **handling instance only** (synchronous; response waits for completion):
   1. Claim a fleet install lock in `app_version_install_locks` for `(app, version)` (`409` if another holder holds a non-expired lock).
   2. If `(app, version)` is already known in `app_version_catalog`, return `400` without fetching the registry.
   3. `RegistryReader.FetchEntry` — HTTP `GET` the published version document from the configured registry.
   4. Download the platform artifact for this instance, verify SHA256, and extract to `{artifactsDir}/registry-installed/{app}/{version}/`.
   5. Append `version_added` to `app_version_catalog` with the install contract in record metadata.
   6. Release the install lock (always, via defer). On failure before step 5, append `install_failed` for audit.
4. Respond `200` with `{ registry, app, installation, materializedPath }`.

GCS fetch and IndexedDB read/write on the handling instance only. Does **not** start a process or bind the app into the runtime provider graph. Other replicas are unchanged until they install or converge (step 7).

### `GET /admin/api/v1/app-installations`

1. `listAdminAppInstallations` requires `AppVersionCatalog` on the server; otherwise respond `503`.
2. `Catalog.ListAllKnownVersions` — read `app_version_catalog` and project known `(app, version)` pairs from `version_added` records.
3. Map each projection to `{ app, version, sourceRef, registry, providerReleaseUrl, artifactChecksums, installedBy, installedAt, updatedAt }`.
4. Respond `200` with the JSON array (empty if nothing installed fleet-wide).

IndexedDB read only. No GCS fetch. Lists **fleet-known installed** versions, not everything published in a registry bucket.

### `GET /admin/api/v1/app-installations/{app}`

1. `getAdminAppInstallation` reads `{app}` from the URL and validates the app name.
2. Requires `AppVersionCatalog` on the server; otherwise respond `503`.
3. `Catalog.ListKnownVersionsByApp` — read `app_version_catalog` and project known versions for that app.
4. If no known versions, respond `404`.
5. Otherwise map results to the same installation object shape and respond `200` with a JSON array.

IndexedDB read only. No GCS fetch. One app may have multiple known versions in the array.
