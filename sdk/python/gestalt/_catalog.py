"""Catalog helpers for integration apps."""

from __future__ import annotations

import dataclasses
import json
import pathlib
from collections.abc import Mapping
from typing import (
    Any,
    Iterable,
    Protocol,
    cast,
    runtime_checkable,
)

import yaml

from ._api import Request
from ._catalog_helpers import catalog_parameters
from ._operations import OperationDefinition

json_format: Any = cast(Any, None)
_struct_pb2: Any = cast(Any, None)
try:
    from google.protobuf import json_format as _json_format
    from google.protobuf import struct_pb2 as _google_struct_pb2
except ModuleNotFoundError:
    pass
else:
    json_format = _json_format
    _struct_pb2 = _google_struct_pb2

app_pb2: Any = cast(Any, None)
try:
    from ._gen.v1 import app_pb2 as _app_pb2_module
except ModuleNotFoundError:
    pass
else:
    app_pb2 = _app_pb2_module

struct_pb2: Any = cast(Any, _struct_pb2)

_DEFAULT_UNSET = object()


@dataclasses.dataclass(slots=True)
class CatalogParameter:
    name: str = ""
    type: str = ""
    description: str = ""
    required: bool = False
    default: Any = dataclasses.field(default=_DEFAULT_UNSET, repr=False)


@dataclasses.dataclass(slots=True)
class OperationAnnotations:
    read_only_hint: bool | None = None
    idempotent_hint: bool | None = None
    destructive_hint: bool | None = None
    open_world_hint: bool | None = None


@dataclasses.dataclass(slots=True)
class UnaryResponseSpec:
    """Unary (fully materialized) operation response. Schema is a JSON-encoded
    JSON Schema object."""

    schema: str = ""


@dataclasses.dataclass(slots=True)
class StreamResponseSpec:
    """Streaming operation response. media_type names the representation (for
    example application/x-ndjson); item_schema is an optional JSON-encoded
    schema describing one yielded item."""

    media_type: str = ""
    item_schema: str = ""


@dataclasses.dataclass(slots=True)
class OperationResponseSpec:
    """Declares how an operation responds. Set unary or stream; both None means
    unary with no schema."""

    unary: UnaryResponseSpec | None = None
    stream: StreamResponseSpec | None = None


@dataclasses.dataclass(slots=True)
class CatalogOperation:
    id: str = ""
    method: str = ""
    title: str = ""
    description: str = ""
    input_schema: str = ""
    output_schema: str = ""
    response: OperationResponseSpec | None = None
    annotations: OperationAnnotations | None = None
    parameters: list[CatalogParameter] = dataclasses.field(default_factory=list)
    required_scopes: list[str] = dataclasses.field(default_factory=list)
    tags: list[str] = dataclasses.field(default_factory=list)
    read_only: bool = False
    visible: bool | None = None
    transport: str = ""
    allowed_roles: list[str] = dataclasses.field(default_factory=list)


@dataclasses.dataclass(slots=True)
class Catalog:
    name: str = ""
    display_name: str = ""
    description: str = ""
    icon_svg: str = ""
    operations: list[CatalogOperation] = dataclasses.field(default_factory=list)


@runtime_checkable
class SessionCatalogProvider(Protocol):
    """Protocol for apps that return a per-request catalog."""

    def catalog_for_request(
        self, request: Request
    ) -> Catalog | Mapping[str, Any] | None: ...


def build_catalog(
    *,
    app_name: str,
    operations: Iterable[OperationDefinition],
) -> Catalog:
    """Build a catalog value from authored operation definitions."""

    return Catalog(
        name=app_name,
        operations=[_catalog_operation(op) for op in operations],
    )


def catalog_to_proto(catalog: Catalog | Mapping[str, Any] | None) -> Any | None:
    """Normalize catalog input to the wire catalog shape."""

    if catalog is None:
        return None
    if _is_proto_catalog(catalog):
        return catalog
    if app_pb2 is None:
        if isinstance(catalog, Catalog):
            return _catalog_to_mapping(catalog)
        if isinstance(catalog, Mapping):
            return dict(catalog)
        raise TypeError("catalog must be a gestalt.Catalog or mapping")
    if isinstance(catalog, Catalog):
        return _catalog_to_proto(catalog)
    if isinstance(catalog, Mapping):
        return _catalog_to_proto(_catalog_from_mapping(catalog))
    raise TypeError("catalog must be a gestalt.Catalog or mapping")


