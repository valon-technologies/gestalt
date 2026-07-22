# App Registry Models

Reference for the JSON documents stored in a GCS app registry by `gestaltd app publish`.

For deploy reader config (`appRegistries`), see [config.md](./config.md). For broader goals (runtime install, IndexedDB state, validation phases), see [plan.md](./plan.md). For the Go package API that reads and writes these documents, see [service.md](./service.md).

## Overview

The registry stores two kinds of JSON per app.

Immutable app archives live alongside published versions:

    apps/{app}/
    ├── index.json
    ├── versions/
    │   └── {version}.json
    └── artifacts/
        └── {version}/
            └── gestalt-app-{app}_v{version}_{platform}.tar.gz

| Document | Path | Purpose |
|----------|------|---------|
| **Index** | `apps/{app}/index.json` | Lightweight catalog of published versions |
| **PublishedVersion** | `apps/{app}/versions/{version}.json` | Full metadata and install contract for one version |

Document type is implied by path. Each JSON document has a root `schemaVersion` field (currently `1` for both index and published version). Readers should reject unsupported values; bump it when the JSON shape changes incompatibly.

Implementation: `gestaltd/internal/appregistry/`.

---

## Index

**Path:** `apps/{app}/index.json`

Answers: *what versions exist, where is their metadata, which platforms were published?*

```json
{
  "schemaVersion": 1,
  "apps": {
    "g-issues": {
      "displayName": "g-issues",
      "description": "Issues workspace",
      "versions": {
        "0.0.0-snapshot.gabc123": {
          "metadata": "apps/g-issues/versions/0.0.0-snapshot.gabc123.json",
          "platforms": ["linux/amd64"],
          "publishedAt": "2026-07-09T12:00:00Z"
        }
      }
    }
  }
}
```

### Object hierarchy

Nested objects are keyed by maps in JSON. Arrows show the Go type at each level — this is an object graph, not a file tree.

    Index
    ├── schemaVersion
    └── apps: map[appName] to AppVersions
        ├── displayName
        ├── description
        └── versions: map[version] to IndexVersion
            ├── metadata
            ├── platforms
            └── publishedAt

In a per-app index file (`apps/g-issues/index.json`), the `apps` map usually has one entry whose key matches the app in the path. The same shape can back a future global `index.json` with many apps.

### Fields

#### Root · `Index`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schemaVersion` | int | yes | Index document format version. Currently `1`. |
| `apps` | map | yes | App name → `AppVersions`. See object hierarchy above. |

#### `Index.apps` · `AppVersions`

Each key in `apps` is an app name (e.g. g-issues). The value is an `AppVersions` object.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `displayName` | string | no | Human-readable app name, copied from the manifest at publish time. |
| `description` | string | no | Optional app description from the manifest. |
| `versions` | map | yes | Version string → `IndexVersion`. See below. |

#### `Index.apps.{app}.versions` · `IndexVersion`

Each key in `versions` is a published version string (e.g. 0.0.0-snapshot.gabc123). The value is an `IndexVersion` summary for that release.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `metadata` | string | yes | Relative path to the full published version JSON, e.g. `apps/g-issues/versions/0.0.1.json`. |
| `platforms` | string array | no | Build targets available for this version (see example JSON). Derived from published version artifact keys at publish time. |
| `publishedAt` | RFC 3339 timestamp | yes | When this version was published (UTC). |

---

## PublishedVersion

**Path:** `apps/{app}/versions/{version}.json`

Answers: *what exactly is this version — artifacts, operations, dependencies, compatibility?*

```json
{
  "schemaVersion": 1,
  "app": "g-issues",
  "version": "0.0.0-snapshot.gabc123",
  "sourceRef": "abc123def456abc123def456abc123def456abcd",
  "manifestPath": "valon-tools/apps/g-issues/manifest.yaml",
  "repository": "github.com/valon-technologies/valon-tools",
  "artifacts": {
    "linux/amd64": {
      "url": "gs://gestalt-app-registry/apps/g-issues/artifacts/0.0.0-snapshot.gabc123/gestalt-app-g-issues_v0.0.0-snapshot.gabc123_linux_amd64.tar.gz",
      "publicUrl": "https://storage.googleapis.com/gestalt-app-registry/apps/g-issues/artifacts/0.0.0-snapshot.gabc123/gestalt-app-g-issues_v0.0.0-snapshot.gabc123_linux_amd64.tar.gz",
      "sha256": "..."
    }
  },
  "interface": {
    "operations": {
      "issues.list": {
        "inputSchema": { "type": "object" },
        "outputSchema": { "type": "object" }
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
  },
  "publishedAt": "2026-07-09T12:00:00Z"
}
```

