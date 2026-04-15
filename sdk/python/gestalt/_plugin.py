"""Plugin registration and decorator helpers for integration providers."""

import inspect
import json
import pathlib
import re
import sys
import types
from typing import Any, Final

import yaml

from ._api import Request
from ._catalog import (
    Catalog,
    SessionCatalogProvider,
    build_catalog,
    catalog_to_dict,
    write_catalog,
)
from ._operations import (
    OperationDefinition,
    OperationResult,
    execute_operation,
    inspect_handler,
    run_sync,
)

DEFAULT_OPERATION_METHOD: Final[str] = "POST"


class Plugin(SessionCatalogProvider):
    """Integration plugin definition and operation registry.

    ``Plugin`` collects operation handlers, optional configuration hooks, and
    optional session catalog hooks before handing control to the runtime:

    .. code-block:: python

        from gestalt import Model, Plugin

        class SearchInput(Model):
            query: str

        plugin = Plugin("search")

        @plugin.operation(title="Search")
        def search(params: SearchInput):
            return {"query": params.query}
    """

    def __init__(self, name: str, *, module_name: str | None = None) -> None:
        self.name = _slug_name(name)
        self._module_name = module_name
        self._operations: dict[str, OperationDefinition] = {}
        self._configure_handler: Any = None
        self._session_catalog_handler: tuple[Any, bool] | None = None

    @classmethod
    def from_manifest(
        cls,
        path: str | pathlib.Path,
        *,
        base_dir: pathlib.Path | None = None,
    ) -> "Plugin":
        """Build a plugin name from a manifest path."""

        manifest_path = pathlib.Path(path)
        if not manifest_path.is_absolute():
            resolved_base = base_dir if base_dir is not None else pathlib.Path.cwd()
            manifest_path = resolved_base / manifest_path
        return cls(_derive_name_from_manifest(manifest_path))

    def configure(self, func: Any) -> Any:
        """Register a configuration hook for provider name and config data."""

        self._configure_handler = func
        return func

    def session_catalog(self, func: Any) -> Any:
        """Register a per-request catalog hook."""

        self._session_catalog_handler = (func, _inspect_session_catalog_handler(func))
        return func

    def operation(
        self,
        func: Any | None = None,
        /,
        *,
        id: str | None = None,
        method: str = DEFAULT_OPERATION_METHOD,
        title: str = "",
        description: str = "",
        allowed_roles: list[str] | None = None,
        tags: list[str] | None = None,
        read_only: bool = False,
        visible: bool | None = None,
    ) -> Any:
        """Register an operation handler on this plugin."""

        def decorator(handler: Any) -> Any:
            operation_id = (id or handler.__name__).strip()
            if not operation_id:
                raise ValueError("operation id is required")
            if operation_id in self._operations:
                raise ValueError(f"duplicate operation id {operation_id!r}")

            input_type, takes_request = inspect_handler(handler)
            self._operations[operation_id] = OperationDefinition(
                id=operation_id,
                method=(method or DEFAULT_OPERATION_METHOD).upper(),
                title=title.strip(),
                description=description.strip(),
                allowed_roles=_normalize_allowed_roles(allowed_roles),
                tags=list(tags or []),
                read_only=read_only,
                visible=visible,
                handler=handler,
                input_type=input_type,
                takes_request=takes_request,
            )
            return handler

        if func is None:
            return decorator
        return decorator(func)

    def configure_provider(self, name: str, config: dict[str, Any]) -> None:
        """Invoke the registered configuration hook if one exists."""

        handler = self._resolve_configure_handler()
        if handler is None:
            return
        run_sync(handler(name, config))

    def execute(
        self, operation: str, params: dict[str, Any], request: Request
    ) -> OperationResult:
        """Execute a registered operation against request parameters."""

        return execute_operation(
            self._operations.get(operation),
            params=params,
            request=request,
        )

    def _static_catalog(self) -> Catalog:
        return build_catalog(
            plugin_name=self.name,
            operations=self._operations.values(),
        )

    def catalog_dict(self) -> dict[str, Any]:
        """Return the static plugin catalog as a plain dictionary."""

        return catalog_to_dict(self._static_catalog())

    def write_catalog(self, path: str | pathlib.Path) -> None:
        """Write the static plugin catalog to disk."""

        write_catalog(path, catalog=self._static_catalog())

    def supports_session_catalog(self) -> bool:
        """Report whether the plugin exposes a session catalog hook."""

        return self._resolve_session_catalog_handler() is not None

    def catalog_for_request(self, request: Request) -> Catalog | dict[str, Any] | None:
        """Return a per-request catalog if the plugin defines one."""

        definition = self._resolve_session_catalog_handler()
        if definition is None:
            return None

        handler, takes_request = definition
        if takes_request:
            return run_sync(handler(request))
        return run_sync(handler())

    def serve(self) -> None:
        """Start the integration runtime for this plugin."""

        from . import _runtime

        _runtime.serve(self)

    def _resolve_configure_handler(self) -> Any:
        if self._configure_handler is not None:
            return self._configure_handler
        if not self._module_name:
            return None

        module = sys.modules.get(self._module_name)
        if module is None:
            return None

        configure = getattr(module, "configure", None)
        if callable(configure):
            self._configure_handler = configure
        return self._configure_handler

    def _resolve_session_catalog_handler(self) -> tuple[Any, bool] | None:
        if self._session_catalog_handler is not None:
            return self._session_catalog_handler
        if not self._module_name:
            return None

        module = sys.modules.get(self._module_name)
        if module is None:
            return None

        session_catalog = getattr(module, "session_catalog", None)
        if (
            callable(session_catalog)
            and getattr(session_catalog, "__module__", None) == module.__name__
        ):
            self._session_catalog_handler = (
                session_catalog,
                _inspect_session_catalog_handler(session_catalog),
            )
        return self._session_catalog_handler


