# App Registry Models

Reference for the JSON documents stored in a GCS app registry by `gestaltd app registry publish`.

## Overview

The registry stores published release JSON plus mutable pending and failed catalogs per app.

Immutable app archives live alongside published versions:

    apps/{app}/
    ├── index.json
    ├── retention.json
    ├── pending.json
    ├── failed.json
    ├── versions/
    │   └── {version}.json
    └── artifacts/
        └── {version}/
            └── gestalt-app-{app}_v{version}_{platform}.tar.gz

| Document | Path | Purpose |
| --- | --- | --- |
| **Index** | `apps/{app}/index.json` | Lightweight catalog of published versions |
| **RetentionIndex** | `apps/{app}/retention.json` | Mutable usage timestamps for retention pruning. See [retention.md](../operations/retention.md). |
| **PendingIndex** | `apps/{app}/pending.json` | In-flight publishes (not installable). See [pending-publish.md](../operations/pending-publish.md). |
| **FailedIndex** | `apps/{app}/failed.json` | Recent failed publishes (not installable). See [pending-publish.md](../operations/pending-publish.md). |
| **PublishedVersion** | `apps/{app}/versions/{version}.json` | Full metadata and install contract for one version |

Document type is implied by path. Each JSON document has a root `schemaVersion` field (currently `1` for index, pending index, and published version). Readers should reject unsupported values; bump it when the JSON shape changes incompatibly.

Implementation: `gestaltd/internal/appregistry/`.

---

## Index

**Path:** `apps/{app}/index.json`

