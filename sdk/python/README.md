# Gestalt Python SDK

Use the Python SDK to build executable Gestalt providers with normal Python
classes, functions, and type annotations.

The package is published to PyPI as `gestalt-sdk` and imported in provider code
as `gestalt`.

```sh
uv add gestalt-sdk
```

```python
import gestalt


class SearchInput(gestalt.Model):
    query: str = gestalt.field(description="Search query")


app = gestalt.App("search")


@app.operation(method="GET", title="Search")
def search(input: SearchInput, request: gestalt.Request):
    return {"results": [input.query]}
```

## Provider projects

Python source providers are discovered through `[tool.gestalt].provider` in
`pyproject.toml`.

```toml
[project]
name = "gestalt-search"
version = "0.0.1"
dependencies = ["gestalt-sdk"]

[tool.uv]
package = false

[tool.gestalt]
provider = "provider"
```

Use the provider manifest for static provider identity, connections, hosted HTTP
routes, passthrough surfaces, and release metadata. Use Python code for
executable operations, provider lifecycle hooks, host-service clients, and
provider-specific runtimes.

## Build cache

Python executable provider builds can opt into the generic Gestalt build cache
by setting `GESTALT_BUILD_CACHE_DIR` to a writable directory before running
`gestaltd sync` or another provider packaging command. The cache stores
SDK-internal packaging accelerators only; it is safe to delete, and unsetting
the variable returns builds to the default clean temporary behavior.

```sh
GESTALT_BUILD_CACHE_DIR="$PWD/.gestalt-build-cache" \
  gestaltd sync --locked --config deploy/config.yaml
```

## Public surface

The top-level `gestalt` package exposes the supported authoring API:

- `Model`, `field`, `App`, `operation`, and `Request` for integration
  providers.
- `AuthenticationProvider`, `CacheProvider`, `S3Provider`, `SecretsProvider`,
  `WorkflowProvider`, `AgentProvider`, and `RuntimeProvider` for
  host-service provider runtimes.
- `Cache`, `IndexedDB`, `S3`, `Workflow`, `AgentHost`,
  `Agent`, and `Request.app()` for calling sibling host services.
- `gestalt.testing` for native fixture helpers used by SDK transport tests.
- `gestalt.telemetry` for provider-authored GenAI spans and metrics.

The SDK also exposes authored provider models for authentication, workflow,
agent, and runtime payloads. Agent provider handlers receive and return
native dataclasses such as `CreateAgentProviderTurnRequest`, `AgentSession`,
and `AgentTurn`; structured fields accept dictionaries or dataclass instances,
and timestamp fields use timezone-aware `datetime` values. Workflow helpers such as
`bound_workflow_target`, `workflow_signal`, and `bound_workflow_run` accept
plain dictionaries, dataclass instances, and native `datetime` values for
structured payloads and timestamp fields. Provider-facing APIs should accept
native Python values and keep transport serialization inside SDK adapters.

## API reference

The authored Python API reference is generated with Sphinx from the SDK's
docstrings. Build it locally from `sdk/python` with:

```sh
uv sync --group dev
uv run sphinx-build -W -b html -d docs/_build/doctrees docs docs/_build/html
```

The generated docs intentionally focus on the handwritten SDK surface. The
checked-in transport stubs live under the private `gestalt/_gen` package and are
not expanded as authored reference pages.

## Publishing

The SDK is published publicly as `gestalt-sdk` while keeping the import path
`gestalt`.

Release tags stay aligned with the repo's SDK tag convention:

- `sdk/python/v0.0.1`
- `sdk/python/v0.0.1-alpha.1`
- `sdk/python/v0.0.1-beta.1`
- `sdk/python/v0.0.1-rc.1`

The release workflow normalizes those tag versions to PEP 440 before building
and publishing with `uv`, so `sdk/python/v0.0.1-alpha.1` becomes package
version `0.0.1a1`.

Releases are published to PyPI through GitHub Actions Trusted Publishing. The
release workflow runs in the `pypi` environment and uses GitHub OIDC rather
than a checked-in upload token.

## Local SDK checks

From `sdk/python`, install the SDK plus its dev tooling and run the checks used
in CI:

```sh
uv sync --group dev
uv run ruff check .
uv run ty check --exclude 'gestalt/_gen/**' gestalt scripts tests
uv run vulture --config pyproject.toml
uv run python -m unittest discover -s tests
uv run sphinx-build -W -b html -d docs/_build/doctrees docs docs/_build/html
```

The vendored transport stubs under `gestalt/_gen` are excluded from the static
analysis tools because they are generated output rather than hand-maintained SDK
code.
