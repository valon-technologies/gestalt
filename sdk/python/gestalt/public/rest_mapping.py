"""Protobuf-JSON request mapping for the public REST transport."""

from __future__ import annotations

import re
from typing import Any
from urllib.parse import quote, urlencode

_PATH_FIELD_RE = re.compile(r"\{([^}]+)\}")


def snake_to_camel(name: str) -> str:
    parts = name.split("_")
    if not parts:
        return name
    head, *tail = parts
    return head + "".join(part[:1].upper() + part[1:] for part in tail if part)


def resolve_path_value(request: dict[str, Any], field_name: str) -> Any:
    if field_name in request:
        return request[field_name]
    camel = snake_to_camel(field_name)
    if camel in request:
        return request[camel]
    return None


def substitute_path(pattern: str, request: dict[str, Any]) -> str:
    def replace(match: re.Match[str]) -> str:
        field = match.group(1)
        value = resolve_path_value(request, field)
        if value is None:
            raise ValueError(f"missing path parameter {field}")
        return quote(str(value), safe="")

    return _PATH_FIELD_RE.sub(replace, pattern)


def path_param_names(pattern: str) -> list[str]:
    return [match.group(1) for match in _PATH_FIELD_RE.finditer(pattern)]


def is_path_field(field: str, path_params: set[str]) -> bool:
    return field in path_params or snake_to_camel(field) in path_params


def encode_query_value(key: str, value: Any, out: list[tuple[str, str]]) -> None:
    if value is None:
        return
    if isinstance(value, list):
        for item in value:
            encode_query_value(key, item, out)
        return
    if isinstance(value, dict):
        for nested_key, nested_value in value.items():
            encode_query_value(f"{key}.{nested_key}", nested_value, out)
        return
    out.append((key, str(value)))


def build_query_params(request: dict[str, Any], path_params: set[str]) -> list[tuple[str, str]]:
    pairs: list[tuple[str, str]] = []
    for key, value in request.items():
        if value is None or is_path_field(key, path_params):
            continue
        encode_query_value(key, value, pairs)
    return pairs


def build_body_map(request: dict[str, Any], path_params: set[str]) -> dict[str, Any]:
    body: dict[str, Any] = {}
    for key, value in request.items():
        if value is None or is_path_field(key, path_params):
            continue
        body[key] = value
    return body


def encode_query_string(pairs: list[tuple[str, str]]) -> str:
    return urlencode(pairs, doseq=True)
