"""Transport-backed Agent SDK tests over real sockets."""
from __future__ import annotations

import os
import tempfile
import unittest
from concurrent import futures
from typing import Any

import grpc
from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import json_format
from google.protobuf import struct_pb2 as _struct_pb2

from gestalt import (
    ENV_AGENT_HOST_SOCKET,
    ENV_AGENT_HOST_SOCKET_TOKEN,
    ENV_AGENT_MANAGER_SOCKET,
    ENV_AGENT_MANAGER_SOCKET_TOKEN,
    AgentHost,
    AgentManager,
    AgentProvider,
    MetadataProvider,
    ProviderKind,
    ProviderMetadata,
    Request,
    WarningsProvider,
    _runtime,
)
from gestalt.gen.v1 import agent_pb2 as _agent_pb2
from gestalt.gen.v1 import agent_pb2_grpc as _agent_pb2_grpc
from gestalt.gen.v1 import runtime_pb2 as _runtime_pb2
from gestalt.gen.v1 import runtime_pb2_grpc as _runtime_pb2_grpc

agent_pb2: Any = _agent_pb2
agent_pb2_grpc: Any = _agent_pb2_grpc
empty_pb2: Any = _empty_pb2
runtime_pb2: Any = _runtime_pb2
runtime_pb2_grpc: Any = _runtime_pb2_grpc
struct_pb2: Any = _struct_pb2

_runtime_server: grpc.Server | None = None
_host_server: grpc.Server | None = None
_manager_server: grpc.Server | None = None
_runtime_socket = ""
_host_socket = ""
_manager_socket = ""
_previous_envs: dict[str, str | None] = {}
_provider: "_AgentRuntimeProvider"
_host_relay_tokens: list[str] = []
_host_search_requests: list[dict[str, Any]] = []
_manager_requests: list[dict[str, str]] = []
_manager_relay_tokens: list[str] = []


