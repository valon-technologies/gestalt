"""Protobuf-JSON request mapping (compatibility re-exports)."""

from __future__ import annotations

from urllib.parse import urlencode

from gestalt.public.generated.transport_kernel import (
    append_query_params,
    build_rest_body,
    build_rest_path,
    build_rest_query,
    excluded_field_keys,
    field_value,
    snake_to_camel,
)


def encode_query_string(pairs: list[tuple[str, str]]) -> str:
    return urlencode(pairs, doseq=True)


__all__ = [
    "append_query_params",
    "build_rest_body",
    "build_rest_path",
    "build_rest_query",
    "encode_query_string",
    "excluded_field_keys",
    "field_value",
    "snake_to_camel",
]
