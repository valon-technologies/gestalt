"""Factory for the public Gestalt transport client."""

from __future__ import annotations

from collections.abc import Callable, Iterator
from contextlib import contextmanager
from dataclasses import dataclass
from typing import Literal, overload
from urllib.parse import urlparse

import grpc as _grpc

from gestalt.public.generated.agent_client import AgentClient, AgentClientREST
from gestalt.public.generated.app_client import AppClient
from gestalt.public.generated.authorization_client import (
    AuthorizationClient,
    AuthorizationClientREST,
)
from gestalt.public.generated.external_credentials_client import (
    ExternalCredentialsClient,
)
from gestalt.public.generated.identity_client import IdentityClient, IdentityClientREST
from gestalt.public.generated.indexeddb_client import IndexedDBClient
from gestalt.public.generated.unary_transport import UnaryTransport
from gestalt.public.generated.workflow_client import WorkflowClient, WorkflowClientREST

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
    """Legacy unified public Gestalt client (App-only surface)."""

    app: AppClient
    _close: Callable[[], None]

    def close(self) -> None:
        self._close()

    def __enter__(self) -> GestaltClient:
        return self

    def __exit__(self, *args: object) -> None:
        self.close()


@dataclass(slots=True)
class RestGestaltClient(GestaltClient):
    """REST-backed public Gestalt client (five REST-capable services)."""

    agent: AgentClientREST
    workflow: WorkflowClientREST
    identity: IdentityClientREST
    authorization: AuthorizationClientREST


@dataclass(slots=True)
class GrpcGestaltClient(GestaltClient):
    """gRPC-backed public Gestalt client (all seven public services)."""

    agent: AgentClient
    workflow: WorkflowClient
    identity: IdentityClient
    authorization: AuthorizationClient
    indexed_db: IndexedDBClient
    external_credentials: ExternalCredentialsClient


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


@dataclass(slots=True)
class _CoreGestaltClients:
    app: AppClient
    agent: AgentClient
    workflow: WorkflowClient
    identity: IdentityClient
    authorization: AuthorizationClient


def _bind_core_clients(transport: UnaryTransport) -> _CoreGestaltClients:
    return _CoreGestaltClients(
        app=AppClient(transport),
        agent=AgentClient(transport),
        workflow=WorkflowClient(transport),
        identity=IdentityClient(transport),
        authorization=AuthorizationClient(transport),
    )


def _bind_rest_clients(
    transport: UnaryTransport, close: Callable[[], None]
) -> RestGestaltClient:
    core = _bind_core_clients(transport)
    return RestGestaltClient(
        app=core.app,
        agent=core.agent,
        workflow=core.workflow,
        identity=core.identity,
        authorization=core.authorization,
        _close=close,
    )


def _bind_grpc_clients(
    transport: UnaryTransport, close: Callable[[], None]
) -> GrpcGestaltClient:
    core = _bind_core_clients(transport)
    return GrpcGestaltClient(
        app=core.app,
        agent=core.agent,
        workflow=core.workflow,
        identity=core.identity,
        authorization=core.authorization,
        indexed_db=IndexedDBClient(transport),
        external_credentials=ExternalCredentialsClient(transport),
        _close=close,
    )


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
        client: RestGestaltClient | GrpcGestaltClient = _bind_rest_clients(
            rest_transport, rest_transport.close
        )
    elif transport.kind == "grpc":
        owns_channel = channel is None
        grpc_channel = channel or grpc_channel_from_address(normalized)
        grpc_transport = GrpcUnaryTransport(
            grpc_channel,
            auth,
            owns_channel=owns_channel,
        )
        client = _bind_grpc_clients(grpc_transport, grpc_transport.close)
    else:
        raise ValueError(f"unsupported transport: {transport!r}")
    try:
        yield client
    finally:
        client.close()