def catalog_to_dict(
    catalog: Catalog | Mapping[str, Any], *, field_style: str = "yaml"
) -> dict[str, Any]:
    """Convert a catalog value or mapping into plain Python data."""

    if _is_proto_catalog(catalog):
        raw = json_format.MessageToDict(
            catalog, preserving_proto_field_name=(field_style == "yaml")
        )
        if "operations" not in raw:
            raw["operations"] = []
        return raw
    if isinstance(catalog, Catalog):
        return _catalog_to_mapping(catalog, field_style=field_style)
    if isinstance(catalog, Mapping):
        raw = dict(catalog)
        if "operations" not in raw:
            raw["operations"] = []
        return raw
    raise TypeError("catalog must be a gestalt.Catalog or mapping")


def write_catalog(
    path: str | pathlib.Path, *, catalog: Catalog | Mapping[str, Any]
) -> None:
    """Write a catalog document to YAML on disk."""

    catalog_path = pathlib.Path(path)
    catalog_path.parent.mkdir(parents=True, exist_ok=True)
    as_dict = catalog_to_dict(catalog, field_style="yaml")
    data = yaml.dump(as_dict, default_flow_style=False, sort_keys=False)
    catalog_path.write_text(data, encoding="utf-8")


def _catalog_operation(operation: OperationDefinition) -> CatalogOperation:
    op = CatalogOperation(
        id=operation.id,
        method=operation.method,
        title=operation.title,
        description=operation.description,
        read_only=operation.read_only,
    )
    op.parameters.extend(_catalog_parameters(operation.input_type))
    op.allowed_roles.extend(operation.allowed_roles)
    op.tags.extend(operation.tags)
    if operation.visible is not None:
        op.visible = operation.visible
    return op


def _catalog_parameters(input_type: Any) -> list[CatalogParameter]:
    parameters: list[CatalogParameter] = []
    for spec in catalog_parameters(input_type):
        param = CatalogParameter(
            name=spec.name,
            type=spec.type,
            description=spec.description,
            required=spec.required,
        )
        if spec.has_default:
            param.default = spec.default
        parameters.append(param)

    return parameters


def _to_proto_value(value: Any) -> Any:
    if struct_pb2 is not None and isinstance(value, struct_pb2.Value):
        return value
    return json_format.ParseDict(value, struct_pb2.Value())


def _is_proto_catalog(value: Any) -> bool:
    return app_pb2 is not None and isinstance(value, app_pb2.Catalog)


def _catalog_to_proto(catalog: Catalog) -> Any:
    proto_catalog = app_pb2.Catalog(
        name=catalog.name,
        display_name=catalog.display_name,
        description=catalog.description,
        icon_svg=catalog.icon_svg,
    )
    proto_catalog.operations.extend(
        _catalog_operation_to_proto(operation) for operation in catalog.operations
    )
    return proto_catalog


def _response_to_proto(
    response: OperationResponseSpec | None, legacy_output_schema: str
) -> Any | None:
    """Maps an OperationResponseSpec (or legacy output_schema) to the proto
    OperationResponseSpec. Returns None when neither is set."""
    if response is not None:
        if response.stream is not None:
            return app_pb2.OperationResponseSpec(
                stream=app_pb2.StreamResponseSpec(
                    media_type=response.stream.media_type,
                    item_schema=_schema_string_to_struct(response.stream.item_schema),
                )
            )
        if response.unary is not None:
            return app_pb2.OperationResponseSpec(
                unary=app_pb2.UnaryResponseSpec(
                    schema=_schema_string_to_struct(response.unary.schema)
                )
            )
    schema = _schema_string_to_struct(legacy_output_schema)
    if schema is None:
        return None
    return app_pb2.OperationResponseSpec(
        unary=app_pb2.UnaryResponseSpec(schema=schema)
    )