class _ModulePluginRegistry:
    def __init__(self) -> None:
        self._plugins: dict[str, Plugin] = {}

    def for_function(self, func: Any) -> "Plugin":
        module = sys.modules.get(func.__module__)
        if module is None:
            raise RuntimeError(f"module {func.__module__!r} is not loaded")
        return self.for_module(module)

    def for_module(self, module: types.ModuleType) -> "Plugin":
        existing_plugin = getattr(module, "plugin", None)
        if isinstance(existing_plugin, Plugin):
            if existing_plugin._module_name is None:
                existing_plugin._module_name = module.__name__
            self._plugins[module.__name__] = existing_plugin
            return existing_plugin

        plugin = self._plugins.get(module.__name__)
        if plugin is None:
            plugin = Plugin(_module_plugin_name(module), module_name=module.__name__)
            self._plugins[module.__name__] = plugin

        if not isinstance(getattr(module, "plugin", None), Plugin):
            setattr(module, "plugin", plugin)
        return plugin


_MODULE_PLUGINS = _ModulePluginRegistry()


def operation(
    func: Any | None = None,
    /,
    *,
    id: str | None = None,
    method: str = DEFAULT_OPERATION_METHOD,
    title: str = "",
    description: str = "",
    allowed_roles: list[str] | None = None,
    tags: list[str] | None = None,
    read_only: bool = False,
    visible: bool | None = None,
) -> Any:
    """Register an operation on the calling module's implicit plugin.

    This decorator is useful when a module-level ``plugin`` object would be
    redundant:

    .. code-block:: python

        from gestalt import Model, operation

        class SearchInput(Model):
            query: str

        @operation(title="Search")
        def search(params: SearchInput):
            return {"query": params.query}
    """

    def decorator(handler: Any) -> Any:
        plugin = _MODULE_PLUGINS.for_function(handler)
        return plugin.operation(
            id=id,
            method=method,
            title=title,
            description=description,
            allowed_roles=allowed_roles,
            tags=tags,
            read_only=read_only,
            visible=visible,
        )(handler)

    if func is None:
        return decorator
    return decorator(func)


