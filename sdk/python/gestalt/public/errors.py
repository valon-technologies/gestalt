"""Gestalt public transport errors."""

from __future__ import annotations

import json
from dataclasses import dataclass

from gestalt.rpc_support import GestaltError, GestaltErrorCode

RESPONSE_KIND_HEADER = "X-Gestalt-Response-Kind"
RESPONSE_KIND_OPERATION_RESULT = "operation-result"


@dataclass(slots=True)
class GatewayError(Exception):
    """grpc-gateway style REST error response."""

    code: int
    message: str
    status: int
    body: bytes

    def __str__(self) -> str:
        return self.message


def is_operation_result(headers: dict[str, str]) -> bool:
    for key, value in headers.items():
        if key.lower() == RESPONSE_KIND_HEADER.lower():
            return value == RESPONSE_KIND_OPERATION_RESULT
    return False


def parse_gateway_error(status: int, body: bytes) -> GestaltError:
    code = _http_status_to_gestalt_code(status)
    message = f"request failed with status {status}"
    if body.strip():
        try:
            payload = json.loads(body)
        except json.JSONDecodeError:
            payload = None
        if isinstance(payload, dict):
            if isinstance(payload.get("message"), str) and payload["message"].strip():
                message = payload["message"]
            nested = payload.get("error")
            if isinstance(nested, dict):
                if isinstance(nested.get("message"), str) and nested["message"].strip():
                    message = nested["message"]
                if isinstance(nested.get("code"), str) and nested["code"].strip():
                    code = _gestalt_code_from_string(nested["code"])
            if isinstance(payload.get("code"), int):
                code = payload["code"]
    return GestaltError(code, message)


def _http_status_to_gestalt_code(status: int) -> int:
    match status:
        case 400:
            return GestaltErrorCode.INVALID_ARGUMENT
        case 401:
            return GestaltErrorCode.UNAUTHENTICATED
        case 403:
            return GestaltErrorCode.PERMISSION_DENIED
        case 404:
            return GestaltErrorCode.NOT_FOUND
        case 409:
            return GestaltErrorCode.ALREADY_EXISTS
        case 412:
            return GestaltErrorCode.FAILED_PRECONDITION
        case 429:
            return GestaltErrorCode.RESOURCE_EXHAUSTED
        case 499:
            return GestaltErrorCode.CANCELED
        case 500:
            return GestaltErrorCode.INTERNAL
        case 501:
            return GestaltErrorCode.UNIMPLEMENTED
        case 503:
            return GestaltErrorCode.UNAVAILABLE
        case 504:
            return GestaltErrorCode.DEADLINE_EXCEEDED
        case _:
            return GestaltErrorCode.UNKNOWN


def _gestalt_code_from_string(raw: str) -> int:
    normalized = raw.strip().upper()
    mapping = {
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
    return mapping.get(normalized, GestaltErrorCode.UNKNOWN)
