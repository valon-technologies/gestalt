import dataclasses
import json
import pathlib
from collections.abc import Mapping
from dataclasses import MISSING
from typing import (
    Any,
    Final,
    Iterable,
    Protocol,
    get_origin,
    get_type_hints,
    runtime_checkable,
)

import yaml

from ._api import FIELD_DESCRIPTION_KEY, FIELD_REQUIRED_KEY, Request
from ._operations import OperationDefinition, is_optional_type, strip_optional
from ._serialization import python_value as _python_value

_UNSET = object()


@dataclasses.dataclass(frozen=True, slots=True)
class OperationAnnotations:
    read_only_hint: bool | None = None
    idempotent_hint: bool | None = None
    destructive_hint: bool | None = None
    open_world_hint: bool | None = None


@dataclasses.dataclass(frozen=True, slots=True)
class CatalogParameter:
    name: str
    type: str
    description: str = ""
    required: bool = False
    default: Any = dataclasses.field(default=_UNSET)


@dataclasses.dataclass(frozen=True, slots=True)
class CatalogOperation:
    id: str
    method: str
    title: str = ""
    description: str = ""
    input_schema: Any = dataclasses.field(default=_UNSET)
    output_schema: Any = dataclasses.field(default=_UNSET)
    annotations: OperationAnnotations | None = None
    parameters: list[CatalogParameter] = dataclasses.field(default_factory=list)
    required_scopes: list[str] = dataclasses.field(default_factory=list)
    tags: list[str] = dataclasses.field(default_factory=list)
    read_only: bool = False
    visible: bool | None = None


@dataclasses.dataclass(frozen=True, slots=True)
class Catalog:
    name: str = ""
    display_name: str = ""
    description: str = ""
    icon_svg: str = ""
    operations: list[CatalogOperation] = dataclasses.field(default_factory=list)


@runtime_checkable
class SessionCatalogProvider(Protocol):
    def catalog_for_request(self, request: Request) -> Catalog | Mapping[str, Any] | None: ...


def build_catalog(
    *,
    plugin_name: str,
    operations: Iterable[OperationDefinition],
) -> Catalog:
    return Catalog(
        name=plugin_name,
        operations=[_catalog_operation(operation) for operation in operations],
    )


def catalog_to_dict(catalog: Catalog | Mapping[str, Any], *, field_style: str = "yaml") -> dict[str, Any]:
    return _serialize_catalog_dict(_catalog_python_dict(catalog), field_style=field_style)


def catalog_to_json(catalog: Catalog | Mapping[str, Any] | None) -> str:
    if catalog is None:
        return ""
    return json.dumps(catalog_to_dict(catalog, field_style="json"), separators=(",", ":"))


def write_catalog(path: str | pathlib.Path, *, catalog: Catalog | Mapping[str, Any]) -> None:
    catalog_path = pathlib.Path(path)
    catalog_path.parent.mkdir(parents=True, exist_ok=True)
    catalog_path.write_text(
        yaml.dump(
            catalog_to_dict(catalog, field_style="yaml"),
            Dumper=_CatalogDumper,
            sort_keys=False,
            default_flow_style=False,
            allow_unicode=True,
        ),
        encoding="utf-8",
    )


def _catalog_operation(operation: OperationDefinition) -> CatalogOperation:
    return CatalogOperation(
        id=operation.id,
        method=operation.method,
        title=operation.title,
        description=operation.description,
        parameters=_catalog_parameters(operation.input_type),
        tags=list(operation.tags),
        read_only=operation.read_only,
        visible=operation.visible,
    )