class _AgentRuntimeProvider(AgentProvider, MetadataProvider, WarningsProvider):
    def __init__(self) -> None:
        self.configured: list[tuple[str, dict[str, object]]] = []

    def configure(self, name: str, config: dict[str, Any]) -> None:
        self.configured.append((name, dict(config)))

    def metadata(self) -> ProviderMetadata:
        return ProviderMetadata(
            kind=ProviderKind.AGENT,
            name="py-agent",
            display_name="Py Agent",
            description="test agent provider",
            version="0.1.0",
        )

    def warnings(self) -> list[str]:
        return ["set OPENAI_API_KEY"]

    def CreateSession(self, request: Any, context: grpc.ServicerContext) -> Any:
        return agent_pb2.AgentSession(
            id=request.session_id,
            provider_name="py-agent",
            model=request.model,
            client_ref=request.client_ref,
            state=agent_pb2.AGENT_SESSION_STATE_ACTIVE,
            metadata=request.metadata,
            created_by=request.created_by,
        )

    def GetSession(self, request: Any, context: grpc.ServicerContext) -> Any:
        metadata = struct_pb2.Struct()
        metadata.update({"source": "py-test"})
        return agent_pb2.AgentSession(
            id=request.session_id,
            provider_name="py-agent",
            model="gpt-5.1",
            client_ref="cli-session-1",
            state=agent_pb2.AGENT_SESSION_STATE_ARCHIVED,
            metadata=metadata,
        )

    def ListSessions(self, request: Any, context: grpc.ServicerContext) -> Any:
        return agent_pb2.ListAgentProviderSessionsResponse(
            sessions=[
                agent_pb2.AgentSession(
                    id="session-1",
                    provider_name="py-agent",
                    model="gpt-5.1",
                    client_ref="cli-session-1",
                    state=agent_pb2.AGENT_SESSION_STATE_ARCHIVED,
                )
            ]
        )

    def UpdateSession(self, request: Any, context: grpc.ServicerContext) -> Any:
        return agent_pb2.AgentSession(
            id=request.session_id,
            provider_name="py-agent",
            model="gpt-5.1",
            client_ref=request.client_ref,
            state=request.state,
            metadata=request.metadata,
        )

    def CreateTurn(self, request: Any, context: grpc.ServicerContext) -> Any:
        return agent_pb2.AgentTurn(
            id=request.turn_id,
            session_id=request.session_id,
            provider_name="py-agent",
            model=request.model,
            status=agent_pb2.AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT,
            messages=request.messages,
            output_text="echo:Plan it",
            status_message="waiting for input",
            created_by=request.created_by,
            execution_ref=request.execution_ref,
        )

    def GetTurn(self, request: Any, context: grpc.ServicerContext) -> Any:
        return agent_pb2.AgentTurn(
            id=request.turn_id,
            session_id="session-1",
            provider_name="py-agent",
            model="gpt-5.1",
            status=agent_pb2.AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT,
            output_text="echo:Plan it",
            status_message="waiting for input",
        )

    def ListTurns(self, request: Any, context: grpc.ServicerContext) -> Any:
        return agent_pb2.ListAgentProviderTurnsResponse(
            turns=[
                agent_pb2.AgentTurn(
                    id="turn-1",
                    session_id=request.session_id,
                    provider_name="py-agent",
                    model="gpt-5.1",
                    status=agent_pb2.AGENT_EXECUTION_STATUS_SUCCEEDED,
                    status_message="done",
                )
            ]
        )

    def CancelTurn(self, request: Any, context: grpc.ServicerContext) -> Any:
        return agent_pb2.AgentTurn(
            id=request.turn_id,
            session_id="session-1",
            provider_name="py-agent",
            model="gpt-5.1",
            status=agent_pb2.AGENT_EXECUTION_STATUS_CANCELED,
            status_message=request.reason,
        )

    def ListTurnEvents(self, request: Any, context: grpc.ServicerContext) -> Any:
        return agent_pb2.ListAgentProviderTurnEventsResponse(
            events=[
                agent_pb2.AgentTurnEvent(
                    id=f"{request.turn_id}-event-1",
                    turn_id=request.turn_id,
                    seq=1,
                    type="turn.started",
                    source="py-agent",
                    visibility="private",
                ),
                agent_pb2.AgentTurnEvent(
                    id=f"{request.turn_id}-event-2",
                    turn_id=request.turn_id,
                    seq=2,
                    type="interaction.requested",
                    source="py-agent",
                    visibility="private",
                ),
            ]
        )

    def GetInteraction(self, request: Any, context: grpc.ServicerContext) -> Any:
        return agent_pb2.AgentInteraction(
            id=request.interaction_id,
            turn_id="turn-1",
            session_id="session-1",
            type=agent_pb2.AGENT_INTERACTION_TYPE_APPROVAL,
            state=agent_pb2.AGENT_INTERACTION_STATE_PENDING,
            title="Approve command",
            prompt="Run git status?",
        )

    def ListInteractions(self, request: Any, context: grpc.ServicerContext) -> Any:
        return agent_pb2.ListAgentProviderInteractionsResponse(
            interactions=[
                agent_pb2.AgentInteraction(
                    id="interaction-1",
                    turn_id=request.turn_id,
                    session_id="session-1",
                    type=agent_pb2.AGENT_INTERACTION_TYPE_APPROVAL,
                    state=agent_pb2.AGENT_INTERACTION_STATE_PENDING,
                    title="Approve command",
                    prompt="Run git status?",
                )
            ]
        )

    def ResolveInteraction(self, request: Any, context: grpc.ServicerContext) -> Any:
        return agent_pb2.AgentInteraction(
            id=request.interaction_id,
            turn_id="turn-1",
            session_id="session-1",
            type=agent_pb2.AGENT_INTERACTION_TYPE_APPROVAL,
            state=agent_pb2.AGENT_INTERACTION_STATE_RESOLVED,
            title="Approve command",
            prompt="Run git status?",
            resolution=request.resolution,
        )

    def GetCapabilities(self, request: Any, context: grpc.ServicerContext) -> Any:
        return agent_pb2.AgentProviderCapabilities(
            streaming_text=True,
            tool_calls=True,
            native_tool_search=True,
            parallel_tool_calls=False,
            structured_output=True,
            interactions=True,
            resumable_turns=True,
            reasoning_summaries=False,
        )


