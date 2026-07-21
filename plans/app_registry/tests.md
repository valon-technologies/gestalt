# App Registry Tests

Reference for behavioral tests in the app registry plan.

Related docs:

- [plan.md](./plan.md) — implementation path and goals
- [models.md](./models.md) — JSON documents exercised by publish and install
- [service.md](./service.md) — Go API behind the CLI
- [config.md](./config.md) — `appRegistries` deploy reader config
- [lifecycle.md](./lifecycle.md) — replica startup, background controller, admin HTTP API

---

## Overview

| Package | File | Tests | Layer | PR |
|---------|------|-------|-------|-----|
| `internal/daemon/e2e/appregistry` | `appregistry_test.go` | 1 | E2E (CLI) | [gestalt#2709](https://github.com/valon-technologies/gestalt/pull/2709) |
| `internal/server` | `handlers_admin_app_install_test.go` | 3 | HTTP integration | [gestalt#2730](https://github.com/valon-technologies/gestalt/pull/2730) |
| `internal/appregistry` | `poller_test.go`, `poller_materialize_test.go` | 21 | Unit | [gestalt#2812](https://github.com/valon-technologies/gestalt/pull/2812) + steps 10–11 |
| `internal/appregistry` | `materializer_test.go` | 5 | Unit | step 10 |
| `internal/appregistry` | `mount_test.go` | 4 | Unit | step 11 |
| `internal/coredata` | `app_rollouts_test.go`, `app_version_install_locks_test.go` | 3 | Unit | [gestalt#2812](https://github.com/valon-technologies/gestalt/pull/2812) |
| `internal/bootstrap` | `app_provider_restart_test.go`, `app_provider_restart_mount_test.go`, `app_provider_lifecycle_test.go` | 8 | Unit/integration | [gestalt#2812](https://github.com/valon-technologies/gestalt/pull/2812) + step 11 |
| `internal/config`, `internal/operator`, `internal/appregistry`, `internal/bootstrap` | registry-only source tests | — | Unit/integration | — |

Test fixture for install HTTP tests: `internal/appregistry/registrytest/fixture.go`

---

## Publish dry-run E2E

Added in [gestalt#2709](https://github.com/valon-technologies/gestalt/pull/2709) (GCS app registry + `gestaltd app publish`).

Run:

```bash
cd gestaltd
go test ./internal/daemon/e2e/appregistry -count=1
```

This is a **behavioral** test: the compiled `gestaltd` binary runs as a subprocess through `provider package` → `app publish --dry-run`. No real GCS uploads.

`--dry-run` always prints a JSON plan (`gestaltd.app.publish.plan.v1`) to stdout.

Supporting helpers live in the same package (`helpers_test.go`, `main_test.go`). `main_test.go` builds only the `gestaltd` binary — not the extra provider fixture binaries used by the broader `e2e` suite.

### `TestRun_AppPublishDryRunPlansVersionedRegistryUploads`

Flow:

1. Create a temp app fixture at `apps/release-test/` (`newAppRegistryPublishFixture`)
2. Initialize a git repo (`initProviderPublishGitRepo`)
3. Run `gestaltd provider package --version … --output dist/`
4. Run `gestaltd app publish --bucket gs://gestalt-app-registry --app release-test --version … --ref … --dist-dir dist/ --dry-run`
5. Decode and assert the JSON plan

Expected plan fields:

- `schema: gestaltd.app.publish.plan.v1`
- `appName: release-test`
- `version` matches the packaged snapshot version
- `entryObject.publicUrl` → `https://storage.googleapis.com/gestalt-app-registry/apps/release-test/versions/{version}.json`
- `artifactObjects[0].storageUrl` → `gs://gestalt-app-registry/apps/release-test/artifacts/{version}/gestalt-app-release-test_v{version}_{os}_{arch}.tar.gz`
- `indexObject.storageUrl` → `gs://gestalt-app-registry/apps/release-test/index.json`

---

## Add and upgrade HTTP integration

Added in [gestalt#2730](https://github.com/valon-technologies/gestalt/pull/2730) (`POST …/install`). Registry-only apps use separate `add` and `upgrade` routes — see [lifecycle.md](./lifecycle.md#post-adminapiv1app-registriesregistryappsappadd).

Run:

```bash
cd gestaltd
go test ./internal/server/... -run TestAdminAppRegistry -count=1
```

All subtests use `newTestServer` (`httptest.NewServer` on localhost), `testutil.NewStubServices` (in-memory IndexedDB stub), and `registrytest.NewInstallFixture` (local mock GCS). **No production instances are contacted.**

### `handlers_admin_app_install_test.go`

- **`TestAdminAppRegistryAdd/adds_and_lists_known_version`** — `POST …/add` returns 200 with known version; `GET …/app-installations` lists one known version.

- **`TestAdminAppRegistryAdd/rejects_when_catalog_not_empty`** — `POST …/add` returns **409** when the app already has fleet-known versions.

- **`TestAdminAppRegistryUpgrade/upgrades_known_version`** — `POST …/upgrade` returns 200; change request `from_version` matches the previous fleet-known version.

- **`TestAdminAppRegistryUpgrade/rejects_when_catalog_empty`** — `POST …/upgrade` returns **400** when the app has no fleet-known versions.

- **`TestAdminAppRegistryAdd/missing_version_returns_not_found`** — Unknown version returns HTTP 404.

- **`TestAdminAppRegistryUpgrade/already_installed_returns_bad_request`** — Re-installing a known `to_version` returns **400**.

- **`TestAdminAppRegistryAdd/get_versions_by_app`** — `GET …/app-installations/{app}` returns an array of known versions after a successful add.

---

## [PLANNED] Multi-replica materialization ack E2E

End-to-end test for fleet install convergence across replicas. Replaces isolated catalog poller unit tests.

**Goal:** After a new version is installed fleet-wide, every running replica acknowledges the `(app, version)` pair in `app_instance_materializations`.

**Flow:**

1. Start multiple `gestaltd` replicas against shared IndexedDB (or a test harness that simulates distinct `instance_id` values with the catalog poller enabled).
2. `POST /admin/api/v1/app-registries/{registry}/apps/{app}/add` or `…/upgrade` with a new version.
3. Assert `app_version_change_requests` contains the change request.
4. Poll each replica's `app_instance_materializations` (or a future admin list endpoint) until every replica has an ack row for `(app, version)`.
5. Assert ack timestamps are recent and `instance_id` values are distinct per replica.

**Not in scope for the first cut:** end-to-end verification that every replica serves the newly mounted binary after restart.

---

## Registry mount tests

Run:

```bash
cd gestaltd
go test ./internal/appregistry -run TestResolveInstalled -count=1
go test ./internal/bootstrap -run TestAppProviderRestarterStartApp -count=1
```

### `mount_test.go`

- **`TestResolveInstalledApp_returns_isolated_provider_entry`** — resolves command, manifest, and executable paths from a materialized registry package without mutating the deploy-time entry.
- **`TestResolveInstalledAppIfPresent_uses_deploy_entry_when_install_missing`** — legacy restart-only rows keep the deploy-time pin when no local install exists.
- **`TestResolveInstalledAppIfPresent_rejects_incomplete_existing_install`** — rejects manifest-only partial trees.
- **`TestResolveInstalledApp_rejects_non_app_manifest`** — rejects packages whose manifest kind is not `app`.

### `app_provider_restart_mount_test.go`

- **`TestAppProviderRestarterStartAppDoesNotResolveWhenProviderRunning`** — idempotent `StartApp` does not resolve a registry install while the provider is already registered.
- **`TestAppProviderRestarterStartAppMountsRegistryInstalledVersion`** — catalog restart rebuilds from the registry-materialized executable when the provider is absent.

### `poller_materialize_test.go`

- **`TestCatalogPollerStartAppPassesDriverVersion`** — the catalog poller passes the desired version selected by `LatestKnownVersion` to `StartApp`.

---

## Artifact materialization tests

Run:

```bash
cd gestaltd
go test ./internal/appregistry -run 'TestMaterializer|TestCatalogPollerMaterializesBeforeStop' -count=1
```

### `materializer_test.go`

- **`TestMaterializer_downloads_and_extracts_artifact`** — downloads a registry archive and extracts `manifest.yaml` under `{artifactsDir}/registry-installed/{app}/{version}`.
- **`TestMaterializer_skips_when_already_materialized`** — idempotent when a complete install already exists on disk.
- **`TestMaterializer_retries_after_partial_install`** — removes manifest-only partial trees and re-downloads.
- **`TestMaterializer_retries_when_manifest_version_mismatches`** — re-downloads when the on-disk manifest version does not match the pending fleet version.
- **`TestMaterializer_rejects_digest_mismatch`** — rejects archives whose digest does not match the registry entry.

### `poller_materialize_test.go`

- **`TestCatalogPollerMaterializesBeforeStop`** — records `materialized_at` and creates the canonical on-disk artifact before `StopApp`, including while `RestartReady` is still open.
- **`TestCatalogPollerSkipsMaterializationForLegacyNonRegistryVersion`** — allows pre-registry catalog rows to retain restart-only convergence when a materializer is configured.
- **`TestCatalogPollerRematerializesWhenArtifactMissing`** — re-downloads when IndexedDB shows materialization complete but the on-disk tree was removed.

---

## Catalog restart tests

Run:

```bash
cd gestaltd
go test ./internal/appregistry -run TestCatalogPoller -count=1
go test ./internal/bootstrap -run 'TestAppProvider(Restarter|Lifecycle)' -count=1
```

### `poller_test.go`

- **`TestCatalogPollerReconcileOnceAcknowledgesAndRestarts`** — with the delay explicitly disabled, one reconcile pass acks, stops, and starts the configured app.
- **`TestCatalogPollerReconcileOnceWaitsForRestartDelay`** — an unset `RestartDelay` uses the one-minute default and defers start until `stopped_at + RestartDelay`.
- **`TestCatalogPollerReconcileOncePreservesDelayAfterStoppedAtWriteFailure`** — retrying a failed `stopped_at` write preserves the original stop time without stopping again.
- **`TestCatalogPollerReconcileOncePropagatesRestartErrors`** — start failures leave `restarted_at` unset after `stopped_at` was recorded.
- **`TestCatalogPollerReconcileOnceRestartsOnceForMultipleVersions`** — multiple unrestarted fleet versions for one app trigger one stop/start cycle and mark every pending row restarted.
- **`TestCatalogPollerReconcileOnceRetriesStartAfterRecordedStop`** — a later pass resumes at `StartApp` when `stopped_at` is already persisted.
- **`TestCatalogPollerReconcileOnceDefersRestartUntilProvidersReady`** — ack proceeds while `RestartReady` is open; stop/start wait until startup providers load.
- **`TestCatalogPollerReconcileOnceDoesNotResetRestartDelayForNewVersion`** — a newly fleet-known version during the restart delay does not stop again or push back `StartApp`.
- **`TestCatalogPollerReconcileOnceDoesNotConvergeWithoutAppRestarter`** — without `AppRestarter`, ack runs but restart timestamps stay unset.
- **`TestCatalogPollerReconcileOnceMarksNonLocalAppsConverged`** — non-local apps are acked and marked converged without stop/start.
- **`TestCatalogPollerReconcileOnceDoesNotConvergeWhenRestartModeFails`** — an unconfigured app is not mistaken for a non-local app and remains pending.
- **`TestCatalogPollerReconcileOnceReportsEveryFailingApp`** — errors from multiple apps are joined instead of hiding all but one.

### `app_provider_restart_test.go`

- **`TestAppProviderRestarterRestartable`** — only stable, locally managed providers are catalog-restartable; remote and dev-active providers converge without a local restart.
- **`TestAppProviderRestarterStopAppNoOpsWhenProviderMissing`** — stop succeeds when a config-local app was removed from `ProviderMap` after a failed build.
- **`TestAppProviderRestarterStartAppRegistersMissingProvider`** — start registers a missing local app and a repeated start is idempotent.
- **`TestAppProviderRestarterStopRemovesProviderAndStartRestoresIt`** — stop removes the closed provider and start registers a fresh one.
- **`TestAppProviderRestarterStopQuarantinesProviderWhenCloseFails`** — a provider whose close fails remains absent and subsequent stop attempts preserve the terminal error.

### `app_provider_lifecycle_test.go`

- **`TestAppProviderLifecycleSerializesOneApp`** — lazy activation and restart work for one app cannot overlap.

---

## Rollout coordination tests

Run:

```bash
cd gestaltd
go test ./internal/coredata -run 'Test(AppRollout|AppInstanceMaterialization|AppVersionInstallLock)' -count=1
go test ./internal/appregistry -run 'Test(Installer|CatalogPollerRollout)' -count=1
```

- Install admission and app-scoped lock tests allow one active rollout per app while allowing different apps concurrently.
- Poller tests cover enrollment, cohort completion, missed deadlines, and late-replica convergence.

---

## Registry-only app tests

Run:

```bash
cd gestaltd
go test ./internal/config/... -run RegistryOnly -count=1
go test ./internal/operator/... -run RegistryOnly -count=1
go test ./internal/appregistry/... -run 'TestInstaller.*RegistryOnly|TestInstallerAdd|TestInstallerUpgrade' -count=1
go test ./internal/bootstrap/... -run RegistryOnly -count=1
go test ./internal/coredata/... -run LatestKnown -count=1
go test ./internal/server/... -run 'RegistryApp|RegistryOnly' -count=1
```

### Config validation

- Accepts `source.registry` when the name matches a configured `appRegistries` entry.
- Rejects an unknown registry name.
- Rejects `source.registry` combined with `source.git`, `source.path`, or other source modes.
- Rejects `source.registry` outside the `apps` map.
- Allows configured static and HTTP surfaces without a deploy-time resolved manifest, operation catalog, or static root.
- Defaults `server.appRegistry.maxReconcileAttempts` to `3`, accepts positive overrides, and rejects zero or negative values.

### Add and upgrade

- **`add`** — accepts the first fleet-known version when `ListKnownVersionsByApp` is empty; returns **409** when the app is already in the catalog.
- **`upgrade`** — accepts a new version when the app has fleet-known versions; sets `from_version` to `LatestKnownVersion`; returns **400** when the catalog is empty.
- **`add`** records `from_version: "registry:first-install"` server-side (audit only; not a runnable version). See [lifecycle.md](./lifecycle.md#post-adminapiv1app-registriesregistryappsappadd).

### Bootstrap startup

- Starts the deterministic latest fleet-known version at `StartAppProviders`.
- Uses the same latest-version ordering as the poller, including equal-timestamp tie-breaking.
- Skips the app and clears any stale running-version map entry and `active-version` marker when the projection is empty.
- Rejects a projected installation whose registry differs from deploy `source.registry`; it does not fall back to another source.
- Does not gate startup on rollout or per-replica materialization rows.
- Keeps core boot available when an individual registry app cannot materialize or start.
- Does not start the catalog poller until all startup-provider initialization attempts finish.
- Starts the poller's first reconciliation pass immediately after startup-provider initialization.
- Allows a replica that missed rollout enrollment during bootstrap to converge locally without reopening the rollout.

### Provider lifecycle and concurrency

- Serializes materialization per app on each replica, including different versions of the same app, while allowing different apps to materialize concurrently.
- Starting an already-running exact version is idempotent; starting over a different or unknown recorded version does not relabel the existing provider.
- A registry-only start requires the exact validated package and never falls back to a deploy-time provider build.
- Build, registration, activation, and stop failures leave the provider registry, running-version map, and `active-version` marker consistent.
- A stale stopped row cannot cause the poller to overwrite a different running provider without stopping it.
- A version already started by bootstrap is marked converged by the poller without an extra restart.
- A failed app reconciliation atomically increments `attempt_count`, replaces `last_error_at` and `last_error_message`, releases its lifecycle lease, and does not prevent other apps from reconciling.
- The next poll retries the failed app from the beginning; repeated materialize, stop, start-same-version, and rollout-progress operations are idempotent.
- Stops retrying one desired version when its `attempt_count` reaches `server.appRegistry.maxReconcileAttempts`, which defaults to `3`.
- A new desired-version row starts with zero attempts; increasing the configured limit resumes rows whose count is below the new limit.
- Logs the error when `RecordFailure` itself cannot write to IndexedDB.
- Unrecoverable local provider state marks Gestalt unhealthy and terminates the process.

### Runtime surfaces

- Server construction accepts a registry app with configured static and HTTP mounts before any package or provider exists.
- Static requests return **503** while the provider is absent or changing, then serve the bundle for the exact running version.
- A concurrent version change cannot combine a static bundle from one version with a provider from another.
- Static request handling does not block on a long-running provider lifecycle operation.
- Deploy-config HTTP bindings mount without startup-time provider catalog lookup and become invocable after the provider starts.
- Stopping or removing the provider immediately makes static, MCP, and operation surfaces unavailable and clears its running-version map entry and `active-version` marker.

### Lock and sync

- `gestalt lock` and `gestalt sync` omit snapshot resolution and artifact download for registry-only apps.
- Lockfile entries record the registry binding only — see [config.md](./config.md#lockfile).

---

## What is not covered yet

Publish tests validate **CLI dry-run behavior** only. Install HTTP tests cover the happy path, 404 on missing version, and get-by-app — but not:

- Real GCS upload integration
- Re-install idempotency (no duplicate change request)
- Multi-replica materialization ack E2E (see [PLANNED] section above)
- Deployed verification that catalog restarts serve the newly mounted binary
