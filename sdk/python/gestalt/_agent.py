from __future__ import annotations

import os
from typing import Any

import grpc

from ._gen.v1 import agent_pb2 as _pb
from ._gen.v1 import agent_pb2_grpc as _pb_grpc
from ._grpc_transport import host_service_channel

pb: Any = _pb
pb_grpc: Any = _pb_grpc

ENV_AGENT_HOST_SOCKET = "GESTALT_AGENT_HOST_SOCKET"
ENV_AGENT_HOST_SOCKET_TOKEN = f"{ENV_AGENT_HOST_SOCKET}_TOKEN"
ENV_AGENT_MANAGER_SOCKET = "GESTALT_AGENT_MANAGER_SOCKET"
ENV_AGENT_MANAGER_SOCKET_TOKEN = f"{ENV_AGENT_MANAGER_SOCKET}_TOKEN"

AGENT_EXECUTION_STATUS_UNSPECIFIED = pb.AGENT_EXECUTION_STATUS_UNSPECIFIED
AGENT_EXECUTION_STATUS_PENDING = pb.AGENT_EXECUTION_STATUS_PENDING
AGENT_EXECUTION_STATUS_RUNNING = pb.AGENT_EXECUTION_STATUS_RUNNING
AGENT_EXECUTION_STATUS_SUCCEEDED = pb.AGENT_EXECUTION_STATUS_SUCCEEDED
AGENT_EXECUTION_STATUS_FAILED = pb.AGENT_EXECUTION_STATUS_FAILED
AGENT_EXECUTION_STATUS_CANCELED = pb.AGENT_EXECUTION_STATUS_CANCELED
AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT = pb.AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT

AGENT_INTERACTION_STATE_UNSPECIFIED = pb.AGENT_INTERACTION_STATE_UNSPECIFIED
AGENT_INTERACTION_STATE_PENDING = pb.AGENT_INTERACTION_STATE_PENDING
AGENT_INTERACTION_STATE_RESOLVED = pb.AGENT_INTERACTION_STATE_RESOLVED
AGENT_INTERACTION_STATE_CANCELED = pb.AGENT_INTERACTION_STATE_CANCELED

AGENT_INTERACTION_TYPE_UNSPECIFIED = pb.AGENT_INTERACTION_TYPE_UNSPECIFIED
AGENT_INTERACTION_TYPE_INPUT = pb.AGENT_INTERACTION_TYPE_INPUT
AGENT_INTERACTION_TYPE_APPROVAL = pb.AGENT_INTERACTION_TYPE_APPROVAL
AGENT_INTERACTION_TYPE_CLARIFICATION = pb.AGENT_INTERACTION_TYPE_CLARIFICATION

AGENT_MESSAGE_PART_TYPE_UNSPECIFIED = pb.AGENT_MESSAGE_PART_TYPE_UNSPECIFIED
AGENT_MESSAGE_PART_TYPE_TEXT = pb.AGENT_MESSAGE_PART_TYPE_TEXT
AGENT_MESSAGE_PART_TYPE_JSON = pb.AGENT_MESSAGE_PART_TYPE_JSON
AGENT_MESSAGE_PART_TYPE_TOOL_CALL = pb.AGENT_MESSAGE_PART_TYPE_TOOL_CALL
AGENT_MESSAGE_PART_TYPE_TOOL_RESULT = pb.AGENT_MESSAGE_PART_TYPE_TOOL_RESULT
AGENT_MESSAGE_PART_TYPE_IMAGE_REF = pb.AGENT_MESSAGE_PART_TYPE_IMAGE_REF

AGENT_SESSION_STATE_UNSPECIFIED = pb.AGENT_SESSION_STATE_UNSPECIFIED
AGENT_SESSION_STATE_ACTIVE = pb.AGENT_SESSION_STATE_ACTIVE
AGENT_SESSION_STATE_ARCHIVED = pb.AGENT_SESSION_STATE_ARCHIVED

AGENT_TOOL_SOURCE_MODE_UNSPECIFIED = pb.AGENT_TOOL_SOURCE_MODE_UNSPECIFIED
AGENT_TOOL_SOURCE_MODE_MCP_CATALOG = pb.AGENT_TOOL_SOURCE_MODE_MCP_CATALOG


def AgentMessage(*args: Any, **kwargs: Any) -> Any:
    """Create an agent message protocol value."""

    return pb.AgentMessage(*args, **kwargs)


def AgentMessagePart(*args: Any, **kwargs: Any) -> Any:
    """Create an agent message part protocol value."""

    return pb.AgentMessagePart(*args, **kwargs)


def AgentActor(*args: Any, **kwargs: Any) -> Any:
    """Create an agent actor protocol value."""

    return pb.AgentActor(*args, **kwargs)


def AgentSubjectContext(*args: Any, **kwargs: Any) -> Any:
    """Create an agent subject context protocol value."""

    return pb.AgentSubjectContext(*args, **kwargs)


def AgentToolRef(*args: Any, **kwargs: Any) -> Any:
    """Create an agent tool reference protocol value."""

    return pb.AgentToolRef(*args, **kwargs)


