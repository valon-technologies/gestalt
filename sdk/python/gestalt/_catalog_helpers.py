"""Shared catalog shape helpers that do not depend on protobuf."""

from __future__ import annotations

import dataclasses
from dataclasses import MISSING, dataclass
from typing import Any, get_origin, get_type_hints

from ._api import FIELD_DESCRIPTION_KEY, FIELD_REQUIRED_KEY
from ._operations import is_optional_type, strip_optional


@dataclass(frozen=True, slots=True)
class CatalogParameterSpec:
    name: str
    type: str
    description: str
    required: bool
    has_default: bool
    default: Any


def catalog_parameters(input_type: Any) -> list[CatalogParameterSpec]:
    if input_type is None:
        return []

    input_type = strip_optional(input_type)
    origin = get_origin(input_type)
    if origin is not None:
        input_type = origin

    if not dataclasses.is_dataclass(input_type):
        return []

    type_hints = get_type_hints(input_type)
    parameters: list[CatalogParameterSpec] = []
    for field_definition in dataclasses.fields(input_type):
        annotation = type_hints.get(field_definition.name, field_definition.type)
        required = field_definition.metadata.get(FIELD_REQUIRED_KEY)
        if required is None:
            required = (
                field_definition.default is MISSING
                and field_definition.default_factory is MISSING
                and not is_optional_type(annotation)
            )

        description = str(
            field_definition.metadata.get(FIELD_DESCRIPTION_KEY, "")
        ).strip()
        has_default = field_definition.default is not MISSING
        default = field_definition.default if has_default else None

        parameters.append(
            CatalogParameterSpec(
                name=field_definition.name,
                type=catalog_type(annotation),
                description=description,
                required=bool(required),
                has_default=has_default,
                default=default,
            )
        )

    return parameters


def catalog_type(annotation: Any) -> str:
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
