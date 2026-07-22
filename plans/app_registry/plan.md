# App Registry and Runtime Installation

The app registry should decouple app publishing from Gestalt deployments. Publishing a new app version should make that version available for installation, but should not require rebuilding or redeploying the `gestaltd` Cloud Run service.

Related references:

- [config.md](./config.md) — deploy reader config and CI publish flags
- [lifecycle.md](./lifecycle.md) — replica startup, background controller, admin HTTP API
- [validation.md](./validation.md) — install-time validation before fleet accept
- [admin.md](./admin.md) — admin UI and rollout observability
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

**Source of truth:** `app_version_change_requests` — an append-only log of fleet version change requests per app (`from_version` → `to_version`). Successful installs append a change request whose metadata carries the install contract (registry, source ref, artifact checksums, release URL, materialized path, `installed_at`, etc.). Failed validation rejects the HTTP request; no row is written.

**Materialized views** (computed in Go, not separate stores):

- **Known versions** — projected from change requests via `ListKnownVersionsByApp` and `ListAllKnownVersions`, one entry per `(app, to_version)` pair. Admin HTTP list/get endpoints read these projections.

There is **no fleet head** or **promotion**. Reverting to an older version uses `POST …/upgrade` with a previously published version.

Store schema and service API: [indexeddb.md](./indexeddb.md#store-app_version_change_requests-source-of-truth).

### Validation Happens at Publish Time and Install Time

Validation should happen in two phases.

At publish time, `gestaltd provider package`, `gestaltd provider publish`, or `gestaltd app publish` should validate the app version before writing it to the registry.

Publish-time validation should ensure:

- the app manifest is valid
- app invokes operations that exist for the dependencies declared

At install or upgrade time, Gestalt should validate the candidate against the actual runtime environment before activation. See [validation.md](./validation.md).

Activation is phased:

1. Validate the candidate version against registry metadata (including install-time validation; see [validation.md](./validation.md)) and append a change request to `app_version_change_requests` (fleet declaration).
2. Each replica acknowledges the catalog row, then progressively downloads, restarts, and mounts the new binary. See [lifecycle.md](./lifecycle.md#polling).

Fleet rollout admission and per-replica convergence are shipped (rollouts, install locks, catalog poller). Admin observability is documented in [admin.md](./admin.md).

### Runtime Materialization

Dynamic apps should be materialized at runtime or startup from the **known-version catalog** (and per-instance materialization state starting in step 8).

On Cloud Run, local disk is ephemeral, so cold starts may need to re-materialize installed apps. To keep startup fast, Gestalt should reuse a remote materialization cache where possible.

### Failure Handling

Dynamic app failures should not prevent Gestalt from booting core functionality.

If an installed non-core app fails to materialize or load:

- Gestalt should continue serving core apps.
- The failed app should be marked unhealthy; operators can `POST …/upgrade` with a previously published version.

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
4. Prototype a Gestalt endpoint that lists available versions in configured registries. See [lifecycle.md](./lifecycle.md).
5. Add IndexedDB change request store and projection helpers (`app_version_change_requests` + known-version views). See [indexeddb.md](./indexeddb.md).
6. Prototype installing one registry app: materialize on the handling instance, record known versions in the catalog, expose via admin HTTP. **Done:** catalog writes; `app_installations` store removed. See [lifecycle.md](./lifecycle.md), [tests.md](./tests.md).
7. Split install from local materialization — `POST …/install` writes `app_version_change_requests` only. See [lifecycle.md](./lifecycle.md).
8. Per-replica catalog polling: acknowledge each new fleet-known `(app, version)` in IndexedDB. **Done:** `app_instance_materializations` + background poller. See [lifecycle.md](./lifecycle.md#polling).
9. Stop the running app and start the same app back up (restart machinery only; no binary change yet). Use a **1 minute** delay between stop and start during early rollout testing so operators can observe that the process started; production restart has no intentional wait.
10. Download and materialize the new version artifact **before** bringing the app down. **Done:** catalog poller downloads registry archives to `{artifactsDir}/registry-installed/{app}/{version}` and records `materialized_at` before `StopApp`. See [lifecycle.md](./lifecycle.md#polling), [tests.md](./tests.md#artifact-materialization-tests).
11. Mount the newly materialized binary instead of the old one when restarting. **Done:** catalog-driven `StartApp` resolves an isolated provider entry from `{artifactsDir}/registry-installed/{app}/{version}` before rebuilding the app. See [lifecycle.md](./lifecycle.md#polling), [tests.md](./tests.md#registry-mount-tests).
12. Registry-only app config and `add` / `upgrade` install routes. **Done.** See [config.md](./config.md#registry-only-app-source), [lifecycle.md](./lifecycle.md), [tests.md](./tests.md#registry-only-app-tests).
13. Install-time validation before fleet accept: platform artifact, `gestaltd` compatibility, and declared app dependencies. No dedicated rollback API — revert via `upgrade` to an older published version. See [validation.md](./validation.md), [tests.md](./tests.md#install-time-validation-tests).
14. Admin observability for registry-only apps: read APIs for rollouts and per-replica materializations, and an App Registry section in the `/admin` UI. **Done.** See [admin.md](./admin.md), [lifecycle.md](./lifecycle.md#admin-observability-api), [tests.md](./tests.md#admin-observability-tests).
