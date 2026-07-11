# App Registry and Runtime Installation

The app registry should decouple app publishing from Gestalt deployments. Publishing a new app version should make that version available for installation, but should not require rebuilding or redeploying the `gestaltd` Cloud Run service.

Related references:

- [config.md](./config.md) — deploy reader config and CI publish flags
- [api.md](./api.md) — admin HTTP API for listing registry versions
- [models.md](./models.md) — JSON document shapes stored in GCS
- [service.md](./service.md) — Go package API for publish and validation

## Registry Responsibilities

App registries are responsible for:

- Storing immutable published app artifacts.
- Exposing available app versions to Gestalt.
- Exposing versioned app metadata, including:
  - manifest
  - operations
  - input/output schemas
  - dependency declarations
  - compatibility constraints
  - artifact checksums
  - publication metadata
- Allowing apps to be installed from the registry.
- Allowing Gestalt to validate app dependencies before install or activation.

The registry should be more than an artifact bucket. It should act as a versioned contract registry for installable apps.

## Expectations

### App Versions and Artifacts Are Immutable

Once an app is published to a registry under a version identifier, that published version cannot be changed.

Each published version should include immutable metadata and artifact references, including archive checksums. If an app changes, it must be published as a new version.

### Registries Are Defined in `config.yaml`

`config.yaml` defines where Gestalt **reads** installable apps (`appRegistries`). That is the deploy/runtime reader config.

CI **publishes** with CLI flags — `gestaltd app publish --bucket BUCKET` — not a publisher block in config. Immutability is enforced by the publish command. See [config.md](./config.md).

Registries may be public or private. A private registry may require credentials or a configured secrets provider.

Reader example (deploy `config.yaml`):

```yaml
appRegistries:
  toolshed:
    kind: gcs
    gcs:
      bucket: gitlab-peach-street-gestalt-app-registry
```

Publish example (CI workflow):

```bash
gestaltd app publish \
  --bucket gs://gitlab-peach-street-gestalt-app-registry \
  --app g-issues \
  --version 0.0.1 \
  --ref abc123def456abc123def456abc123def456abcd \
  --dist-dir dist/
```

`gestaltd app publish` writes immutable objects under `apps/{app}/` in the bucket:

```text
apps/{app}/
├── index.json
├── versions/
│   └── {version}.json
└── artifacts/
    └── {version}/
        └── gestalt-app-{app}_v{version}_{platform}.tar.gz
```

- `index.json` — catalog of published versions (updated on each publish)
- `versions/{version}.json` — full published version metadata and install contract ([models.md](./models.md))
- `artifacts/{version}/…` — platform archives uploaded from `--dist-dir`

At startup, Gestalt should only need enough registry configuration to discover app metadata and materialize installed app artifacts.

### Registry Metadata Includes App Contracts

A registry entry should describe both the artifacts and the app contract.

For each app version, the registry should expose:

- app name
- version
- source ref
- manifest path
- artifact URLs
- artifact checksums
- interface operations
- input/output schemas for operations
- required permissions/auth modes
- declared dependencies
- compatible `gestaltd` versions or protocol versions
- publish timestamp

Example:

```json
{
  "app": "dealHub",
  "version": "1.2.0",
  "sourceRef": "abc123...",
  "manifestPath": "valon-tools/apps/deal-hub/manifest.yaml",
  "artifacts": {
    "linux/amd64": {
      "url": "gs://.../gestalt-app-deal-hub_v1.2.0_linux_amd64.tar.gz",
      "sha256": "..."
    }
  },
  "interface": {
    "operations": {
      "matters.list": {
        "inputSchema": {
          "type": "object"
        },
        "outputSchema": {
          "type": "object"
        }
      }
    }
  },
  "requires": {
    "apps": {
      "slack": {
        "version": "^1.4.0",
        "operations": {
          "chat.postMessage": {
            "inputSchemaHash": "sha256:..."
          }
        }
      }
    }
  },
  "compatibility": {
    "minGestaltdVersion": "0.20.0"
  }
}
```

### Apps Declare Their Dependencies

Apps that invoke other apps must declare those dependencies.

