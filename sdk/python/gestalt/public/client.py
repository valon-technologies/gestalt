"""Factory for the public Gestalt transport client."""

from __future__ import annotations

from collections.abc import Callable, Iterator
from contextlib import contextmanager
from dataclasses import dataclass
from typing import Literal, overload
from urllib.parse import urlparse

import grpc as _grpc

from gestalt.public.generated.app_client import (
    AgentClient,
    AppClient,
    AuthorizationClient,
    ExternalCredentialsClient,
    IdentityClient,
    IndexedDBClient,
    WorkflowClient,
)

from .auth import Auth
from .grpc_transport import GrpcUnaryTransport, grpc_channel_from_address
from .rest_transport import HttpClient, RestUnaryTransport


@dataclass(frozen=True, slots=True)
class RestTransport:
    kind: Literal["rest"] = "rest"


@dataclass(frozen=True, slots=True)
class GrpcTransport:
    kind: Literal["grpc"] = "grpc"


def rest() -> RestTransport:
    return RestTransport()


def grpc() -> GrpcTransport:
    return GrpcTransport()


@dataclass(slots=True)
class GestaltClient:
    app: AppClient

    def close(self) -> None: ...


@dataclass(slots=True)
class BoundGestaltClient(GestaltClient):
    _close: Callable[[], None]

    def close(self) -> None:
        self._close()


@dataclass(slots=True)
class RestGestaltClient(GestaltClient):
    agent: AgentClient
    authorization: AuthorizationClient
    identity: IdentityClient
    workflow: WorkflowClient
    _close: Callable[[], None]

    def close(self) -> None:
        self._close()


@dataclass(slots=True)
class GrpcGestaltClient(RestGestaltClient):
    external_credentials: ExternalCredentialsClient
    indexed_db: IndexedDBClient


def _normalize_address(address: str) -> str:
    trimmed = address.strip()
    if not trimmed:
        raise ValueError("address is required")
    parsed = urlparse(trimmed)
    if not parsed.scheme or not parsed.netloc:
        raise ValueError(f"address must be an absolute URL: {address!r}")
    if parsed.scheme not in {"http", "https"}:
        raise ValueError(f"address must use http or https: {parsed.scheme!r}")
    return trimmed.rstrip("/")


@overload
@contextmanager
def create_gestalt_client(
    *,
    address: str,
    transport: RestTransport,
    auth: Auth,
    channel: _grpc.Channel | None = None,
    httpx_client: HttpClient | None = None,
) -> Iterator[RestGestaltClient]: ...


@overload
@contextmanager
def create_gestalt_client(
    *,
    address: str,
    transport: GrpcTransport,
    auth: Auth,
    channel: _grpc.Channel | None = None,
    httpx_client: HttpClient | None = None,
) -> Iterator[GrpcGestaltClient]: ...


@contextmanager
def create_gestalt_client(
    *,
    address: str,
    transport: RestTransport | GrpcTransport,
    auth: Auth,
    channel: _grpc.Channel | None = None,
    httpx_client: HttpClient | None = None,
) -> Iterator[RestGestaltClient | GrpcGestaltClient]:
    """Create an external public Gestalt client over REST or gRPC."""
    normalized = _normalize_address(address)
    if transport.kind == "rest":
        rest_transport = RestUnaryTransport(
            normalized,
            auth,
            client=httpx_client,
        )
        client = RestGestaltClient(
            app=AppClient(rest_transport),
            agent=AgentClient(rest_transport),
            authorization=AuthorizationClient(rest_transport),
            identity=IdentityClient(rest_transport),
            workflow=WorkflowClient(rest_transport),
            _close=rest_transport.close,
        )
    elif transport.kind == "grpc":
        owns_channel = channel is None
        grpc_channel = channel or grpc_channel_from_address(normalized)
        grpc_transport = GrpcUnaryTransport(
            grpc_channel,
            auth,
            owns_channel=owns_channel,
        )
        client = GrpcGestaltClient(
            app=AppClient(grpc_transport),
            agent=AgentClient(grpc_transport),
            authorization=AuthorizationClient(grpc_transport),
            identity=IdentityClient(grpc_transport),
            workflow=WorkflowClient(grpc_transport),
            external_credentials=ExternalCredentialsClient(grpc_transport),
            indexed_db=IndexedDBClient(grpc_transport),
            _close=grpc_transport.close,
        )
    else:
        raise ValueError(f"unsupported transport: {transport!r}")
    try:
        yield client
    finally:
        client.close()