def session_catalog(func: Any | None = None, /) -> Any:
    """Register a per-request catalog hook on the implicit module plugin."""

    def decorator(handler: Any) -> Any:
        plugin = _MODULE_PLUGINS.for_function(handler)
        return plugin.session_catalog(handler)

    if func is None:
        return decorator
    return decorator(func)


def _module_plugin(module: types.ModuleType) -> "Plugin":
    return _MODULE_PLUGINS.for_module(module)


def _normalize_allowed_roles(allowed_roles: list[str] | None) -> list[str]:
    normalized: list[str] = []
    seen: set[str] = set()
    for role in allowed_roles or []:
        trimmed = role.strip()
        if not trimmed or trimmed in seen:
            continue
        seen.add(trimmed)
        normalized.append(trimmed)
    return normalized


def _inspect_session_catalog_handler(func: Any) -> bool:
    signature = inspect.signature(func)
    parameters = list(signature.parameters.values())
    type_hints = inspect.get_annotations(func, eval_str=True)

    if len(parameters) > 1:
        raise TypeError("session catalog handlers may declare at most one parameter")
    if not parameters:
        return False

    annotation = type_hints.get(parameters[0].name, parameters[0].annotation)
    if annotation not in (inspect.Signature.empty, Request):
        raise TypeError(
            "session catalog handler parameter must be annotated as gestalt.Request"
        )
    return True


def _module_plugin_name(module: types.ModuleType) -> str:
    file_path = getattr(module, "__file__", None)
    if file_path:
        manifest_path = pathlib.Path(file_path).resolve().parent / "manifest.yaml"
        return _derive_name_from_manifest(manifest_path)
    return _slug_name(module.__name__.rsplit(".", 1)[-1])


def _derive_name_from_manifest(path: pathlib.Path) -> str:
    manifest_path = path / "manifest.yaml" if path.is_dir() else path
    fallback_name = manifest_path.parent.name or "plugin"
    manifest_format = manifest_path.suffix.lower()

    try:
        text = manifest_path.read_text(encoding="utf-8")
    except OSError:
        return _slug_name(fallback_name)

    if manifest_format == ".json":
        return _name_from_json_manifest(text, fallback_name)

    return _name_from_yaml_manifest(text, fallback_name)


def _name_from_manifest_dict(data: Any, fallback_name: str) -> str:
    if not isinstance(data, dict):
        return _slug_name(fallback_name)
    source = data.get("source")
    if isinstance(source, str) and source.strip():
        return _slug_name(source.rsplit("/", 1)[-1])
    display_name = data.get("display_name")
    if isinstance(display_name, str) and display_name.strip():
        return _slug_name(display_name)
    return _slug_name(fallback_name)


def _name_from_json_manifest(text: str, fallback_name: str) -> str:
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return _slug_name(fallback_name)
    return _name_from_manifest_dict(data, fallback_name)


class _TagIgnoringLoader(yaml.SafeLoader):
    pass


def _construct_ignore_tag(
    loader: yaml.SafeLoader, _suffix: str, node: yaml.Node
) -> Any:
    if isinstance(node, yaml.ScalarNode):
        return loader.construct_scalar(node)
    if isinstance(node, yaml.SequenceNode):
        return loader.construct_sequence(node)
    if isinstance(node, yaml.MappingNode):
        return loader.construct_mapping(node)
    return None


_TagIgnoringLoader.add_multi_constructor("", _construct_ignore_tag)


def _name_from_yaml_manifest(text: str, fallback_name: str) -> str:
    try:
        data = yaml.load(text, Loader=_TagIgnoringLoader)
    except yaml.YAMLError:
        return _slug_name(fallback_name)
    return _name_from_manifest_dict(data, fallback_name)


def _slug_name(value: str) -> str:
    cleaned = re.sub(r"[^A-Za-z0-9._-]+", "-", value.strip()).strip("-")
    return cleaned or "plugin"
