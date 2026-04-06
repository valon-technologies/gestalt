# Gestalt Python SDK

This package provides the Python authoring surface for executable Gestalt
provider plugins.

It is intended to be used by source plugins discovered through
`[tool.gestalt]` in `pyproject.toml` and by packaged plugins built from that
same source tree.

Python source plugins are developed locally via `from.source.path` and
released through `gestaltd plugin release` as current-platform executable
artifacts.
