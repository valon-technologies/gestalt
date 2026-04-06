# Gestalt Python SDK

This package provides the Python authoring surface for executable Gestalt
provider plugins.

The published package name is `gestalt`. Plugin authors should install it into
their Python environment from the internal wheel or private index, then point
`[tool.gestalt]` in `pyproject.toml` at the plugin object Gestalt should load.

Python source plugins are developed locally via `from.source.path` and
released through `gestaltd plugin release` as current-platform executable
artifacts. Release uses the selected Python environment directly, so that
environment must already have `gestalt` and `PyInstaller` installed.
