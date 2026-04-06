# Python SDK V1 Proposal

Status: Proposed

Related:

- Source-plugin authoring direction in `valon-technologies/gestalt#585`
- Existing hybrid provider example: `github.com/valon-technologies/gestalt-plugins/bigquery`

## Summary

This proposal adds a first-party Python SDK for executable Gestalt plugins with
the same high-level authoring shape that PR `#585` introduces for Go:

- `plugin.yaml` remains the source of truth for static metadata, auth, and
  passthrough surfaces
- Python code defines only executable helper operations
- local development uses `from.source.path`
- packaging and release produce a normal executable plugin artifact
- deployers do not need Python installed in the `gestaltd` runtime image

V1 intentionally keeps packaging backend details private. Python authors do not
configure PyInstaller or any other freezer directly. If a plugin cannot be
packaged by the default Gestalt Python pipeline, it is unsupported in V1.

## Goals

- Match the simplified authoring model being introduced for Go in PR `#585`
- Make a `bigquery`-style hybrid provider easy to write in Python
- Keep deployer UX language-agnostic
- Ship packaged Python plugins as executable artifacts, not interpreter-backed
  source trees
- Avoid exposing backend-specific packaging knobs in the public API

## Non-Goals

- Public PyInstaller configuration
- Host-Python-backed packaged plugins
- Container-per-plugin execution
- Multi-runtime deployer configuration in `gestaltd`
- Guaranteeing support for every Python dependency or packaging pattern

## Design Principles

1. Authoring language is not deployment runtime.
   Python authors write Python, but deployers consume a normal executable plugin
   package.

2. `plugin.yaml` is authoritative for static contract.
   Display name, description, icon, auth, connections, and passthrough
   surfaces stay in the manifest, not in Python code.

3. Python code owns only executable behavior.
   The SDK should make authors define handler functions and typed input/output
   models, not raw protocol servers.

4. Packaging backend is an implementation detail.
   Gestalt may use PyInstaller internally in V1, but the public authoring model
   should not leak that choice.

5. Source-plugin lifecycle is shared across languages.
   The Python SDK plugs into the same source-plugin lifecycle as the new Go
   authoring model from PR `#585`. Python just needs a different internal build
   backend.

## User-Facing Model

### Project Layout

```text
my-plugin/
  plugin.yaml
  pyproject.toml
  provider.py
  assets/
```

### `plugin.yaml`

`plugin.yaml` remains the source of truth for static provider behavior.

Example:

```yaml
source: github.com/valon-technologies/gestalt-plugins/bigquery
version: 0.1.0
display_name: BigQuery
description: Google BigQuery data warehouse
icon_file: assets/icon.svg

provider:
  connections:
    default:
      auth:
        type: oauth2
        authorization_url: https://accounts.google.com/o/oauth2/v2/auth
        token_url: https://oauth2.googleapis.com/token
        scopes:
          - https://www.googleapis.com/auth/bigquery

  surfaces:
    rest:
      base_url: https://bigquery.googleapis.com/bigquery/v2
      operations:
        - name: list_datasets
          description: List datasets in a project
          method: GET
          path: /projects/{project_id}/datasets
          parameters:
            - name: project_id
              type: string
              in: path
              required: true
```

What stays in `plugin.yaml`:

- display name and description
- icon
- auth and connection model
- config schema
- passthrough surfaces (`rest`, `openapi`, `graphql`, `mcp`)
- allowed operation aliases and response mapping

What does not go in `plugin.yaml`:

- Python entrypoint command lines
- freezer/backend settings
- generated executable operation catalog

### `pyproject.toml`

Python source plugins declare their SDK entrypoint in `pyproject.toml`.

Example:

```toml
[project]
name = "gestalt-bigquery"
version = "0.1.0"
dependencies = [
  "gestalt-sdk-python",
  "google-cloud-bigquery",
]

[tool.gestalt]
plugin = "provider:plugin"
```

This gives Gestalt a language-specific way to locate the plugin object in a
source tree without adding Python-specific fields to `plugin.yaml`.

V1 only needs one public setting:

- `tool.gestalt.plugin`: import target in `module:attribute` form

No public packaging settings are part of the V1 contract.

### Python Authoring API

The Python SDK should be manifest-aware and handler-based.

Example:

```python
import gestalt

plugin = gestalt.Plugin.from_manifest("plugin.yaml")


class QueryInput(gestalt.Model):
    project_id: str = gestalt.field(description="GCP project ID")
    sql: str = gestalt.field(description="SQL query to execute")
    max_results: int = gestalt.field(default=1000)
    timeout_ms: int = gestalt.field(default=30000)
    use_legacy_sql: bool = gestalt.field(default=False)


class QueryOutput(gestalt.Model):
    schema: list[dict]
    rows: list[dict]
    total_rows: int
    job_complete: bool = True


@plugin.configure
def configure(name: str, config: dict) -> None:
    pass


@plugin.operation(
    id="query",
    method="POST",
    description="Execute a BigQuery SQL query",
    read_only=True,
)
async def query(input: QueryInput, req: gestalt.Request) -> QueryOutput:
    rows, schema, total_rows = await run_bigquery(
        project_id=input.project_id,
        sql=input.sql,
        access_token=req.token,
        max_results=input.max_results,
        timeout_ms=input.timeout_ms,
        use_legacy_sql=input.use_legacy_sql,
    )
    return QueryOutput(
        schema=schema,
        rows=rows,
        total_rows=total_rows,
    )
```

