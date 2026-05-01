from __future__ import annotations

import os
from typing import Any

import grpc

from ._grpc_transport import host_service_channel
from .gen.v1 import agent_pb2 as _pb
from .gen.v1 import agent_pb2_grpc as _pb_grpc

pb: Any = _pb
pb_grpc: Any = _pb_grpc

ENV_AGENT_HOST_SOCKET = "GESTALT_AGENT_HOST_SOCKET"
ENV_AGENT_HOST_SOCKET_TOKEN = f"{ENV_AGENT_HOST_SOCKET}_TOKEN"
ENV_AGENT_MANAGER_SOCKET = "GESTALT_AGENT_MANAGER_SOCKET"
ENV_AGENT_MANAGER_SOCKET_TOKEN = f"{ENV_AGENT_MANAGER_SOCKET}_TOKEN"


class AgentHost:
    """Client for the agent host service available inside agent providers.

    ``AgentHost`` reads ``GESTALT_AGENT_HOST_SOCKET`` and its optional relay
    token from the environment and exposes the host RPCs that agent providers
    use to discover and call tools during a turn.
    """

    def __init__(self) -> None:
        target = os.environ.get(ENV_AGENT_HOST_SOCKET, "")
        if not target:
            raise RuntimeError(f"{ENV_AGENT_HOST_SOCKET} is not set")
        relay_token = os.environ.get(ENV_AGENT_HOST_SOCKET_TOKEN, "")
        self._channel = host_service_channel("agent host", target, token=relay_token)
        self._stub = pb_grpc.AgentHostStub(self._channel)

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def execute_tool(self, request: Any) -> Any:
        """Execute a host tool using an agent protocol request message."""

        return _grpc_call(self._stub.ExecuteTool, request)

    def list_tools(self, request: Any) -> Any:
        """List host tools visible to the current agent request."""

        return _grpc_call(self._stub.ListTools, request)

    def __enter__(self) -> AgentHost:
        """Return the client for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the client at the end of a context manager block."""

        self.close()


class AgentManager:
    """Client for managing agent sessions, turns, events, and interactions.

    The manager is for provider code that receives an invocation token and then
    needs to call the host's agent-management API. Each request passed to a
    method is mutated to include that invocation token before the RPC is sent.
    """

    def __init__(self, invocation_token: str) -> None:
        trimmed_token = invocation_token.strip()
        if not trimmed_token:
            raise RuntimeError("agent manager: invocation token is not available")

        target = os.environ.get(ENV_AGENT_MANAGER_SOCKET, "")
        if not target:
            raise RuntimeError(f"agent manager: {ENV_AGENT_MANAGER_SOCKET} is not set")
        relay_token = os.environ.get(ENV_AGENT_MANAGER_SOCKET_TOKEN, "")

        self._channel = host_service_channel("agent manager", target, token=relay_token)
        self._stub = pb_grpc.AgentManagerHostStub(self._channel)
        self._invocation_token = trimmed_token

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def create_session(self, request: Any) -> Any:
        """Create an agent session."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.CreateSession, request)

    def get_session(self, request: Any) -> Any:
        """Fetch one agent session."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.GetSession, request)

    def list_sessions(self, request: Any) -> Any:
        """List agent sessions visible to the invocation token."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ListSessions, request)

    def update_session(self, request: Any) -> Any:
        """Update mutable fields on an agent session."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.UpdateSession, request)

    def create_turn(self, request: Any) -> Any:
        """Create an agent turn."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.CreateTurn, request)

    def get_turn(self, request: Any) -> Any:
        """Fetch one agent turn."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.GetTurn, request)

    def list_turns(self, request: Any) -> Any:
        """List turns for an agent session."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ListTurns, request)

    def cancel_turn(self, request: Any) -> Any:
        """Cancel an in-progress agent turn."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.CancelTurn, request)

    def list_turn_events(self, request: Any) -> Any:
        """List events emitted for an agent turn."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ListTurnEvents, request)

    def list_interactions(self, request: Any) -> Any:
        """List pending or completed agent interactions."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ListInteractions, request)

    def resolve_interaction(self, request: Any) -> Any:
        """Resolve an agent interaction with a host response."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ResolveInteraction, request)

    def __enter__(self) -> AgentManager:
        """Return the client for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the client at the end of a context manager block."""

        self.close()


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError:
        raise
