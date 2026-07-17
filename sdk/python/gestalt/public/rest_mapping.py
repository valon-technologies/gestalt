"""Protobuf-JSON request mapping (compatibility re-exports)."""

from __future__ import annotations

from typing import Any
from urllib.parse import urlencode

from gestalt.public.generated.transport_kernel import (
    append_query_params as _append_query_params,
)
from gestalt.public.generated.transport_kernel import (
    build_rest_body as _build_rest_body,
)
from gestalt.public.generated.transport_kernel import (
    build_rest_path as _build_rest_path,
)
from gestalt.public.generated.transport_kernel import (
    build_rest_query as _build_rest_query,
)
from gestalt.public.generated.transport_kernel import (
    excluded_field_keys,
    field_value,
    snake_to_camel,
)
from gestalt.rpc_support import GestaltError, GestaltErrorCode


def _reraise_invalid_argument_as_value_error(err: GestaltError) -> None:
    if err.code == GestaltErrorCode.INVALID_ARGUMENT:
        raise ValueError(err.message) from err
    raise err


def build_rest_path(method, request: dict[str, Any]) -> str:
    try:
        return _build_rest_path(method, request)
    except GestaltError as err:
        _reraise_invalid_argument_as_value_error(err)
        raise


def append_query_params(
    pairs: list[tuple[str, str]],
    prefix: str,
    value: Any,
) -> None:
    try:
        _append_query_params(pairs, prefix, value)
    except GestaltError as err:
        _reraise_invalid_argument_as_value_error(err)


def build_rest_query(method, request: dict[str, Any]) -> list[tuple[str, str]]:
    try:
        return _build_rest_query(method, request)
    except GestaltError as err:
        _reraise_invalid_argument_as_value_error(err)
        raise


def build_rest_body(method, request: dict[str, Any]) -> dict[str, Any] | None:
    return _build_rest_body(method, request)


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
