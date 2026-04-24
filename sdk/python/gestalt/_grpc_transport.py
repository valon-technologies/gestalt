"""Shared gRPC transport helpers for Gestalt-internal host-service channels."""

from __future__ import annotations

from typing import Any, cast

import grpc as _grpc

grpc: Any = cast(Any, _grpc)

_INTERNAL_CHANNEL_OPTIONS = (("grpc.enable_http_proxy", 0),)


def insecure_internal_channel(target: str) -> grpc.Channel:
    """Return an insecure internal gRPC channel with proxy use disabled."""

    return grpc.insecure_channel(target, options=_INTERNAL_CHANNEL_OPTIONS)


def secure_internal_channel(
    target: str, credentials: grpc.ChannelCredentials | None = None
) -> grpc.Channel:
    """Return a TLS internal gRPC channel with proxy use disabled."""

    if credentials is None:
        credentials = grpc.ssl_channel_credentials()
    return grpc.secure_channel(
        target,
        credentials,
        options=_INTERNAL_CHANNEL_OPTIONS,
    )
