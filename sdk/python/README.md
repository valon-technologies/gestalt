# Gestalt Python SDK

This package provides the Python authoring surface for executable Gestalt
providers.

It is published to PyPI as `gestalt-sdk` and imported in provider code as
`gestalt`.

It is intended to be used by source providers discovered through
`[tool.gestalt].provider` in `pyproject.toml` and by packaged providers
built from that same source tree.

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

## API Reference

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

Releases are published to PyPI through GitHub Actions Trusted Publishing.
The release workflow runs in the `pypi` environment and uses GitHub OIDC rather
than a checked-in upload token. To enable publishing for this repo:

- create or confirm the `gestalt-sdk` project on PyPI
- add a Trusted Publisher pointing at `valon-technologies/gestalt`
- set the workflow filename to `release-sdk.yml`
- set the environment name to `pypi`

Once that publisher is configured, pushing an SDK release tag such as
`sdk/python/v0.0.1-alpha.1` is enough for the workflow to build and publish the
package.

Install it in a provider repo with:

```sh
uv add gestalt-sdk
```

Then import the authored surface as:

```python
from gestalt import Model, Plugin
```

## Local SDK Checks

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