Answers: _what versions exist, where is their metadata, which platforms were published, and what commit/workflow published each version?_

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
          "publishedAt": "2026-07-09T12:00:00Z",
          "publishStartedAt": "2026-07-09T11:55:28Z",
          "sourceRef": "abc123def456abc123def456abc123def456abcd",
          "repository": "github.com/valon-technologies/valon-tools",
          "publication": {
            "workflowRunUrl": "https://github.com/valon-technologies/valon-tools/actions/runs/123456789",
            "triggerPullRequest": {
              "number": 3251,
              "url": "https://github.com/valon-technologies/valon-tools/pull/3251"
            }
          }
        }
      }
    }
  }
}
```

### Object Hierarchy

Nested objects are keyed by maps in JSON. Arrows show the Go type at each level — this is an object graph, not a file tree.

    Index
    ├── schemaVersion
    └── apps: map[appName] to AppVersions
        ├── displayName
        ├── description
        └── versions: map[version] to IndexVersion
            ├── metadata
            ├── platforms
            ├── publishedAt
            ├── publishStartedAt
            ├── sourceRef
            ├── repository
            └── publication

In a per-app index file (`apps/g-issues/index.json`), the `apps` map usually has one entry whose key matches the app in the path. The same shape can back a future global `index.json` with many apps.

### Fields

#### Root · `Index`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `schemaVersion` | int | yes | Index document format version. Currently `1`. |
| `apps` | map | yes | App name → `AppVersions`. See object hierarchy above. |

#### `Index.apps` · `AppVersions`

Each key in `apps` is an app name (e.g. g-issues). The value is an `AppVersions` object.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `displayName` | string | no | Human-readable app name, copied from the manifest at publish time. |
| `description` | string | no | Optional app description from the manifest. |
| `versions` | map | yes | Version string → `IndexVersion`. See below. |

#### `Index.apps.{app}.versions` · `IndexVersion`

Each key in `versions` is a published version string (e.g. 0.0.0-snapshot.gabc123). The value is an `IndexVersion` summary for that release.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `metadata` | string | yes | Relative path to the full published version JSON, e.g. `apps/g-issues/versions/0.0.1.json`. |
| `platforms` | string array | no | Build targets available for this version (see example JSON). Derived from published version artifact keys at publish time. |
| `publishedAt` | RFC 3339 timestamp | yes | When this version was published (UTC). |
| `publishStartedAt` | RFC 3339 timestamp | no | When CI recorded the version as pending. Copied from `PendingVersion.startedAt` at publish time. Present on CI publishes after `pending set`; omitted on legacy entries and manual publishes without a pending entry. See [pending-publish.md](../operations/pending-publish.md#publish-duration). |
| `sourceRef` | string | no | Packaged commit SHA. Copied from `PublishedVersion.sourceRef`. |
| `repository` | string | no | Source repository. Copied from `PublishedVersion.repository`. |
| `publication` | object | no | Publish workflow provenance. Copied from `PublishedVersion.publication`. |

---

## PendingIndex

**Path:** `apps/{app}/pending.json`

Answers: _which versions are currently being published for this app, and what provenance is known so far?_

Mutable catalog updated by CI at publish start. Removed on successful publish via `gestaltd app registry pending clear`, or recorded in `failed.json` via `gestaltd app registry pending fail` or stale prune. Pending versions are **not** installable. See [pending-publish.md](../operations/pending-publish.md).

```json
{
  "schemaVersion": 1,
  "app": "traffic-cop",
  "pending": {
    "0.0.0-snapshot.gabc123def456abc123def456abc123def456abcd": {
      "version": "0.0.0-snapshot.gabc123def456abc123def456abc123def456abcd",
      "sourceRef": "abc123def456abc123def456abc123def456abcd",
      "repository": "github.com/valon-technologies/toolshed",
      "startedAt": "2026-07-24T19:00:00Z",
      "updatedAt": "2026-07-24T19:04:12Z",
      "phase": "publishing",
      "publication": {
        "workflowRunUrl": "https://github.com/valon-technologies/toolshed/actions/runs/123456789",
        "triggerPullRequest": {
          "number": 3740,
          "url": "https://github.com/valon-technologies/toolshed/pull/3740",
          "title": "Wire traffic-cop to app registry"
        }
      }
    }
  }
}
```

For comparison, the same app's `index.json` entry for a completed publish uses `apps` nesting and `publishedAt` / `metadata` instead of `phase` / `startedAt`. See [Index](#index).

### Object Hierarchy

    PendingIndex
    ├── schemaVersion
    ├── app
    └── pending: map[version] to PendingVersion
        ├── version
        ├── sourceRef
        ├── repository
        ├── startedAt
        ├── updatedAt
        ├── phase
        └── publication

### Fields

#### Root · `PendingIndex`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `schemaVersion` | int | yes | Pending index document format version. Start at `1`. |
| `app` | string | yes | App name. Must match the `{app}` path segment. |
| `pending` | map | yes | Version string → `PendingVersion`. Empty map when no publishes are in flight. |

#### `PendingIndex.pending` · `PendingVersion`

Each key in `pending` is a version string being published (e.g. `0.0.0-snapshot.gabc123`). The value is a `PendingVersion` summary.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `version` | string | yes | Snapshot version being published. Same semver rules as published versions. |
| `sourceRef` | string | yes | Commit SHA the publish is building from. |
| `repository` | string | no | Source repository. Same format as `IndexVersion.repository`. |
| `startedAt` | RFC 3339 timestamp | yes | When the pending record was created (UTC). |
| `updatedAt` | RFC 3339 timestamp | yes | Last phase or metadata update (UTC). |
| `phase` | string | yes | Always `publishing` while the version is pending. See [pending-publish.md](../operations/pending-publish.md#phases). |
| `publication` | object | no | Same `Publication` shape as `PublishedVersion.publication`. Written at pending start so the UI can link the workflow run immediately. |

Writes use the same optimistic-concurrency pattern as `index.json` (read GCS generation, merge, upload with `if-generation-match`). `gestaltd app registry pending set` runs `PrunePendingIndex` and `PruneFailedIndex` before upserting. See [pending-publish.md](../operations/pending-publish.md#self-healing).

---

## FailedIndex

**Path:** `apps/{app}/failed.json`

Answers: _which recent publish attempts failed for this app, and when?_

Mutable catalog updated when CI calls `gestaltd app registry pending fail` or when `PrunePendingIndex` moves a stale pending entry. Failed versions are **not** installable. See [pending-publish.md](../operations/pending-publish.md).

```json
{
  "schemaVersion": 1,
  "app": "traffic-cop",
  "failed": {
    "0.0.0-snapshot.gabc123def456abc123def456abc123def456abcd": {
      "version": "0.0.0-snapshot.gabc123def456abc123def456abc123def456abcd",
      "sourceRef": "abc123def456abc123def456abc123def456abcd",
      "repository": "github.com/valon-technologies/toolshed",
      "startedAt": "2026-07-24T19:00:00Z",
      "failedAt": "2026-07-24T19:35:00Z",
      "reason": "stale",
      "publication": {
        "workflowRunUrl": "https://github.com/valon-technologies/toolshed/actions/runs/123456789",
        "triggerPullRequest": {
          "number": 3740,
          "url": "https://github.com/valon-technologies/toolshed/pull/3740",
          "title": "Wire traffic-cop to app registry"
        }
      }
    }
  }
}
```

### Object Hierarchy

    FailedIndex
    ├── schemaVersion
    ├── app
    └── failed: map[version] to FailedVersion
        ├── version
        ├── sourceRef
        ├── repository
        ├── startedAt
        ├── failedAt
        ├── reason
        └── publication

### Fields

#### Root · `FailedIndex`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `schemaVersion` | int | yes | Failed index document format version. Start at `1`. |
| `app` | string | yes | App name. Must match the `{app}` path segment. |
| `failed` | map | yes | Version string → `FailedVersion`. Empty map when there are no recent failures. |

#### `FailedIndex.failed` · `FailedVersion`

Each key is a version string whose publish attempt failed.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `version` | string | yes | Snapshot version that failed to publish. |
| `sourceRef` | string | yes | Commit SHA the publish was building from. |
| `repository` | string | no | Source repository. Same format as `IndexVersion.repository`. |
| `startedAt` | RFC 3339 timestamp | yes | When the pending record was created (copied from pending). |
| `failedAt` | RFC 3339 timestamp | yes | When the failure was recorded (UTC). |
| `reason` | string | yes | `workflow_failed` — CI called `pending fail`. `stale` — pending `startedAt` exceeded 30 minutes during `PrunePendingIndex`. |
| `publication` | object | no | Same `Publication` shape as `PublishedVersion.publication`. Copied from the pending row when present. |

Writes use the same optimistic-concurrency pattern as `pending.json`. `PruneFailedIndex` removes entries older than 30 days on `gestaltd app registry pending set`. See [pending-publish.md](../operations/pending-publish.md#prunefailedindex).

---

## RetentionIndex

**Path:** `apps/{app}/retention.json`

Answers: _was this version deployed, when did it last stop being desired, and is it still eligible for historical redeployment?_

Mutable overlay used by retention pruning. Not installable metadata. See [retention.md](../operations/retention.md) for policy rules and cleanup scope.

```json
{
  "schemaVersion": 1,
  "versions": {
    "0.0.0-snapshot.gabc123": {
      "publishedAt": "2026-07-20T12:00:00Z",
      "lastDeactivatedAt": "2026-07-22T14:00:00Z",
      "deployableUntil": "2026-08-21T14:00:00Z",
      "firstDeployedAt": "2026-07-22T14:00:00Z",
      "everDeployed": true
    }
  }
}
```

### Fields

#### Root · `RetentionIndex`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `schemaVersion` | int | yes | Retention overlay format version. Currently `1`. |
| `versions` | map | yes | Version string → `RetentionVersion`. |

#### `RetentionIndex.versions` · `RetentionVersion`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `publishedAt` | RFC 3339 timestamp | yes | Starts the unused-version retention window. |
| `lastDeactivatedAt` | RFC 3339 timestamp | no | Most recent time this version stopped being desired. Omitted while it has never been deactivated. |
| `deployableUntil` | RFC 3339 timestamp | no | Historical-redeploy deadline captured as `lastDeactivatedAt + deployedRetention` for the current deactivation interval. Cleared while this version is desired. |
| `firstDeployedAt` | RFC 3339 timestamp | no | First fleet admission. |
| `everDeployed` | bool | yes | Sticky flag; once true, deploy-chain records and version metadata are permanently protected from pruning. |
| `lockedAt` | RFC 3339 timestamp | no | Sticky timestamp recorded after `deployableUntil`; the version can no longer become desired. |

---

## PublishedVersion

**Path:** `apps/{app}/versions/{version}.json`

Answers: _what exactly is this version — artifacts, operations, dependencies, compatibility?_

```json
{
  "schemaVersion": 1,
  "app": "g-issues",
  "version": "0.0.0-snapshot.gabc123",
  "sourceRef": "abc123def456abc123def456abc123def456abcd",
  "manifestPath": "valon-tools/apps/g-issues/manifest.yaml",
  "repository": "github.com/valon-technologies/valon-tools",
  "publication": {
    "workflowRunUrl": "https://github.com/valon-technologies/valon-tools/actions/runs/123456789",
    "triggerPullRequest": {
      "number": 3251,
      "url": "https://github.com/valon-technologies/valon-tools/pull/3251"
    }
  },
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
  "publishedAt": "2026-07-09T12:00:00Z",
  "publishStartedAt": "2026-07-09T11:55:28Z"
}
```

### Object Hierarchy

    PublishedVersion
    ├── schemaVersion
    ├── app
    ├── version
    ├── sourceRef
    ├── manifestPath
    ├── repository
    ├── publishedAt
    ├── publishStartedAt
    ├── publication
    │   ├── workflowRunUrl
    │   ├── triggerPullRequest
    │   │   ├── number
    │   │   └── url
    │   └── triggerCommit
    │       ├── sha
    │       └── url
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
| --- | --- | --- | --- |
| `schemaVersion` | int | yes | Published version document format version. Currently `1`. |
| `app` | string | yes | Short app id, e.g. `g-issues`. Must match the app segment in manifest `source` (`apps/{app}`). |
| `version` | string | yes | Published version identifier. Must pass Gestalt semver validation (including snapshot forms like `0.0.0-snapshot.gabc123`). |
| `sourceRef` | string | yes | 40-character lowercase git commit SHA the release was built from. |
| `manifestPath` | string | yes | Repo-relative path to `manifest.yaml`, e.g. `valon-tools/apps/g-issues/manifest.yaml`. |
| `repository` | string | yes | Source repository as `host/owner/repo`, e.g. `github.com/valon-technologies/valon-tools`. The manifest `source` must be `{repository}/apps/{app}`. |
| `publication` | object | no | Publish workflow run and trigger. Required for new publishes; omitted on legacy entries. |
| `artifacts` | map | yes | Platform target → `Artifact`. At least one artifact is required. |
| `interface` | object | no | Operations this app exposes. Copied from the provider release catalog at publish time. |
| `requires` | object | no | Declared dependencies on other apps. Copied from the provider release `staticValidation.requires` block at publish time. |
| `compatibility` | object | no | Runtime constraints, e.g. minimum `gestaltd` version. Copied from the provider release `staticValidation.compatibility` block at publish time. |
| `publishedAt` | RFC 3339 timestamp | yes | When this version was published (UTC). |
| `publishStartedAt` | RFC 3339 timestamp | no | When CI recorded the version as pending. Copied from `PendingVersion.startedAt` at publish time. Present on CI publishes after `pending set`; omitted on legacy entries and manual publishes without a pending entry. See [pending-publish.md](../operations/pending-publish.md#publish-duration). |

#### `PublishedVersion.publication` · `Publication`

Recorded by the publish workflow. Not looked up at read time.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `workflowRunUrl` | string | yes | GitHub Actions run that performed the publish. |
| `triggerPullRequest` | object | one-of | `{ number, url }` when publish was triggered by a PR. |
| `triggerCommit` | object | one-of | `{ sha, url }` when publish was triggered by a direct commit. |

Exactly one trigger variant is required. `sourceRef` is the packaged commit and may differ from `triggerCommit.sha`.

#### `PublishedVersion.artifacts` · `Artifact`

Each key is a platform target (e.g. `linux/amd64`, `darwin/arm64`). The value is download metadata for that archive.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `url` | string | yes | GCS URI (`gs://...`) used by `gestaltd` to download the archive. |
| `publicUrl` | string | yes | HTTPS URL for external or browser access. |
| `sha256` | string | yes | Hex checksum of the archive bytes. Used for integrity verification on install. |

