# App Registry

The app registry decouples app publishing from Gestalt deployments. Publishing
a new app version makes it available for installation without rebuilding or
redeploying the `gestaltd` Cloud Run service.

## Documentation

<pre>
app_registry/
├── <a href="./readme.md">readme.md</a>
├── architecture/
│   ├── <a href="./architecture/config.md">config.md</a>
│   ├── <a href="./architecture/indexeddb.md">indexeddb.md</a>
│   ├── <a href="./architecture/models.md">models.md</a>
│   └── <a href="./architecture/validation.md">validation.md</a>
├── operations/
│   ├── <a href="./operations/admin.md">admin.md</a>
│   ├── <a href="./operations/lifecycle.md">lifecycle.md</a>
│   ├── <a href="./operations/pending-publish.md">pending-publish.md</a>
│   └── <a href="./operations/retention.md">retention.md</a>
└── project/
    ├── <a href="./project/changelog.md">changelog.md</a>
    └── <a href="./project/tests.md">tests.md</a>
</pre>

### Architecture

- [Configuration](./architecture/config.md) — deploy reader config and CI publish flags
- [IndexedDB state](./architecture/indexeddb.md) — fleet version changes and rollout state
- [Registry models](./architecture/models.md) — JSON documents stored in GCS
- [Install-time validation](./architecture/validation.md) — admission checks before fleet acceptance

### Operations

- [Administration](./operations/admin.md) — admin UI capabilities
- [Replica lifecycle](./operations/lifecycle.md) — startup, background controller, and admin HTTP API
- [Pending publishes](./operations/pending-publish.md) — in-flight and failed publish visibility
- [Retention](./operations/retention.md) — version cleanup and retention policy

### Project history

- [Implementation changelog](./project/changelog.md) — milestones and pull requests
- [Tests](./project/tests.md) — behavioral test coverage

## Registry responsibilities

App registries:

- Store immutable published app artifacts.
- Expose available app versions to Gestalt.
- Store versioned app metadata:
  - manifest
  - operations and input/output schemas
  - dependency declarations
  - compatibility constraints
  - artifact checksums
  - publication metadata
- Allow registry-only apps to be installed.
- Allow Gestalt to validate app dependencies before fleet admission.

The registry is a versioned contract registry for installable apps, not only an
artifact bucket.

## Published versions are immutable

Once an app is published under a version identifier, that version cannot
change. A published version includes immutable metadata and artifact references,
including archive checksums. App changes require a new version.

`gestaltd app registry publish` writes:

```text
apps/{app}/
├── index.json
├── versions/
│   └── {version}.json
└── artifacts/
    └── {version}/
        └── gestalt-app-{app}_v{version}_{platform}.tar.gz
```

- `index.json` — catalog of published versions
- `versions/{version}.json` — full metadata and install contract
- `artifacts/{version}/…` — platform archives

See [models.md](./architecture/models.md) for document shapes.

## Registry configuration

Deploy config defines where `gestaltd` reads installable apps:

```yaml
appRegistries:
  toolshed:
    kind: gcs
    gcs:
      bucket: gitlab-peach-street-gestalt-app-registry
```

CI publishes with CLI flags rather than a publisher block in deploy config:

```bash
gestaltd app registry publish \
  --bucket gs://gitlab-peach-street-gestalt-app-registry \
  --app g-issues \
  --version 0.0.1 \
  --ref abc123def456abc123def456abc123def456abcd \
  --dist-dir dist/
```

See [config.md](./architecture/config.md).

## Registry metadata includes app contracts

Each published version describes its artifacts and app contract:

- app name and version
- source ref and manifest path
- artifact URLs and checksums
- interface operations and schemas
- declared app dependencies
- compatible `gestaltd` versions
- publication timestamp and provenance

Apps that invoke other apps declare those dependencies in their manifest or
package metadata. Publishing validates and copies the declarations into the
registry entry's `requires` metadata:

```yaml
dependencies:
  apps:
    slack:
      version: "^1.4.0"
      operations:
        chat.postMessage:
          inputSchemaHash: sha256:...
```

Generated typed SDKs are not required. Apps can use the existing SDK invocation
style while package and install validation verify referenced operations and
declared input shapes.

## Core providers remain deploy-pinned

Providers required for Gestalt to boot, authorize requests, store installation
state, or recover from a broken runtime app remain pinned in `config.yaml` and
`gestalt.lock.json` and are materialized into the deployment image.

Examples:

- IndexedDB
- Authorization
- Identity
- Secrets
- Registry/admin app
- Minimal recovery UI

Registry-only apps are materialized at runtime and do not participate in the
deploy-time provider graph.

## Installation state lives in IndexedDB

`app_version_change_requests` is the append-only source of truth for fleet
version changes (`from_version` → `to_version`). Successful admission appends a
change request with the install contract. Failed validation rejects the request
without writing a row.

Gestalt projects known versions from change requests through
`ListKnownVersionsByApp` and `ListAllKnownVersions`. There is no separate fleet
head or promotion record. The ordered change requests form the permanent
revision history. A version already in that history cannot be selected again.

Per-replica rollout progress lives in `app_instance_materializations`; fleet
rollout state lives in `app_rollouts`. See [indexeddb.md](./architecture/indexeddb.md).

## Validation and activation

Validation happens in two phases:

1. Publish validates the app manifest and packaged contract before writing the
   version.
2. Install or upgrade validates platform artifacts, `gestaltd` compatibility,
   dependencies, and reverse dependents against the running fleet.

After admission:

1. Gestalt appends a change request to `app_version_change_requests`.
2. Each replica acknowledges the desired version.
3. Each replica downloads and validates the artifact before stopping the
   current app.
4. Each replica restarts with the registry-materialized package and records
   convergence.

See [validation.md](./architecture/validation.md) and [lifecycle.md](./operations/lifecycle.md).

## Runtime materialization and failure handling

Each replica materializes the latest desired registry version under
`{artifactsDir}/registry-installed/{app}/{version}`. Local disk is ephemeral, so
cold starts may need to materialize the package again.

A dynamic app failure does not prevent Gestalt from serving core functionality.
Recovery publishes a new version, even when its code intentionally matches an
older revision. Core recovery paths do not depend on registry-only apps.

## Future work

### Version retention and cleanup

Automatically delete never-deployed snapshots after 3 days by default. Retain
every version that entered the fleet deploy chain permanently, including its
metadata and artifacts. Add `apps/{app}/retention.json`, fleet-use tracking,
reader-owned `gestaltd app registry retention prune`, and a read-only Revision
history tab on the app-admin page. See [retention.md](./operations/retention.md).

### Packaged workflow metadata

Packaged apps declare workflow definitions in provider source. Bootstrap
registers them from `DeclaredWorkflowDefinitions` after install, which is too
late to reject a bad version before fleet admission.

Install validation should read workflow app-call targets from
`versions/{version}.json`, like `interface` and `requires`, without downloading
artifacts or running providers.

At publish time:

- derive `workflows.yaml` during `gestaltd provider package`
  (`GESTALT_APP_WRITE_WORKFLOWS`), including when `catalog.yaml` already exists
- copy app-call steps into the registry entry
- require identical workflow metadata across platform archives

At install time, validate `entry.workflows` before `AppendRequest`. Config-managed
`workflows.definitions` remain separate and are validated at config load. When
`entry.workflows` is absent, skip workflow checks.