def _catalog_parameters(input_type: Any) -> list[CatalogParameter]:
    if input_type is None:
        return []

    input_type = strip_optional(input_type)
    origin = get_origin(input_type)
    if origin is not None:
        input_type = origin

    if not dataclasses.is_dataclass(input_type):
        return []

    type_hints = get_type_hints(input_type)
    parameters: list[CatalogParameter] = []
    for field_definition in dataclasses.fields(input_type):
        annotation = type_hints.get(field_definition.name, field_definition.type)
        parameter = CatalogParameter(
            name=field_definition.name,
            type=_catalog_type(annotation),
        )

        description = str(field_definition.metadata.get(FIELD_DESCRIPTION_KEY, "")).strip()
        required = field_definition.metadata.get(FIELD_REQUIRED_KEY)
        if required is None:
            required = (
                field_definition.default is MISSING
                and field_definition.default_factory is MISSING
                and not is_optional_type(annotation)
            )

        parameter = dataclasses.replace(
            parameter,
            description=description,
            required=bool(required),
            default=field_definition.default if field_definition.default is not MISSING else _UNSET,
        )
        parameters.append(parameter)

    return parameters


def _catalog_type(annotation: Any) -> str:
    actual_type = strip_optional(annotation)
    origin = get_origin(actual_type)
    if origin in (list, tuple, set):
        return "array"
    if origin is dict:
        return "object"

    if actual_type is str:
        return "string"
    if actual_type is bool:
        return "boolean"
    if actual_type is int:
        return "integer"
    if actual_type is float:
        return "number"
    if dataclasses.is_dataclass(actual_type):
        return "object"
    if actual_type in (dict, list, tuple, set):
        return "object" if actual_type is dict else "array"
    return "object"


def _catalog_python_dict(catalog: Catalog | Mapping[str, Any]) -> dict[str, Any]:
    if dataclasses.is_dataclass(catalog):
        return _python_value(catalog)
    if isinstance(catalog, Mapping):
        return _normalize_mapping(dict(catalog))
    raise TypeError("catalog must be a gestalt.Catalog or mapping")


def _serialize_catalog_dict(value: dict[str, Any], *, field_style: str) -> dict[str, Any]:
    return _serialize_catalog(value, field_style=field_style)


def _omit_catalog_value(key: str, value: Any) -> bool:
    if value is _UNSET:
        return True
    if key == "default":
        return False
    if value in ("", None):
        return True
    if key != "operations" and value == []:
        return True
    return key in {"read_only", "required"} and value is False


_JSON_RENAMES: Final[dict[str, str]] = {
    "display_name": "displayName",
    "icon_svg": "iconSvg",
    "input_schema": "inputSchema",
    "output_schema": "outputSchema",
    "required_scopes": "requiredScopes",
    "read_only": "readOnly",
    "read_only_hint": "readOnlyHint",
    "idempotent_hint": "idempotentHint",
    "destructive_hint": "destructiveHint",
    "open_world_hint": "openWorldHint",
}

_YAML_RENAMES: Final[dict[str, str]] = {v: k for k, v in _JSON_RENAMES.items()}

_OPAQUE_KEYS: Final[frozenset[str]] = frozenset({"input_schema", "output_schema"})


def _serialize_catalog(value: Any, *, field_style: str) -> Any:
    if isinstance(value, dict):
        return {
            _rename_key(k, field_style): (
                _python_value(v) if k in _OPAQUE_KEYS
                else _serialize_catalog(v, field_style=field_style)
            )
            for k, v in value.items()
            if not _omit_catalog_value(k, v)
        }
    if isinstance(value, (list, tuple)):
        return [_serialize_catalog(item, field_style=field_style) for item in value]
    return value


def _rename_key(key: str, field_style: str) -> str:
    if field_style == "json":
        return _JSON_RENAMES.get(key, key)
    return key


def _normalize_mapping(value: Any, *, opaque: bool = False) -> Any:
    if isinstance(value, dict):
        if opaque:
            return _python_value(value)
        return {
            _YAML_RENAMES.get(k, k): _normalize_mapping(v, opaque=k in _OPAQUE_KEYS)
            for k, v in value.items()
        }
    if isinstance(value, (list, tuple)):
        return [_normalize_mapping(item) for item in value]
    return _python_value(value)


class _CatalogDumper(yaml.SafeDumper):
    def increase_indent(self, flow: bool = False, indentless: bool = False) -> Any:
        del indentless
        return super().increase_indent(flow, False)
