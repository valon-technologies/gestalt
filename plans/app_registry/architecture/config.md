# App Registry Config

Reference for app registry configuration in `gestaltd.yaml` / `config.yaml`.

- **`appRegistries`** in deploy config — where `gestaltd` reads app metadata and artifacts at deploy/runtime
- **`gestaltd app registry publish`** — takes `--bucket` on the CLI; no publisher config block. Replaces `gestaltd app publish`.

This does **not** change the app author's `manifest.yaml`.

Implementation: `gestaltd/internal/config/` (`AppRegistryConfig`, `validateAppRegistries`).

---

## Overview

`appRegistries` is a map of named registries. Each entry is a **discriminated union** on `kind`:

- `kind: gcs` — payload under `gcs`

`gestaltd` uses this block when it deploys and serves: discover versions, fetch `index.json` / `versions/*.json`, download artifacts.

```yaml
apiVersion: gestaltd.config/v8

appRegistries:
  toolshed:
    kind: gcs
    gcs:
      bucket: gs://gestalt-app-registry
    retention:
      unusedRetention: 72h
      deployedRetention: 720h
```

See [retention.md](../operations/retention.md) for policy semantics and the `retention.json` overlay. On publish, `unusedRetention` is written as `expiresAt = publishedAt + unusedRetention`. When a version stops being desired, `deployedRetention` is written as `expiresAt = now + deployedRetention` at that transition. Redeploying clears `expiresAt`; a later deactivation overwrites it with a fresh `now + deployedRetention`. Config changes apply on the next write only, not retroactively to an existing `expiresAt`. Audit metadata remains permanent after the deployability window closes.

`gcs.bucket` accepts a bare bucket name or `gs://{bucket}`. Gestalt derives both URL forms:

- **Storage URL** — `gs://{bucket}` for gestaltd downloads
- **Public URL** — `https://storage.googleapis.com/{bucket}` for HTTPS links in registry entry JSON

### Publishing

CI publishes with CLI flags only:

```bash
gestaltd app registry publish \
  --bucket gs://gestalt-app-registry \
  --app g-issues \
  --version 0.0.1 \
  --ref abc123def456abc123def456abc123def456abcd \
  --dist-dir dist/
```

- `--app` — app name; manifest must live at `apps/{app}/manifest.yaml` under the git root
- `--bucket` — GCS bucket name or `gs://` URL for uploads; identifies the target registry for now

Immutability is enforced by `gestaltd app registry publish` (create-only uploads, no silent overwrites) — not by config.

Upload layout:

```
gs://{bucket}/apps/{app}/index.json
gs://{bucket}/apps/{app}/versions/{version}.json
gs://{bucket}/apps/{app}/artifacts/{version}/*.tar.gz
```

Published registry entry JSON stores both derived forms per artifact: `url` (`gs://...`) and `publicUrl` (`https://storage.googleapis.com/...`).

Full CI flow today:

```bash
cd valon-tools/apps/g-issues

gestaltd provider package \
  --version "0.0.0-snapshot.g${COMMIT_SHA}" \
  --platform linux/amd64,darwin/arm64 \
  --output dist/

gestaltd app registry publish \
  --bucket gs://gestalt-app-registry \
  --app g-issues \
  --version "0.0.0-snapshot.g${COMMIT_SHA}" \
  --ref "${COMMIT_SHA}" \
  --dist-dir dist/
```

- `--version` — must match the `version` embedded in each release archive from `provider package`
- `--ref` — 40-character git commit SHA for registry `sourceRef` and GCS upload metadata
- `--dist-dir` — directory containing `*.tar.gz` files from `provider package` (default output: `dist/`)

### Follow-Up

`--ref` is passed at publish time today, but source commit provenance logically belongs with the build. A follow-up is to record the ref during packaging instead:

```bash
gestaltd provider package \
  --version "0.0.0-snapshot.g${COMMIT_SHA}" \
  --ref "${COMMIT_SHA}" \
  --platform linux/amd64,darwin/arm64 \
  --output dist/
```