For example, if an app uses the SDK to invoke `slack.chat.postMessage`, it should declare that dependency in its manifest or package metadata. During publishing, those declarations are validated and copied into the registry entry's `requires` metadata.

The author-facing manifest may use a shape like:

```yaml
dependencies:
  apps:
    slack:
      version: "^1.4.0"
      operations:
        chat.postMessage:
          inputSchemaHash: sha256:...
```

Generated typed SDKs are not required for this model. Apps can use the existing SDK invocation style, while packaging and install validation statically verify that referenced operations exist and that declared input shapes are compatible.

### Core Providers and Apps Are Treated Differently

Core providers and apps should still be deployed with `gestaltd`.

Core providers include anything required for Gestalt to boot, serve requests, authorize requests, store install state, or recover from a broken runtime app install.

Examples:

- IndexedDB
- Authorization
- Identity
- Secrets
- Registry/Admin app
- Minimal recovery UI

These providers are prerequisites for serving dynamic apps. They should remain pinned in `config.yaml` and `gestalt.lock.json`, and should be materialized into the deployment image.

### Installation State Lives in IndexedDB

Non-core app installation state should live in IndexedDB.

**Source of truth:** `app_version_catalog` — an append-only catalog of versions Gestalt knows about per app. Successful installs append a `version_added` record whose metadata carries the install contract (registry, source ref, artifact checksums, release URL, materialized path, `installed_at`, etc.). Failed attempts append `install_failed` records for audit only.

**Materialized views** (computed in Go, not separate stores):

- **Known versions** — projected from `version_added` records, one entry per `(app, version)` pair. Answers “what versions has the fleet installed or discovered?”

Step 6 implements known-version projection in `AppVersionCatalogService` (`ListKnownVersionsByApp`, `ListAllKnownVersions`). Admin HTTP list/get endpoints read these projections. The step 5 `app_installations` upsert store was removed; install state is catalog-only.

There is **no fleet head**, **no promotion**, and **no rollback** in step 6. Those belong to later activation/rollout steps.

At runtime (target state after steps 6–7), `gestaltd serve` should:

1. Load the committed core lockfile.
2. Read known versions per app from `app_version_catalog` (projected).
3. Resolve those records into lock entries.
4. Build an effective in-memory runtime graph.
5. Materialize required app artifacts locally per instance.
6. Serve core and installed apps together.

The deploy image should not need to include every non-core app artifact.

Example store:

```text
app_version_catalog              ← source of truth (append-only)
  - id
  - app                          # app name, e.g. g-issues
  - version
  - type                         # version_added | install_failed
  - actor
  - timestamp
  - metadata                     # on version_added: install contract snapshot
```

Record types in the install prototype:

| Type | When | Effect on known versions |
|------|------|--------------------------|
| `version_added` | Artifacts on disk + validation OK | Adds `(app, version)` to catalog |
| `install_failed` | Install error | Audit only |

### Multi-Instance Convergence and Lazy Installation

Cloud Run may run many `gestaltd` instances at the same time. If one instance handles an install request, the other instances also need to observe and serve the installed app.

The install request should not directly command every instance. Instead, the handling instance appends a `version_added` record to shared IndexedDB; other instances **read the catalog** and converge independently.

**Step 6 (implemented):** only the handling instance materializes artifacts and appends catalog records. Other instances do not read install state yet.

**Step 7 (planned):** each instance reads known versions from `app_version_catalog`, then lazily materializes missing versions locally.

The expected flow is:

1. One `gestaltd` instance receives the install request.
2. It validates the selected app version against registry metadata.
3. It downloads and materializes the app artifact locally (in place under `registry-installed/{app}/{version}/`).
4. If successful and `(app, version)` is not already known, it appends `version_added` with the full install contract in record metadata. If not, it appends `install_failed` for audit and returns an error.
5. Other instances (step 7) notice new catalog entries on startup, polling, or first request to that app.
6. Each other instance lazily materializes missing versions into its own local artifacts directory.
7. Once local materialization succeeds, that instance starts or binds the app and serves traffic for it.

