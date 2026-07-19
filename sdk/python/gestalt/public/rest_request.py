"""Shared request building and response decoding for REST transports.

Both the sync ``RestUnaryTransport`` and the async ``AsyncRestUnaryTransport``
use these pure helpers so request assembly and response decoding live in one
place. Only the HTTP I/O half differs between the transports.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

import httpx
from google.protobuf import json_format
from google.protobuf.message import Message

from gestalt._gen.v1 import app_pb2 as _app_pb2
from gestalt.rpc_support import GestaltError, GestaltErrorCode

from .auth import AuthProvider
from .errors import is_operation_result, parse_gateway_error
from .generated.metadata import Method
from .rest_mapping import (
    build_rest_body,
    build_rest_path,
    build_rest_query,
    encode_query_string,
)


@dataclass(frozen=True, slots=True)
class PreparedRestRequest:
    """Assembled REST request, ready for an HTTP send."""

    verb: str
    url: str
    headers: dict[str, str]
    params: str | None
    json_body: dict[str, Any] | None


def build_rest_request(
    method: Method,
    request: Message,
    base_url: str,
    auth: AuthProvider,
) -> PreparedRestRequest:
    """Build the REST request: serialize to protobuf-JSON, substitute path
    parameters, assemble headers, and build the query string or JSON body.

    Pure; performs no I/O.
    """
    if not method.http_verb or not method.http_path:
        raise ValueError(f"method {method.full_method} has no HTTP binding")

    request_json: dict[str, Any] = json_format.MessageToDict(
        request,
        preserving_proto_field_name=False,
    )
    path = build_rest_path(method, request_json)
    url = f"{base_url}{path}"
    headers: dict[str, str] = {
        "Accept": "application/json",
        "Content-Type": "application/json",
    }
    authorization = auth.authorization_header()
    if authorization:
        headers["Authorization"] = authorization

    params: str | None = None
    json_body: dict[str, Any] | None = None
    if method.http_verb in {"GET", "DELETE"}:
        query = build_rest_query(method, request_json)
        if query:
            params = encode_query_string(query)
    else:
        json_body = build_rest_body(method, request_json)

    return PreparedRestRequest(
        verb=method.http_verb,
        url=url,
        headers=headers,
        params=params,
        json_body=json_body,
    )


def decode_rest_response(
    response_type: type[Message],
    status_code: int,
    content: bytes,
    headers: dict[str, str],
) -> Message:
    """Decode a REST response body into a protobuf message.

    Handles the OperationResult envelope, error-status mapping, empty bodies,
    and protobuf-JSON decode. Pure; performs no I/O.
    """
    if is_operation_result(headers):
        return _fill_operation_result(response_type, status_code, content, headers)

    if status_code >= 400:
        raise parse_gateway_error(status_code, content)
    if not content.strip():
        return response_type()
    message = response_type()
    json_format.Parse(content, message)
    return message


def map_httpx_error(err: Exception) -> GestaltError:
    """Map an httpx transport error to a GestaltError with the right code."""
    if isinstance(err, httpx.TimeoutException):
        return GestaltError(
            GestaltErrorCode.DEADLINE_EXCEEDED,
            str(err) or "request deadline exceeded",
        )
    return GestaltError(GestaltErrorCode.UNAVAILABLE, str(err))


def _fill_operation_result(
    response_type: type[Message],
    status_code: int,
    body: bytes,
    headers: dict[str, str],
) -> Message:
    message = response_type()
    if not isinstance(message, _app_pb2.OperationResult):
        if not body.strip():
            return message
        json_format.Parse(body, message)
        return message

    header_values: dict[str, list[str]] = {}
    for key, value in headers.items():
        if key.lower() == "x-gestalt-response-kind":
            continue
        header_values.setdefault(key, []).append(value)

    message.status = status_code
    message.body = body
    message.headers.clear()
    for key, values in header_values.items():
        entry = message.headers[key]
        entry.values.extend(values)
    return message
