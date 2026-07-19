"""grpcio transports for the public Gestalt API.

``GrpcUnaryTransport`` is sync (backed by ``grpc.Channel``);
``AsyncGrpcUnaryTransport`` is async (backed by ``grpc.aio.Channel``).
"""

from __future__ import annotations

from typing import TypeVar
from urllib.parse import urlparse

import grpc
import grpc.aio
from google.protobuf.message import Message

from gestalt._codec.support import call_unary
from gestalt._grpc_transport import (
    _INTERNAL_CHANNEL_OPTIONS,
    insecure_internal_channel,
    secure_internal_channel,
)
from gestalt.public.generated.metadata import Method
from gestalt.rpc_support import GestaltError, GestaltErrorCode

from .auth import AuthProvider

ResponseT = TypeVar("ResponseT", bound=Message)


class GrpcUnaryTransport:
    """gRPC transport implementing the generated UnaryTransport protocol."""

    def __init__(
        self,
        channel: grpc.Channel,
        auth: AuthProvider,
        *,
        owns_channel: bool = False,
    ) -> None:
        self._channel = channel
        self._auth = auth
        self._owns_channel = owns_channel

    def unary(
        self,
        method: Method,
        request: Message,
        response_type: type[ResponseT],
    ) -> ResponseT:
        metadata: list[tuple[str, str]] = []
        authorization = self._auth.authorization_header()
        if authorization:
            metadata.append(("authorization", authorization))

        return call_unary(
            lambda: self._channel.unary_unary(
                method.full_method,
                request_serializer=request.SerializeToString,
                response_deserializer=response_type.FromString,
            )(request, metadata=metadata or None),
        )

    def close(self) -> None:
        if self._owns_channel:
            self._channel.close()


def grpc_channel_from_address(address: str) -> grpc.Channel:
    parsed = urlparse(address)
    target = parsed.netloc
    if parsed.scheme == "https":
        return secure_internal_channel(target)
    return insecure_internal_channel(target)


class AsyncGrpcUnaryTransport:
    """Async gRPC transport implementing the generated AsyncUnaryTransport protocol."""

    def __init__(
        self,
        channel: grpc.aio.Channel,
        auth: AuthProvider,
        *,
        owns_channel: bool = False,
    ) -> None:
        self._channel = channel
        self._auth = auth
        self._owns_channel = owns_channel

    async def unary(
        self,
        method: Method,
        request: Message,
        response_type: type[ResponseT],
    ) -> ResponseT:
        metadata: list[tuple[str, str]] = []
        authorization = self._auth.authorization_header()
        if authorization:
            metadata.append(("authorization", authorization))

        call = self._channel.unary_unary(
            method.full_method,
            request_serializer=lambda r: r.SerializeToString(),
            response_deserializer=lambda b: response_type.FromString(b),
        )(request, metadata=metadata or None)
        try:
            return await call
        except grpc.aio.AioRpcError as error:
            # AioRpcError is not a grpc.Call subclass, so to_gestalt_error
            # (which checks grpc.Call) cannot map it directly. Build the
            # GestaltError from the status code and details here.
            code = error.code()
            numeric = code.value[0] if code is not None else GestaltErrorCode.UNKNOWN
            raise GestaltError(int(numeric), str(error.details() or "")) from error

    async def close(self) -> None:
        if self._owns_channel:
            await self._channel.close()


def async_grpc_channel_from_address(address: str) -> grpc.aio.Channel:
    """Return an async gRPC channel for the given address.

    Applies the same internal channel options as the sync path
    (grpc.enable_http_proxy=0 and 64MB message-size limits) so async
    callers don't diverge from grpc_channel_from_address.
    """
    parsed = urlparse(address)
    target = parsed.netloc
    if parsed.scheme == "https":
        return grpc.aio.secure_channel(
            target,
            grpc.ssl_channel_credentials(),
            options=_INTERNAL_CHANNEL_OPTIONS,
        )
    return grpc.aio.insecure_channel(target, options=_INTERNAL_CHANNEL_OPTIONS)
