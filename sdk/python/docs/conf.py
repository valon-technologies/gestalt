"""Sphinx configuration for the Gestalt Python SDK reference."""

from __future__ import annotations

import importlib.metadata
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

project = "Gestalt Python SDK"
author = "Valon Technologies"

try:
    release = importlib.metadata.version("gestalt-sdk")
except importlib.metadata.PackageNotFoundError:
    release = "development"
version = release

extensions = [
    "sphinx.ext.autodoc",
    "sphinx.ext.autosummary",
    "sphinx.ext.napoleon",
    "sphinx.ext.viewcode",
]

templates_path = ["_templates"]
exclude_patterns = ["_build", "generated"]

autodoc_member_order = "bysource"
autodoc_typehints = "description"
autodoc_preserve_defaults = True
autosummary_generate = False
autosummary_imported_members = False
add_module_names = False
napoleon_google_docstring = True
napoleon_numpy_docstring = False

html_theme = "alabaster"
html_title = project
html_static_path: list[str] = []
modindex_common_prefix = ["gestalt."]
