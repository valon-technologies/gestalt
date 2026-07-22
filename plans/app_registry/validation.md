# Install-Time Validation

Checks on `POST …/add` and `POST …/upgrade` after the registry entry is fetched and before `Rollouts.Create` and `AppendRequest`. Validation failures return **400**, write nothing to IndexedDB, and wrap `ErrInstallValidationFailed` with a stable **reason** code (see below).

Related docs:

- [lifecycle.md](./lifecycle.md) — handler pipeline
- [models.md](./models.md) — `requires` and `compatibility` on published version JSON
- [tests.md](./tests.md#install-time-validation-tests) — tests

## Checks

| Check | Source | Reason code |
|-------|--------|-------------|
| Platform artifact | `entry.Artifacts` for gestaltd host OS/arch | `platform_artifact_missing` |
| gestaltd version | `entry.compatibility.minGestaltdVersion` vs running gestaltd | `gestaltd_version_incompatible`, `gestaltd_version_invalid`, `gestaltd_version_unknown` |
| Dependency present | Each `entry.requires.apps` entry has a fleet-known version, or a deploy-pinned non-registry provider | `dependency_not_installed` |
| Dependency version | Fleet-known version satisfies `requires.apps.{app}.version` | `dependency_version_unsatisfied` |
| Dependency operations | Dependency published `interface` exposes required operations; `inputSchemaHash` matches when set | `dependency_operation_missing`, `dependency_operation_schema_mismatch`, `dependency_metadata_missing` |
| Reverse dependents | No fleet-known app has a broken `requires.apps.{app}` on the candidate version or `interface` | `reverse_dependent_version_unsatisfied`, `reverse_dependent_operation_missing`, `reverse_dependent_operation_schema_mismatch`, `reverse_dependent_metadata_missing` |
| Workflow target app | Each app step in the candidate's declared workflow definitions references a configured deploy app slot | `workflow_target_app_missing` |
| Workflow target operation | Each app step references an operation published on the target app's `interface` (when metadata is available) | `workflow_target_operation_missing`, `workflow_target_operation_metadata_missing` |

## Validation errors

Each failure is an `InstallValidationError` with a stable `InstallValidationReason`. Programmatic callers use `InstallValidationReasonFrom(err)`. The admin API returns the full string in the `error` field.

| Reason | HTTP | IndexedDB | When |
|--------|------|-----------|------|
| `platform_artifact_missing` | **400** | none | Candidate `entry.Artifacts` has no usable artifact for the gestaltd host platform (missing platform key, URL, or SHA256) |
| `gestaltd_version_incompatible` | **400** | none | Running gestaltd is older than `entry.compatibility.minGestaltdVersion` |
| `gestaltd_version_invalid` | **400** | none | Candidate declares an unparseable `minGestaltdVersion` |
| `gestaltd_version_unknown` | **400** | none | Running gestaltd version is not semver and not a skipped non-production stamp (see [Running gestaltd version](#running-gestaltd-version)) |
| `dependency_not_installed` | **400** | none | Candidate `requires.apps.{app}` but app has no fleet-known version and is not deploy-pinned |
| `dependency_version_unsatisfied` | **400** | none | Fleet-known dependency version does not satisfy the candidate's version constraint |
| `dependency_metadata_missing` | **400** | none | Operation check needed but fleet-known dependency's `versions/{version}.json` is missing |
| `dependency_operation_missing` | **400** | none | Dependency published `interface` does not expose a required operation |
| `dependency_operation_schema_mismatch` | **400** | none | Dependency operation `inputSchemaHash` does not match |
| `reverse_dependent_metadata_missing` | **400** | none | Reverse check needed but a fleet-known dependent's published entry is missing |
| `reverse_dependent_version_unsatisfied` | **400** | none | Candidate version does not satisfy a reverse dependent's version constraint |
| `reverse_dependent_operation_missing` | **400** | none | Candidate `interface` missing an operation required by a reverse dependent |
| `reverse_dependent_operation_schema_mismatch` | **400** | none | Candidate operation `inputSchemaHash` does not match a reverse dependent's requirement |
| `workflow_target_app_missing` | **400** | none | Candidate declares a workflow app step whose `name` is not a key in deploy `config.apps` |
| `workflow_target_operation_missing` | **400** | none | Workflow app step `operation` is not published on the target app's `interface` |
| `workflow_target_operation_metadata_missing` | **400** | none | Operation check needed but the target app's fleet-known `versions/{version}.json` is missing |

### Non-validation failures during install

These are **not** `InstallValidationError` and are enforced outside `InstallValidator`:

| Situation | HTTP | IndexedDB |
|-----------|------|-----------|
| Candidate `versions/{version}.json` not found (installer fetch) | **404** | none |
| Registry transport or infra error fetching the candidate | **502** | none |
| Registry named in a fleet-known installation is missing from gestaltd config (`ErrAppRegistryNotConfigured`) | **502** | none |
| Active rollout, install lock, duplicate version, `add`/`upgrade` mode mismatch | **409** / **400** | none — see [lifecycle.md](./lifecycle.md) |

Missing dependency or reverse-dependent `versions/{version}.json` documents during validation return **400** (or are skipped when the dependent is unrelated). They must not map to **404** just because the error text contains “registry not found”. Only the candidate's own missing version document surfaces **404** — see [lifecycle.md](./lifecycle.md#errors).

## Running gestaltd version

`minGestaltdVersion` is compared to `server.Config.GestaltdVersion` (ldflags-injected build version wired through `gestaltd serve` → `Installer.GestaltdVersion`).

The check is skipped when the running version is a non-production stamp:

| Stamp | Example |
|-------|---------|
| Local dev build | `dev` |
| Go toolchain devel | `(devel)` |
| CI image stamp | `0.0.0-ci+g<sha12>` |

HTTP and unit tests that exercise `gestaltd_version_incompatible` must set `GestaltdVersion` explicitly. When unset, the validator falls back to `dev` and skips the check.

## Dependency checks

### `requires.apps` keys

Published `requires.apps` keys are copied verbatim from manifest `dependencies.apps`. Keys may be short fleet names (`slack`) or full manifest source addresses (`github.com/org/repo/apps/slack`). Fleet catalog, deploy config app slots, and registry object paths (`apps/{app}/…`) always use the short name. Install validation normalizes source-address keys to the short name before fleet lookup and reverse-dependent matching. See [models.md](./models.md).

### Metadata fetch scope

| Check | Uses fleet-known version from IndexedDB | Fetches `versions/{version}.json` |
|-------|----------------------------------------|-----------------------------------|
| Dependency present | yes | no |
| Dependency version | yes | no |
| Dependency operations / `inputSchemaHash` | yes | yes — missing → `dependency_metadata_missing` |
| Reverse dependents | scans all fleet-known apps | yes, per dependent |

Dependency metadata is not fetched when the candidate declares only a version constraint and no operations.

### Deploy-pinned dependencies

When `config.yaml` pins a dependency app with a non-registry source (git, package, local, etc.), install validation skips presence checks for that app. Fleet-known registry versions are not required.

## Workflow definition checks

Apps can ship workflow definitions inside their published artifact. During `gestaltd provider package` / snapshot publish, those definitions are bundled with the app binary the same way operations and UI assets are. At runtime, gestaltd reads them from the started provider as `DeclaredWorkflowDefinitions` and registers them with the workflow manager.

Install admission must validate those declarations **before** writing `app_version_change_requests`. Today bootstrap enforces workflow targets for config-managed `workflows.definitions` entries, but registry `add` / `upgrade` does not yet run the same checks — a broken packaged workflow can be fleet-admitted and only fails when gestaltd starts the app.

### What gestalt inspects

For each workflow definition declared by the candidate app:

1. Walk every workflow step whose action is an app call (`steps[].app.name`, `steps[].app.operation`).
2. Require `steps[].app.name` to match a key in deploy `config.apps` on the handling replica. This is the same configured app slot gestaltd uses at bootstrap (`workflow target app "gIssues" is not configured` when only `g-issues` exists).
3. When the target app is fleet-known or deploy-pinned, require `steps[].app.operation` to exist on that target's published `interface`. Missing target metadata → `workflow_target_operation_metadata_missing`.

Self-targets are allowed when the candidate app name matches the step target (for example, `g-issues` invoking `g-issues.handle_slack_event`).

Workflow definitions are not part of today's `versions/{version}.json` contract (`interface`, `requires`, `compatibility` only). Install validation should extract them from the candidate artifact during admission — either by mounting the packaged app in a validation-only path or by copying workflow specs into registry metadata at publish time. Publish-time extraction is preferred so admission can fail fast without downloading archives.

### Metadata fetch scope

| Check | Uses deploy `config.apps` | Fetches target `versions/{version}.json` |
|-------|---------------------------|------------------------------------------|
| Workflow target app present | yes | no |
| Workflow target operation | yes | yes, when the target is fleet-known — missing → `workflow_target_operation_metadata_missing` |

Operation checks are skipped when the workflow step does not name a target operation or when the target app is deploy-pinned and has no fleet-known version with published metadata.

## Reverse dependents

For each fleet-known app other than the candidate:

1. Fetch the dependent's published entry.
2. If the dependent's `requires.apps` does not reference the candidate (after key normalization), skip.
3. Otherwise validate the candidate version against the dependent's version constraint and the candidate `interface` against required operations and `inputSchemaHash` values.

Missing published metadata:

| Case | Behavior |
|------|----------|
| Fleet-known app unrelated to the candidate; published entry missing | Skip |
| Fleet-known dependent requires the candidate; published entry missing | **400** `reverse_dependent_metadata_missing` |
| Registry not configured for a dependent's installation | **502** `ErrAppRegistryNotConfigured` |

Reverse-dependent operation failures attribute the missing operation to the candidate (for example, `reverse dependent g-issues requires candidate slack: operation postMessage is not published`).

## Semver constraint matching

Fleet versions commonly use snapshot prereleases (`2.0.0-snapshot.gabc123`, `0.0.0-snapshot.gabc123`). Masterminds semver excludes prereleases by default, so install validation compares the fleet version's `major.minor.patch` release line against constraints such as `^2.0.0`:

- `2.0.0-snapshot.*` satisfies `^2.0.0`.
- `0.0.0-snapshot.*` does not satisfy `^1.4.0` or `^2.0.0`.
- When the constraint itself contains prerelease terms (for example `^2.0.0-beta`), matching stays strict with no release-line fallback.

## Failure examples

| Check | Example |
|-------|---------|
| Platform artifact | Upgrading `g-issues` to a version published with only `darwin/arm64` while gestaltd runs on `linux/amd64` in Kubernetes. |
| Platform artifact (incomplete) | Candidate has a `linux/amd64` artifact entry but `sha256` is empty. |
| gestaltd version | Candidate sets `compatibility.minGestaltdVersion: "0.7.0"` but the handling replica is running gestaltd `0.6.2`. |
| Dependency present | Candidate `requires.apps.slack` but `slack` has no fleet-known version and is not a deploy-pinned provider in `config.yaml`. |
| Dependency present (source address key) | Candidate `requires.apps["github.com/org/repo/apps/slack"]` with fleet-known `slack` at `2.0.0`. |
| Dependency version | Candidate requires `slack` at `^2.0.0` but the fleet-known slack version is `1.4.0`. |
| Dependency version (snapshot) | Candidate requires `slack` at `^2.0.0` and fleet-known slack is `2.0.0-snapshot.gabc123`. |
| Dependency operations | Candidate requires slack operation `channels.list` with `inputSchemaHash: sha256:abc…` but the fleet-known slack version's published `interface` dropped that operation or changed the input schema. |
| Reverse dependents (version) | Upgrading `slack` to `3.0.0` while fleet-known `g-issues` requires `slack: ^2.0.0`. |
| Reverse dependents (operations) | Upgrading `slack` to a version that removes `postMessage` from its published `interface` while fleet-known `g-issues` still requires that operation. |
| Workflow target app | Installing `g-issues` `0.0.0-snapshot.g…` whose packaged `slack_v2_smoke_test` workflow still targets app `gIssues` after deploy config renamed the slot to `g-issues`. Bootstrap fails with `workflow target app "gIssues" is not configured`; install admission should reject with `workflow_target_app_missing` instead. |
| Workflow target operation | Installing an app whose workflow calls `slack.postMessage` while fleet-known `slack` no longer publishes that operation. |
| Registry not configured | Fleet-known `g-issues` references registry `toolshed`, but gestaltd config has no `toolshed` entry — **502**, not **404**. |

Other admission rules (registry binding, `add`/`upgrade` mode, duplicate version, active rollout, install lock) are enforced in `Installer.install` — see [lifecycle.md](./lifecycle.md).