def AgentProviderCapabilities(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider capabilities protocol value."""

    return pb.AgentProviderCapabilities(*args, **kwargs)


def AgentSession(*args: Any, **kwargs: Any) -> Any:
    """Create an agent session protocol value."""

    return pb.AgentSession(*args, **kwargs)


def AgentTurn(*args: Any, **kwargs: Any) -> Any:
    """Create an agent turn protocol value."""

    return pb.AgentTurn(*args, **kwargs)


def AgentTurnEvent(*args: Any, **kwargs: Any) -> Any:
    """Create an agent turn event protocol value."""

    return pb.AgentTurnEvent(*args, **kwargs)


def AgentInteraction(*args: Any, **kwargs: Any) -> Any:
    """Create an agent interaction protocol value."""

    return pb.AgentInteraction(*args, **kwargs)


def CreateAgentProviderSessionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider session request."""

    return pb.CreateAgentProviderSessionRequest(*args, **kwargs)


def GetAgentProviderSessionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider get-session request."""

    return pb.GetAgentProviderSessionRequest(*args, **kwargs)


def ListAgentProviderSessionsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-sessions request."""

    return pb.ListAgentProviderSessionsRequest(*args, **kwargs)


def ListAgentProviderSessionsResponse(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-sessions response."""

    return pb.ListAgentProviderSessionsResponse(*args, **kwargs)


def UpdateAgentProviderSessionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider update-session request."""

    return pb.UpdateAgentProviderSessionRequest(*args, **kwargs)


def CreateAgentProviderTurnRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider turn request."""

    return pb.CreateAgentProviderTurnRequest(*args, **kwargs)


def GetAgentProviderTurnRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider get-turn request."""

    return pb.GetAgentProviderTurnRequest(*args, **kwargs)


def ListAgentProviderTurnsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-turns request."""

    return pb.ListAgentProviderTurnsRequest(*args, **kwargs)


def ListAgentProviderTurnsResponse(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-turns response."""

    return pb.ListAgentProviderTurnsResponse(*args, **kwargs)


def CancelAgentProviderTurnRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider cancel-turn request."""

    return pb.CancelAgentProviderTurnRequest(*args, **kwargs)


def ListAgentProviderTurnEventsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-turn-events request."""

    return pb.ListAgentProviderTurnEventsRequest(*args, **kwargs)


def ListAgentProviderTurnEventsResponse(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-turn-events response."""

    return pb.ListAgentProviderTurnEventsResponse(*args, **kwargs)


def ListAgentProviderInteractionsResponse(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-interactions response."""

    return pb.ListAgentProviderInteractionsResponse(*args, **kwargs)


def ResolveAgentProviderInteractionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider resolve-interaction request."""

    return pb.ResolveAgentProviderInteractionRequest(*args, **kwargs)


def GetAgentProviderCapabilitiesRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider capabilities request."""

    return pb.GetAgentProviderCapabilitiesRequest(*args, **kwargs)


def ExecuteAgentToolRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent host ExecuteTool request."""

    return pb.ExecuteAgentToolRequest(*args, **kwargs)


def ExecuteAgentToolResponse(*args: Any, **kwargs: Any) -> Any:
    """Create an agent host ExecuteTool response."""

    return pb.ExecuteAgentToolResponse(*args, **kwargs)


def ListAgentToolsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent host ListTools request."""

    return pb.ListAgentToolsRequest(*args, **kwargs)


def ListAgentToolsResponse(*args: Any, **kwargs: Any) -> Any:
    """Create an agent host ListTools response."""

    return pb.ListAgentToolsResponse(*args, **kwargs)


def AgentManagerCreateSessionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager create-session request."""

    return pb.AgentManagerCreateSessionRequest(*args, **kwargs)


def AgentManagerGetSessionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager get-session request."""

    return pb.AgentManagerGetSessionRequest(*args, **kwargs)


def AgentManagerListSessionsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager list-sessions request."""

    return pb.AgentManagerListSessionsRequest(*args, **kwargs)


def AgentManagerUpdateSessionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager update-session request."""

    return pb.AgentManagerUpdateSessionRequest(*args, **kwargs)


def AgentManagerCreateTurnRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager create-turn request."""

    return pb.AgentManagerCreateTurnRequest(*args, **kwargs)


def AgentManagerGetTurnRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager get-turn request."""

    return pb.AgentManagerGetTurnRequest(*args, **kwargs)


def AgentManagerListTurnsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager list-turns request."""

    return pb.AgentManagerListTurnsRequest(*args, **kwargs)


def AgentManagerCancelTurnRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager cancel-turn request."""

    return pb.AgentManagerCancelTurnRequest(*args, **kwargs)


def AgentManagerListTurnEventsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager list-turn-events request."""

    return pb.AgentManagerListTurnEventsRequest(*args, **kwargs)


def AgentManagerListInteractionsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager list-interactions request."""

    return pb.AgentManagerListInteractionsRequest(*args, **kwargs)


def AgentManagerResolveInteractionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager resolve-interaction request."""

    return pb.AgentManagerResolveInteractionRequest(*args, **kwargs)


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
