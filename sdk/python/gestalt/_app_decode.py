from __future__ import annotations

import json
from typing import cast

from ._api import Response
from ._protocol import JsonValue


def _body_bytes(value: bytes | str) -> bytes:
    return value.encode("utf-8") if isinstance(value, str) else bytes(value)


class InvokeError(Exception):
    """Decoded app invocation failure."""

    def __init__(
        self,
        message: str = "app invoke failed",
        *,
        app: str = "",
        operation: str = "",
        status: int | None = None,
        code: str | None = None,
        body: JsonValue = None,
        raw_body: bytes | str = b"",
    ) -> None:
        self.app = app
        self.operation = operation
        self.status = status
        self.code = code
        self.body = body
        self.raw_body = _body_bytes(raw_body)
        super().__init__(message)


def decode_app_result(app: str, operation: str, result: Response[bytes]) -> JsonValue:
    return decode_app_body(app, operation, int(result.status or 0), result.body)


def decode_graphql_result(app: str, result: Response[bytes]) -> JsonValue:
    decoded = decode_app_result(app, "graphql", result)
    try:
        _raise_graphql_errors(app, result.body, parse_operation_result_json(result.body))
    except ValueError:
        pass
    _raise_graphql_errors(app, result.body, decoded)
    return decoded


def _raise_graphql_errors(app: str, raw_body: bytes, value: JsonValue) -> None:
    if not isinstance(value, dict):
        return
    errors = value.get("errors")
    if isinstance(errors, list) and errors:
        raise InvokeError(
            _graphql_error_message(errors),
            app=app,
            operation="graphql",
            code="graphql_errors",
            body=value,
            raw_body=raw_body,
        )


def decode_app_body(app: str, operation: str, status: int, body: bytes) -> JsonValue:
    try:
        parsed = parse_operation_result_json(body)
    except ValueError:
        if status >= 400:
            raise InvokeError(
                f"app invoke failed with status {status}",
                app=app,
                operation=operation,
                status=status,
                raw_body=body,
            )
        raise InvokeError(
            "app invoke response is not valid JSON",
            app=app,
            operation=operation,
            raw_body=body,
        )

    if status >= 400:
        message, code = _message_code_from_body(parsed)
        raise InvokeError(
            message or f"app invoke failed with status {status}",
            app=app,
            operation=operation,
            status=status,
            code=code,
            body=parsed,
            raw_body=body,
        )

    if isinstance(parsed, dict) and isinstance(parsed.get("status"), str):
        if parsed["status"] == "error":
            message, code = _message_code_from_body(parsed)
            raise InvokeError(
                message or "app invoke failed",
                app=app,
                operation=operation,
                code=code,
                body=parsed,
                raw_body=body,
            )
        if parsed["status"] == "success" and "data" in parsed:
            return parsed["data"]
    return parsed


def parse_operation_result_json(body: bytes) -> JsonValue:
    if body.strip() == b"":
        return {}
    return cast(JsonValue, json.loads(body))


def _message_code_from_body(value: JsonValue) -> tuple[str | None, str | None]:
    if not isinstance(value, dict):
        return None, None
    message: str | None = None
    code: str | None = None
    error = value.get("error")
    if isinstance(error, dict):
        if isinstance(error.get("message"), str) and error["message"].strip():
            message = error["message"]
        if isinstance(error.get("code"), str) and error["code"].strip():
            code = error["code"]
    if message is None and isinstance(value.get("message"), str) and value["message"].strip():
        message = value["message"]
    if code is None and isinstance(value.get("code"), str) and value["code"].strip():
        code = value["code"]
    return message, code


def _graphql_error_message(errors: list[JsonValue]) -> str:
    first = errors[0] if errors else None
    if isinstance(first, dict) and isinstance(first.get("message"), str) and first["message"].strip():
        return first["message"]
    return "GraphQL returned errors"
