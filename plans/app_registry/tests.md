# App Registry Tests

Reference for behavioral tests in the app registry plan.

Related docs:

- [plan.md](./plan.md) — implementation path and goals
- [models.md](./models.md) — JSON documents exercised by publish and install
- [service.md](./service.md) — Go API behind the CLI
- [config.md](./config.md) — `appRegistries` deploy reader config
- [api.md](./api.md) — admin HTTP API for listing registry versions and installing apps

---

## Overview

| Package | File | Tests | Layer | PR |
|---------|------|-------|-------|-----|
| `internal/daemon/e2e/appregistry` | `appregistry_test.go` | 1 | E2E (CLI) | [gestalt#2709](https://github.com/valon-technologies/gestalt/pull/2709) |
| `internal/coredata` | `app_version_catalog_test.go` | 5 | Unit (projection) | [gestalt#2730](https://github.com/valon-technologies/gestalt/pull/2730) |
| `internal/server` | `handlers_admin_app_install_test.go` | 3 | HTTP integration | [gestalt#2730](https://github.com/valon-technologies/gestalt/pull/2730) |
| `internal/appregistry` | `converger_test.go` | 2 | Unit (convergence) | step 7 |

Test fixture for install HTTP tests: `internal/appregistry/registrytest/fixture.go`

---

## Step 1: publish dry-run E2E

Added in [gestalt#2709](https://github.com/valon-technologies/gestalt/pull/2709) (step 1: GCS app registry + `gestaltd app publish`).

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

## Step 6: install HTTP integration

Added in [gestalt#2730](https://github.com/valon-technologies/gestalt/pull/2730) (step 6: registry app install via `app_version_catalog` + local materialization).

Run:

```bash
cd gestaltd
go test ./internal/server/... -run TestAdminAppRegistryInstall -count=1
```

All three subtests use `newTestServer` (`httptest.NewServer` on localhost), `testutil.NewStubServices` (in-memory IndexedDB stub), and `registrytest.NewInstallFixture` (local mock GCS). **No production instances are contacted.**

### `handlers_admin_app_install_test.go`

- **`TestAdminAppRegistryInstall/installs_and_lists_known_version`** — `POST …/install` returns 200 with known version and materialized path; `GET …/app-installations` lists one known version.

- **`TestAdminAppRegistryInstall/missing_version_returns_not_found`** — Unknown version returns HTTP 404.

- **`TestAdminAppRegistryInstall/get_versions_by_app`** — `GET …/app-installations/{app}` returns an array of known versions after a successful install.

---

## Step 6: catalog projection unit tests

Added in [gestalt#2730](https://github.com/valon-technologies/gestalt/pull/2730). Exercise `ListKnownVersionsByApp` and `ListAllKnownVersions` over in-memory stub IndexedDB — no HTTP, no GCS.

Run:

```bash
cd gestaltd
go test ./internal/coredata/... -run TestAppVersionCatalog -count=1
```

### `app_version_catalog_test.go`

- **`TestAppVersionCatalogService/append_and_list_records_by_app`** — Records append in timestamp order per app.

- **`TestAppVersionCatalogService/has_known_version`** — `HasKnownVersion` returns true after `version_added`.

- **`TestAppVersionCatalogProjection/ListKnownVersionsByApp_dedupes_version_added`** — Multiple `version_added` records project to one entry per version.

- **`TestAppVersionCatalogProjection/ListKnownVersionsByApp_skips_install_failed`** — Failed records are not projected.

- **`TestAppVersionCatalogProjection/ListAllKnownVersions_returns_latest_per_app_version`** — Fleet list includes distinct `(app, version)` pairs.

---

## Step 7: lazy local materialization

Run:

```bash
cd gestaltd
go test ./internal/appregistry/... -run TestConverger -count=1
```

### `converger_test.go`

- **`TestConverger_materializes_catalog_known_version_locally`** — catalog `version_added` without local artifacts; `ConvergeOnce` downloads and extracts to this instance's artifacts dir.

- **`TestConverger_skips_already_materialized_version`** — after a successful install, convergence is a no-op.

---

## What is not covered yet

Publish tests validate **CLI dry-run behavior** only. Install HTTP tests cover the happy path, 404 on missing version, and get-by-app — but not:

- Real GCS upload integration
- Failed install `install_failed` record assertions
- Re-install idempotency (no duplicate `version_added`)
- Lazy per-instance materialization on other instances (startup convergence — see step 7 tests below)
- First-request convergence and `app_instance_materializations` IndexedDB store

See [plan.md](./plan.md) steps 7–8 for planned follow-up coverage.