### Object hierarchy

    PublishedVersion
    ├── schemaVersion
    ├── app
    ├── version
    ├── sourceRef
    ├── manifestPath
    ├── repository
    ├── publishedAt
    ├── artifacts: map[platform] to Artifact
    │   ├── url
    │   ├── publicUrl
    │   └── sha256
    ├── interface to Interface
    │   └── operations: map[opId] to OperationContract
    │       ├── inputSchema
    │       └── outputSchema
    ├── requires to Requires
    │   └── apps: map[appName] to AppRequirement
    │       ├── version
    │       └── operations: map[opId] to OperationRequirement
    │           └── inputSchemaHash
    └── compatibility to Compatibility
        └── minGestaltdVersion

### Fields

#### Root · `PublishedVersion`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schemaVersion` | int | yes | Published version document format version. Currently `1`. |
| `app` | string | yes | Short app id, e.g. `g-issues`. Must match the app segment in manifest `source` (`apps/{app}`). |
| `version` | string | yes | Published version identifier. Must pass Gestalt semver validation (including snapshot forms like `0.0.0-snapshot.gabc123`). |
| `sourceRef` | string | yes | 40-character lowercase git commit SHA the release was built from. |
| `manifestPath` | string | yes | Repo-relative path to `manifest.yaml`, e.g. `valon-tools/apps/g-issues/manifest.yaml`. |
| `repository` | string | yes | Source repository as `host/owner/repo`, e.g. `github.com/valon-technologies/valon-tools`. The manifest `source` must be `{repository}/apps/{app}`. |
| `artifacts` | map | yes | Platform target → `Artifact`. At least one artifact is required. |
| `interface` | object | no | Operations this app exposes. Copied from the provider release catalog at publish time. |
| `requires` | object | no | Declared dependencies on other apps. Copied from the provider release `staticValidation.requires` block at publish time. |
| `compatibility` | object | no | Runtime constraints, e.g. minimum `gestaltd` version. Copied from the provider release `staticValidation.compatibility` block at publish time. |
| `publishedAt` | RFC 3339 timestamp | yes | When this version was published (UTC). |

#### `PublishedVersion.artifacts` · `Artifact`

Each key is a platform target (e.g. `linux/amd64`, `darwin/arm64`). The value is download metadata for that archive.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | yes | GCS URI (`gs://...`) used by `gestaltd` to download the archive. |
| `publicUrl` | string | yes | HTTPS URL for external or browser access. |
| `sha256` | string | yes | Hex checksum of the archive bytes. Used for integrity verification on install. |

#### `PublishedVersion.interface` · `Interface`

Copied from the provider release catalog at publish time (not authored in the manifest).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `operations` | map | no | Operation id → `OperationContract`. See below. |

#### `PublishedVersion.interface.operations` · `OperationContract`

Each key in `operations` is an operation id (e.g. `issues.list`). The value describes the operation contract.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `inputSchema` | JSON | no | Full JSON Schema for operation inputs. |
| `outputSchema` | JSON | no | Full JSON Schema for operation outputs. |

#### `PublishedVersion.requires` · `Requires`

Copied from the provider release `staticValidation.requires` block at publish time (snapshotted from manifest `dependencies.apps` at release finalization).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `apps` | map | no | Dependency app name → `AppRequirement`. See below. |

#### `PublishedVersion.requires.apps` · `AppRequirement`

Each key in `apps` identifies a dependency app. The value describes what this app needs from that dependency.

**Key format.** Keys are copied verbatim from manifest `dependencies.apps` at publish time. They may be either:

| Form | Example |
|------|---------|
| Short fleet app name | `slack` |
| Full manifest source address | `github.com/valon-technologies/valon-tools/apps/slack` |

Fleet catalog entries, deploy config app slots, and registry object paths (`apps/{app}/…`) always use the **short name** (`slack`). Install-time validation normalizes source-address keys to the short name before fleet lookup and reverse-dependent matching. New manifests should prefer short names for readability, but both forms are valid in published JSON.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `version` | string | no | Semver constraint on the dependency app, e.g. ^1.4.0. Matched against fleet-known versions using snapshot-aware rules — see [validation.md](./validation.md#semver-constraint-matching). |
| `operations` | map | no | Operation id → `OperationRequirement`. See below. |

#### `PublishedVersion.requires.apps.{app}.operations` · `OperationRequirement`

Each key in `operations` is an operation id on the dependency app (e.g. `chat.postMessage`).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `inputSchemaHash` | string | no | Expected input shape as sha256:{hex}. Install-time validation can compare this against the dependency published version's `interface` schema without embedding the full schema twice. |

#### `PublishedVersion.compatibility` · `Compatibility`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `minGestaltdVersion` | string | no | Lowest `gestaltd` version this app supports. |
