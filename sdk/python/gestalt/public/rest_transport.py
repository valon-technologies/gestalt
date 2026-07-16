"""httpx-based REST transport for the public Gestalt API (/api/v2)."""

from __future__ import annotations

from typing import Any

import httpx
from google.protobuf import json_format
from google.protobuf.message import Message

from gestalt._gen.v1 import app_pb2 as _app_pb2
from gestalt.rpc_support import GestaltError, GestaltErrorCode

from .auth import Auth
from .errors import is_operation_result, parse_gateway_error
from .generated.metadata import Method
from .rest_mapping import (
    build_body_map,
    build_query_params,
    encode_query_string,
    path_param_names,
    substitute_path,
)


class RestTransport:
    """Protobuf-JSON REST transport for generated public clients."""

    def __init__(
        self,
        base_url: str,
        auth: Auth,
        *,
        client: httpx.Client | None = None,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._auth = auth
        self._client = client

    def call_unary(self, method: Method, request: Message | None, response_type: type[Message] | None):
        if not method.http_verb or not method.http_path:
            raise ValueError(f"method {method.full_method} has no HTTP binding")

        request_json: dict[str, Any] = {}
        if request is not None:
            request_json = json_format.MessageToDict(request, preserving_proto_field_name=False)

        path_params = set(path_param_names(method.http_path))
        path = substitute_path(method.http_path, request_json)
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
            query = build_query_params(request_json, path_params)
            if query:
                params = encode_query_string(query)
        elif request is not None:
            json_body = build_body_map(request_json, path_params)

        client = self._client or httpx.Client()
        owns_client = self._client is None
        try:
            response = client.request(
                method.http_verb,
                url,
                headers=headers,
                params=params,
                json=json_body,
            )
        except httpx.HTTPError as err:
            raise GestaltError(GestaltErrorCode.UNAVAILABLE, str(err)) from err
        finally:
            if owns_client:
                client.close()

        header_map = {key: value for key, value in response.headers.items()}
        if is_operation_result(header_map):
            if response_type is None:
                return None
            return _fill_operation_result(
                response_type,
                response.status_code,
                response.content,
                header_map,
            )

        if response.status_code >= 400:
            raise parse_gateway_error(response.status_code, response.content)
        if response_type is None:
            return None
        if not response.content.strip():
            return response_type()
        message = response_type()
        json_format.Parse(response.content, message)
        return message


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