class _AgentHostServicer(agent_pb2_grpc.AgentHostServicer):
    def SearchTools(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_host_relay_tokens(context)
        _host_search_requests.append(
            {
                "session_id": request.session_id,
                "turn_id": request.turn_id,
                "query": request.query,
                "max_results": request.max_results,
                "candidate_limit": request.candidate_limit,
                "load_refs": [
                    {
                        "plugin": ref.plugin,
                        "operation": ref.operation,
                        "connection": ref.connection,
                        "instance": ref.instance,
                        "credential_mode": ref.credential_mode,
                    }
                    for ref in request.load_refs
                ],
            }
        )
        return agent_pb2.SearchAgentToolsResponse(
            tools=[
                agent_pb2.ResolvedAgentTool(
                    id="slack.send_message",
                    name="Send Slack message",
                    description="Send a direct message",
                    target=agent_pb2.BoundAgentToolTarget(
                        plugin="slack",
                        operation="send_message",
                        connection="workspace",
                        instance="primary",
                        credential_mode="user",
                    ),
                ),
                agent_pb2.ResolvedAgentTool(
                    id="system.workflow.schedules.list",
                    name="List workflow schedules",
                    description="List schedules owned by the caller",
                    target=agent_pb2.BoundAgentToolTarget(
                        system="workflow",
                        operation="schedules.list",
                    ),
                )
            ],
            candidates=[
                agent_pb2.AgentToolCandidate(
                    ref=agent_pb2.AgentToolRef(
                        plugin="slack",
                        operation="search_messages",
                        connection="workspace",
                        instance="primary",
                        credential_mode="user",
                    ),
                    id="slack/search_messages/workspace/primary/user",
                    name="Search Slack messages",
                    description="Search messages",
                    parameters=["query", "channel"],
                    score=12.5,
                )
            ],
            has_more=True,
        )

    def ExecuteTool(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_host_relay_tokens(context)
        return agent_pb2.ExecuteAgentToolResponse(
            status=207,
            body=f"{request.session_id}:{request.turn_id}:{request.tool_call_id}:{request.tool_id}",
        )


class _AgentManagerServicer(agent_pb2_grpc.AgentManagerHostServicer):
    def CreateSession(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "create_session",
                "invocation_token": request.invocation_token,
                "provider_name": request.provider_name,
                "session_id": "",
                "turn_id": "",
                "interaction_id": "",
                "reason": "",
            }
        )
        return agent_pb2.AgentSession(
            id="session-managed-1",
            provider_name=request.provider_name,
            model=request.model,
            client_ref=request.client_ref,
            state=agent_pb2.AGENT_SESSION_STATE_ACTIVE,
        )

    def GetSession(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "get_session",
                "invocation_token": request.invocation_token,
                "provider_name": "",
                "session_id": request.session_id,
                "turn_id": "",
                "interaction_id": "",
                "reason": "",
            }
        )
        return agent_pb2.AgentSession(
            id=request.session_id,
            provider_name="openai",
            model="gpt-5.1",
            client_ref="cli-session-1",
            state=agent_pb2.AGENT_SESSION_STATE_ARCHIVED,
        )

    def ListSessions(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "list_sessions",
                "invocation_token": request.invocation_token,
                "provider_name": request.provider_name,
                "session_id": "",
                "turn_id": "",
                "interaction_id": "",
                "reason": "",
            }
        )
        return agent_pb2.AgentManagerListSessionsResponse(
            sessions=[
                agent_pb2.AgentSession(
                    id="session-managed-1",
                    provider_name="openai",
                    model="gpt-5.1",
                    client_ref="cli-session-1",
                    state=agent_pb2.AGENT_SESSION_STATE_ACTIVE,
                )
            ]
        )

    def UpdateSession(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "update_session",
                "invocation_token": request.invocation_token,
                "provider_name": "",
                "session_id": request.session_id,
                "turn_id": "",
                "interaction_id": "",
                "reason": "",
            }
        )
        return agent_pb2.AgentSession(
            id=request.session_id,
            provider_name="openai",
            model="gpt-5.1",
            client_ref=request.client_ref,
            state=request.state,
            metadata=request.metadata,
        )

    def CreateTurn(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "create_turn",
                "invocation_token": request.invocation_token,
                "provider_name": "",
                "session_id": request.session_id,
                "turn_id": "",
                "interaction_id": "",
                "reason": "",
            }
        )
        return agent_pb2.AgentTurn(
            id="turn-managed-1",
            session_id=request.session_id,
            provider_name="openai",
            model=request.model,
            status=agent_pb2.AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT,
            messages=request.messages,
            output_text="echo:Summarize this",
            status_message="waiting for input",
        )

    def GetTurn(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "get_turn",
                "invocation_token": request.invocation_token,
                "provider_name": "",
                "session_id": "",
                "turn_id": request.turn_id,
                "interaction_id": "",
                "reason": "",
            }
        )
        return agent_pb2.AgentTurn(
            id=request.turn_id,
            session_id="session-managed-1",
            provider_name="openai",
            model="gpt-5.1",
            status=agent_pb2.AGENT_EXECUTION_STATUS_SUCCEEDED,
            output_text="done",
            status_message="completed",
        )

    def ListTurns(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "list_turns",
                "invocation_token": request.invocation_token,
                "provider_name": "",
                "session_id": request.session_id,
                "turn_id": "",
                "interaction_id": "",
                "reason": "",
            }
        )
        return agent_pb2.AgentManagerListTurnsResponse(
            turns=[
                agent_pb2.AgentTurn(
                    id="turn-managed-1",
                    session_id=request.session_id,
                    provider_name="openai",
                    model="gpt-5.1",
                    status=agent_pb2.AGENT_EXECUTION_STATUS_RUNNING,
                    status_message="running",
                )
            ]
        )

    def CancelTurn(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "cancel_turn",
                "invocation_token": request.invocation_token,
                "provider_name": "",
                "session_id": "",
                "turn_id": request.turn_id,
                "interaction_id": "",
                "reason": request.reason,
            }
        )
        return agent_pb2.AgentTurn(
            id=request.turn_id,
            session_id="session-managed-1",
            provider_name="openai",
            model="gpt-5.1",
            status=agent_pb2.AGENT_EXECUTION_STATUS_CANCELED,
            status_message=request.reason,
        )

    def ListTurnEvents(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "list_turn_events",
                "invocation_token": request.invocation_token,
                "provider_name": "",
                "session_id": "",
                "turn_id": request.turn_id,
                "interaction_id": "",
                "reason": "",
            }
        )
        return agent_pb2.AgentManagerListTurnEventsResponse(
            events=[
                agent_pb2.AgentTurnEvent(
                    id=f"{request.turn_id}-event-1",
                    turn_id=request.turn_id,
                    seq=1,
                    type="turn.started",
                    source="openai",
                    visibility="private",
                )
            ]
        )

    def ListInteractions(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "list_interactions",
                "invocation_token": request.invocation_token,
                "provider_name": "",
                "session_id": "",
                "turn_id": request.turn_id,
                "interaction_id": "",
                "reason": "",
            }
        )
        return agent_pb2.AgentManagerListInteractionsResponse(
            interactions=[
                agent_pb2.AgentInteraction(
                    id="interaction-1",
                    turn_id=request.turn_id,
                    session_id="session-managed-1",
                    type=agent_pb2.AGENT_INTERACTION_TYPE_APPROVAL,
                    state=agent_pb2.AGENT_INTERACTION_STATE_PENDING,
                    title="Approve command",
                    prompt="Run git status?",
                )
            ]
        )

    def ResolveInteraction(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "resolve_interaction",
                "invocation_token": request.invocation_token,
                "provider_name": "",
                "session_id": "",
                "turn_id": request.turn_id,
                "interaction_id": request.interaction_id,
                "reason": "",
            }
        )
        return agent_pb2.AgentInteraction(
            id=request.interaction_id,
            turn_id=request.turn_id,
            session_id="session-managed-1",
            type=agent_pb2.AGENT_INTERACTION_TYPE_APPROVAL,
            state=agent_pb2.AGENT_INTERACTION_STATE_RESOLVED,
            title="Approve command",
            prompt="Run git status?",
            resolution=request.resolution,
        )


