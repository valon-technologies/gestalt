import dataclasses
import json
import pathlib
from collections.abc import Mapping
from dataclasses import MISSING
from typing import (
    Any,
    Iterable,
    Protocol,
    cast,
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
        return _normalize_catalog_mapping(cast(Mapping[str, Any], catalog), _CATALOG_FIELDS)
    raise TypeError("catalog must be a gestalt.Catalog or mapping")


def _serialize_catalog_dict(value: dict[str, Any], *, field_style: str) -> dict[str, Any]:
    return _serialize_catalog_mapping(value, _CATALOG_FIELDS, field_style=field_style)


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


@dataclasses.dataclass(frozen=True, slots=True)
class _CatalogFieldSpec:
    json_name: str | None = None
    children: dict[str, "_CatalogFieldSpec"] | None = None
    sequence: bool = False


def _normalize_catalog_mapping(
    value: Mapping[str, Any],
    fields: dict[str, _CatalogFieldSpec],
) -> dict[str, Any]:
    aliases = {
        alias: name
        for name, field_spec in fields.items()
        for alias in (name, field_spec.json_name)
        if alias
    }
    data: dict[str, Any] = {}
    for raw_key, raw_value in value.items():
        key = aliases.get(str(raw_key), str(raw_key))
        field_spec = fields.get(key)
        data[key] = _normalize_catalog_value(raw_value, field_spec)
    return data


def _normalize_catalog_value(value: Any, field_spec: _CatalogFieldSpec | None) -> Any:
    if field_spec is None or field_spec.children is None:
        return _python_value(value)
    if field_spec.sequence:
        items = value if isinstance(value, (list, tuple)) else []
        return [
            _normalize_catalog_mapping(item, field_spec.children) if isinstance(item, Mapping) else _python_value(item)
            for item in items
        ]
    if isinstance(value, Mapping):
        return _normalize_catalog_mapping(value, field_spec.children)
    return _python_value(value)


def _serialize_catalog_mapping(
    value: Mapping[str, Any],
    fields: dict[str, _CatalogFieldSpec],
    *,
    field_style: str,
) -> dict[str, Any]:
    serialized: dict[str, Any] = {}
    for key, item in value.items():
        if _omit_catalog_value(key, item):
            continue
        field_spec = fields.get(key)
        output_key = key if field_style == "yaml" or field_spec is None or field_spec.json_name is None else field_spec.json_name
        serialized[output_key] = _serialize_catalog_value(item, field_spec, field_style=field_style)
    return serialized


def _serialize_catalog_value(value: Any, field_spec: _CatalogFieldSpec | None, *, field_style: str) -> Any:
    if field_spec is None or field_spec.children is None:
        return _python_value(value)
    if field_spec.sequence:
        return [
            _serialize_catalog_mapping(item, field_spec.children, field_style=field_style)
            if isinstance(item, Mapping)
            else _python_value(item)
            for item in value
        ]
    if isinstance(value, Mapping):
        return _serialize_catalog_mapping(value, field_spec.children, field_style=field_style)
    return _python_value(value)


class _CatalogDumper(yaml.SafeDumper):
    def increase_indent(self, flow: bool = False, indentless: bool = False) -> Any:
        del indentless
        return super().increase_indent(flow, False)


_CATALOG_ANNOTATION_FIELDS = {
    "read_only_hint": _CatalogFieldSpec(json_name="readOnlyHint"),
    "idempotent_hint": _CatalogFieldSpec(json_name="idempotentHint"),
    "destructive_hint": _CatalogFieldSpec(json_name="destructiveHint"),
    "open_world_hint": _CatalogFieldSpec(json_name="openWorldHint"),
}

_CATALOG_PARAMETER_FIELDS = {
    "name": _CatalogFieldSpec(),
    "type": _CatalogFieldSpec(),
    "description": _CatalogFieldSpec(),
    "required": _CatalogFieldSpec(),
    "default": _CatalogFieldSpec(),
}

_CATALOG_OPERATION_FIELDS = {
    "id": _CatalogFieldSpec(),
    "method": _CatalogFieldSpec(),
    "title": _CatalogFieldSpec(),
    "description": _CatalogFieldSpec(),
    "input_schema": _CatalogFieldSpec(json_name="inputSchema"),
    "output_schema": _CatalogFieldSpec(json_name="outputSchema"),
    "annotations": _CatalogFieldSpec(children=_CATALOG_ANNOTATION_FIELDS),
    "parameters": _CatalogFieldSpec(children=_CATALOG_PARAMETER_FIELDS, sequence=True),
    "required_scopes": _CatalogFieldSpec(json_name="requiredScopes"),
    "tags": _CatalogFieldSpec(),
    "read_only": _CatalogFieldSpec(json_name="readOnly"),
    "visible": _CatalogFieldSpec(),
}

_CATALOG_FIELDS = {
    "name": _CatalogFieldSpec(),
    "display_name": _CatalogFieldSpec(json_name="displayName"),
    "description": _CatalogFieldSpec(),
    "icon_svg": _CatalogFieldSpec(json_name="iconSvg"),
    "operations": _CatalogFieldSpec(children=_CATALOG_OPERATION_FIELDS, sequence=True),
}
