"""grpcio transport for the public Gestalt API."""

from __future__ import annotations

from typing import TypeVar
from urllib.parse import urlparse

import grpc
from google.protobuf.message import Message

from gestalt._codec.support import call_unary
from gestalt._grpc_transport import insecure_internal_channel, secure_internal_channel
from gestalt.public.generated.metadata import Method

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
        self._rpc_cache: dict[tuple[str, type[Message]], grpc.UnaryUnaryMultiCallable] = {}

    def unary(
        self,
        method: Method,
        request: Message,
        response_type: type[ResponseT],
    ) -> ResponseT:
        cache_key = (method.full_method, response_type)
        rpc = self._rpc_cache.get(cache_key)
        if rpc is None:
            rpc = self._channel.unary_unary(
                method.full_method,
                request_serializer=lambda value: value.SerializeToString(),
                response_deserializer=response_type.FromString,
                _registered_method=True,
            )
            self._rpc_cache[cache_key] = rpc

        metadata: list[tuple[str, str]] = []
        authorization = self._auth.authorization_header()
        if authorization:
            metadata.append(("authorization", authorization))

        return call_unary(
            lambda: rpc(request, metadata=metadata or None),
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