def _record_relay_tokens(context: grpc.ServicerContext) -> None:
    _manager_relay_tokens.extend(
        value
        for key, value in context.invocation_metadata()
        if key == "x-gestalt-host-service-relay-token"
    )


def _record_host_relay_tokens(context: grpc.ServicerContext) -> None:
    _host_relay_tokens.extend(
        value
        for key, value in context.invocation_metadata()
        if key == "x-gestalt-host-service-relay-token"
    )


def _fresh_socket(name: str) -> str:
    path = os.path.join(tempfile.gettempdir(), f"{name}-{os.getpid()}.sock")
    if os.path.exists(path):
        os.remove(path)
    return path


def setUpModule() -> None:
    global _runtime_server, _host_server, _manager_server
    global _runtime_socket, _host_socket, _manager_socket, _provider

    _provider = _AgentRuntimeProvider()
    _runtime_socket = _fresh_socket("py-agent-runtime")
    _host_socket = _fresh_socket("py-agent-host")
    _manager_socket = _fresh_socket("py-agent-manager")

    _runtime_server = grpc.server(futures.ThreadPoolExecutor(max_workers=2))
    adapter = _runtime._servable_target(_provider, runtime_kind=ProviderKind.AGENT)
    _runtime._register_services(server=_runtime_server, servable=adapter)
    _runtime_server.add_insecure_port(f"unix:{_runtime_socket}")
    _runtime_server.start()

    _host_server = grpc.server(futures.ThreadPoolExecutor(max_workers=2))
    agent_pb2_grpc.add_AgentHostServicer_to_server(_AgentHostServicer(), _host_server)
    _host_server.add_insecure_port(f"unix:{_host_socket}")
    _host_server.start()

    _manager_server = grpc.server(futures.ThreadPoolExecutor(max_workers=2))
    agent_pb2_grpc.add_AgentManagerHostServicer_to_server(
        _AgentManagerServicer(),
        _manager_server,
    )
    _manager_server.add_insecure_port(f"unix:{_manager_socket}")
    _manager_server.start()

    for env_name, value in (
        (ENV_AGENT_HOST_SOCKET, _host_socket),
        (ENV_AGENT_HOST_SOCKET_TOKEN, "relay-token-py"),
        (ENV_AGENT_MANAGER_SOCKET, _manager_socket),
        (ENV_AGENT_MANAGER_SOCKET_TOKEN, "relay-token-py"),
    ):
        _previous_envs[env_name] = os.environ.get(env_name)
        os.environ[env_name] = value


