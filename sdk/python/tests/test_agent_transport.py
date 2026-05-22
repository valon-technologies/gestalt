"""Transport-backed Agent SDK tests over real sockets."""

from __future__ import annotations

import os
import tempfile
import unittest
from concurrent import futures
from dataclasses import dataclass
from datetime import datetime, timezone
from importlib import resources
from typing import Any

import grpc
from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import json_format
from google.protobuf import struct_pb2 as _struct_pb2

from gestalt import (
    AGENT_EXECUTION_STATUS_CANCELED,
    AGENT_EXECUTION_STATUS_SUCCEEDED,
    AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT,
    AGENT_INTERACTION_STATE_PENDING,
    AGENT_INTERACTION_STATE_RESOLVED,
    AGENT_INTERACTION_TYPE_APPROVAL,
    AGENT_SESSION_STATE_ACTIVE,
    AGENT_SESSION_STATE_ARCHIVED,
    ENV_HOST_SERVICE_SOCKET,
    ENV_HOST_SERVICE_TOKEN,
    AgentHost,
    AgentInteraction,
    AgentManager,
    AgentManagerCreateTurn,
    AgentManagerResolveInteraction,
    AgentMessage,
    AgentMessagePart,
    AgentMessagePartImageRef,
    AgentMessagePartToolCall,
    AgentMessagePartToolResult,
    AgentProvider,
    AgentProviderCapabilities,
    AgentSession,
    AgentToolRef,
    AgentTurn,
    AgentTurnEvent,
    Error,
    ListAgentProviderInteractionsResponse,
    ListAgentProviderSessionsResponse,
    ListAgentProviderTurnEventsResponse,
    ListAgentProviderTurnsResponse,
    ListAgentToolsRequest,
    MetadataProvider,
    ProviderKind,
    ProviderMetadata,
    Request,
    WarningsProvider,
    _runtime,
    agent_message_from_dict,
    agent_message_part_from_dict,
    agent_message_to_dict,
)
from gestalt._gen.v1 import agent_pb2 as _agent_pb2
from gestalt._gen.v1 import agent_pb2_grpc as _agent_pb2_grpc
from gestalt._gen.v1 import plugin_pb2 as _plugin_pb2
from gestalt._gen.v1 import runtime_pb2 as _runtime_pb2
from gestalt._gen.v1 import runtime_pb2_grpc as _runtime_pb2_grpc

agent_pb2: Any = _agent_pb2
agent_pb2_grpc: Any = _agent_pb2_grpc
empty_pb2: Any = _empty_pb2
plugin_pb2: Any = _plugin_pb2
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
_host_list_requests: list[dict[str, Any]] = []
_host_execute_requests: list[dict[str, Any]] = []
_host_connection_requests: list[dict[str, Any]] = []
_manager_requests: list[dict[str, str]] = []
_manager_relay_tokens: list[str] = []


