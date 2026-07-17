"""Factory for the public Gestalt transport client."""

from __future__ import annotations

from collections.abc import Callable, Iterator
from contextlib import contextmanager
from dataclasses import dataclass
from typing import Literal, overload

import grpc as _grpc
from typing_extensions import Never

from gestalt.public.generated.app_client import AppClient

from .address import normalize_address
from .auth import Auth, AuthProvider, auth_to_provider
from .grpc_transport import GrpcUnaryTransport, grpc_channel_from_address
from .rest_transport import RestUnaryTransport


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
    _close: Callable[[], None]

    def close(self) -> None:
        self._close()

    def __enter__(self) -> GestaltClient:
        return self

    def __exit__(self, *args: object) -> None:
        self.close()


@overload
def create_gestalt_client(
    *,
    address: str,
    transport: RestTransport,
    auth: Auth,
    httpx_client: object | None = None,
) -> Iterator[GestaltClient]: ...


@overload
def create_gestalt_client(
    *,
    address: str,
    transport: GrpcTransport,
    auth: Auth,
    channel: _grpc.Channel | None = None,
) -> Iterator[GestaltClient]: ...


@contextmanager
def create_gestalt_client(
    *,
    address: str,
    transport: RestTransport | GrpcTransport,
    auth: Auth,
    channel: _grpc.Channel | None = None,
    httpx_client: object | None = None,
) -> Iterator[GestaltClient]:
    """Create an external public Gestalt client over REST or gRPC."""
    normalized = normalize_address(address)
    provider = auth_to_provider(auth)
    if transport.kind == "rest":
        client = _build_rest_client(normalized, provider, httpx_client=httpx_client)
    elif transport.kind == "grpc":
        client = _build_grpc_client(normalized, provider, channel=channel)
    else:
        unknown: Never = transport
        raise ValueError(f"unsupported transport: {unknown!r}")
    try:
        yield client
    finally:
        client.close()


def _build_rest_client(
    base_url: str,
    auth: AuthProvider,
    *,
    httpx_client: object | None,
) -> GestaltClient:
    transport = RestUnaryTransport(
        base_url,
        auth,
        client=httpx_client,  # type: ignore[arg-type]
    )
    return GestaltClient(app=AppClient(transport), _close=transport.close)


def _build_grpc_client(
    address: str,
    auth: AuthProvider,
    *,
    channel: _grpc.Channel | None,
) -> GestaltClient:
    owns_channel = channel is None
    grpc_channel = channel or grpc_channel_from_address(address)
    transport = GrpcUnaryTransport(grpc_channel, auth, owns_channel=owns_channel)
    return GestaltClient(app=AppClient(transport), _close=transport.close)
