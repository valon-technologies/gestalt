"""Catalog helpers for integration plugins.

The handwritten helpers in this module build and serialize catalog documents
around the generated ``Catalog`` protobuf messages exported by :mod:`gestalt`.
"""

import pathlib
from collections.abc import Mapping
from typing import (
    Any,
    Iterable,
    Protocol,
    runtime_checkable,
)

import yaml
from google.protobuf import json_format
from google.protobuf import struct_pb2 as _struct_pb2

from ._api import Request
from ._catalog_helpers import catalog_parameters
from ._operations import OperationDefinition
from .gen.v1 import plugin_pb2 as _plugin_pb2

plugin_pb2: Any = _plugin_pb2
struct_pb2: Any = _struct_pb2

Catalog: Any = plugin_pb2.Catalog  # ty: ignore[unresolved-attribute]
CatalogOperation: Any = plugin_pb2.CatalogOperation  # ty: ignore[unresolved-attribute]
CatalogParameter: Any = plugin_pb2.CatalogParameter  # ty: ignore[unresolved-attribute]
OperationAnnotations: Any = plugin_pb2.OperationAnnotations  # ty: ignore[unresolved-attribute]


@runtime_checkable
class SessionCatalogProvider(Protocol):
    """Protocol for plugins that return a per-request catalog."""

    def catalog_for_request(
        self, request: Request
    ) -> Catalog | Mapping[str, Any] | None: ...


def build_catalog(
    *,
    plugin_name: str,
    operations: Iterable[OperationDefinition],
) -> Catalog:
    """Build a catalog protobuf from authored operation definitions."""

    return Catalog(
        name=plugin_name,
        operations=[_catalog_operation(op) for op in operations],
    )


def catalog_to_proto(catalog: Catalog | Mapping[str, Any] | None) -> Catalog | None:
    """Normalize catalog input to a protobuf message."""

    if catalog is None:
        return None
    if isinstance(catalog, Catalog):
        return catalog
    if isinstance(catalog, Mapping):
        return _catalog_from_mapping(catalog)
    raise TypeError("catalog must be a gestalt.Catalog or mapping")


def catalog_to_dict(
    catalog: Catalog | Mapping[str, Any], *, field_style: str = "yaml"
) -> dict[str, Any]:
    """Convert a catalog protobuf or mapping into plain Python data."""

    if isinstance(catalog, Catalog):
        raw = json_format.MessageToDict(
            catalog, preserving_proto_field_name=(field_style == "yaml")
        )
        if "operations" not in raw:
            raw["operations"] = []
        return raw
    if isinstance(catalog, Mapping):
        return dict(catalog)
    raise TypeError("catalog must be a gestalt.Catalog or mapping")


def write_catalog(
    path: str | pathlib.Path, *, catalog: Catalog | Mapping[str, Any]
) -> None:
    """Write a catalog document to YAML on disk."""

    if isinstance(catalog, Mapping):
        catalog = _catalog_from_mapping(catalog)
    catalog_path = pathlib.Path(path)
    catalog_path.parent.mkdir(parents=True, exist_ok=True)
    as_dict = json_format.MessageToDict(catalog, preserving_proto_field_name=True)
    if "operations" not in as_dict:
        as_dict["operations"] = []
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
    for parameter in catalog_parameters(input_type):
        param = CatalogParameter(
            name=parameter.name,
            type=parameter.type,
        )
        param.description = parameter.description
        param.required = parameter.required
        if parameter.has_default:
            param.default.CopyFrom(
                struct_pb2.Value(string_value=str(parameter.default))
                if isinstance(parameter.default, str)
                else _to_proto_value(parameter.default)
            )
        parameters.append(param)

    return parameters


def _to_proto_value(value: Any) -> struct_pb2.Value:  # ty: ignore[unresolved-attribute]
    if value is None:
        return struct_pb2.Value(null_value=0)
    if isinstance(value, bool):
        return struct_pb2.Value(bool_value=value)
    if isinstance(value, (int, float)):
        return struct_pb2.Value(number_value=float(value))
    if isinstance(value, str):
        return struct_pb2.Value(string_value=value)
    return struct_pb2.Value(string_value=str(value))


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
            op.annotations.CopyFrom(
                OperationAnnotations(
                    read_only_hint=raw_ann.get(
                        "read_only_hint", raw_ann.get("readOnlyHint")
                    ),
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
            )
        for raw_param in raw_op.get("parameters", []):
            param = CatalogParameter(
                name=raw_param.get("name", ""),
                type=raw_param.get("type", ""),
                description=raw_param.get("description", ""),
                required=raw_param.get("required", False),
            )
            op.parameters.append(param)
        op.tags.extend(raw_op.get("tags", []))
        op.required_scopes.extend(
            raw_op.get("required_scopes", raw_op.get("requiredScopes", []))
        )
        catalog.operations.append(op)
    return catalog
