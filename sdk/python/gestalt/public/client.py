"""Factory for the public Gestalt transport client."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Literal, overload
from urllib.parse import urlparse

import grpc

from .auth import Auth, NoAuth
from .generated.agent import AgentGRPC, AgentREST
from .generated.app import AppGRPC, AppREST
from .generated.authorization import AuthorizationGRPC, AuthorizationREST
from .generated.external_credential import ExternalCredentialsGRPC
from .generated.identity import IdentityGRPC, IdentityREST
from .generated.indexeddb import IndexedDBGRPC
from .generated.workflow import WorkflowGRPC, WorkflowREST
from .rest_transport import RestTransport

CALLER_BEARER_TOKEN_METADATA_KEY = "x-gestalt-caller-bearer-token"


@dataclass(slots=True)
class GestaltRestClient:
    transport: RestTransport
    app: AppREST
    agent: AgentREST
    workflow: WorkflowREST
    identity: IdentityREST
    authorization: AuthorizationREST


@dataclass(slots=True)
class GestaltGrpcClient:
    app: AppGRPC
    agent: AgentGRPC
    workflow: WorkflowGRPC
    identity: IdentityGRPC
    authorization: AuthorizationGRPC
    indexed_db: IndexedDBGRPC
    external_credentials: ExternalCredentialsGRPC


@overload
def create_gestalt_client(
    *,
    address: str,
    auth: Auth,
    transport: Literal["rest"] = "rest",
) -> GestaltRestClient: ...


@overload
def create_gestalt_client(
    *,
    address: str,
    auth: Auth,
    transport: Literal["grpc"],
    channel: grpc.Channel | None = None,
) -> GestaltGrpcClient: ...


def create_gestalt_client(
    *,
    address: str = "",
    auth: Auth,
    transport: Literal["rest", "grpc"] = "rest",
    channel: grpc.Channel | None = None,
    base_url: str | None = None,
) -> GestaltRestClient | GestaltGrpcClient:
    resolved_address = (address or base_url or "").strip()
    if transport == "grpc":
        if not resolved_address and channel is None:
            raise ValueError(
                "address is required for external gRPC "
                "(use gestalt_from_context for bound provider access)"
            )
        grpc_channel = channel
        if grpc_channel is None:
            grpc_channel = _grpc_channel_from_address(
                _normalize_address(resolved_address)
            )
        return _grpc_clients(_with_auth_interceptor(grpc_channel, auth))
    rest = RestTransport(_normalize_address(resolved_address), auth)
    return GestaltRestClient(
        transport=rest,
        app=AppREST(rest),
        agent=AgentREST(rest),
        workflow=WorkflowREST(rest),
        identity=IdentityREST(rest),
        authorization=AuthorizationREST(rest),
    )


def gestalt_from_context(
    *,
    context: Any | None = None,
    caller_bearer_token: str = "",
) -> GestaltGrpcClient:
    """Returns a gRPC client bound to the host-service relay."""
    import os

    from gestalt._api import current_plugin_request
    from gestalt._grpc_transport import host_service_channel

    active = current_plugin_request()
    if context is None and active is not None:
        context = active.context
    resolved_caller = caller_bearer_token or (
        active.caller_bearer_token if active else ""
    )

    target = os.environ.get("GESTALT_HOST_SERVICE_SOCKET", "").strip()
    if not target:
        raise RuntimeError("GESTALT_HOST_SERVICE_SOCKET is not set")
    token = os.environ.get("GESTALT_HOST_SERVICE_TOKEN", "").strip()
    channel = host_service_channel("app", target, token=token)
    wire_context = _wire_request_context(context)
    channel = grpc.intercept_channel(
        channel,
        _BoundRequestInterceptor(
            context=wire_context,
            caller_bearer_token=resolved_caller,
        ),
    )
    return _grpc_clients(channel)


def _grpc_clients(channel: grpc.Channel) -> GestaltGrpcClient:
    return GestaltGrpcClient(
        app=AppGRPC(channel),
        agent=AgentGRPC(channel),
        workflow=WorkflowGRPC(channel),
        identity=IdentityGRPC(channel),
        authorization=AuthorizationGRPC(channel),
        indexed_db=IndexedDBGRPC(channel),
        external_credentials=ExternalCredentialsGRPC(channel),
    )


def _normalize_address(address: str) -> str:
    address = address.strip()
    if not address:
        raise ValueError(
            "address is required for external clients "
            "(use gestalt_from_context for bound provider access)"
        )
    parsed = urlparse(address)
    if not parsed.scheme or not parsed.netloc:
        raise ValueError(f"invalid address {address!r}")
    return address.rstrip("/")


def _grpc_channel_from_address(address: str) -> grpc.Channel:
    parsed = urlparse(address)
    target = parsed.netloc
    if parsed.scheme == "https":
        return grpc.secure_channel(target, grpc.ssl_channel_credentials())
    return grpc.insecure_channel(target)


def _wire_request_context(context: Any | None) -> Any | None:
    if context is None:
        return None
    if hasattr(context, "DESCRIPTOR"):
        return context
    from gestalt._codec.app import to_wire_request_context

    return to_wire_request_context(context)


def _with_auth_interceptor(channel: grpc.Channel, auth: Auth) -> grpc.Channel:
    if isinstance(auth, NoAuth):
        return channel
    authorization = auth.authorization_header()
    if not authorization:
        return channel
    return grpc.intercept_channel(channel, _AuthorizationInterceptor(authorization))


class _AuthorizationInterceptor(
    grpc.UnaryUnaryClientInterceptor,
    grpc.UnaryStreamClientInterceptor,
    grpc.StreamUnaryClientInterceptor,
    grpc.StreamStreamClientInterceptor,
):
    def __init__(self, authorization: str) -> None:
        self._authorization = authorization

    def _inject(self, client_call_details):
        metadata = list(client_call_details.metadata or [])
        metadata.append(("authorization", self._authorization))
        return client_call_details._replace(metadata=metadata)

    def intercept_unary_unary(self, continuation, client_call_details, request):
        return continuation(self._inject(client_call_details), request)

    def intercept_unary_stream(self, continuation, client_call_details, request):
        return continuation(self._inject(client_call_details), request)

    def intercept_stream_unary(self, continuation, client_call_details, request_iterator):
        return continuation(self._inject(client_call_details), request_iterator)

    def intercept_stream_stream(self, continuation, client_call_details, request_iterator):
        return continuation(self._inject(client_call_details), request_iterator)


class _BoundRequestInterceptor(
    grpc.UnaryUnaryClientInterceptor,
    grpc.UnaryStreamClientInterceptor,
    grpc.StreamUnaryClientInterceptor,
    grpc.StreamStreamClientInterceptor,
):
    def __init__(self, *, context: Any | None, caller_bearer_token: str) -> None:
        self._context = context
        self._caller_bearer_token = caller_bearer_token.strip()

    def _inject_metadata(self, client_call_details):
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
