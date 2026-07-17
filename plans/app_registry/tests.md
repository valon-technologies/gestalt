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
| `internal/appregistry` | `poller_test.go`, `poller_materialize_test.go` | 17 | Unit | [gestalt#2812](https://github.com/valon-technologies/gestalt/pull/2812) |
| `internal/appregistry` | `materializer_test.go` | 3 | Unit | step 10 |
| `internal/coredata` | `app_rollouts_test.go`, `app_version_install_locks_test.go` | 3 | Unit | [gestalt#2812](https://github.com/valon-technologies/gestalt/pull/2812) |
| `internal/bootstrap` | `app_provider_restart_test.go`, `app_provider_lifecycle_test.go` | 6 | Unit/integration | [gestalt#2812](https://github.com/valon-technologies/gestalt/pull/2812) |

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

## Install HTTP integration

Added in [gestalt#2730](https://github.com/valon-technologies/gestalt/pull/2730) (registry app install via `app_version_change_requests`).

Run:

```bash
cd gestaltd
go test ./internal/server/... -run TestAdminAppRegistryInstall -count=1
```

All three subtests use `newTestServer` (`httptest.NewServer` on localhost), `testutil.NewStubServices` (in-memory IndexedDB stub), and `registrytest.NewInstallFixture` (local mock GCS). **No production instances are contacted.**

### `handlers_admin_app_install_test.go`

- **`TestAdminAppRegistryInstall/installs_and_lists_known_version`** — `POST …/install` returns 200 with known version; `GET …/app-installations` lists one known version.

- **`TestAdminAppRegistryInstall/missing_version_returns_not_found`** — Unknown version returns HTTP 404.

- **`TestAdminAppRegistryInstall/get_versions_by_app`** — `GET …/app-installations/{app}` returns an array of known versions after a successful install.

---

## [PLANNED] Multi-replica materialization ack E2E

End-to-end test for fleet install convergence across replicas. Replaces isolated catalog poller unit tests.

**Goal:** After a new version is installed fleet-wide, every running replica acknowledges the `(app, version)` pair in `app_instance_materializations`.

**Flow:**

1. Start multiple `gestaltd` replicas against shared IndexedDB (or a test harness that simulates distinct `instance_id` values with the catalog poller enabled).
2. `POST /admin/api/v1/app-registries/{registry}/apps/{app}/install` with a new version.
3. Assert `app_version_change_requests` contains the change request.
4. Poll each replica's `app_instance_materializations` (or a future admin list endpoint) until every replica has an ack row for `(app, version)`.
5. Assert ack timestamps are recent and `instance_id` values are distinct per replica.

**Not in scope for the first cut:** mount swap on restart — ack, materialize, and restart-only convergence.

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
- **`TestMaterializer_rejects_digest_mismatch`** — rejects archives whose digest does not match the registry entry.

### `poller_materialize_test.go`

- **`TestCatalogPollerMaterializesBeforeStop`** — records `materialized_at` and on-disk artifact path before `StopApp`, including while `RestartReady` is still open.

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

## What is not covered yet

Publish tests validate **CLI dry-run behavior** only. Install HTTP tests cover the happy path, 404 on missing version, and get-by-app — but not:

- Real GCS upload integration
- Re-install idempotency (no duplicate change request)
- Multi-replica materialization ack E2E (see [PLANNED] section above)
