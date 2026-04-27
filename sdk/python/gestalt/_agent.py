from __future__ import annotations

import os
from typing import Any
from urllib import parse as _urlparse

import grpc

from ._grpc_transport import (
    insecure_internal_channel,
    internal_channel_target,
    secure_internal_channel,
)
from .gen.v1 import agent_pb2 as _pb
from .gen.v1 import agent_pb2_grpc as _pb_grpc

pb: Any = _pb
pb_grpc: Any = _pb_grpc

ENV_AGENT_HOST_SOCKET = "GESTALT_AGENT_HOST_SOCKET"
ENV_AGENT_HOST_SOCKET_TOKEN = f"{ENV_AGENT_HOST_SOCKET}_TOKEN"
ENV_AGENT_MANAGER_SOCKET = "GESTALT_AGENT_MANAGER_SOCKET"
ENV_AGENT_MANAGER_SOCKET_TOKEN = f"{ENV_AGENT_MANAGER_SOCKET}_TOKEN"
_AGENT_MANAGER_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token"


class AgentHost:
    def __init__(self) -> None:
        target = os.environ.get(ENV_AGENT_HOST_SOCKET, "")
        if not target:
            raise RuntimeError(f"{ENV_AGENT_HOST_SOCKET} is not set")
        relay_token = os.environ.get(ENV_AGENT_HOST_SOCKET_TOKEN, "")
        self._channel = _host_service_channel("agent host", target, token=relay_token)
        self._stub = pb_grpc.AgentHostStub(self._channel)

    def close(self) -> None:
        self._channel.close()

    def execute_tool(self, request: Any) -> Any:
        return _grpc_call(self._stub.ExecuteTool, request)

    def search_tools(self, request: Any) -> Any:
        return _grpc_call(self._stub.SearchTools, request)

    def __enter__(self) -> AgentHost:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


class AgentManager:
    def __init__(self, invocation_token: str) -> None:
        trimmed_token = invocation_token.strip()
        if not trimmed_token:
            raise RuntimeError("agent manager: invocation token is not available")

        target = os.environ.get(ENV_AGENT_MANAGER_SOCKET, "")
        if not target:
            raise RuntimeError(f"agent manager: {ENV_AGENT_MANAGER_SOCKET} is not set")
        relay_token = os.environ.get(ENV_AGENT_MANAGER_SOCKET_TOKEN, "")

        self._channel = _host_service_channel("agent manager", target, token=relay_token)
        self._stub = pb_grpc.AgentManagerHostStub(self._channel)
        self._invocation_token = trimmed_token

    def close(self) -> None:
        self._channel.close()

    def create_session(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.CreateSession, request)

    def get_session(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.GetSession, request)

    def list_sessions(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ListSessions, request)

    def update_session(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.UpdateSession, request)

    def create_turn(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.CreateTurn, request)

    def get_turn(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.GetTurn, request)

    def list_turns(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ListTurns, request)

    def cancel_turn(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.CancelTurn, request)

    def list_turn_events(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ListTurnEvents, request)

    def list_interactions(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ListInteractions, request)

    def resolve_interaction(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ResolveInteraction, request)

    def __enter__(self) -> AgentManager:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


class _RelayTokenInterceptor(grpc.UnaryUnaryClientInterceptor):
    def __init__(self, token: str) -> None:
        self._token = token

    def intercept_unary_unary(self, continuation: Any, client_call_details: Any, request: Any) -> Any:
        metadata = list(client_call_details.metadata or [])
        metadata.append((_AGENT_MANAGER_RELAY_TOKEN_HEADER, self._token))
        details = _ClientCallDetails(
            method=client_call_details.method,
            timeout=client_call_details.timeout,
            metadata=metadata,
            credentials=client_call_details.credentials,
            wait_for_ready=client_call_details.wait_for_ready,
            compression=client_call_details.compression,
        )
        return continuation(details, request)


class _ClientCallDetails(grpc.ClientCallDetails):
    def __init__(
        self,
        *,
        method: str,
        timeout: float | None,
        metadata: list[tuple[str, str]],
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


def _host_service_channel(service_name: str, target: str, *, token: str = "") -> Any:
    scheme, address = _parse_host_service_target(service_name, target)
    if scheme == "unix":
        channel = insecure_internal_channel(internal_channel_target("unix", address))
    elif scheme == "tcp":
        channel = insecure_internal_channel(internal_channel_target("tcp", address))
    elif scheme == "tls":
        channel = secure_internal_channel(internal_channel_target("tls", address))
    else:
        raise RuntimeError(f"unsupported {service_name} transport scheme {scheme!r}")
    if token:
        channel = grpc.intercept_channel(channel, _RelayTokenInterceptor(token))
    return channel


def _parse_host_service_target(service_name: str, raw: str) -> tuple[str, str]:
    target = raw.strip()
    if not target:
        raise RuntimeError(f"{service_name}: transport target is required")
    if target.startswith("tcp://"):
        address = target.removeprefix("tcp://").strip()
        if not address:
            raise RuntimeError(f"{service_name}: tcp target {raw!r} is missing host:port")
        return "tcp", address
    if target.startswith("tls://"):
        address = target.removeprefix("tls://").strip()
        if not address:
            raise RuntimeError(f"{service_name}: tls target {raw!r} is missing host:port")
        return "tls", address
    if target.startswith("unix://"):
        address = target.removeprefix("unix://").strip()
        if not address:
            raise RuntimeError(f"{service_name}: unix target {raw!r} is missing a socket path")
        return "unix", address
    if "://" in target:
        parsed = _urlparse.urlparse(target)
        raise RuntimeError(f"{service_name}: unsupported target scheme {parsed.scheme!r}")
    return "unix", target


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError:
        raise
