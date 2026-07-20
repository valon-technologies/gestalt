# App Registry Config

Reference for app registry configuration in `gestaltd.yaml` / `config.yaml` from [gestalt#2709](https://github.com/valon-technologies/gestalt/pull/2709) commit 2.

- **`appRegistries`** in deploy config — where `gestaltd` reads app metadata and artifacts at deploy/runtime
- **`gestaltd app publish`** — takes `--bucket` on the CLI; no publisher config block

This does **not** change the app author's `manifest.yaml`.

Related docs:

- [plan.md](./plan.md) — product goals
- [lifecycle.md](./lifecycle.md) — replica startup, background controller, admin HTTP API
- [models.md](./models.md) — JSON documents stored in the registry bucket
- [service.md](./service.md) — Go API for building those documents

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
```

`gcs.bucket` accepts a bare bucket name or `gs://{bucket}`. Gestalt derives both URL forms:

- **Storage URL** — `gs://{bucket}` for gestaltd downloads
- **Public URL** — `https://storage.googleapis.com/{bucket}` for HTTPS links in registry entry JSON

### Publishing

CI publishes with CLI flags only:

```bash
gestaltd app publish \
  --bucket gs://gestalt-app-registry \
  --app g-issues \
  --version 0.0.1 \
  --ref abc123def456abc123def456abc123def456abcd \
  --dist-dir dist/
```

- `--app` — app name; manifest must live at `apps/{app}/manifest.yaml` under the git root
- `--bucket` — GCS bucket name or `gs://` URL for uploads; identifies the target registry for now

Immutability is enforced by `gestaltd app publish` (create-only uploads, no silent overwrites) — not by config.

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

gestaltd app publish \
  --bucket gs://gestalt-app-registry \
  --app g-issues \
  --version "0.0.0-snapshot.g${COMMIT_SHA}" \
  --ref "${COMMIT_SHA}" \
  --dist-dir dist/
```

- `--version` — must match the `version` embedded in each release archive from `provider package`
- `--ref` — 40-character git commit SHA for registry `sourceRef` and GCS upload metadata
- `--dist-dir` — directory containing `*.tar.gz` files from `provider package` (default output: `dist/`)

### Follow-up

`--ref` is passed at publish time today, but source commit provenance logically belongs with the build. A follow-up is to record the ref during packaging instead:

```bash
gestaltd provider package \
  --version "0.0.0-snapshot.g${COMMIT_SHA}" \
  --ref "${COMMIT_SHA}" \
  --platform linux/amd64,darwin/arm64 \
  --output dist/
```

`gestaltd app publish` would then read `sourceRef` from the release archives and drop `--ref`. That binds version to commit at build time and removes the chance of a mismatched ref at upload.

---

## Registry-only app source

Registry-managed apps declare the **app slot** in deploy config — authorization, indexeddb, static mount, MCP, and so on — without pinning a git ref or baking a snapshot into the gestaltd image. The running binary version comes from the app registry.

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
|-------|---------|
| `source.registry` | Registry name from `appRegistries`. Mutually exclusive with `source.git`, `source.path`, and other source modes. |

### Behavior

- **Config validation** — `source.registry` must name a configured `appRegistries` entry. No `ref`, `repo`, or `path` on the app entry.
- **`gestalt lock` / `gestalt sync`** — skip snapshot resolution and artifact download. The lockfile records the app name and registry binding only.
- **Bootstrap** — registry-only apps with an empty `ListKnownVersionsByApp` result are skipped by `StartAppProviders`. When a fleet-known version exists, start the latest (`LatestKnownVersion`) from `{artifactsDir}/registry-installed/{app}/{version}`. See [lifecycle.md](./lifecycle.md#registry-only-apps).
- **First install** — `POST …/install` records the fleet version in IndexedDB; replicas materialize and start the app from the registry. No deploy-time pin is required.
- **Upgrades** — catalog poller materializes the new artifact, then restarts the provider with the registry-mounted binary. See [lifecycle.md](./lifecycle.md#polling).

