from __future__ import annotations

import dataclasses
import pathlib
from dataclasses import MISSING
from typing import Any, Iterable, get_origin, get_type_hints

import yaml

from ._api import FIELD_DESCRIPTION_KEY, FIELD_REQUIRED_KEY
from ._operations import OperationDefinition
from ._typing_utils import is_optional_type, strip_optional


def build_catalog(
    *,
    plugin_name: str,
    operations: Iterable[OperationDefinition],
) -> dict[str, Any]:
    return {
        "name": plugin_name,
        "operations": [_catalog_operation(operation) for operation in operations],
    }


def write_catalog(path: str | pathlib.Path, *, catalog: dict[str, Any]) -> None:
    catalog_path = pathlib.Path(path)
    catalog_path.parent.mkdir(parents=True, exist_ok=True)
    catalog_path.write_text(
        yaml.dump(
            catalog,
            Dumper=_CatalogDumper,
            sort_keys=False,
            default_flow_style=False,
            allow_unicode=True,
        ),
        encoding="utf-8",
    )


def _catalog_operation(operation: OperationDefinition) -> dict[str, Any]:
    data: dict[str, Any] = {
        "id": operation.id,
        "method": operation.method,
    }
    if operation.title:
        data["title"] = operation.title
    if operation.description:
        data["description"] = operation.description
    if operation.tags:
        data["tags"] = operation.tags
    if operation.read_only:
        data["read_only"] = True
    if operation.visible is not None:
        data["visible"] = operation.visible

    parameters = _catalog_parameters(operation.input_type)
    if parameters:
        data["parameters"] = parameters

    return data


def _catalog_parameters(input_type: Any) -> list[dict[str, Any]]:
    if input_type is None:
        return []

    input_type = strip_optional(input_type)
    origin = get_origin(input_type)
    if origin is not None:
        input_type = origin

    if not dataclasses.is_dataclass(input_type):
        return []

    type_hints = get_type_hints(input_type)
    parameters: list[dict[str, Any]] = []
    for field_definition in dataclasses.fields(input_type):
        annotation = type_hints.get(field_definition.name, field_definition.type)
        parameter: dict[str, Any] = {
            "name": field_definition.name,
            "type": _catalog_type(annotation),
        }

        description = str(field_definition.metadata.get(FIELD_DESCRIPTION_KEY, "")).strip()
        if description:
            parameter["description"] = description

        required = field_definition.metadata.get(FIELD_REQUIRED_KEY)
        if required is None:
            required = (
                field_definition.default is MISSING
                and field_definition.default_factory is MISSING
                and not is_optional_type(annotation)
            )
        if required:
            parameter["required"] = True

        if field_definition.default is not MISSING:
            parameter["default"] = field_definition.default

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


class _CatalogDumper(yaml.SafeDumper):
    def increase_indent(self, flow: bool = False, indentless: bool = False) -> Any:
        del indentless
        return super().increase_indent(flow, False)
