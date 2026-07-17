"""Protobuf-JSON request mapping for the public REST transport."""

from __future__ import annotations

import re
from typing import Any
from urllib.parse import quote, urlencode

from .generated.metadata import Method

_PATH_FIELD_RE = re.compile(r"\{([^}]+)\}")


def snake_to_camel(name: str) -> str:
    parts = name.split("_")
    if not parts:
        return name
    head, *tail = parts
    return head + "".join(part[:1].upper() + part[1:] for part in tail if part)


def field_value(
    request: dict[str, Any],
    field_name: str,
    json_name: str,
) -> Any:
    if json_name in request:
        return request[json_name]
    if field_name in request:
        return request[field_name]
    camel = snake_to_camel(field_name)
    if camel in request:
        return request[camel]
    return None


def excluded_field_keys(method: Method) -> set[str]:
    keys: set[str] = set()
    for field in method.http_path_fields:
        keys.add(field.name)
        keys.add(field.json_name)
        keys.add(snake_to_camel(field.name))
    for field in method.http_query_fields:
        keys.add(field.name)
        keys.add(field.json_name)
        keys.add(snake_to_camel(field.name))
    for name in (*method.fill, *method.reject):
        keys.add(name)
        keys.add(snake_to_camel(name))
    return keys


def build_rest_path(method: Method, request: dict[str, Any]) -> str:
    path = method.http_path
    for field in method.http_path_fields:
        value = field_value(request, field.name, field.json_name)
        if value is None:
            raise ValueError(f"missing path parameter {field.name}")
        if isinstance(value, (dict, list)):
            raise ValueError(f"path parameter {field.name} must be scalar")
        path = path.replace(f"{{{field.name}}}", quote(str(value), safe=""))
    return path


def append_query_params(
    pairs: list[tuple[str, str]],
    prefix: str,
    value: Any,
) -> None:
    if value is None:
        return
    if isinstance(value, list):
        for item in value:
            if item is None:
                continue
            if isinstance(item, (dict, list)):
                raise ValueError(f"repeated query field {prefix} must contain scalars")
            pairs.append((prefix, str(item)))
        return
    if isinstance(value, dict):
        for key, nested in value.items():
            nested_prefix = f"{prefix}.{key}" if prefix else key
            append_query_params(pairs, nested_prefix, nested)
        return
    pairs.append((prefix, str(value)))


def build_rest_query(method: Method, request: dict[str, Any]) -> list[tuple[str, str]]:
    pairs: list[tuple[str, str]] = []
    for field in method.http_query_fields:
        value = field_value(request, field.name, field.json_name)
        if value is None:
            continue
        append_query_params(pairs, field.json_name, value)
    return pairs


def build_rest_body(method: Method, request: dict[str, Any]) -> dict[str, Any] | None:
    if method.http_verb in {"GET", "DELETE"}:
        return None
    if method.http_body == "*":
        excluded = excluded_field_keys(method)
        body: dict[str, Any] = {}
        for key, value in request.items():
            if key in excluded or value is None:
                continue
            body[key] = value
        return body
    if not method.http_body:
        return None
    value = field_value(request, method.http_body, snake_to_camel(method.http_body))
    if value is None:
        value = request.get(method.http_body)
    if value is None:
        return {}
    if isinstance(value, dict):
        return value
    return {method.http_body: value}


def encode_query_string(pairs: list[tuple[str, str]]) -> str:
    return urlencode(pairs, doseq=True)
