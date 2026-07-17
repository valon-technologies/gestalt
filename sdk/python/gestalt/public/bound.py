"""Bound provider gRPC client derived from request context."""

from __future__ import annotations

import os
from typing import Any

import grpc

from gestalt._api import native_request_context
from gestalt._codec.app import to_wire_request_context
from gestalt._grpc_transport import (
    ENV_HOST_SERVICE_SOCKET,
    ENV_HOST_SERVICE_TOKEN,
    host_service_channel,
)
from gestalt.public.generated.app_client import AppClient

from .client import GestaltClient
from .grpc_transport import GrpcUnaryTransport

CALLER_BEARER_TOKEN_METADATA_KEY = "x-gestalt-caller-bearer-token"


def gestalt_from_request(
    request: Any,
    *,
    caller_bearer_token: str = "",
) -> GestaltClient:
    """Return a bound public client for provider-originated relay calls."""
    target = os.environ.get(ENV_HOST_SERVICE_SOCKET, "").strip()
    if not target:
        raise RuntimeError(f"{ENV_HOST_SERVICE_SOCKET} is not set")
    token = os.environ.get(ENV_HOST_SERVICE_TOKEN, "").strip()
    channel = host_service_channel("app", target, token=token)
    wire_context = _wire_request_context(getattr(request, "context", None))
    resolved_caller = caller_bearer_token.strip() or getattr(request, "token", "")
    channel = grpc.intercept_channel(
        channel,
        _BoundRequestInterceptor(
            context=wire_context,
            caller_bearer_token=resolved_caller,
        ),
    )
    transport = GrpcUnaryTransport(channel, _NoAuth(), owns_channel=True)
    return GestaltClient(app=AppClient(transport), _close=transport.close)


def _wire_request_context(context: Any | None) -> Any | None:
    native = native_request_context(context)
    if native is None:
        return None
    return to_wire_request_context(native)


class _NoAuth:
    def authorization_header(self) -> str | None:
        return None


class _BoundRequestInterceptor(
    grpc.UnaryUnaryClientInterceptor,
    grpc.UnaryStreamClientInterceptor,
    grpc.StreamUnaryClientInterceptor,
    grpc.StreamStreamClientInterceptor,
):
    def __init__(self, *, context: Any | None, caller_bearer_token: str) -> None:
        self._context = context
        self._caller_bearer_token = caller_bearer_token.strip()

    def _inject_metadata(self, client_call_details: Any) -> Any:
        if not self._caller_bearer_token:
            return client_call_details
        metadata = list(client_call_details.metadata or [])
        metadata.append(
            (CALLER_BEARER_TOKEN_METADATA_KEY, self._caller_bearer_token)
        )
        return client_call_details._replace(metadata=metadata)

    def _inject_context(self, request: Any) -> Any:
        if self._context is None or not hasattr(request, "context"):
            return request
        if hasattr(request, "HasField") and request.HasField("context"):
            return request
        if getattr(request, "context", None):
            return request
        request.context.CopyFrom(self._context)
        return request

    def intercept_unary_unary(self, continuation, client_call_details, request):
        return continuation(
            self._inject_metadata(client_call_details),
            self._inject_context(request),
        )

    def intercept_unary_stream(self, continuation, client_call_details, request):
        return continuation(
            self._inject_metadata(client_call_details),
            self._inject_context(request),
        )

    def intercept_stream_unary(self, continuation, client_call_details, request_iterator):
        return continuation(self._inject_metadata(client_call_details), request_iterator)

    def intercept_stream_stream(self, continuation, client_call_details, request_iterator):
        return continuation(self._inject_metadata(client_call_details), request_iterator)