@dataclass
class ToolArguments:
    query: str


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

    def create_session(self, request: Any) -> Any:
        return AgentSession(
            id=request.session_id,
            provider_name="py-agent",
            model=request.model,
            client_ref=request.client_ref,
            state=AGENT_SESSION_STATE_ACTIVE,
            metadata=request.metadata,
            created_by=request.created_by,
        )

    def get_session(self, request: Any) -> Any:
        if request.session_id == "missing-session":
            raise Error(404, "agent session 'missing-session' was not found")
        return AgentSession(
            id=request.session_id,
            provider_name="py-agent",
            model="gpt-5.1",
            client_ref="cli-session-1",
            state=AGENT_SESSION_STATE_ARCHIVED,
            metadata={"source": "py-test"},
        )

    def list_sessions(self, request: Any) -> Any:
        return ListAgentProviderSessionsResponse(
            sessions=[
                AgentSession(
                    id="session-1",
                    provider_name="py-agent",
                    model="gpt-5.1",
                    client_ref="cli-session-1",
                    state=AGENT_SESSION_STATE_ARCHIVED,
                )
            ]
        )

    def update_session(self, request: Any) -> Any:
        return AgentSession(
            id=request.session_id,
            provider_name="py-agent",
            model="gpt-5.1",
            client_ref=request.client_ref,
            state=request.state,
            metadata=request.metadata,
        )

    def create_turn(self, request: Any) -> Any:
        return AgentTurn(
            id=request.turn_id,
            session_id=request.session_id,
            provider_name="py-agent",
            model=request.model,
            status=AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT,
            messages=request.messages,
            output_text="echo:Plan it",
            status_message="waiting for input",
            created_by=request.created_by,
            execution_ref=request.execution_ref,
        )

    def get_turn(self, request: Any) -> Any:
        return AgentTurn(
            id=request.turn_id,
            session_id="session-1",
            provider_name="py-agent",
            model="gpt-5.1",
            status=AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT,
            output_text="echo:Plan it",
            status_message="waiting for input",
        )

    def list_turns(self, request: Any) -> Any:
        return ListAgentProviderTurnsResponse(
            turns=[
                AgentTurn(
                    id="turn-1",
                    session_id=request.session_id,
                    provider_name="py-agent",
                    model="gpt-5.1",
                    status=AGENT_EXECUTION_STATUS_SUCCEEDED,
                    status_message="done",
                )
            ]
        )

    def cancel_turn(self, request: Any) -> Any:
        return AgentTurn(
            id=request.turn_id,
            session_id="session-1",
            provider_name="py-agent",
            model="gpt-5.1",
            status=AGENT_EXECUTION_STATUS_CANCELED,
            status_message=request.reason,
        )

    def list_turn_events(self, request: Any) -> Any:
        return ListAgentProviderTurnEventsResponse(
            events=[
                AgentTurnEvent(
                    id=f"{request.turn_id}-event-1",
                    turn_id=request.turn_id,
                    seq=1,
                    type="turn.started",
                    source="py-agent",
                    visibility="private",
                ),
                AgentTurnEvent(
                    id=f"{request.turn_id}-event-2",
                    turn_id=request.turn_id,
                    seq=2,
                    type="interaction.requested",
                    source="py-agent",
                    visibility="private",
                ),
            ]
        )

    def get_interaction(self, request: Any) -> Any:
        return AgentInteraction(
            id=request.interaction_id,
            turn_id="turn-1",
            session_id="session-1",
            type=AGENT_INTERACTION_TYPE_APPROVAL,
            state=AGENT_INTERACTION_STATE_PENDING,
            title="Approve command",
            prompt="Run git status?",
        )

    def list_interactions(self, request: Any) -> Any:
        return ListAgentProviderInteractionsResponse(
            interactions=[
                AgentInteraction(
                    id="interaction-1",
                    turn_id=request.turn_id,
                    session_id="session-1",
                    type=AGENT_INTERACTION_TYPE_APPROVAL,
                    state=AGENT_INTERACTION_STATE_PENDING,
                    title="Approve command",
                    prompt="Run git status?",
                )
            ]
        )

    def resolve_interaction(self, request: Any) -> Any:
        return AgentInteraction(
            id=request.interaction_id,
            turn_id="turn-1",
            session_id="session-1",
            type=AGENT_INTERACTION_TYPE_APPROVAL,
            state=AGENT_INTERACTION_STATE_RESOLVED,
            title="Approve command",
            prompt="Run git status?",
            resolution=request.resolution,
        )

    def get_capabilities(self, request: Any) -> Any:
        return AgentProviderCapabilities(
            streaming_text=True,
            tool_calls=True,
            parallel_tool_calls=False,
            structured_output=True,
            interactions=True,
            resumable_turns=True,
            reasoning_summaries=False,
        )