def _schema_string_to_struct(schema: str) -> Any | None:
    trimmed = schema.strip()
    if not trimmed:
        return None
    try:
        parsed = json.loads(trimmed)
    except (json.JSONDecodeError, TypeError):
        return None
    if not isinstance(parsed, dict):
        return None
    return json_format.ParseDict(parsed, struct_pb2.Struct())


def _catalog_operation_to_proto(operation: CatalogOperation) -> Any:
    proto_operation = app_pb2.CatalogOperation(
        id=operation.id,
        method=operation.method,
        title=operation.title,
        description=operation.description,
        input_schema=operation.input_schema,
        read_only=operation.read_only,
        transport=operation.transport,
    )
    response_spec = _response_to_proto(operation.response, operation.output_schema)
    if response_spec is not None:
        proto_operation.response.CopyFrom(response_spec)
    if operation.annotations is not None and _has_annotations(operation.annotations):
        proto_operation.annotations.CopyFrom(
            _operation_annotations_to_proto(operation.annotations)
        )
    proto_operation.parameters.extend(
        _catalog_parameter_to_proto(parameter) for parameter in operation.parameters
    )
    proto_operation.required_scopes.extend(operation.required_scopes)
    proto_operation.tags.extend(operation.tags)
    if operation.visible is not None:
        proto_operation.visible = operation.visible
    proto_operation.allowed_roles.extend(operation.allowed_roles)
    return proto_operation


def _operation_annotations_to_proto(annotations: OperationAnnotations) -> Any:
    proto_annotations = app_pb2.OperationAnnotations()
    if annotations.read_only_hint is not None:
        proto_annotations.read_only_hint = annotations.read_only_hint
    if annotations.idempotent_hint is not None:
        proto_annotations.idempotent_hint = annotations.idempotent_hint
    if annotations.destructive_hint is not None:
        proto_annotations.destructive_hint = annotations.destructive_hint
    if annotations.open_world_hint is not None:
        proto_annotations.open_world_hint = annotations.open_world_hint
    return proto_annotations


def _catalog_parameter_to_proto(parameter: CatalogParameter) -> Any:
    proto_parameter = app_pb2.CatalogParameter(
        name=parameter.name,
        type=parameter.type,
        description=parameter.description,
        required=parameter.required,
    )
    if parameter.default is not _DEFAULT_UNSET:
        proto_parameter.default.CopyFrom(_to_proto_value(parameter.default))
    return proto_parameter


def _catalog_to_mapping(
    catalog: Catalog, *, field_style: str = "yaml"
) -> dict[str, Any]:
    raw: dict[str, Any] = {}
    if catalog.name:
        raw["name"] = catalog.name
    if catalog.display_name:
        raw[_field("display_name", "displayName", field_style)] = catalog.display_name
    if catalog.description:
        raw["description"] = catalog.description
    if catalog.icon_svg:
        raw[_field("icon_svg", "iconSvg", field_style)] = catalog.icon_svg
    raw["operations"] = [
        _catalog_operation_to_mapping(operation, field_style=field_style)
        for operation in catalog.operations
    ]
    return raw


def _catalog_operation_to_mapping(
    operation: CatalogOperation, *, field_style: str
) -> dict[str, Any]:
    raw: dict[str, Any] = {}
    if operation.id:
        raw["id"] = operation.id
    if operation.method:
        raw["method"] = operation.method
    if operation.title:
        raw["title"] = operation.title
    if operation.description:
        raw["description"] = operation.description
    if operation.input_schema:
        raw[_field("input_schema", "inputSchema", field_style)] = operation.input_schema
    if operation.output_schema:
        raw[_field("output_schema", "outputSchema", field_style)] = (
            operation.output_schema
        )
    if operation.annotations is not None and _has_annotations(operation.annotations):
        raw["annotations"] = _operation_annotations_to_mapping(
            operation.annotations, field_style=field_style
        )
    if operation.parameters:
        raw["parameters"] = [
            _catalog_parameter_to_mapping(parameter, field_style=field_style)
            for parameter in operation.parameters
        ]
    if operation.required_scopes:
        raw[_field("required_scopes", "requiredScopes", field_style)] = list(
            operation.required_scopes
        )
    if operation.tags:
        raw["tags"] = list(operation.tags)
    if operation.read_only:
        raw[_field("read_only", "readOnly", field_style)] = True
    if operation.visible is not None:
        raw["visible"] = operation.visible
    if operation.transport:
        raw["transport"] = operation.transport
    if operation.allowed_roles:
        raw[_field("allowed_roles", "allowedRoles", field_style)] = list(
            operation.allowed_roles
        )
    return raw