### Public Python SDK Primitives

V1 should expose only these author-facing primitives:

- `Plugin.from_manifest(path)`
- `@plugin.configure`
- `@plugin.operation(...)`
- optional `@plugin.session_catalog`
- `Model`
- `field(...)`
- `Request`
- typed return values or `Result.ok(...)`
- a small error type for structured failures

`Request` should expose:

- `token`
- `connection_params`
- `connection_param(name)`

The SDK should not require authors to handle:

- raw gRPC plumbing
- Unix socket setup
- protocol version constants
- generated catalog YAML
- freezer/backend invocations

### Derived Executable Catalog

Like the typed Go router in PR `#585`, the Python SDK should derive the
executable operation catalog from handler registrations and typed input models.

The executable catalog is generated from Python code and remains distinct from
the static contract defined in `plugin.yaml`.

This means:

- manifest-backed passthrough operations still come from `plugin.yaml`
- executable helper operations come from Python registration
- a hybrid provider is the merge of both

Authors should not hand-write a separate `catalog.yaml` in V1.

## Runtime Model

### Local Source Execution

Python source plugins should use the same local config shape as the Go source
authoring model from PR `#585`:

```yaml
providers:
  bigquery:
    from:
      source:
        path: ./plugin.yaml
```

`gestaltd serve` is responsible for preparing a runnable local artifact from the
source tree before starting the plugin process. This is an internal build step,
not a public user-facing concept.

### Packaging And Release

Python source plugins should use the same public commands as other source
plugins:

```sh
gestaltd plugin package --input ./bigquery --output ./dist/bigquery.tar.gz
gestaltd plugin release --version 0.1.0
```

The result is a normal executable plugin package with a manifest, executable
artifact, and any bundled assets required by the internal Python packaging
backend.

### Deployment

Packaged Python plugins are deployed like any other executable plugin:

- `from.package` for local archives/directories
- `from.source.ref` plus `version` for managed plugins

Deployers do not need Python in the `gestaltd` image in packaged or released
mode. The deployer contract remains:

- install `gestaltd`
- install the plugin package
- run `gestaltd`

## Internal Implementation Contract

This section describes internal boundaries only. It is not a public API.

### Language Backend Selection

When `gestaltd` sees `from.source.path`, it should select a source-plugin
backend based on the contents of the source tree:

- Go backend if the tree matches the Go source-plugin contract from PR `#585`
- Python backend if `pyproject.toml` contains `tool.gestalt.plugin`

### Internal Python Backend Responsibilities

The internal Python source-plugin backend is responsible for:

1. creating an isolated build environment
2. installing declared dependencies
3. importing the configured plugin object
4. deriving the executable operation catalog from registered handlers
5. building an executable artifact from the source tree
6. returning the prepared artifact path and generated executable catalog

The output of that backend should be whatever `gestaltd` already expects for an
executable plugin:

- executable artifact path
- generated executable catalog
- packaged files/assets as needed

### Packaging Backend

V1 uses a single internal packaging backend for Python source plugins.

Current assumption:

- the backend is PyInstaller or an equivalent freezer

Public contract:

- none

Implications:

- Python authors do not configure backend flags
- Gestalt may change the internal backend later without changing the Python SDK
  authoring model

## Support Boundary

V1 should state this explicitly:

- supported: Python plugins that can be packaged successfully by Gestalt's
  default Python packaging pipeline
- unsupported: plugins that require author-specified backend configuration,
  host-Python deployment, or containerized execution

This is the necessary tradeoff for keeping the public interface simple and
backend-agnostic.

## Multi-Platform Release Semantics

Go can cross-compile conveniently. Python freezing generally cannot.

Therefore V1 should define Python release behavior like this:

- `gestaltd plugin release` supports building the current platform on the
  current runner
- requesting unsupported target platforms errors clearly
- multi-platform Python releases are assembled by running release in a CI matrix
  across target platforms

This keeps the public command consistent without pretending Python source
plugins are cross-compiled the same way as Go binaries.

## Why This Fits `bigquery`

The existing `bigquery` plugin already has the target shape:

- static OAuth and REST surfaces in `plugin.yaml`
- one custom executable operation (`query`)

The Python SDK V1 makes that shape first-class:

- manifest defines static metadata and passthrough surfaces
- Python defines only the custom helper operation
- generated executable catalog replaces hand-authored executable catalog files

That is exactly the sort of plugin this proposal should optimize for.

## Open Questions

1. Should the Python SDK ship its own lightweight `Model` implementation, or
   standardize on an existing schema library?
2. What is the exact internal artifact contract between `gestaltd` and
   language-specific source-plugin backends?
3. Should the Python backend live inside the main repo or in a separately
   versioned SDK package with a small shim in `gestaltd`?

## Recommended Next Steps

1. Land the source-plugin interfaces from PR `#585`.
2. Define a small internal source-plugin backend contract shared by Go and
   Python.
3. Scaffold `sdk/python` around the public primitives in this proposal.
4. Implement the internal Python source-plugin backend with one default
   packaging path and no public backend knobs.
5. Port a `bigquery`-style hybrid plugin as the reference example.
