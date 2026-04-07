from __future__ import annotations

import inspect
import json
import pathlib
import re
import sys
import types
from typing import Any, Final

import yaml

from ._api import Request
from ._catalog import build_catalog, write_catalog
from ._operations import (
    OperationDefinition,
    execute_operation,
    inspect_handler,
    maybe_await,
)

DEFAULT_OPERATION_METHOD: Final[str] = "POST"


class Plugin:
    def __init__(self, name: str, *, module_name: str | None = None) -> None:
        self.name = _slug_name(name)
        self._module_name = module_name
        self._operations: dict[str, OperationDefinition] = {}
        self._configure_handler: Any = None

    @classmethod
    def from_manifest(cls, path: str | pathlib.Path) -> Plugin:
        caller_file, caller_module_name = _caller_context()
        manifest_path = pathlib.Path(path)
        if not manifest_path.is_absolute() and not manifest_path.exists() and caller_file:
            manifest_path = pathlib.Path(caller_file).resolve().parent / manifest_path
        return cls(
            _derive_name_from_manifest(manifest_path),
            module_name=caller_module_name,
        )

    def configure(self, func: Any) -> Any:
        self._configure_handler = func
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
        tags: list[str] | None = None,
        read_only: bool = False,
        visible: bool | None = None,
    ) -> Any:
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
        handler = self._resolve_configure_handler()
        if handler is None:
            return
        maybe_await(handler(name, config))

    def execute(self, operation: str, params: dict[str, Any], request: Request) -> tuple[int, str]:
        return execute_operation(
            self._operations.get(operation),
            params=params,
            request=request,
        )

    def catalog_dict(self) -> dict[str, Any]:
        return build_catalog(
            plugin_name=self.name,
            operations=self._operations.values(),
        )

    def write_catalog(self, path: str | pathlib.Path) -> None:
        write_catalog(path, catalog=self.catalog_dict())

    def serve(self) -> None:
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


class _ModulePluginRegistry:
    def __init__(self) -> None:
        self._plugins: dict[str, Plugin] = {}

    def for_function(self, func: Any) -> Plugin:
        module = sys.modules.get(func.__module__)
        if module is None:
            raise RuntimeError(f"module {func.__module__!r} is not loaded")
        return self.for_module(module)

    def for_module(self, module: types.ModuleType) -> Plugin:
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
    tags: list[str] | None = None,
    read_only: bool = False,
    visible: bool | None = None,
) -> Any:
    def decorator(handler: Any) -> Any:
        plugin = _MODULE_PLUGINS.for_function(handler)
        return plugin.operation(
            id=id,
            method=method,
            title=title,
            description=description,
            tags=tags,
            read_only=read_only,
            visible=visible,
        )(handler)

    if func is None:
        return decorator
    return decorator(func)


def _module_plugin(module: types.ModuleType) -> Plugin:
    return _MODULE_PLUGINS.for_module(module)


def _caller_context() -> tuple[str | None, str | None]:
    frame = inspect.currentframe()
    from_manifest_frame = frame.f_back if frame is not None else None
    caller_frame = from_manifest_frame.f_back if from_manifest_frame is not None else None
    try:
        if caller_frame is None:
            return None, None
        return caller_frame.f_code.co_filename, caller_frame.f_globals.get("__name__")
    finally:
        del frame
        del from_manifest_frame
        del caller_frame


def _module_plugin_name(module: types.ModuleType) -> str:
    file_path = getattr(module, "__file__", None)
    if file_path:
        manifest_path = pathlib.Path(file_path).resolve().parent / "plugin.yaml"
        return _derive_name_from_manifest(manifest_path)
    return _slug_name(module.__name__.rsplit(".", 1)[-1])
def _derive_name_from_manifest(path: pathlib.Path) -> str:
    manifest_path = path / "plugin.yaml" if path.is_dir() else path
    fallback_name = manifest_path.parent.name or "plugin"

    try:
        text = manifest_path.read_text(encoding="utf-8")
    except OSError:
        return _slug_name(fallback_name)

    try:
        if manifest_path.suffix.lower() == ".json":
            data = json.loads(text)
        else:
            data = yaml.safe_load(text)
    except (json.JSONDecodeError, yaml.YAMLError):
        return _slug_name(fallback_name)
    if not isinstance(data, dict):
        return _slug_name(fallback_name)

    source = data.get("source")
    if isinstance(source, str) and source.strip():
        return _slug_name(source.rsplit("/", 1)[-1])

    display_name = data.get("display_name")
    if isinstance(display_name, str) and display_name.strip():
        return _slug_name(display_name)

    return _slug_name(fallback_name)


def _slug_name(value: str) -> str:
    cleaned = re.sub(r"[^A-Za-z0-9._-]+", "-", value.strip()).strip("-")
    return cleaned or "plugin"
