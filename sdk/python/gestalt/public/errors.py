"""Gestalt public transport errors (compatibility re-exports)."""

from __future__ import annotations

from collections.abc import Mapping

from gestalt.public.generated.transport_kernel import (
    RESPONSE_KIND_HEADER,
    RESPONSE_KIND_OPERATION_RESULT,
    parse_gateway_error,
)
from gestalt.public.generated.transport_kernel import (
    is_operation_result as _is_operation_result_pairs,
)

__all__ = [
    "RESPONSE_KIND_HEADER",
    "RESPONSE_KIND_OPERATION_RESULT",
    "is_operation_result",
    "parse_gateway_error",
]


def is_operation_result(
    headers: Mapping[str, str] | tuple[tuple[str, str], ...],
) -> bool:
    if isinstance(headers, Mapping):
        pairs = tuple(
            (key, value)
            for key, value in headers.items()
            if isinstance(key, str) and isinstance(value, str)
        )
        return _is_operation_result_pairs(pairs)
    return _is_operation_result_pairs(headers)