class _AgentHostServicer(agent_pb2_grpc.AgentHostServicer):
    def ListTools(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_host_relay_tokens(context)
        _host_list_requests.append(
            {
                "session_id": request.session_id,
                "turn_id": request.turn_id,
                "page_size": request.page_size,
                "page_token": request.page_token,
                "run_grant": request.run_grant,
                "query": request.query,
            }
        )
        if request.page_token == "large":
            return agent_pb2.ListAgentToolsResponse(
                tools=[
                    agent_pb2.ListedAgentTool(
                        id="tool-large",
                        mcp_name="large__tool",
                        title="Large tool",
                        description="x" * (5 * 1024 * 1024),
                        input_schema='{"type":"object"}',
                    )
                ]
            )
        return agent_pb2.ListAgentToolsResponse(
            tools=[
                agent_pb2.ListedAgentTool(
                    id="tool-1",
                    mcp_name="slack__chat_post_message",
                    title="Send Slack message",
                    description="Send a direct message",
                    input_schema='{"type":"object"}',
                    ref=plugin_pb2.AgentToolRef(
                        plugin="slack",
                        operation="chat.postMessage",
                    ),
                )
            ],
            next_page_token="next-1",
        )

    def ExecuteTool(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_host_relay_tokens(context)
        _host_execute_requests.append(
            {
                "session_id": request.session_id,
                "turn_id": request.turn_id,
                "tool_call_id": request.tool_call_id,
                "tool_id": request.tool_id,
                "arguments": json_format.MessageToDict(
                    request.arguments,
                    preserving_proto_field_name=True,
                )
                if request.HasField("arguments")
                else {},
                "idempotency_key": request.idempotency_key,
                "run_grant": request.run_grant,
            }
        )
        return agent_pb2.ExecuteAgentToolResponse(
            status=207,
            body=f"{request.session_id}:{request.turn_id}:{request.tool_call_id}:{request.tool_id}:{request.idempotency_key}",
        )

    def ResolveConnection(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_host_relay_tokens(context)
        _host_connection_requests.append(
            {
                "session_id": request.session_id,
                "turn_id": request.turn_id,
                "connection": request.connection,
                "instance": request.instance,
                "run_grant": request.run_grant,
            }
        )
        return agent_pb2.ResolvedAgentConnection(
            connection_id="vertex-ai",
            connection=request.connection,
            instance=request.instance,
            mode="user",
            headers={"authorization": "Bearer token"},
            params={"endpoint": "vertex-endpoint"},
        )


class _AgentManagerServicer(agent_pb2_grpc.AgentProviderServicer):
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
        return agent_pb2.ListAgentProviderSessionsResponse(
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
                "tool_source": request.tool_source,
                "tool_refs_set": request.tool_refs_set,
                "timeout_seconds": request.timeout_seconds,
                "has_response_schema": request.HasField("response_schema"),
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
        return agent_pb2.ListAgentProviderTurnsResponse(
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
        return agent_pb2.ListAgentProviderTurnEventsResponse(
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
        return agent_pb2.ListAgentProviderInteractionsResponse(
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
    agent_pb2_grpc.add_AgentProviderServicer_to_server(
        _AgentManagerServicer(),
        _host_server,
    )
    _host_server.add_insecure_port(f"unix:{_host_socket}")
    _host_server.start()

    _manager_server = grpc.server(futures.ThreadPoolExecutor(max_workers=2))
    agent_pb2_grpc.add_AgentProviderServicer_to_server(
        _AgentManagerServicer(),
        _manager_server,
    )
    _manager_server.add_insecure_port(f"unix:{_manager_socket}")
    _manager_server.start()

    for env_name, value in {
        ENV_HOST_SERVICE_SOCKET: _host_socket,
        ENV_HOST_SERVICE_TOKEN: "relay-token-py",
    }.items():
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
        _host_list_requests.clear()
        _host_execute_requests.clear()
        _host_connection_requests.clear()
        _manager_requests.clear()
        _manager_relay_tokens.clear()

    def test_private_generated_stubs_are_packaged(self) -> None:
        self.assertTrue(resources.files("gestalt").joinpath("py.typed").is_file())
        self.assertTrue(
            resources.files("gestalt").joinpath("_gen/v1/agent_pb2.pyi").is_file()
        )

    def test_agent_protocol_wrappers_accept_native_datetimes(self) -> None:
        created_at = datetime(2026, 5, 8, 12, 0, tzinfo=timezone.utc)

        session = AgentSession(id="session-1", created_at=created_at)
        turn = AgentTurn(id="turn-1", created_at=created_at)
        event = AgentTurnEvent(id="event-1", created_at=created_at)

        self.assertEqual(session.created_at, created_at)
        self.assertEqual(turn.created_at, created_at)
        self.assertEqual(event.created_at, created_at)

    def test_agent_message_dict_helpers_preserve_presence(self) -> None:
        message = agent_message_from_dict(
            {
                "role": "assistant",
                "parts": [
                    {
                        "type": agent_pb2.AGENT_MESSAGE_PART_TYPE_TOOL_CALL,
                        "tool_call": {
                            "id": "call-1",
                            "tool_id": "tool-1",
                            "arguments": {},
                        },
                    },
                    {
                        "type": agent_pb2.AGENT_MESSAGE_PART_TYPE_TOOL_RESULT,
                        "tool_result": {
                            "tool_call_id": "call-1",
                            "status": 0,
                            "output": {"ok": True},
                        },
                    },
                    {
                        "type": agent_pb2.AGENT_MESSAGE_PART_TYPE_IMAGE_REF,
                        "image_ref": {
                            "uri": "s3://bucket/image.png",
                            "mime_type": "image/png",
                        },
                    },
                ],
                "metadata": {},
            }
        )

        self.assertEqual(
            agent_message_to_dict(message),
            {
                "role": "assistant",
                "parts": [
                    {
                        "type": agent_pb2.AGENT_MESSAGE_PART_TYPE_TOOL_CALL,
                        "tool_call": {
                            "id": "call-1",
                            "tool_id": "tool-1",
                            "arguments": {},
                        },
                    },
                    {
                        "type": agent_pb2.AGENT_MESSAGE_PART_TYPE_TOOL_RESULT,
                        "tool_result": {
                            "tool_call_id": "call-1",
                            "status": 0,
                            "output": {"ok": True},
                        },
                    },
                    {
                        "type": agent_pb2.AGENT_MESSAGE_PART_TYPE_IMAGE_REF,
                        "image_ref": {
                            "uri": "s3://bucket/image.png",
                            "mime_type": "image/png",
                        },
                    },
                ],
                "metadata": {},
            },
        )

    def test_agent_message_dict_helpers_accept_nested_dataclass_inputs(self) -> None:
        message = agent_message_from_dict(
            AgentMessage(
                role="assistant",
                parts=[
                    AgentMessagePart(
                        tool_call=AgentMessagePartToolCall(
                            id="call-1",
                            tool_id="tool-1",
                            arguments={"ok": True},
                        )
                    ),
                    AgentMessagePart(
                        tool_result=AgentMessagePartToolResult(
                            tool_call_id="call-1",
                            status=0,
                            output={"accepted": True},
                        )
                    ),
                    AgentMessagePart(
                        image_ref=AgentMessagePartImageRef(
                            uri="s3://bucket/image.png",
                            mime_type="image/png",
                        )
                    ),
                ],
            )
        )

        self.assertEqual(
            message.parts[0].type,
            agent_pb2.AGENT_MESSAGE_PART_TYPE_TOOL_CALL,
        )
        self.assertEqual(message.parts[0].tool_call.id, "call-1")
        self.assertEqual(message.parts[0].tool_call.arguments, {"ok": True})
        self.assertEqual(
            message.parts[1].type,
            agent_pb2.AGENT_MESSAGE_PART_TYPE_TOOL_RESULT,
        )
        self.assertEqual(message.parts[1].tool_result.tool_call_id, "call-1")
        self.assertEqual(message.parts[1].tool_result.output, {"accepted": True})
        self.assertEqual(
            message.parts[2].type,
            agent_pb2.AGENT_MESSAGE_PART_TYPE_IMAGE_REF,
        )
        self.assertEqual(message.parts[2].image_ref.mime_type, "image/png")

        direct_part = agent_message_part_from_dict(
            AgentMessagePart(
                tool_call=AgentMessagePartToolCall(
                    id="call-2",
                    tool_id="tool-2",
                )
            )
        )
        self.assertEqual(direct_part.tool_call.id, "call-2")

    def test_agent_message_dict_helpers_preserve_native_shape(
        self,
    ) -> None:
        message = AgentMessage(
            role="assistant",
            metadata={"tenant": "acme"},
            parts=[
                AgentMessagePart(
                    type=agent_pb2.AGENT_MESSAGE_PART_TYPE_TEXT,
                    text="hello",
                )
            ],
        )

        raw = agent_message_to_dict(message)

        self.assertEqual(
            raw,
            {
                "role": "assistant",
                "parts": [
                    {
                        "type": agent_pb2.AGENT_MESSAGE_PART_TYPE_TEXT,
                        "text": "hello",
                    },
                ],
                "metadata": {"tenant": "acme"},
            },
        )
        self.assertEqual(agent_message_to_dict(agent_message_from_dict(raw)), raw)

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
            agent_pb2.GetAgentProviderInteractionRequest(interaction_id="interaction-1")
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
        self.assertEqual(
            [session.id for session in listed_sessions.sessions], ["session-1"]
        )
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

    def test_agent_provider_native_errors_map_to_grpc_statuses(self) -> None:
        channel = grpc.insecure_channel(f"unix:{_runtime_socket}")
        provider_client = agent_pb2_grpc.AgentProviderStub(channel)

        with self.assertRaises(grpc.RpcError) as raised:
            provider_client.GetSession(
                agent_pb2.GetAgentProviderSessionRequest(session_id="missing-session")
            )

        rpc_error: Any = raised.exception
        self.assertEqual(rpc_error.code(), grpc.StatusCode.NOT_FOUND)
        self.assertIn("missing-session", rpc_error.details())

    def test_agent_provider_missing_methods_map_to_unimplemented(self) -> None:
        socket_path = _fresh_socket("py-agent-partial-runtime")
        server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))
        adapter = _runtime._servable_target(
            AgentProvider(), runtime_kind=ProviderKind.AGENT
        )
        _runtime._register_services(server=server, servable=adapter)
        server.add_insecure_port(f"unix:{socket_path}")
        server.start()
        try:
            channel = grpc.insecure_channel(f"unix:{socket_path}")
            provider_client = agent_pb2_grpc.AgentProviderStub(channel)

            with self.assertRaises(grpc.RpcError) as raised:
                provider_client.GetCapabilities(
                    agent_pb2.GetAgentProviderCapabilitiesRequest()
                )

            rpc_error: Any = raised.exception
            self.assertEqual(rpc_error.code(), grpc.StatusCode.UNIMPLEMENTED)
            self.assertIn("get_capabilities", rpc_error.details())
        finally:
            server.stop(grace=0).wait()
            if os.path.exists(socket_path):
                os.remove(socket_path)

    def test_agent_host_roundtrip(self) -> None:
        with AgentHost() as host:
            list_response = host.list_tools_for_turn(
                "session-1",
                "turn-1",
                page_size=10,
                page_token="page-0",
                run_grant="grant-token",
                query="person",
            )
            response = host.execute_tool_for_turn(
                "session-1",
                "turn-1",
                tool_call_id="call-7",
                tool_id="lookup",
                arguments=ToolArguments(query="Ada Lovelace"),
                run_grant="grant-token",
                idempotency_key="tool-call-key-7",
            )
            connection = host.resolve_connection_for_turn(
                "session-1",
                "turn-1",
                connection="model",
                instance="default",
                run_grant="grant-token",
            )

        self.assertEqual(len(list_response.tools), 1)
        self.assertEqual(list_response.tools[0].mcp_name, "slack__chat_post_message")
        self.assertEqual(list_response.next_page_token, "next-1")
        self.assertEqual(response.status, 207)
        self.assertEqual(
            response.body, "session-1:turn-1:call-7:lookup:tool-call-key-7"
        )
        self.assertEqual(connection.connection_id, "vertex-ai")
        self.assertEqual(connection.headers["authorization"], "Bearer token")
        self.assertEqual(connection.params["endpoint"], "vertex-endpoint")
        self.assertEqual(
            _host_relay_tokens, ["relay-token-py", "relay-token-py", "relay-token-py"]
        )
        self.assertEqual(
            _host_list_requests,
            [
                {
                    "session_id": "session-1",
                    "turn_id": "turn-1",
                    "page_size": 10,
                    "page_token": "page-0",
                    "run_grant": "grant-token",
                    "query": "person",
                }
            ],
        )
        self.assertEqual(
            _host_execute_requests,
            [
                {
                    "session_id": "session-1",
                    "turn_id": "turn-1",
                    "tool_call_id": "call-7",
                    "tool_id": "lookup",
                    "arguments": {"query": "Ada Lovelace"},
                    "idempotency_key": "tool-call-key-7",
                    "run_grant": "grant-token",
                }
            ],
        )
        self.assertEqual(
            _host_connection_requests,
            [
                {
                    "session_id": "session-1",
                    "turn_id": "turn-1",
                    "connection": "model",
                    "instance": "default",
                    "run_grant": "grant-token",
                }
            ],
        )

    def test_agent_host_accepts_large_internal_responses(self) -> None:
        with AgentHost() as host:
            list_response = host.list_tools(
                ListAgentToolsRequest(
                    session_id="session-1",
                    turn_id="turn-1",
                    page_token="large",
                )
            )

        self.assertEqual(list_response.tools[0].id, "tool-large")
        self.assertEqual(len(list_response.tools[0].description), 5 * 1024 * 1024)

    def test_agent_manager_roundtrip(self) -> None:
        with AgentManager("token-123") as manager:
            created_session = manager.create_session(
                agent_pb2.CreateAgentProviderSessionRequest(
                    provider_name="openai",
                    model="gpt-5.1",
                    client_ref="cli-session-1",
                )
            )
            fetched_session = manager.get_session(
                agent_pb2.GetAgentProviderSessionRequest(session_id="session-managed-1")
            )
            listed_sessions = manager.list_sessions(
                agent_pb2.ListAgentProviderSessionsRequest(provider_name="openai")
            )
            updated_session = manager.update_session(
                agent_pb2.UpdateAgentProviderSessionRequest(
                    session_id="session-managed-1",
                    client_ref="cli-session-2",
                    state=agent_pb2.AGENT_SESSION_STATE_ARCHIVED,
                )
            )
            created_turn = manager.create_turn(
                agent_pb2.CreateAgentProviderTurnRequest(
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
                    tool_source=agent_pb2.AGENT_TOOL_SOURCE_MODE_NONE,
                    response_schema={"type": "object"},
                )
            )
            fetched_turn = manager.get_turn(
                agent_pb2.GetAgentProviderTurnRequest(turn_id="turn-managed-1")
            )
            listed_turns = manager.list_turns(
                agent_pb2.ListAgentProviderTurnsRequest(session_id="session-managed-1")
            )
            canceled_turn = manager.cancel_turn(
                agent_pb2.CancelAgentProviderTurnRequest(
                    turn_id="turn-managed-1",
                    reason="user canceled",
                )
            )
            turn_events = manager.list_turn_events(
                agent_pb2.ListAgentProviderTurnEventsRequest(
                    turn_id="turn-managed-1",
                    after_seq=0,
                    limit=10,
                )
            )
            interactions = manager.list_interactions(
                agent_pb2.ListAgentProviderInteractionsRequest(turn_id="turn-managed-1")
            )
            resolve_request = agent_pb2.ResolveAgentProviderInteractionRequest(
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
                    "tool_source": agent_pb2.AGENT_TOOL_SOURCE_MODE_NONE,
                    "tool_refs_set": False,
                    "timeout_seconds": 0,
                    "has_response_schema": True,
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
                agent_pb2.GetAgentProviderSessionRequest(session_id="session-managed-1")
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

    def test_agent_manager_accepts_native_inputs(self) -> None:
        with AgentManager("token-123") as manager:
            created_turn = manager.create_turn(
                AgentManagerCreateTurn(
                    session_id="session-managed-1",
                    model="gpt-5.1",
                    messages=[
                        AgentMessage(
                            role="user",
                            text="Summarize this",
                            parts=[AgentMessagePart(text="Summarize this")],
                            metadata={"source": "native"},
                        )
                    ],
                    tool_refs=[
                        AgentToolRef(
                            plugin="github",
                            operation="issues.get",
                            connection="default",
                        )
                    ],
                    tool_refs_set=True,
                    tool_source=agent_pb2.AGENT_TOOL_SOURCE_MODE_NONE,
                    response_schema={"type": "object"},
                    metadata={"request": "native"},
                    model_options={"temperature": 0},
                    timeout_seconds=120,
                )
            )
            resolved = manager.resolve_interaction(
                AgentManagerResolveInteraction(
                    turn_id="turn-managed-1",
                    interaction_id="interaction-1",
                    resolution={"approved": True},
                )
            )

        self.assertEqual(created_turn.id, "turn-managed-1")
        self.assertEqual(created_turn.messages[0].role, "user")
        self.assertEqual(created_turn.messages[0].metadata["source"], "native")
        self.assertEqual(
            created_turn.messages[0].parts[0].type,
            agent_pb2.AGENT_MESSAGE_PART_TYPE_TEXT,
        )
        self.assertEqual(resolved.id, "interaction-1")
        self.assertEqual(_manager_relay_tokens, ["relay-token-py", "relay-token-py"])
        self.assertEqual(
            [item["method"] for item in _manager_requests],
            ["create_turn", "resolve_interaction"],
        )
        self.assertEqual(_manager_requests[0]["tool_source"], agent_pb2.AGENT_TOOL_SOURCE_MODE_NONE)
        self.assertTrue(_manager_requests[0]["tool_refs_set"])
        self.assertEqual(_manager_requests[0]["timeout_seconds"], 120)
        self.assertTrue(_manager_requests[0]["has_response_schema"])


if __name__ == "__main__":
    unittest.main()