`gestaltd app registry publish` would then read `sourceRef` from the release archives and drop `--ref`. That binds version to commit at build time and removes the chance of a mismatched ref at upload.

---

## Registry-Only App Source

Registry-managed apps declare the **app slot** in deploy config — authorization, IndexedDB, static mounts, MCP, and so on — without pinning a git ref or baking a snapshot into the gestaltd image. The running binary version comes from the app registry.

```yaml
apps:
  g-issues:
    authorizationPolicy: gIssues
    indexeddb:
      provider: main
      db: g_issues
    static:
      mount: /g-issues
    config:
      appBasePath: /g-issues
    mcp: true
    source:
      registry: toolshed
```

| Field | Meaning |
| --- | --- |
| `source.registry` | Registry name from `appRegistries`. Must name a configured entry. Mutually exclusive with `source.git`, `source.path`, and other source modes. |

`source.registry` is valid only for entries under `apps`; runtime-provider entries cannot use it. Across upgrades, the app's entry in the Gestalt YAML deploy configuration remains authoritative for how the app integrates with Gestalt. The registry package supplies the version-specific executable and static assets:

| Gestalt YAML fields | Gestalt YAML responsibility | Registry package responsibility |
| --- | --- | --- |
| `authorizationPolicy`, `indexeddb`, `mcp`, `http`, and `config` | Defines the app's capabilities and integration settings. These remain unchanged across package upgrades. | None — controlled entirely by Gestalt YAML. |
| `static.mount`, visibility, and theme | Defines the URL, access policy, and theme for the app's UI. These remain unchanged across package upgrades. | Supplies the version-specific HTML, JavaScript, CSS, and other static assets served at that URL. |
| `source.registry` | Defines the registry from which this app may be installed. | Must come from that registry. Catalog history associated with a different registry is not eligible to run. |

Because no package exists during config loading, validation must not require a resolved provider manifest, operation catalog, or static root for a registry-only app.

`gestalt lock` and `gestalt sync` skip snapshot resolution and artifact download for registry-only apps. Runtime behavior — bootstrap, `add`, and `upgrade` — is documented in [lifecycle.md](../operations/lifecycle.md).

### Reconciliation Retry Limit

```yaml
server:
  appRegistry:
    maxReconcileAttempts: 3
```

`server.appRegistry.maxReconcileAttempts` is the maximum failed background-poller reconciliation attempts for one `(replica, app, desired version)`. It defaults to `3` and must be a positive integer. When the corresponding `app_instance_materializations.attempt_count` reaches this value, the poller stops retrying materialization and provider lifecycle work for that desired version.

This limit does not apply to bootstrap. Bootstrap never consults rollout-progress rows or `attempt_count`, so a process restart still attempts to start the desired version selected by `LatestKnownVersion`. If bootstrap succeeds, the poller may record that observed convergence despite its retry limit because no additional materialization or provider lifecycle attempt is required.

A newly accepted desired version uses a new materialization row and starts with zero failed attempts. Increasing the configured limit allows rows below the new limit to resume retrying.

### Lockfile

Registry-only apps appear in `gestalt.lock.json` with a registry binding and no baked artifacts:

```json
{
  "source": "registry",
  "sourceRef": {
    "type": "registry",
    "resolvedGestaltRef": "toolshed"
  }
}
```

---

## Appendix

### Related Changelogs

<pre>
├── <a href="../project/changelog.md#changelog-01">01 — GCS Registry and Publish Command</a>
└── <a href="../project/changelog.md#changelog-12">12 — Complete Registry-Only Lifecycle</a>
</pre>

### Related Docs

<pre>
├── <a href="../readme.md">readme.md</a> — architecture and future work
├── <a href="../project/changelog.md">changelog.md</a> — implementation milestones and pull requests
├── <a href="../operations/lifecycle.md">lifecycle.md</a> — replica startup, background controller, admin HTTP API
├── <a href="./models.md">models.md</a> — JSON documents stored in the registry bucket
└── <a href="../operations/retention.md">retention.md</a> — version cleanup policy and retention config
</pre>