def tearDownModule() -> None:
    for env_name, previous in _previous_envs.items():
        if previous is None:
            os.environ.pop(env_name, None)
        else:
            os.environ[env_name] = previous
    for server in (_runtime_server, _host_server, _manager_server):
        if server is not None:
            server.stop(grace=0).wait()
    for path in (_runtime_socket, _host_socket, _manager_socket):
        if path and os.path.exists(path):
            os.remove(path)


class AgentTransportTests(unittest.TestCase):
    def setUp(self) -> None:
        _provider.configured.clear()
        _host_relay_tokens.clear()
        _host_search_requests.clear()
        _manager_requests.clear()
        _manager_relay_tokens.clear()

    def test_agent_runtime_and_server_roundtrip(self) -> None:
        channel = grpc.insecure_channel(f"unix:{_runtime_socket}")
        runtime_client = runtime_pb2_grpc.ProviderLifecycleStub(channel)
        provider_client = agent_pb2_grpc.AgentProviderStub(channel)

        identity = runtime_client.GetProviderIdentity(empty_pb2.Empty())
        configure_request = runtime_pb2.ConfigureProviderRequest(
            name="agent-runtime",
            protocol_version=_runtime.CURRENT_PROTOCOL_VERSION,
        )
        json_format.ParseDict({"tenant": "acme"}, configure_request.config)
        configured = runtime_client.ConfigureProvider(configure_request)

        create_session_request = agent_pb2.CreateAgentProviderSessionRequest(
            session_id="session-1",
            idempotency_key="session-req-1",
            model="gpt-5.1",
            client_ref="cli-session-1",
        )
        create_session_metadata = struct_pb2.Struct()
        create_session_metadata.update({"source": "py-test"})
        create_session_request.metadata.CopyFrom(create_session_metadata)
        created_session = provider_client.CreateSession(create_session_request)
        listed_sessions = provider_client.ListSessions(
            agent_pb2.ListAgentProviderSessionsRequest()
        )
        fetched_session = provider_client.GetSession(
            agent_pb2.GetAgentProviderSessionRequest(session_id="session-1")
        )

        update_session_request = agent_pb2.UpdateAgentProviderSessionRequest(
            session_id="session-1",
            client_ref="cli-session-2",
            state=agent_pb2.AGENT_SESSION_STATE_ARCHIVED,
        )
        updated_session_metadata = struct_pb2.Struct()
        updated_session_metadata.update({"source": "py-test-updated"})
        update_session_request.metadata.CopyFrom(updated_session_metadata)
        updated_session = provider_client.UpdateSession(update_session_request)

        created_turn = provider_client.CreateTurn(
            agent_pb2.CreateAgentProviderTurnRequest(
                turn_id="turn-1",
                session_id="session-1",
                model="gpt-5.1",
                messages=[
                    agent_pb2.AgentMessage(
                        role="user",
                        text="Plan it",
                        parts=[
                            agent_pb2.AgentMessagePart(
                                type=agent_pb2.AGENT_MESSAGE_PART_TYPE_TEXT,
                                text="Plan it",
                            )
                        ],
                    )
                ],
                execution_ref="exec-turn-1",
            )
        )
        listed_turns = provider_client.ListTurns(
            agent_pb2.ListAgentProviderTurnsRequest(session_id="session-1")
        )
        fetched_turn = provider_client.GetTurn(
            agent_pb2.GetAgentProviderTurnRequest(turn_id="turn-1")
        )
        turn_events = provider_client.ListTurnEvents(
            agent_pb2.ListAgentProviderTurnEventsRequest(
                turn_id="turn-1",
                after_seq=0,
                limit=10,
            )
        )
        listed_interactions = provider_client.ListInteractions(
            agent_pb2.ListAgentProviderInteractionsRequest(turn_id="turn-1")
        )
        fetched_interaction = provider_client.GetInteraction(
            agent_pb2.GetAgentProviderInteractionRequest(
                interaction_id="interaction-1"
            )
        )
        resolve_interaction_request = agent_pb2.ResolveAgentProviderInteractionRequest(
            interaction_id="interaction-1"
        )
        resolved_interaction_payload = struct_pb2.Struct()
        resolved_interaction_payload.update({"approved": True})
        resolve_interaction_request.resolution.CopyFrom(resolved_interaction_payload)
        resolved_interaction = provider_client.ResolveInteraction(
            resolve_interaction_request
        )
        capabilities = provider_client.GetCapabilities(
            agent_pb2.GetAgentProviderCapabilitiesRequest()
        )

        self.assertEqual(identity.kind, runtime_pb2.ProviderKind.PROVIDER_KIND_AGENT)
        self.assertEqual(identity.name, "py-agent")
        self.assertEqual(list(identity.warnings), ["set OPENAI_API_KEY"])
        self.assertEqual(configured.protocol_version, _runtime.CURRENT_PROTOCOL_VERSION)
        self.assertEqual(_provider.configured, [("agent-runtime", {"tenant": "acme"})])
        self.assertEqual(created_session.id, "session-1")
        self.assertEqual(created_session.state, agent_pb2.AGENT_SESSION_STATE_ACTIVE)
        self.assertEqual([session.id for session in listed_sessions.sessions], ["session-1"])
        self.assertEqual(fetched_session.state, agent_pb2.AGENT_SESSION_STATE_ARCHIVED)
        self.assertEqual(updated_session.client_ref, "cli-session-2")
        self.assertEqual(created_turn.id, "turn-1")
        self.assertEqual(
            created_turn.status,
            agent_pb2.AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT,
        )
        self.assertEqual(len(created_turn.messages[0].parts), 1)
        self.assertEqual([turn.id for turn in listed_turns.turns], ["turn-1"])
        self.assertEqual(fetched_turn.status_message, "waiting for input")
        self.assertEqual(
            [event.type for event in turn_events.events],
            ["turn.started", "interaction.requested"],
        )
        self.assertEqual(
            [interaction.id for interaction in listed_interactions.interactions],
            ["interaction-1"],
        )
        self.assertEqual(
            fetched_interaction.state,
            agent_pb2.AGENT_INTERACTION_STATE_PENDING,
        )
        self.assertEqual(
            resolved_interaction.state,
            agent_pb2.AGENT_INTERACTION_STATE_RESOLVED,
        )
        self.assertTrue(capabilities.streaming_text)
        self.assertTrue(capabilities.tool_calls)
        self.assertTrue(capabilities.interactions)
        self.assertTrue(capabilities.resumable_turns)

    def test_agent_host_roundtrip(self) -> None:
        arguments = struct_pb2.Struct()
        arguments.update({"query": "Ada Lovelace"})

        with AgentHost() as host:
            search_response = host.search_tools(
                agent_pb2.SearchAgentToolsRequest(
                    session_id="session-1",
                    turn_id="turn-1",
                    query="send slack dm",
                    max_results=3,
                    candidate_limit=12,
                    load_refs=[
                        agent_pb2.AgentToolRef(
                            plugin="slack",
                            operation="search_messages",
                            connection="workspace",
                            instance="primary",
                            credential_mode="user",
                        )
                    ],
                )
            )
            response = host.execute_tool(
                agent_pb2.ExecuteAgentToolRequest(
                    session_id="session-1",
                    turn_id="turn-1",
                    tool_call_id="call-7",
                    tool_id="lookup",
                    arguments=arguments,
                )
            )

        self.assertEqual(len(search_response.tools), 2)
        self.assertEqual(search_response.tools[0].target.plugin, "slack")
        self.assertEqual(search_response.tools[0].target.operation, "send_message")
        self.assertEqual(search_response.tools[0].target.credential_mode, "user")
        self.assertEqual(len(search_response.candidates), 1)
        self.assertEqual(search_response.candidates[0].ref.operation, "search_messages")
        self.assertEqual(search_response.candidates[0].ref.credential_mode, "user")
        self.assertTrue(search_response.has_more)
        self.assertEqual(search_response.tools[1].target.system, "workflow")
        self.assertEqual(search_response.tools[1].target.operation, "schedules.list")
        self.assertEqual(response.status, 207)
        self.assertEqual(response.body, "session-1:turn-1:call-7:lookup")
        self.assertEqual(_host_relay_tokens, ["relay-token-py", "relay-token-py"])
        self.assertEqual(
            _host_search_requests,
            [
                {
                    "session_id": "session-1",
                    "turn_id": "turn-1",
                    "query": "send slack dm",
                    "max_results": 3,
                    "candidate_limit": 12,
                    "load_refs": [
                        {
                            "plugin": "slack",
                            "operation": "search_messages",
                            "connection": "workspace",
                            "instance": "primary",
                            "credential_mode": "user",
                        }
                    ],
                }
            ],
        )

    def test_agent_manager_roundtrip(self) -> None:
        with AgentManager("token-123") as manager:
            created_session = manager.create_session(
                agent_pb2.AgentManagerCreateSessionRequest(
                    provider_name="openai",
                    model="gpt-5.1",
                    client_ref="cli-session-1",
                )
            )
            fetched_session = manager.get_session(
                agent_pb2.AgentManagerGetSessionRequest(session_id="session-managed-1")
            )
            listed_sessions = manager.list_sessions(
                agent_pb2.AgentManagerListSessionsRequest(provider_name="openai")
            )
            updated_session = manager.update_session(
                agent_pb2.AgentManagerUpdateSessionRequest(
                    session_id="session-managed-1",
                    client_ref="cli-session-2",
                    state=agent_pb2.AGENT_SESSION_STATE_ARCHIVED,
                )
            )
            created_turn = manager.create_turn(
                agent_pb2.AgentManagerCreateTurnRequest(
                    session_id="session-managed-1",
                    model="gpt-5.1",
                    messages=[
                        agent_pb2.AgentMessage(
                            role="user",
                            text="Summarize this",
                            parts=[
                                agent_pb2.AgentMessagePart(
                                    type=agent_pb2.AGENT_MESSAGE_PART_TYPE_TEXT,
                                    text="Summarize this",
                                )
                            ],
                        )
                    ],
                    tool_source=agent_pb2.AGENT_TOOL_SOURCE_MODE_NATIVE_SEARCH,
                )
            )
            fetched_turn = manager.get_turn(
                agent_pb2.AgentManagerGetTurnRequest(turn_id="turn-managed-1")
            )
            listed_turns = manager.list_turns(
                agent_pb2.AgentManagerListTurnsRequest(session_id="session-managed-1")
            )
            canceled_turn = manager.cancel_turn(
                agent_pb2.AgentManagerCancelTurnRequest(
                    turn_id="turn-managed-1",
                    reason="user canceled",
                )
            )
            turn_events = manager.list_turn_events(
                agent_pb2.AgentManagerListTurnEventsRequest(
                    turn_id="turn-managed-1",
                    after_seq=0,
                    limit=10,
                )
            )
            interactions = manager.list_interactions(
                agent_pb2.AgentManagerListInteractionsRequest(turn_id="turn-managed-1")
            )
            resolve_request = agent_pb2.AgentManagerResolveInteractionRequest(
                turn_id="turn-managed-1",
                interaction_id="interaction-1",
            )
            resolution = struct_pb2.Struct()
            resolution.update({"approved": True})
            resolve_request.resolution.CopyFrom(resolution)
            resolved = manager.resolve_interaction(resolve_request)

        self.assertEqual(created_session.id, "session-managed-1")
        self.assertEqual(fetched_session.id, "session-managed-1")
        self.assertEqual(len(listed_sessions.sessions), 1)
        self.assertEqual(updated_session.client_ref, "cli-session-2")
        self.assertEqual(created_turn.id, "turn-managed-1")
        self.assertEqual(len(created_turn.messages[0].parts), 1)
        self.assertEqual(fetched_turn.id, "turn-managed-1")
        self.assertEqual(len(listed_turns.turns), 1)
        self.assertEqual(canceled_turn.status_message, "user canceled")
        self.assertEqual(len(turn_events.events), 1)
        self.assertEqual(len(interactions.interactions), 1)
        self.assertEqual(resolved.id, "interaction-1")
        self.assertEqual(resolved.state, agent_pb2.AGENT_INTERACTION_STATE_RESOLVED)
        self.assertEqual(_manager_relay_tokens, ["relay-token-py"] * 11)
        self.assertEqual(
            _manager_requests,
            [
                {
                    "method": "create_session",
                    "invocation_token": "token-123",
                    "provider_name": "openai",
                    "session_id": "",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "get_session",
                    "invocation_token": "token-123",
                    "provider_name": "",
                    "session_id": "session-managed-1",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "list_sessions",
                    "invocation_token": "token-123",
                    "provider_name": "openai",
                    "session_id": "",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "update_session",
                    "invocation_token": "token-123",
                    "provider_name": "",
                    "session_id": "session-managed-1",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "create_turn",
                    "invocation_token": "token-123",
                    "provider_name": "",
                    "session_id": "session-managed-1",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "get_turn",
                    "invocation_token": "token-123",
                    "provider_name": "",
                    "session_id": "",
                    "turn_id": "turn-managed-1",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "list_turns",
                    "invocation_token": "token-123",
                    "provider_name": "",
                    "session_id": "session-managed-1",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "cancel_turn",
                    "invocation_token": "token-123",
                    "provider_name": "",
                    "session_id": "",
                    "turn_id": "turn-managed-1",
                    "interaction_id": "",
                    "reason": "user canceled",
                },
                {
                    "method": "list_turn_events",
                    "invocation_token": "token-123",
                    "provider_name": "",
                    "session_id": "",
                    "turn_id": "turn-managed-1",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "list_interactions",
                    "invocation_token": "token-123",
                    "provider_name": "",
                    "session_id": "",
                    "turn_id": "turn-managed-1",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "resolve_interaction",
                    "invocation_token": "token-123",
                    "provider_name": "",
                    "session_id": "",
                    "turn_id": "turn-managed-1",
                    "interaction_id": "interaction-1",
                    "reason": "",
                },
            ],
        )

    def test_request_agent_manager_roundtrip(self) -> None:
        request = Request(invocation_token="token-embedded")

        with request.agent_manager() as manager:
            fetched = manager.get_session(
                agent_pb2.AgentManagerGetSessionRequest(session_id="session-managed-1")
            )

        self.assertEqual(fetched.id, "session-managed-1")
        self.assertEqual(_manager_relay_tokens, ["relay-token-py"])
        self.assertEqual(
            _manager_requests,
            [
                {
                    "method": "get_session",
                    "invocation_token": "token-embedded",
                    "provider_name": "",
                    "session_id": "session-managed-1",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                }
            ],
        )


if __name__ == "__main__":
    unittest.main()