def _operation_annotations_to_mapping(
    annotations: OperationAnnotations, *, field_style: str
) -> dict[str, Any]:
    raw: dict[str, Any] = {}
    if annotations.read_only_hint is not None:
        raw[_field("read_only_hint", "readOnlyHint", field_style)] = (
            annotations.read_only_hint
        )
    if annotations.idempotent_hint is not None:
        raw[_field("idempotent_hint", "idempotentHint", field_style)] = (
            annotations.idempotent_hint
        )
    if annotations.destructive_hint is not None:
        raw[_field("destructive_hint", "destructiveHint", field_style)] = (
            annotations.destructive_hint
        )
    if annotations.open_world_hint is not None:
        raw[_field("open_world_hint", "openWorldHint", field_style)] = (
            annotations.open_world_hint
        )
    return raw


def _catalog_parameter_to_mapping(
    parameter: CatalogParameter, *, field_style: str
) -> dict[str, Any]:
    raw: dict[str, Any] = {}
    if parameter.name:
        raw["name"] = parameter.name
    if parameter.type:
        raw["type"] = parameter.type
    if parameter.description:
        raw["description"] = parameter.description
    if parameter.required:
        raw["required"] = True
    if parameter.default is not _DEFAULT_UNSET:
        raw["default"] = parameter.default
    return raw


def _field(snake_case: str, lower_camel: str, field_style: str) -> str:
    return snake_case if field_style == "yaml" else lower_camel


def _has_annotations(annotations: OperationAnnotations) -> bool:
    return (
        annotations.read_only_hint is not None
        or annotations.idempotent_hint is not None
        or annotations.destructive_hint is not None
        or annotations.open_world_hint is not None
    )


def _catalog_from_mapping(data: Mapping[str, Any]) -> Catalog:
    catalog = Catalog(
        name=data.get("name", ""),
        display_name=data.get("display_name", data.get("displayName", "")),
        description=data.get("description", ""),
        icon_svg=data.get("icon_svg", data.get("iconSvg", "")),
    )
    for raw_op in data.get("operations", []):
        op = CatalogOperation(
            id=raw_op.get("id", ""),
            method=raw_op.get("method", ""),
            title=raw_op.get("title", ""),
            description=raw_op.get("description", ""),
            input_schema=raw_op.get("input_schema", raw_op.get("inputSchema", "")),
            output_schema=raw_op.get("output_schema", raw_op.get("outputSchema", "")),
            read_only=raw_op.get("read_only", raw_op.get("readOnly", False)),
            transport=raw_op.get("transport", ""),
        )
        visible = raw_op.get("visible")
        if visible is not None:
            op.visible = visible
        op.allowed_roles.extend(
            raw_op.get("allowed_roles", raw_op.get("allowedRoles", []))
        )
        raw_ann = raw_op.get("annotations") or {}
        if raw_ann:
            op.annotations = OperationAnnotations(
                read_only_hint=raw_ann.get("read_only_hint", raw_ann.get("readOnlyHint")),
                idempotent_hint=raw_ann.get(
                    "idempotent_hint", raw_ann.get("idempotentHint")
                ),
                destructive_hint=raw_ann.get(
                    "destructive_hint", raw_ann.get("destructiveHint")
                ),
                open_world_hint=raw_ann.get(
                    "open_world_hint", raw_ann.get("openWorldHint")
                ),
            )
        for raw_param in raw_op.get("parameters", []):
            param = CatalogParameter(
                name=raw_param.get("name", ""),
                type=raw_param.get("type", ""),
                description=raw_param.get("description", ""),
                required=raw_param.get("required", False),
            )
            if "default" in raw_param:
                param.default = raw_param["default"]
            op.parameters.append(param)
        op.tags.extend(raw_op.get("tags", []))
        op.required_scopes.extend(
            raw_op.get("required_scopes", raw_op.get("requiredScopes", []))
        )
        catalog.operations.append(op)
    return catalog
