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
| `internal/appregistry` | `poller_test.go` | 2 | Unit (poller) | [gestalt#2750](https://github.com/valon-technologies/gestalt/pull/2750) |
| `internal/server` | `handlers_admin_app_install_test.go` | 3 | HTTP integration | [gestalt#2730](https://github.com/valon-technologies/gestalt/pull/2730) |

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

## Catalog poller unit tests

Added in [gestalt#2750](https://github.com/valon-technologies/gestalt/pull/2750). Exercise `CatalogPoller.ReconcileOnce` and `Start` against stub change requests + materialization services — no `gestaltd serve`, no real ticker timing.

Run:

```bash
cd gestaltd
go test ./internal/appregistry/... -run TestCatalogPoller -count=1
```

### `poller_test.go`

- **`TestCatalogPoller_ReconcileOnce_acknowledges_known_versions`** — After a change request exists, reconcile writes an ack row for the poller `instance_id`; a second reconcile is a no-op.

- **`TestCatalogPoller_Start_is_idempotent`** — Calling `Start` twice does not panic when the loop context is already cancelled.

---

## What is not covered yet

Publish tests validate **CLI dry-run behavior** only. Install HTTP tests cover the happy path, 404 on missing version, and get-by-app — but not:

- Real GCS upload integration
- Re-install idempotency (no duplicate change request)
- Catalog poller integration test against real `gestaltd serve` startup
