# Gestalt Python SDK

This package provides the Python authoring surface for executable Gestalt
providers.

It is intended to be used by source providers discovered through
`[tool.gestalt].provider` in `pyproject.toml` (or the legacy
`[tool.gestalt].plugin` key) and by packaged providers built from that
same source tree.

Python source providers are developed locally via `from.source.path` and
released through `gestaltd provider release` for the host platform by default,
or for every requested target platform when you pass `--platform`. In CI,
prefer `--platform all` to build the full supported release matrix.

For non-host targets, configure a matching Python build interpreter with
`GESTALT_PYTHON_<GOOS>_<GOARCH>` or a target-specific virtualenv such as
`.venv-<goos>-<goarch>/`.

## Regenerating Protobuf Stubs

The checked-in Python protobuf stubs live in `gestalt/gen/v1`.

This is an SDK maintainer workflow. Provider authors consume the checked-in
stubs through the `gestalt` package and do not need to regenerate them in
provider repositories.

Regenerate them from the repo root with:

```sh
uv run python sdk/python/scripts/generate_stubs.py
```

The script uses pinned `buf` remote Python plugins so the generated stubs stay
reproducible while `plugin_pb2.py` tracks the protobuf `6.33.1` runtime floor
used by this SDK package and remains compatible with protobuf 7 runtimes.
`buf` must be available on `PATH`.

## Publishing

The SDK is published as the `gestalt` package to a private Python index.

Release tags stay aligned with the repo's SDK tag convention:

- `sdk/python/v0.0.1`
- `sdk/python/v0.0.1-alpha.1`
- `sdk/python/v0.0.1-beta.1`
- `sdk/python/v0.0.1-rc.1`

The release workflow normalizes those tag versions to PEP 440 before building
and publishing with `uv`, so `sdk/python/v0.0.1-alpha.1` becomes package
version `0.0.1a1`.

The GitHub Actions workflow expects these repository secrets:

- `GESTALT_PYTHON_PUBLISH_URL`
- either `GESTALT_PYTHON_PUBLISH_TOKEN`
- or `GESTALT_PYTHON_PUBLISH_USERNAME` and `GESTALT_PYTHON_PUBLISH_PASSWORD`

## Consuming From A Private Index

In an internal provider repo, pin `gestalt` to the private index with `uv` so
the package does not fall back to PyPI:

```toml
[[tool.uv.index]]
name = "valon-internal"
url = "https://packages.example.com/simple"
explicit = true
authenticate = "always"

[tool.uv.sources]
gestalt = { index = "valon-internal" }

[project]
dependencies = ["gestalt==0.0.1a1"]
```

At install time, provide credentials via the environment variables derived from
the index name:

```sh
export UV_INDEX_VALON_INTERNAL_USERNAME=...
export UV_INDEX_VALON_INTERNAL_PASSWORD=...
```

That lets `~/src/gestalt-providers` depend on `gestalt` like any other Python
package while keeping the SDK off the public package indexes.

## Local SDK Checks

From `sdk/python`, install the SDK plus its dev tooling and run the checks used
in CI:

```sh
uv sync --group dev
uv run ruff check .
uv run ty check --exclude 'gestalt/gen/**' gestalt scripts tests
uv run vulture --config pyproject.toml
uv run python -m unittest discover -s tests
```

The generated protobuf stubs under `gestalt/gen` are excluded from the static
analysis tools because they are vendored output rather than hand-maintained SDK
code.
