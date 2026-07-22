# App Registry Tests

Reference for behavioral tests in the app registry plan.

Related docs:

- [plan.md](./plan.md) — implementation path and goals
- [validation.md](./validation.md) — install-time validation
- [admin.md](./admin.md) — admin UI and rollout read APIs
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
| `internal/appregistry` | `poller_test.go`, `poller_materialize_test.go` | 21 | Unit | [gestalt#2812](https://github.com/valon-technologies/gestalt/pull/2812) |
| `internal/appregistry` | `materializer_test.go` | 5 | Unit | — |
| `internal/appregistry` | `mount_test.go` | 4 | Unit | — |
| `internal/coredata` | `app_rollouts_test.go`, `app_version_install_locks_test.go` | 3 | Unit | [gestalt#2812](https://github.com/valon-technologies/gestalt/pull/2812) |
| `internal/bootstrap` | `app_provider_restart_test.go`, `app_provider_restart_mount_test.go`, `app_provider_lifecycle_test.go` | 8 | Unit/integration | [gestalt#2812](https://github.com/valon-technologies/gestalt/pull/2812) |
| `internal/config`, `internal/operator`, `internal/appregistry`, `internal/bootstrap` | registry-only source tests | — | Unit/integration | — |
| `internal/appregistry` | `install_validator_test.go`, `install_validation_errors_test.go` | — | Unit | [gestalt#2887](https://github.com/valon-technologies/gestalt/pull/2887) |
| `internal/server` | `handlers_admin_app_rollout_test.go` | — | HTTP integration | planned |
| `services/ui/adminui` | registry UI smoke | — | Manual / browser | planned |

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
4. Poll `GET /admin/api/v1/app-rollouts/{app}/materializations` until every replica has an ack row for `(app, version)`.
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
go test ./internal/config/... -run 'RegistryOnly|AppRegistry' -count=1
go test ./internal/operator/... -run RegistryOnly -count=1
go test ./internal/appregistry/... -run 'TestInstaller.*RegistryOnly|TestInstallerAdd|TestInstallerUpgrade|TestCatalogPoller|TestMaterializer' -count=1
go test ./internal/bootstrap/... -run RegistryOnly -count=1
go test ./internal/coredata/... -run 'LatestKnown|AppInstanceMaterialization' -count=1
go test ./internal/server/... -run 'RegistryApp|RegistryOnly' -count=1
```

### Config validation

- Accepts a registry-only app without package-derived metadata; rejects an unknown registry, use outside `apps`, or combination with another source mode.
- Defaults `server.appRegistry.maxReconcileAttempts` to `3`, accepts positive overrides, and rejects zero or negative values.

### Add and upgrade

- **`add`** accepts only an empty catalog, records server-written `from_version: "registry:first-install"`, and returns **409** when a version is already known.
- **`upgrade`** requires a non-empty catalog, sets `from_version` to `LatestKnownVersion`, and returns **400** when the catalog is empty.

### Bootstrap startup

- Starts the same deterministic latest version as the poller only when its registry matches `source.registry`; an empty projection leaves the app stopped and clears any stale running-version map entry and `active-version` marker.
- Attempts every registry app without consulting rollout-progress rows or `attempt_count`, including after the poller reached its retry limit; keeps core boot available after an individual failure; and starts the poller only after all startup attempts finish.
- A replica that misses rollout enrollment during bootstrap converges on its first poll without reopening the terminal rollout.

### Provider lifecycle and concurrency

- Serializes materialization per app on each replica while allowing different apps to materialize concurrently.
- When multiple versions are pending for one app, materializes and retains only the desired version selected by `LatestKnownVersion`; superseded versions are not downloaded, and older local package directories are removed only after the desired version is active.
- Starting the already-running desired version is idempotent. A different or unknown version is stopped and replaced; any failed start cleans up the provider registry, running-version map, and `active-version` marker.
- A version already started by bootstrap has its desired package validated and `materialized_at` recorded before pending rows are marked restarted without another restart. Superseded rows are marked converged without materialization, while a historical `stopped_at` cannot hide a different provider that is actually running.
- A failed reconciliation durably records its error, releases the app lease, and does not block other apps. Retries are idempotent and stop at the configured limit; passive convergence accounting remains allowed when no retryable work remains, and a newly accepted version starts with a fresh attempt count.
- Unrecoverable local provider state marks Gestalt unhealthy and terminates the process.

### Runtime surfaces

- Server construction accepts both a backend-only registry app and configured static or HTTP surfaces before any package or provider exists.
- Static requests return **503** until the provider registry, running-version map, and `active-version` marker agree on one version, and also during a concurrent version change.
- YAML-declared HTTP bindings mount before provider startup and become invocable afterward; stopping the provider makes all configured surfaces unavailable and clears its running-version map entry and `active-version` marker.

### Lock and sync

- `gestalt lock` and `gestalt sync` omit artifact resolution for registry-only apps and record only the registry binding.

---

## Install-time validation tests

Implemented in [gestalt#2887](https://github.com/valon-technologies/gestalt/pull/2887). Checks: [validation.md](./validation.md).

Run:

```bash
cd gestaltd
go test ./internal/appregistry/... -run 'TestInstallValidator|TestInstaller_validation_failure|TestInstallValidationReasonFrom' -count=1
go test ./internal/providerregistry/... -run TestVersionSatisfiesFleetConstraint -count=1
go test ./internal/server/... -run TestAdminAppRegistryInstall/validation_failure -count=1
```

### `install_validator_test.go`

Table-driven `TestInstallValidator` with stub fleet catalog (`AppVersionChangeRequests`) and synthetic `Entry` documents served over `httptest`.

| Subtest | Reason code | Covers |
|---------|-------------|--------|
| `accepts_dependencies/release_with_operations` | — | Happy path: fleet-known dependency version and published `interface` satisfy declared `requires` (version + operation + `inputSchemaHash`) |
| `accepts_dependencies/snapshot_version` | — | Fleet-known dependency at `2.0.0-snapshot.*` satisfies candidate `requires` at `^2.0.0` |
| `accepts_dependencies/source_address_key` | — | Candidate `requires.apps` key is a full manifest source address (`github.com/…/apps/slack`); validator resolves to short fleet name `slack` |
| `rejects_platform_artifact/missing_host_platform` | `platform_artifact_missing` | Candidate `entry.Artifacts` has no artifact for gestaltd host platform |
| `rejects_platform_artifact/incomplete_sha256` | `platform_artifact_missing` | Platform key exists but artifact URL or SHA256 is incomplete (detail preserved from `resolveRegistryArtifact`) |
| `gestaltd_version/incompatible` | `gestaltd_version_incompatible` | `entry.compatibility.minGestaltdVersion` above `InstallValidator.GestaltdVersion` |
| `gestaltd_version/ci_stamp_skipped` | — | `minGestaltdVersion` check skipped for CI build stamp `0.0.0-ci+g…` |
| `rejects_unsatisfied_dependency/missing_dependency_app` | `dependency_not_installed` | Declared dependency has no fleet-known version |
| `rejects_unsatisfied_dependency/version_outside_range` | `dependency_version_unsatisfied` | Fleet-known dependency version outside declared semver constraint |
| `rejects_unsatisfied_dependency/missing_required_operation` | `dependency_operation_missing` | Fleet-known dependency published `interface` does not expose a required operation |
| `reverse_dependents/operation_missing` | `reverse_dependent_operation_missing` | Fleet-known dependent requires an operation the candidate `interface` does not publish |
| `reverse_dependents/snapshot_version_accepted` | — | Upgrading dependency to `2.0.0-snapshot.*` satisfies reverse dependent `requires` at `^2.0.0` |
| `reverse_dependents/version_outside_range` | `reverse_dependent_version_unsatisfied` | Candidate version outside reverse dependent's declared semver constraint (short app key) |
| `reverse_dependents/version_outside_range_source_address_key` | `reverse_dependent_version_unsatisfied` | Same as above when dependent `requires.apps` key is a full source address |
| `reverse_dependent_infra/ignores_unrelated_missing_metadata` | — | Fleet-known app with missing published entry does not block install of an unrelated candidate |
| `reverse_dependent_infra/registry_not_configured` | — | Missing registry config during reverse-dependent fetch returns `ErrAppRegistryNotConfigured` (HTTP **502**), not **404** |

`TestInstaller_validation_failure_writes_nothing` — validation failure before `Rollouts.Create` / `AppendRequest`; asserts no rollout or change-request rows were written.

Source-address `requires.apps` keys are covered by `accepts_dependencies/source_address_key` and `reverse_dependents/version_outside_range_source_address_key`.

### `install_validation_errors_test.go`

| Test | Covers |
|------|--------|
| `TestInstallValidationReasonFrom` | `InstallValidationReasonFrom` returns stable reason codes; failures wrap `ErrInstallValidationFailed` |

### `providerregistry/registry_test.go`

| Test | Covers |
|------|--------|
| `TestVersionSatisfiesFleetConstraint` | Snapshot versions match release constraints on `major.minor.patch`; prerelease constraints stay strict; release versions behave like `VersionSatisfiesConstraint` |

### `handlers_admin_app_install_test.go`

| Subtest | Covers |
|---------|--------|
| `validation_failure` | `POST …/add` with `minGestaltdVersion` above `cfg.GestaltdVersion` returns **400** and writes nothing to IndexedDB. `upgrade` shares the same `Installer.install` path. |

### Not covered yet

| Gap | Reason code / behavior |
|-----|------------------------|
| `gestaltd_version_invalid` | Candidate declares unparseable `minGestaltdVersion` |
| `gestaltd_version_unknown` | Running gestaltd version is not semver (and not `dev` / `(devel)` / `0.0.0-ci+g…`) |
| `dependency_metadata_missing` | Operation check needed but fleet-known dependency `versions/{version}.json` is missing |
| `dependency_operation_schema_mismatch` | Dependency operation `inputSchemaHash` does not match |
| `reverse_dependent_metadata_missing` | Reverse check needed but fleet-known dependent published entry is missing |
| `reverse_dependent_operation_schema_mismatch` | Candidate operation `inputSchemaHash` does not match reverse dependent requirement |
| Deploy-pinned dependency skip | Candidate `requires.apps.{app}` satisfied by non-registry `config.yaml` provider |
| `dev` / `(devel)` / CI gestaltd skip | `minGestaltdVersion` check skipped when running version is non-production (`dev`, `(devel)`, `0.0.0-ci+g…`) |
| Registry transport vs validation HTTP split | Missing published metadata → **400**; registry fetch/transport failure → **502** |
| Skip dependency fetch when no operations | Version-only `requires` does not fetch dependency `versions/{version}.json` |

---

## Admin observability tests

API shapes: [admin.md](./admin.md). Route registration: [lifecycle.md](./lifecycle.md#admin-observability-api).

Run:

```bash
cd gestaltd
go test ./internal/server/... -run TestAdminAppRollout -count=1
```

### `handlers_admin_app_rollout_test.go`

Use the same harness as install tests: `newTestServer`, `registrytest.NewInstallFixture`, in-memory IndexedDB stub services.

- **`TestAdminRegistryApps/lists_registry_managed_apps`** — `GET …/registry-apps` returns apps with `source.registry` from deploy config; includes apps with empty fleet catalog.
- **`TestAdminRegistryApps/merges_desired_version_and_rollout`** — after `POST …/add`, list/detail include `desiredVersion`, rollout `state`, and cohort counts.
- **`TestAdminAppRollouts/lists_active_rollout`** — `GET …/app-rollouts` returns enrolling/restarting rollouts.
- **`TestAdminAppRolloutsMaterializations/lists_replica_rows`** — `GET …/app-rollouts/{app}/materializations` returns distinct `instanceId` rows with ack/materialized/restarted timestamps after poller convergence.
- **`TestAdminAppRolloutsMaterializations/labels_cohort_membership`** — replicas that ack after `enrollment_ends_at` have `inCohort: false` and do not block rollout completion in the API summary.
- **`TestAdminRegistryApps/rejects_non_registry_app`** — `GET …/registry-apps/{app}` returns **404** for snapshot-pinned apps.

### Admin UI smoke

Manual or Playwright-style check against a local multi-replica harness:

1. Open `/admin/registry` while authenticated as a `gestaltAdmin` user.
2. Confirm `g-issues` appears with desired version after `POST …/add`.
3. Watch rollout badge transition `enrolling` → `restarting` → `complete` and replica rows populate.
4. Confirm **Upgrade** is disabled while rollout is non-terminal.

---

## What is not covered yet

Publish tests validate **CLI dry-run behavior** only. Install HTTP tests cover the happy path, 404 on missing version, get-by-app, and one validation-failure case — but not:

- Real GCS upload integration
- Re-install idempotency (no duplicate change request)
- Full install-time validation reason-code matrix (see [install-time validation tests](#install-time-validation-tests))
- Multi-replica materialization ack E2E (see [PLANNED section above](#planned-multi-replica-materialization-ack-e2e))
- Admin rollout read APIs and `/admin/registry` UI (see [Admin observability tests](#admin-observability-tests))
- Per-replica **observed** running version heartbeats (phase 2; see [admin.md](./admin.md#out-of-scope-runtime-heartbeats))
- Deployed verification that catalog restarts serve the newly mounted binary