#### `PublishedVersion.interface` · `Interface`

Copied from the provider release catalog at publish time (not authored in the manifest).

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `operations` | map | no | Operation id → `OperationContract`. See below. |

#### `PublishedVersion.interface.operations` · `OperationContract`

Each key in `operations` is an operation id (e.g. `issues.list`). The value describes the operation contract.

| Field          | Type | Required | Description                             |
| -------------- | ---- | -------- | --------------------------------------- |
| `inputSchema`  | JSON | no       | Full JSON Schema for operation inputs.  |
| `outputSchema` | JSON | no       | Full JSON Schema for operation outputs. |

#### `PublishedVersion.requires` · `Requires`

Copied from the provider release `staticValidation.requires` block at publish time (snapshotted from manifest `dependencies.apps` at release finalization).

| Field  | Type | Required | Description                                       |
| ------ | ---- | -------- | ------------------------------------------------- |
| `apps` | map  | no       | Dependency app key → `AppRequirement`. See below. |

#### `PublishedVersion.requires.apps` · `AppRequirement`

Each key in `apps` identifies a dependency app. Keys are copied verbatim from manifest `dependencies.apps` at publish time. A key may be a short fleet app name (`slack`) or a full manifest source address (`github.com/valon-technologies/valon-tools/apps/slack`). Fleet catalog entries, deploy config app slots, and registry object paths (`apps/{app}/…`) always use the short name. Install-time validation normalizes source-address keys before fleet lookup and reverse-dependent matching — see [validation.md](./validation.md#requiresapps-keys).

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `version` | string | no | Semver constraint on the dependency app, e.g. ^1.4.0. Matched against fleet-known versions using snapshot-aware rules — see [validation.md](./validation.md#semver-constraint-matching). |
| `operations` | map | no | Operation id → `OperationRequirement`. See below. |

#### `PublishedVersion.requires.apps.{app}.operations` · `OperationRequirement`

Each key in `operations` is an operation id on the dependency app (e.g. `chat.postMessage`).

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `inputSchemaHash` | string | no | Expected input shape as sha256:{hex}. Install-time validation can compare this against the dependency published version's `interface` schema without embedding the full schema twice. |

#### `PublishedVersion.compatibility` · `Compatibility`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `minGestaltdVersion` | string | no | Lowest `gestaltd` version this app supports. |

---

## Appendix

### Related Changelogs

<pre>
├── <a href="../project/changelog.md#changelog-01">01 — GCS registry and publish command</a>
└── <a href="../project/changelog.md#changelog-16">16 — Pending and failed publish visibility</a>
</pre>

### Related Docs

<pre>
├── <a href="./config.md">config.md</a> — deploy reader configuration
├── <a href="../readme.md">readme.md</a> — broader architecture
└── <a href="../project/changelog.md">changelog.md</a> — implementation history
</pre>
