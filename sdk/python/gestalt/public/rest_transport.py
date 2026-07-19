"""httpx-based REST transports for the public Gestalt API (/api/v2).

``RestUnaryTransport`` is sync (backed by ``httpx.Client``);
``AsyncRestUnaryTransport`` is async (backed by ``httpx.AsyncClient``). Both
share request building and response decoding via ``rest_request``.
"""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any, Protocol, TypeVar, cast

import httpx
from google.protobuf.message import Message

from .auth import AuthProvider
from .generated.metadata import Method
from .rest_request import (
    build_rest_request,
    decode_rest_response,
    map_httpx_error,
)

ResponseT = TypeVar("ResponseT", bound=Message)


class HttpClient(Protocol):
    def request(
        self,
        method: str,
        url: str,
        *,
        headers: Mapping[str, str] | None = None,
        params: str | None = None,
        json: dict[str, Any] | None = None,
        timeout: float | None = None,
    ) -> httpx.Response: ...


class AsyncHttpClient(Protocol):
    async def request(
        self,
        method: str,
        url: str,
        *,
        headers: Mapping[str, str] | None = None,
        params: str | None = None,
        json: dict[str, Any] | None = None,
        timeout: float | None = None,
    ) -> httpx.Response: ...


class RestUnaryTransport:
    """Protobuf-JSON REST transport implementing the generated UnaryTransport protocol."""

    def __init__(
        self,
        base_url: str,
        auth: AuthProvider,
        *,
        client: HttpClient | None = None,
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
        prepared = build_rest_request(method, request, self._base_url, self._auth)
        client = self._client or self._owned_client
        if client is None:
            raise RuntimeError("REST transport client is not available")

        try:
            response = client.request(
                prepared.verb,
                prepared.url,
                headers=prepared.headers,
                params=prepared.params,
                json=prepared.json_body,
            )
        except (httpx.TimeoutException, httpx.HTTPError) as err:
            raise map_httpx_error(err) from err

        header_map = {key: value for key, value in response.headers.items()}
        return cast(
            ResponseT,
            decode_rest_response(
                response_type, response.status_code, response.content, header_map
            ),
        )

    def close(self) -> None:
        if self._owns_client and self._owned_client is not None:
            self._owned_client.close()
            self._owned_client = None


class AsyncRestUnaryTransport:
    """Async protobuf-JSON REST transport implementing the generated AsyncUnaryTransport protocol."""

    def __init__(
        self,
        base_url: str,
        auth: AuthProvider,
        *,
        client: AsyncHttpClient | None = None,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._auth = auth
        self._client = client
        self._owns_client = client is None
        self._owned_client: httpx.AsyncClient | None = None
        if self._owns_client:
            self._owned_client = httpx.AsyncClient(
                verify=True,
                transport=httpx.AsyncHTTPTransport(retries=0),
            )

    async def unary(
        self,
        method: Method,
        request: Message,
        response_type: type[ResponseT],
    ) -> ResponseT:
        prepared = build_rest_request(method, request, self._base_url, self._auth)
        client = self._client or self._owned_client
        if client is None:
            raise RuntimeError("async REST transport client is not available")

        try:
            response = await client.request(
                prepared.verb,
                prepared.url,
                headers=prepared.headers,
                params=prepared.params,
                json=prepared.json_body,
            )
        except (httpx.TimeoutException, httpx.HTTPError) as err:
            raise map_httpx_error(err) from err

        header_map = {key: value for key, value in response.headers.items()}
        return cast(
            ResponseT,
            decode_rest_response(
                response_type, response.status_code, response.content, header_map
            ),
        )

    async def close(self) -> None:
        if self._owns_client and self._owned_client is not None:
            await self._owned_client.aclose()
            self._owned_client = None
