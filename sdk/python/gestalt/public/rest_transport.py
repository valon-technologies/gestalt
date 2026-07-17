"""httpx-based REST transport for the public Gestalt API (/api/v2)."""

from __future__ import annotations

from typing import Any, TypeVar, cast

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

ResponseT = TypeVar("ResponseT", bound=Message)


class RestUnaryTransport:
    """Protobuf-JSON REST transport implementing the generated UnaryTransport protocol."""

    def __init__(
        self,
        base_url: str,
        auth: AuthProvider,
        *,
        client: httpx.Client | None = None,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._auth = auth
        self._client = client
        self._owns_client = client is None
        self._owned_client: httpx.Client | None = None
        if self._owns_client:
            self._owned_client = httpx.Client(
                verify=True,
                transport=httpx.HTTPTransport(retries=0),
            )

    def unary(
        self,
        method: Method,
        request: Message,
        response_type: type[ResponseT],
    ) -> ResponseT:
        if not method.http_verb or not method.http_path:
            raise ValueError(f"method {method.full_method} has no HTTP binding")

        request_json: dict[str, Any] = json_format.MessageToDict(
            request,
            preserving_proto_field_name=False,
        )
        path = build_rest_path(method, request_json)
        url = f"{self._base_url}{path}"
        headers = {
            "Accept": "application/json",
            "Content-Type": "application/json",
        }
        authorization = self._auth.authorization_header()
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

        client = self._client or self._owned_client
        if client is None:
            raise RuntimeError("REST transport client is not available")

        try:
            response = client.request(
                method.http_verb,
                url,
                headers=headers,
                params=params,
                json=json_body,
            )
        except httpx.TimeoutException as err:
            raise GestaltError(
                GestaltErrorCode.DEADLINE_EXCEEDED,
                str(err) or "request deadline exceeded",
            ) from err
        except httpx.HTTPError as err:
            raise GestaltError(GestaltErrorCode.UNAVAILABLE, str(err)) from err

        header_map = {key: value for key, value in response.headers.items()}
        if is_operation_result(header_map):
            return cast(
                ResponseT,
                _fill_operation_result(
                    response_type,
                    response.status_code,
                    response.content,
                    header_map,
                ),
            )

        if response.status_code >= 400:
            raise parse_gateway_error(response.status_code, response.content)
        if not response.content.strip():
            return response_type()
        message = response_type()
        json_format.Parse(response.content, message)
        return message

    def close(self) -> None:
        if self._owns_client and self._owned_client is not None:
            self._owned_client.close()
            self._owned_client = None


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
