"""Gestalt public transport errors."""

from __future__ import annotations

import json
import re
from typing import cast

from gestalt.rpc_support import GestaltError, GestaltErrorCode

RESPONSE_KIND_HEADER = "X-Gestalt-Response-Kind"
RESPONSE_KIND_OPERATION_RESULT = "operation-result"

_HTTP_STATUS_CODES = {
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

_GESTALT_CODE_NAMES = {
    "CANCELED": GestaltErrorCode.CANCELED,
    "INVALID_ARGUMENT": GestaltErrorCode.INVALID_ARGUMENT,
    "DEADLINE_EXCEEDED": GestaltErrorCode.DEADLINE_EXCEEDED,
    "NOT_FOUND": GestaltErrorCode.NOT_FOUND,
    "ALREADY_EXISTS": GestaltErrorCode.ALREADY_EXISTS,
    "PERMISSION_DENIED": GestaltErrorCode.PERMISSION_DENIED,
    "RESOURCE_EXHAUSTED": GestaltErrorCode.RESOURCE_EXHAUSTED,
    "FAILED_PRECONDITION": GestaltErrorCode.FAILED_PRECONDITION,
    "ABORTED": GestaltErrorCode.ABORTED,
    "OUT_OF_RANGE": GestaltErrorCode.OUT_OF_RANGE,
    "UNIMPLEMENTED": GestaltErrorCode.UNIMPLEMENTED,
    "INTERNAL": GestaltErrorCode.INTERNAL,
    "UNAVAILABLE": GestaltErrorCode.UNAVAILABLE,
    "DATA_LOSS": GestaltErrorCode.DATA_LOSS,
    "UNAUTHENTICATED": GestaltErrorCode.UNAUTHENTICATED,
}


def is_operation_result(headers: dict[str, str]) -> bool:
    for key, value in headers.items():
        if key.lower() == RESPONSE_KIND_HEADER.lower():
            return value.lower() == RESPONSE_KIND_OPERATION_RESULT
    return False


def parse_gateway_error(status: int, body: bytes) -> GestaltError:
    code = _HTTP_STATUS_CODES.get(status, GestaltErrorCode.UNKNOWN)
    message = f"request failed with status {status}"
    if not body.strip():
        return GestaltError(code, message)

    try:
        payload = json.loads(body)
    except json.JSONDecodeError:
        return GestaltError(code, message)

    if not isinstance(payload, dict):
        return GestaltError(code, message)

    parsed_code, parsed_message = _gateway_fields(payload)
    if parsed_message:
        message = parsed_message
    if parsed_code is not None:
        code = parsed_code
    return GestaltError(code, message)


def _gateway_fields(payload: dict[str, object]) -> tuple[int | None, str | None]:
    message: str | None = None
    code: int | None = None

    raw_message = payload.get("message")
    if isinstance(raw_message, str) and raw_message.strip():
        message = raw_message

    error = payload.get("error")
    if isinstance(error, str) and error.strip():
        message = error
    elif isinstance(error, dict):
        err = cast(dict[str, object], error)
        nested_message = err.get("message")
        if isinstance(nested_message, str) and nested_message.strip():
            message = nested_message
        nested_code = err.get("code")
        if isinstance(nested_code, str) and nested_code.strip():
            code = _gestalt_code_from_string(nested_code)

    raw_code = payload.get("code")
    if isinstance(raw_code, int):
        code = raw_code
    elif isinstance(raw_code, str) and raw_code.strip():
        code = _gestalt_code_from_string(raw_code)

    return code, message


def _gestalt_code_from_string(raw: str) -> int:
    normalized = re.sub(r"(?<!^)(?=[A-Z])", "_", raw.strip()).upper()
    return _GESTALT_CODE_NAMES.get(normalized, GestaltErrorCode.UNKNOWN)
