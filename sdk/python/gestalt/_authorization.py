"""Read-only transport client for the host authorization provider."""

from __future__ import annotations

import os
import threading
from typing import Any, Protocol, cast
from urllib import parse as _urlparse

import grpc
from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import json_format

from ._gen.v1 import authorization_pb2 as _authorization_pb2
from ._gen.v1 import authorization_pb2_grpc as _authorization_pb2_grpc
from ._grpc_transport import (
    insecure_internal_channel,
    internal_channel_target,
    secure_internal_channel,
)

empty_pb2: Any = _empty_pb2
authorization_pb2: Any = _authorization_pb2
authorization_pb2_grpc: Any = _authorization_pb2_grpc

ENV_AUTHORIZATION_SOCKET = "GESTALT_AUTHORIZATION_SOCKET"
ENV_AUTHORIZATION_SOCKET_TOKEN = f"{ENV_AUTHORIZATION_SOCKET}_TOKEN"
_AUTHORIZATION_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token"

_shared_authorization_transport: dict[str, Any] = {
    "target": "",
    "token": "",
    "client": None,
}
_shared_authorization_lock = threading.Lock()


def AuthorizationSubject(*args: Any, **kwargs: Any) -> Any:
    """Create an authorization subject protocol value."""

    return authorization_pb2.Subject(*args, **kwargs)


def AuthorizationResource(*args: Any, **kwargs: Any) -> Any:
    """Create an authorization resource protocol value."""

    return authorization_pb2.Resource(*args, **kwargs)


def AuthorizationAction(*args: Any, **kwargs: Any) -> Any:
    """Create an authorization action protocol value."""

    return authorization_pb2.Action(*args, **kwargs)


def AccessEvaluationRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an authorization access-evaluation request."""

    return authorization_pb2.AccessEvaluationRequest(*args, **kwargs)


def ResourceSearchRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an authorization resource-search request."""

    return authorization_pb2.ResourceSearchRequest(*args, **kwargs)


def SubjectSearchRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an authorization subject-search request."""

    return authorization_pb2.SubjectSearchRequest(*args, **kwargs)


def ActionSearchRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an authorization action-search request."""

    return authorization_pb2.ActionSearchRequest(*args, **kwargs)


def ReadRelationshipsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an authorization relationship-read request."""

    return authorization_pb2.ReadRelationshipsRequest(*args, **kwargs)


class AuthorizationClient:
    """Read-only transport client for the host authorization provider."""

    def __init__(
        self,
        socket_target: str | None = None,
        relay_token: str | None = None,
        *,
        _shared: bool = False,
    ) -> None:
        target = _resolve_authorization_socket_target(socket_target)
        token = (
            relay_token
            if relay_token is not None
            else os.environ.get(ENV_AUTHORIZATION_SOCKET_TOKEN, "")
        ).strip()
        self._channel = _authorization_channel(target, token=token)
        self._stub = authorization_pb2_grpc.AuthorizationProviderStub(self._channel)
        self._closed = False
        self._shared = _shared

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        if self._shared:
            return
        self._close_channel()

    def _close_channel(self) -> None:
        if self._closed:
            return
        self._closed = True
        self._channel.close()

    def evaluate(self, request: Any) -> Any:
        """Evaluate one authorization request."""

        return self._stub.Evaluate(
            _authorization_message(
                request,
                authorization_pb2.AccessEvaluationRequest,
            )
        )

    def search_resources(self, request: Any) -> Any:
        """Search resources visible to a subject for an action."""

        return self._stub.SearchResources(
            _authorization_message(
                request,
                authorization_pb2.ResourceSearchRequest,
            )
        )

    def search_subjects(self, request: Any) -> Any:
        """Search subjects related to a resource and action."""

        return self._stub.SearchSubjects(
            _authorization_message(
                request,
                authorization_pb2.SubjectSearchRequest,
            )
        )

    def search_actions(self, request: Any) -> Any:
        """Search actions available between a subject and resource."""

        return self._stub.SearchActions(
            _authorization_message(
                request,
                authorization_pb2.ActionSearchRequest,
            )
        )

    def read_relationships(self, request: Any) -> Any:
        """Read authorization relationships matching a request."""

        return self._stub.ReadRelationships(
            _authorization_message(
                request,
                authorization_pb2.ReadRelationshipsRequest,
            )
        )

    def get_metadata(self) -> Any:
        """Return host authorization provider metadata."""

        return self._stub.GetMetadata(empty_pb2.Empty())

    def __enter__(self) -> AuthorizationClient:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


def Authorization() -> AuthorizationClient:
    """Return a cached read-only client for the host authorization provider."""

    target = _resolve_authorization_socket_target()
    token = os.environ.get(ENV_AUTHORIZATION_SOCKET_TOKEN, "").strip()
    shared = _shared_authorization_transport
    with _shared_authorization_lock:
        client = shared.get("client")
        if (
            client is not None
            and shared.get("target") == target
            and shared.get("token") == token
        ):
            return client

        client = AuthorizationClient(target, token, _shared=True)
        stale = shared.get("client")
        shared["target"] = target
        shared["token"] = token
        shared["client"] = client
        if stale is not None:
            stale._close_channel()
        return client


def _authorization_message(value: Any, message_type: Any) -> Any:
    if isinstance(value, message_type):
        return value
    message = message_type()
    if value is None:
        return message
    if isinstance(value, dict):
        json_format.ParseDict(value, message)
        return message
    raise TypeError(
        "authorization: expected "
        f"{message_type.__name__} or dict, got {type(value).__name__}"
    )


def _resolve_authorization_socket_target(
    socket_target: str | None = None,
) -> str:
    target = (
        socket_target
        if socket_target is not None
        else os.environ.get(ENV_AUTHORIZATION_SOCKET, "")
    ).strip()
    if not target:
        raise RuntimeError(f"authorization: {ENV_AUTHORIZATION_SOCKET} is not set")
    return target


def _authorization_channel(raw_target: str, *, token: str = "") -> grpc.Channel:
    target = raw_target.strip()
    if not target:
        raise RuntimeError("authorization: transport target is required")
    if target.startswith("tcp://"):
        address = target[len("tcp://") :].strip()
        if not address:
            raise RuntimeError(
                f"authorization: tcp target {raw_target!r} is missing host:port"
            )
        return _with_authorization_relay_token(
            insecure_internal_channel(internal_channel_target("tcp", address)),
            token,
        )
    if target.startswith("tls://"):
        address = target[len("tls://") :].strip()
        if not address:
            raise RuntimeError(
                f"authorization: tls target {raw_target!r} is missing host:port"
            )
        return _with_authorization_relay_token(
            secure_internal_channel(internal_channel_target("tls", address)),
            token,
        )
    if target.startswith("unix://"):
        socket_path = target[len("unix://") :].strip()
        if not socket_path:
            raise RuntimeError(
                f"authorization: unix target {raw_target!r} is missing a socket path"
            )
        return _with_authorization_relay_token(
            insecure_internal_channel(internal_channel_target("unix", socket_path)),
            token,
        )
    if "://" in target:
        parsed = _urlparse.urlparse(target)
        raise RuntimeError(
            f"authorization: unsupported target scheme {parsed.scheme!r}"
        )
    return _with_authorization_relay_token(
        insecure_internal_channel(internal_channel_target("unix", target)),
        token,
    )


def _with_authorization_relay_token(
    channel: grpc.Channel,
    token: str,
) -> grpc.Channel:
    token = token.strip()
    if not token:
        return channel
    return grpc.intercept_channel(channel, _RelayTokenInterceptor(token))


class _ClientCallDetails(grpc.ClientCallDetails):
    def __init__(
        self,
        method: str,
        timeout: float | None,
        metadata: Any,
        credentials: Any,
        wait_for_ready: bool | None,
        compression: Any,
    ) -> None:
        self.method = method
        self.timeout = timeout
        self.metadata = metadata
        self.credentials = credentials
        self.wait_for_ready = wait_for_ready
        self.compression = compression


class _RelayTokenInterceptor(grpc.UnaryUnaryClientInterceptor):
    def __init__(self, token: str) -> None:
        self._token = token

    def intercept_unary_unary(
        self,
        continuation: Any,
        client_call_details: grpc.ClientCallDetails,
        request: Any,
    ) -> Any:
        fields = cast(_ClientCallDetailsFields, client_call_details)
        metadata = list(fields.metadata or [])
        metadata.append((_AUTHORIZATION_RELAY_TOKEN_HEADER, self._token))
        updated_details = _ClientCallDetails(
            method=fields.method,
            timeout=fields.timeout,
            metadata=metadata,
            credentials=fields.credentials,
            wait_for_ready=fields.wait_for_ready,
            compression=fields.compression,
        )
        return continuation(updated_details, request)


class _ClientCallDetailsFields(Protocol):
    method: str
    timeout: float | None
    metadata: Any
    credentials: Any
    wait_for_ready: bool | None
    compression: Any
