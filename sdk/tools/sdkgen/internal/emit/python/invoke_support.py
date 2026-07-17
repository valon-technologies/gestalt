"""Decode runtime for json_result methods: the standard JSON operation
envelope semantics shared by app invocation results. Success envelopes return
their data, error envelopes and HTTP-error statuses raise InvokeError, and any
other JSON body passes through unchanged. The requests-style ``ok`` and
``raise_for_status`` helpers expose the HTTP status semantics directly."""

from __future__ import annotations

import json
from typing import Any

from .rpc_support import GestaltError, GestaltErrorCode, JsonValue, http_status_to_gestalt_code


def _body_bytes(value: bytes | str) -> bytes:
    return value.encode("utf-8") if isinstance(value, str) else bytes(value)


class InvokeError(GestaltError):
    """Decoded app invocation failure: an HTTP-error status, an error
    envelope, or an undecodable result body.

    Envelope ``error.code`` on the wire maps to :attr:`reason`.
    """

    def __init__(
        self,
        message: str = "app invoke failed",
        *,
        app: str = "",
        operation: str = "",
        status: int | None = None,
        reason: str | None = None,
        body: JsonValue = None,
        raw_body: bytes | str = b"",
    ) -> None:
        if status is not None:
            gestalt_code = http_status_to_gestalt_code(status)
        elif reason == "graphql_errors" or message in (
            "app invoke response is not valid JSON",
            "operation result body is not valid JSON",
        ):
            gestalt_code = GestaltErrorCode.INTERNAL
        else:
            gestalt_code = GestaltErrorCode.UNKNOWN
        super().__init__(gestalt_code, message)
        self.app = app
        self.operation = operation
        self.status = status
        self.reason = reason
        self.body = body
        self.raw_body = _body_bytes(raw_body)


def ok(status: int) -> bool:
    """Report whether an HTTP status is a success (200-299), mirroring the
    requests library's ``Response.ok``."""

    return 200 <= status <= 299


def raise_for_status(app: str, operation: str, status: int, body: bytes) -> None:
    """Raise :class:`InvokeError` for a non-success HTTP status (outside
    200-299), decoding the result body for the error message and reason exactly
    like :func:`decode_app_result`. Success statuses return None, mirroring the
    requests library's ``Response.raise_for_status``."""

    if not ok(status):
        raise _http_status_error(app, operation, status, body)


def _http_status_error(
    app: str, operation: str, status: int, body: bytes
) -> InvokeError:
    """Build the :class:`InvokeError` for an HTTP-error status: a JSON body
    supplies the decoded message and reason, any other body raises on the status
    alone."""

    try:
        parsed = _parse_json_result_body(body)
    except ValueError:
        return InvokeError(
            f"app invoke failed with status {status}",
            app=app,
            operation=operation,
            status=status,
            raw_body=body,
        )
    message, reason = _message_reason_from_body(parsed)
    return InvokeError(
        message or f"app invoke failed with status {status}",
        app=app,
        operation=operation,
        status=status,
        reason=reason,
        body=parsed,
        raw_body=body,
    )


def decode_app_result(app: str, operation: str, status: int, body: bytes) -> Any:
    """Decode one app operation result with the standard JSON envelope
    semantics: success envelopes return their data, error envelopes and
    HTTP-error statuses raise :class:`InvokeError`, and any other JSON body
    passes through unchanged."""

    if not ok(status):
        raise _http_status_error(app, operation, status, body)

    try:
        parsed = _parse_json_result_body(body)
    except ValueError:
        raise InvokeError(
            "app invoke response is not valid JSON",
            app=app,
            operation=operation,
            raw_body=body,
        ) from None

    if isinstance(parsed, dict) and isinstance(parsed.get("status"), str):
        if parsed["status"] == "error":
            message, reason = _message_reason_from_body(parsed)
            raise InvokeError(
                message or "app invoke failed",
                app=app,
                operation=operation,
                reason=reason,
                body=parsed,
                raw_body=body,
            )
        if parsed["status"] == "success" and "data" in parsed:
            return parsed["data"]
    return parsed


def decode_graphql_result(app: str, status: int, body: bytes) -> Any:
    """Decode one GraphQL invocation result like :func:`decode_app_result` and
    additionally raise :class:`InvokeError` when the response carries a
    non-empty GraphQL errors array."""

    decoded = decode_app_result(app, "graphql", status, body)
    try:
        _raise_graphql_errors(app, body, _parse_json_result_body(body))
    except ValueError:
        pass
    _raise_graphql_errors(app, body, decoded)
    return decoded


def _parse_json_result_body(body: bytes) -> JsonValue:
    if body.strip() == b"":
        return {}
    return json.loads(body)


def _raise_graphql_errors(app: str, raw_body: bytes, value: JsonValue) -> None:
    if not isinstance(value, dict):
        return
    errors = value.get("errors")
    if isinstance(errors, list) and errors:
        raise InvokeError(
            _graphql_error_message(errors),
            app=app,
            operation="graphql",
            reason="graphql_errors",
            body=value,
            raw_body=raw_body,
        )


def _message_reason_from_body(value: JsonValue) -> tuple[str | None, str | None]:
    if not isinstance(value, dict):
        return None, None
    message: str | None = None
    reason: str | None = None
    error = value.get("error")
    if isinstance(error, dict):
        if isinstance(error.get("message"), str) and error["message"].strip():
            message = error["message"]
        if isinstance(error.get("code"), str) and error["code"].strip():
            reason = error["code"]
    if (
        message is None
        and isinstance(value.get("message"), str)
        and value["message"].strip()
    ):
        message = value["message"]
    if reason is None and isinstance(value.get("code"), str) and value["code"].strip():
        reason = value["code"]
    return message, reason


def _graphql_error_message(errors: list[JsonValue]) -> str:
    first = errors[0] if errors else None
    if (
        isinstance(first, dict)
        and isinstance(first.get("message"), str)
        and first["message"].strip()
    ):
        return first["message"]
    return "GraphQL returned errors"
