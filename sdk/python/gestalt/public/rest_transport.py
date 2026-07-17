"""httpx-based REST transport for the public Gestalt API (/api/v2)."""

from __future__ import annotations

from collections.abc import Mapping, Sequence
from typing import Any, Protocol, TypeVar

import httpx
from google.protobuf.message import Message

from gestalt.rpc_support import GestaltError, GestaltErrorCode

from .auth import AuthProvider
from .generated.metadata import Method
from .generated.transport_kernel import (
    RawRestResponse,
    decode_rest_response,
    prepare_rest_request,
)

ResponseT = TypeVar("ResponseT", bound=Message)


class HttpClient(Protocol):
    def request(
        self,
        method: str,
        url: str,
        *,
        headers: Mapping[str, str] | None = None,
        params: Sequence[tuple[str, str]] | str | None = None,
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
        prepared = prepare_rest_request(method, request)
        headers = {
            "Accept": "application/json",
            "Content-Type": "application/json",
        }
        authorization = self._auth.authorization_header()
        if authorization:
            headers["Authorization"] = authorization

        client = self._client or self._owned_client
        if client is None:
            raise RuntimeError("REST transport client is not available")

        try:
            response = client.request(
                prepared.verb,
                f"{self._base_url}{prepared.path}",
                headers=headers,
                params=list(prepared.query) or None,
                json=prepared.body,
            )
        except httpx.TimeoutException as err:
            raise GestaltError(
                GestaltErrorCode.DEADLINE_EXCEEDED,
                str(err) or "request deadline exceeded",
            ) from err
        except httpx.HTTPError as err:
            raise GestaltError(GestaltErrorCode.UNAVAILABLE, str(err)) from err

        return decode_rest_response(
            method,
            response_type,
            RawRestResponse(
                status=response.status_code,
                headers=tuple(response.headers.multi_items()),
                body=response.content,
            ),
        )

    def close(self) -> None:
        if self._owns_client and self._owned_client is not None:
            self._owned_client.close()
            self._owned_client = None
