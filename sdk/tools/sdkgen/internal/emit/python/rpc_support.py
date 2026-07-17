"""Public support surface for generated Gestalt SDK clients: the canonical
error model and the native representations of well-known types that appear in
generated signatures. The call helpers and wire converters live in the
internal _codec package."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Iterator

JsonValue = bool | int | float | str | list["JsonValue"] | dict[str, "JsonValue"] | None
"""The native representation of a JSON value carried in structured payloads."""


@dataclass(frozen=True, slots=True)
class RpcStatus:
    """The native representation of google.rpc.Status carried in response
    payloads, mirroring the canonical error model."""

    code: int = 0
    message: str = ""


class GestaltErrorCode:
    """Canonical SDK error codes, drawn from the standard gRPC status codes."""

    CANCELED = 1
    UNKNOWN = 2
    INVALID_ARGUMENT = 3
    DEADLINE_EXCEEDED = 4
    NOT_FOUND = 5
    ALREADY_EXISTS = 6
    PERMISSION_DENIED = 7
    RESOURCE_EXHAUSTED = 8
    FAILED_PRECONDITION = 9
    ABORTED = 10
    OUT_OF_RANGE = 11
    UNIMPLEMENTED = 12
    INTERNAL = 13
    UNAVAILABLE = 14
    DATA_LOSS = 15
    UNAUTHENTICATED = 16


class GestaltError(Exception):
    """Canonical SDK error: one code, a message, and the underlying cause.
    Transport error types never appear in the public SDK surface."""

    def __init__(self, code: int, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


def http_status_to_gestalt_code(status: int) -> int:
    """Map an HTTP status code to the canonical Gestalt gRPC error code."""
    mapping = {
        400: GestaltErrorCode.INVALID_ARGUMENT,
        401: GestaltErrorCode.UNAUTHENTICATED,
        403: GestaltErrorCode.PERMISSION_DENIED,
        404: GestaltErrorCode.NOT_FOUND,
        409: GestaltErrorCode.ALREADY_EXISTS,
        412: GestaltErrorCode.FAILED_PRECONDITION,
        429: GestaltErrorCode.RESOURCE_EXHAUSTED,
        499: GestaltErrorCode.CANCELED,
        500: GestaltErrorCode.INTERNAL,
        501: GestaltErrorCode.UNIMPLEMENTED,
        503: GestaltErrorCode.UNAVAILABLE,
        504: GestaltErrorCode.DEADLINE_EXCEEDED,
    }
    return mapping.get(status, GestaltErrorCode.UNKNOWN)
