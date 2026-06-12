"""Sphinx configuration for the Gestalt Python SDK reference."""

from __future__ import annotations

import importlib.metadata
import os
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

# The package resolves its flat exports lazily (PEP 562). autodoc only
# documents module data it can see in the module dict, so resolve every
# export up front; __getattr__ caches each one into the module namespace.
import gestalt  # noqa: E402  (requires the sys.path insertion above)

for _name in gestalt.__all__:
    getattr(gestalt, _name)

project = "Gestalt Python SDK"
author = "Valon Technologies"

release = os.environ.get("GESTALT_SDK_DOCS_VERSION")
if release is None:
    try:
        release = importlib.metadata.version("gestalt-sdk")
    except importlib.metadata.PackageNotFoundError:
        release = "development"
version = release

extensions = [
    "sphinx.ext.autodoc",
    "sphinx.ext.autosummary",
    "sphinx.ext.napoleon",
]

templates_path = ["_templates"]
exclude_patterns = ["_build", "generated"]

autodoc_member_order = "bysource"
autodoc_typehints = "description"
autodoc_preserve_defaults = True
autosummary_generate = True
# automodule documents classes and functions (including imported ones), but
# cannot see module data resolved through the lazy __getattr__ — constants
# and typing aliases. Compute exactly that remainder for the module template.
import inspect  # noqa: E402

autosummary_context = {
    "gestalt_lazy_data": [
        _name
        for _name in gestalt.__all__
        if not (
            inspect.isclass(getattr(gestalt, _name))
            or inspect.isroutine(getattr(gestalt, _name))
            or inspect.ismodule(getattr(gestalt, _name))
        )
    ]
}
# Generated docstrings name types that legitimately exist in more than one
# documented module (the flat gestalt.X alias plus the service module's own
# class), which the python domain reports as ambiguous targets. Everything
# else stays warnings-as-errors.
suppress_warnings = ["ref.python"]
autosummary_imported_members = False
add_module_names = False
napoleon_google_docstring = True
napoleon_numpy_docstring = False

html_theme = "alabaster"
html_title = project
html_static_path: list[str] = []
modindex_common_prefix = ["gestalt."]


def _strip_lazy_data_docstrings(app, what, name, obj, options, lines):
    # The explicitly rendered constants and aliases have no authored
    # docstrings; without this, autodata falls back to the underlying
    # builtin type's raw (non-RST) docstring.
    if what == "data" and name.startswith("gestalt."):
        lines.clear()


def setup(app):
    app.connect("autodoc-process-docstring", _strip_lazy_data_docstrings)
