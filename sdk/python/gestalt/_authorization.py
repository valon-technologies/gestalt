"""Read-only authorization transport helpers for authored Python plugins."""

from __future__ import annotations

import os
import threading
from collections.abc import Mapping
from typing import Any

import grpc
from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import json_format

from .gen.v1 import authorization_pb2 as _pb
from .gen.v1 import authorization_pb2_grpc as _pb_grpc

pb: Any = _pb
pb_grpc: Any = _pb_grpc
empty_pb2: Any = _empty_pb2

ENV_AUTHORIZATION_SOCKET = "GESTALT_AUTHORIZATION_SOCKET"

_shared_transport_lock = threading.Lock()
_shared_socket_path = ""
_shared_client: "AuthorizationClient | None" = None


class AuthorizationClient:
    """Read-only client for the host-configured authorization provider."""

    def __init__(self, socket_path: str | None = None) -> None:
        resolved_socket_path = _resolve_authorization_socket_path(socket_path)
        self._socket_path = resolved_socket_path
        self._channel = grpc.insecure_channel(f"unix:{resolved_socket_path}")
        self._stub = pb_grpc.AuthorizationProviderStub(self._channel)

    def close(self) -> None:
        self._channel.close()

    def evaluate(self, request: Mapping[str, Any] | Any) -> Any:
        return self._stub.Evaluate(
            _coerce_request_message(request, pb.AccessEvaluationRequest)
        )

    def search_resources(self, request: Mapping[str, Any] | Any) -> Any:
        return self._stub.SearchResources(
            _coerce_request_message(request, pb.ResourceSearchRequest)
        )

    def search_subjects(self, request: Mapping[str, Any] | Any) -> Any:
        return self._stub.SearchSubjects(
            _coerce_request_message(request, pb.SubjectSearchRequest)
        )

    def search_actions(self, request: Mapping[str, Any] | Any) -> Any:
        return self._stub.SearchActions(
            _coerce_request_message(request, pb.ActionSearchRequest)
        )

    def read_relationships(self, request: Mapping[str, Any] | Any) -> Any:
        return self._stub.ReadRelationships(
            _coerce_request_message(request, pb.ReadRelationshipsRequest)
        )

    def get_metadata(self) -> Any:
        return self._stub.GetMetadata(empty_pb2.Empty())


def Authorization() -> AuthorizationClient:
    """Return the shared read-only authorization client for the current plugin."""

    socket_path = _resolve_authorization_socket_path()

    global _shared_client, _shared_socket_path
    with _shared_transport_lock:
        if _shared_client is not None and _shared_socket_path == socket_path:
            return _shared_client

        client = AuthorizationClient(socket_path)
        if _shared_client is not None:
            _shared_client.close()

        _shared_client = client
        _shared_socket_path = socket_path
        return client


def _resolve_authorization_socket_path(socket_path: str | None = None) -> str:
    trimmed = (socket_path or os.environ.get(ENV_AUTHORIZATION_SOCKET, "")).strip()
    if not trimmed:
        raise RuntimeError(
            f"authorization: {ENV_AUTHORIZATION_SOCKET} is not set"
        )
    return trimmed


def _coerce_request_message(value: Mapping[str, Any] | Any, factory: Any) -> Any:
    if isinstance(value, factory):
        return value
    if isinstance(value, Mapping):
        message = factory()
        json_format.ParseDict(dict(value), message)
        return message
    raise TypeError(
        f"authorization request must be a mapping or {factory.__name__}"
    )
