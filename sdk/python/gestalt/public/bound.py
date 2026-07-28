"""Bound provider gRPC client derived from request context."""

from __future__ import annotations

import os
from dataclasses import dataclass
from typing import Any, Callable

import grpc

from gestalt._api import native_request_context
from gestalt._codec.app import to_wire_request_context
from gestalt._grpc_transport import (
    ENV_HOST_SERVICE_SOCKET,
    host_service_channel,
)

from .auth import unauthenticated
from .generated.app_client import AppClient
from .grpc_transport import GrpcUnaryTransport

CALLER_BEARER_TOKEN_METADATA_KEY = "x-gestalt-caller-bearer-token"


@dataclass(slots=True)
class BoundGestaltClient:
    """App-only public client bound to the provider host-service relay."""

    app: AppClient
    _close: Callable[[], None]

    def close(self) -> None:
        self._close()


def gestalt_from_request(
    request: Any,
    *,
    caller_bearer_token: str = "",
) -> BoundGestaltClient:
    """Return a bound public client for provider-originated relay calls."""
    target = os.environ.get(ENV_HOST_SERVICE_SOCKET, "").strip()
    if not target:
        raise RuntimeError(f"{ENV_HOST_SERVICE_SOCKET} is not set")
    relay_token = getattr(request, "relay_token", "").strip()
    channel = host_service_channel("app", target, token=relay_token)
    native = native_request_context(getattr(request, "context", None))
    wire_context = to_wire_request_context(native) if native is not None else None
    resolved_caller = caller_bearer_token.strip() or getattr(request, "token", "")
    channel = grpc.intercept_channel(
        channel,
        _BoundRequestInterceptor(
            context=wire_context,
            caller_bearer_token=resolved_caller,
        ),
    )
    transport = GrpcUnaryTransport(channel, unauthenticated(), owns_channel=True)
    return BoundGestaltClient(app=AppClient(transport), _close=transport.close)


class _BoundRequestInterceptor(grpc.UnaryUnaryClientInterceptor):
    def __init__(self, *, context: Any | None, caller_bearer_token: str) -> None:
        self._context = context
        self._caller_bearer_token = caller_bearer_token.strip()

    def intercept_unary_unary(self, continuation, client_call_details, request):
        if self._caller_bearer_token:
            metadata = list(client_call_details.metadata or [])
            metadata.append(
                (CALLER_BEARER_TOKEN_METADATA_KEY, self._caller_bearer_token)
            )
            client_call_details = client_call_details._replace(metadata=metadata)
        if (
            self._context is not None
            and hasattr(request, "context")
            and not (hasattr(request, "HasField") and request.HasField("context"))
            and not getattr(request, "context", None)
        ):
            request.context.CopyFrom(self._context)
        return continuation(client_call_details, request)
