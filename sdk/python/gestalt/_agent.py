from __future__ import annotations

import os
from typing import Any
from urllib import parse as _urlparse

import grpc

from .gen.v1 import agent_pb2 as _pb
from .gen.v1 import agent_pb2_grpc as _pb_grpc

pb: Any = _pb
pb_grpc: Any = _pb_grpc

ENV_AGENT_HOST_SOCKET = "GESTALT_AGENT_HOST_SOCKET"
ENV_AGENT_MANAGER_SOCKET = "GESTALT_AGENT_MANAGER_SOCKET"
ENV_AGENT_MANAGER_SOCKET_TOKEN = f"{ENV_AGENT_MANAGER_SOCKET}_TOKEN"
_AGENT_MANAGER_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token"


class AgentHost:
    def __init__(self) -> None:
        socket_path = os.environ.get(ENV_AGENT_HOST_SOCKET, "")
        if not socket_path:
            raise RuntimeError(f"{ENV_AGENT_HOST_SOCKET} is not set")
        self._channel = grpc.insecure_channel(f"unix:{socket_path}")
        self._stub = pb_grpc.AgentHostStub(self._channel)

    def close(self) -> None:
        self._channel.close()

    def execute_tool(self, request: Any) -> Any:
        return _grpc_call(self._stub.ExecuteTool, request)

    def emit_event(self, request: Any) -> Any:
        return _grpc_call(self._stub.EmitEvent, request)

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

        self._channel = _agent_manager_channel(target, token=relay_token)
        self._stub = pb_grpc.AgentManagerHostStub(self._channel)
        self._invocation_token = trimmed_token

    def close(self) -> None:
        self._channel.close()

    def run(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.Run, request)

    def get_run(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.GetRun, request)

    def list_runs(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ListRuns, request)

    def cancel_run(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.CancelRun, request)

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


def _agent_manager_channel(target: str, *, token: str = "") -> Any:
    scheme, address = _parse_agent_manager_target(target)
    if scheme == "unix":
        channel = grpc.insecure_channel(f"unix:{address}")
    elif scheme == "tcp":
        channel = grpc.insecure_channel(address)
    elif scheme == "tls":
        channel = grpc.secure_channel(address, grpc.ssl_channel_credentials())
    else:
        raise RuntimeError(f"unsupported agent manager transport scheme {scheme!r}")
    if token:
        channel = grpc.intercept_channel(channel, _RelayTokenInterceptor(token))
    return channel


def _parse_agent_manager_target(raw: str) -> tuple[str, str]:
    target = raw.strip()
    if not target:
        raise RuntimeError("agent manager: transport target is required")
    if target.startswith("tcp://"):
        address = target.removeprefix("tcp://").strip()
        if not address:
            raise RuntimeError(
                f"agent manager: tcp target {raw!r} is missing host:port"
            )
        return "tcp", address
    if target.startswith("tls://"):
        address = target.removeprefix("tls://").strip()
        if not address:
            raise RuntimeError(
                f"agent manager: tls target {raw!r} is missing host:port"
            )
        return "tls", address
    if target.startswith("unix://"):
        address = target.removeprefix("unix://").strip()
        if not address:
            raise RuntimeError(
                f"agent manager: unix target {raw!r} is missing a socket path"
            )
        return "unix", address
    if "://" in target:
        parsed = _urlparse.urlparse(target)
        raise RuntimeError(
            f"agent manager: unsupported target scheme {parsed.scheme!r}"
        )
    return "unix", target


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError:
        raise
