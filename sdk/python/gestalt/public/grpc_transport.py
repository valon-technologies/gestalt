"""grpcio transport for the public Gestalt API."""

from __future__ import annotations

from typing import TypeVar
from urllib.parse import urlparse

import grpc
from google.protobuf.message import Message

from gestalt._codec.support import call_unary
from gestalt._gen.v1 import app_pb2_grpc as _app_pb2_grpc
from gestalt.public.generated.metadata import (
    METHOD_APP_INVOKE,
    METHOD_APP_INVOKE_GRAPHQL,
    Method,
)

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
        self._stub = _app_pb2_grpc.AppStub(channel)

    def unary(
        self,
        method: Method,
        request: Message,
        response_type: type[ResponseT],
    ) -> ResponseT:
        timeout: float | None = None
        metadata: list[tuple[str, str]] = []
        authorization = self._auth.authorization_header()
        if authorization:
            metadata.append(("authorization", authorization))

        if method.full_method == METHOD_APP_INVOKE.full_method:
            return call_unary(
                lambda: self._stub.Invoke(
                    request,
                    timeout=timeout,
                    metadata=metadata or None,
                )
            )
        if method.full_method == METHOD_APP_INVOKE_GRAPHQL.full_method:
            return call_unary(
                lambda: self._stub.InvokeGraphQL(
                    request,
                    timeout=timeout,
                    metadata=metadata or None,
                )
            )
        raise ValueError(f"unknown public gRPC method {method.full_method}")

    def close(self) -> None:
        if self._owns_channel:
            self._channel.close()


def grpc_channel_from_address(address: str) -> grpc.Channel:
    parsed = urlparse(address)
    target = parsed.netloc
    if parsed.scheme == "https":
        return grpc.secure_channel(target, grpc.ssl_channel_credentials())
    return grpc.insecure_channel(target)
