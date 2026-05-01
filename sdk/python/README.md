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


plugin = gestalt.Plugin("search")


@plugin.operation(method="GET", title="Search")
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

## Public surface

The top-level `gestalt` package exposes the supported authoring API:

- `Model`, `field`, `Plugin`, `operation`, and `Request` for integration
  providers.
- `AuthenticationProvider`, `CacheProvider`, `S3Provider`, `SecretsProvider`,
  `WorkflowProvider`, `AgentProvider`, and `PluginRuntimeProvider` for
  host-service provider runtimes.
- `Cache`, `IndexedDB`, `S3`, `WorkflowHost`, `WorkflowManager`, `AgentHost`,
  `AgentManager`, and `PluginInvoker` for calling sibling host services.
- `gestalt.telemetry` for provider-authored GenAI spans and metrics.

Generated protobuf bindings remain available under `gestalt.gen`, but provider
authors should prefer the handwritten SDK surface unless they need a low-level
protocol message directly.

## Regenerating protobuf stubs

This is an SDK maintainer workflow. Provider authors consume the checked-in
stubs through the `gestalt` package and do not need to regenerate them in
provider repositories.

Regenerate them from the repo root with:

```sh
uv run python sdk/python/scripts/generate_stubs.py
```

The script uses pinned Buf remote Python plugins so the generated stubs stay
reproducible while `plugin_pb2.py` tracks the protobuf `6.33.1` runtime floor
used by this SDK package and remains compatible with protobuf 7 runtimes.
`buf` must be available on `PATH`.

## API reference

The authored Python API reference is generated with Sphinx from the SDK's
docstrings. Build it locally from `sdk/python` with:

```sh
uv sync --group dev
uv run sphinx-build -W -b html -d docs/_build/doctrees docs docs/_build/html
```

The generated docs intentionally focus on the handwritten SDK surface. The
checked-in protobuf stubs under `gestalt/gen` remain importable, but they are
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
uv run ty check --exclude 'gestalt/gen/**' gestalt scripts tests
uv run vulture --config pyproject.toml
uv run python -m unittest discover -s tests
uv run sphinx-build -W -b html -d docs/_build/doctrees docs docs/_build/html
```

The generated protobuf stubs under `gestalt/gen` are excluded from the static
analysis tools because they are vendored output rather than hand-maintained SDK
code.