This is similar to app migrations and configure-on-first-request behavior: the catalog records what versions exist, while each instance performs local preparation when it needs to serve that app.

For the first implementation, lazy installation can happen on startup or first request to the app. A later version can add polling or pub/sub notifications so instances converge sooner without waiting for traffic.

Install state should distinguish global activation from per-instance readiness:

```
app_instance_materializations
  - app_name
  - resolved_version
  - instance_id
  - materialization_state
  - materialized_at
  - last_error
  - updated_at
```

Per-instance readiness is ephemeral operational state. The **known-version catalog** in `app_version_catalog` records which versions exist fleet-wide.

If an instance receives a request for an active app that has not been materialized locally yet, it should return `503 Service Unavailable`.

Materialization should happen during startup or in a background convergence loop, not synchronously in the request path. Once local materialization succeeds, the instance can start or bind the app and begin serving traffic for it.

### Validation Happens at Publish Time and Install Time

Validation should happen in two phases.

At publish time, `gestaltd provider package`, `gestaltd provider publish`, or `gestaltd app publish` should validate the app version before writing it to the registry.

Publish-time validation should ensure:

- the app manifest is valid
- app invokes operations that exist for the dependencies declared

At install or upgrade time, Gestalt should validate the candidate against the actual runtime environment before activation.

Install-time validation should ensure:

- the selected app version exists in a configured registry
- activating the candidate does not break existing installed dependents
- the candidate can be materialized in the runtime environment

Activation should be two-phase:

1. Materialize and validate the candidate version on the handling instance.
2. Append `version_added` to `app_version_catalog` only after validation succeeds (and only if the version is not already known).

Fleet activation, rollback, and head selection are planned for later steps (7–8).

### Runtime Materialization

Dynamic apps should be materialized at runtime or startup from the **known-version catalog** (and per-instance materialization state in step 7).

On Cloud Run, local disk is ephemeral, so cold starts may need to re-materialize installed apps. To keep startup fast, Gestalt should reuse a remote materialization cache where possible.

### Failure Handling

Dynamic app failures should not prevent Gestalt from booting core functionality.

If an installed non-core app fails to materialize or load:

- Gestalt should continue serving core apps.
- The failed app should be marked unhealthy and it should be easy to roll back (future activation/rollback steps).

Core recovery paths must not depend on dynamically installed apps.

## Current Flow

1. `toolshed` app code changes on `main` trigger `auto-publish-snapshots.yml`.
2. That workflow calls `publish-app-snapshot.yml` for changed apps that are present in `deploy/config.yaml`.
3. `publish-app-snapshot.yml` packages the app with `gestaltd provider package` and publishes snapshot artifacts to GCS.
4. `deploy/config.yaml` pins each app via `source.git.repo/ref/path`, with `materialization: snapshot` and `artifactRepository: valon`.
5. `gestaltd lock` turns `config.yaml` into the committed `gestalt.lock.json`.
6. `deploy-valon-tools.yml` builds a Docker image using a pinned `gestaltd` image from the `gestalt` repo.
7. During Docker build, `gestaltd sync --locked` downloads and materializes locked artifacts from GCS into the image.
8. Cloud Run runs `gestaltd serve --locked --no-sync` from those baked artifacts.

## Implementation Path

1. Prototype a GCS registry and publish a single app there (`publish-app-registry.yml` + `gestaltd app publish --bucket`).
2. Use a parallel path to `publish-app-snapshot.yml` so normal app publishing is not interrupted.
3. Start with publishing `g-issues`.
4. Prototype a Gestalt endpoint that lists available versions in configured registries. See [api.md](./api.md).
5. Add IndexedDB version catalog store and projection helpers (`app_version_catalog` + known-version views). See [indexeddb.md](./indexeddb.md).
6. Prototype installing one registry app: materialize on the handling instance, record known versions in the catalog, expose via admin HTTP. **Done:** catalog-only writes; `app_installations` store removed. See [api.md](./api.md), [tests.md](./tests.md).
7. Add lazy per-instance materialization on startup or first request — each instance reads known versions from the catalog and materializes locally.
8. Add install-time validation, fleet activation/rollback, and concurrency guards.
