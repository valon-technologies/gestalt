"""Transport-backed Agent SDK tests over real sockets."""

from __future__ import annotations

import os
import tempfile
import unittest
from concurrent import futures
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
    Agent,
    AgentCatalogToolConfig,
    AgentCreateSession,
    AgentCreateTurn,
    AgentInteraction,
    AgentMessage,
    AgentMessagePart,
    AgentMessagePartImageRef,
    AgentMessagePartToolCall,
    AgentMessagePartToolResult,
    AgentOutput,
    AgentProvider,
    AgentProviderCapabilities,
    AgentResolveInteraction,
    AgentSession,
    AgentStructuredOutput,
    AgentTextOutput,
    AgentToolRef,
    AgentTurn,
    AgentTurnEvent,
    AgentTurnOutput,
    Error,
    ListAgentProviderInteractionsResponse,
    ListAgentProviderSessionsResponse,
    ListAgentProviderTurnEventsResponse,
    ListAgentProviderTurnsResponse,
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
from gestalt._gen.v1 import app_pb2 as _app_pb2
from gestalt._gen.v1 import runtime_pb2 as _runtime_pb2
from gestalt._gen.v1 import runtime_pb2_grpc as _runtime_pb2_grpc

agent_pb2: Any = _agent_pb2
agent_pb2_grpc: Any = _agent_pb2_grpc
empty_pb2: Any = _empty_pb2
app_pb2: Any = _app_pb2
runtime_pb2: Any = _runtime_pb2
runtime_pb2_grpc: Any = _runtime_pb2_grpc
struct_pb2: Any = _struct_pb2

_runtime_server: grpc.Server | None = None
_host_server: grpc.Server | None = None
_runtime_socket = ""
_host_socket = ""
_previous_envs: dict[str, str | None] = {}
_provider: "_AgentRuntimeProvider"
_manager_requests: list[dict[str, Any]] = []
_manager_contexts: list[dict[str, Any]] = []
_manager_workflows: list[dict[str, Any]] = []
_manager_session_tools: list[dict[str, str]] = []
_manager_relay_tokens: list[str] = []


def _native_context_subject_id(context: Any) -> str:
    if context is None or not context.HasField("subject"):
        return ""
    return context.subject.id


class _AgentRuntimeProvider(AgentProvider, MetadataProvider, WarningsProvider):
    def __init__(self) -> None:
        self.configured: list[tuple[str, dict[str, object]]] = []
        self.context_subject_ids: list[str] = []
        self.session_tools: list[dict[str, str]] = []

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
        self.context_subject_ids.append(_native_context_subject_id(request.context))
        self.session_tools.append(
            {
                "tool_ref_operation": request.tools.refs[0].operation
                if request.tools is not None and request.tools.refs
                else "",
                "listed_tool_mcp_name": request.tools.tools[0].mcp_name
                if request.tools is not None and request.tools.tools
                else "",
            }
        )
        return AgentSession(
            id=request.session_id,
            provider_name="py-agent",
            model=request.model,
            client_ref=request.client_ref,
            state=AGENT_SESSION_STATE_ACTIVE,
            metadata=request.metadata,
            created_by_subject_id=request.created_by_subject_id,
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
        self.context_subject_ids.append(_native_context_subject_id(request.context))
        return AgentTurn(
            id=request.turn_id,
            session_id=request.session_id,
            provider_name="py-agent",
            model=request.model,
            status=AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT,
            messages=request.messages,
            output=AgentTurnOutput(
                text="echo:Plan it",
            ),
            status_message="waiting for input",
            created_by_subject_id=request.created_by_subject_id,
            execution_ref=request.execution_ref,
        )

    def get_turn(self, request: Any) -> Any:
        return AgentTurn(
            id=request.turn_id,
            session_id="session-1",
            provider_name="py-agent",
            model="gpt-5.1",
            status=AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT,
            output=AgentTurnOutput(
                text="echo:Plan it",
            ),
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
            interactions=True,
            resumable_turns=True,
            reasoning_summaries=False,
        )


class _AgentServicer(agent_pb2_grpc.AgentProviderServicer):
    def CreateSession(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _record_manager_request(
            request,
            "create_session",
            provider_name=request.provider_name,
        )
        _manager_session_tools.append(
            {
                "tool_ref_operation": request.tools.catalog.refs[0].operation
                if request.HasField("tools")
                and request.tools.HasField("catalog")
                and request.tools.catalog.refs
                else "",
                "listed_tool_mcp_name": request.tools.catalog.tools[0].mcp_name
                if request.HasField("tools")
                and request.tools.HasField("catalog")
                and request.tools.catalog.tools
                else "",
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
        _record_manager_workflow(request)
        _record_manager_request(request, "get_session", session_id=request.session_id)
        return agent_pb2.AgentSession(
            id=request.session_id,
            provider_name="openai",
            model="gpt-5.1",
            client_ref="cli-session-1",
            state=agent_pb2.AGENT_SESSION_STATE_ARCHIVED,
        )

    def ListSessions(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _record_manager_request(
            request,
            "list_sessions",
            provider_name=request.provider_name,
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
        _record_manager_request(request, "update_session", session_id=request.session_id)
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
        _record_manager_request(
            request,
            "create_turn",
            session_id=request.session_id,
            timeout_seconds=request.timeout_seconds,
            output_kind=request.output.WhichOneof("kind")
            if request.HasField("output")
            else "",
            has_structured_schema=request.output.structured.HasField("schema")
            if request.HasField("output")
            and request.output.WhichOneof("kind") == "structured"
            else False,
        )
        return agent_pb2.AgentTurn(
            id="turn-managed-1",
            session_id=request.session_id,
            provider_name="openai",
            model=request.model,
            status=agent_pb2.AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT,
            messages=request.messages,
            text=agent_pb2.AgentTurnTextOutput(text="echo:Summarize this"),
            status_message="waiting for input",
        )

    def GetTurn(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _record_manager_request(request, "get_turn", turn_id=request.turn_id)
        return agent_pb2.AgentTurn(
            id=request.turn_id,
            session_id="session-managed-1",
            provider_name="openai",
            model="gpt-5.1",
            status=agent_pb2.AGENT_EXECUTION_STATUS_SUCCEEDED,
            text=agent_pb2.AgentTurnTextOutput(text="done"),
            status_message="completed",
        )

    def ListTurns(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _record_manager_request(request, "list_turns", session_id=request.session_id)
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
        _record_manager_request(
            request,
            "cancel_turn",
            turn_id=request.turn_id,
            reason=request.reason,
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
        _record_manager_request(request, "list_turn_events", turn_id=request.turn_id)
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
        _record_manager_request(request, "list_interactions", turn_id=request.turn_id)
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
        _record_manager_request(
            request,
            "resolve_interaction",
            turn_id=request.turn_id,
            interaction_id=request.interaction_id,
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


def _record_manager_request(request: Any, method: str, **fields: Any) -> None:
    _record_manager_context(request)
    entry = {
        "method": method,
        "provider_name": "",
        "session_id": "",
        "turn_id": "",
        "interaction_id": "",
        "reason": "",
    }
    entry.update(fields)
    _manager_requests.append(entry)


def _record_manager_context(request: Any) -> None:
    _manager_contexts.append(
        json_format.MessageToDict(
            request.context,
            preserving_proto_field_name=True,
        )
        if request.HasField("context")
        else {}
    )


def _record_relay_tokens(context: grpc.ServicerContext) -> None:
    _manager_relay_tokens.extend(
        value
        for key, value in context.invocation_metadata()
        if key == "x-gestalt-host-service-relay-token"
    )


def _record_manager_workflow(request: Any) -> None:
    _manager_workflows.append(
        json_format.MessageToDict(
            request.context.workflow,
            preserving_proto_field_name=True,
        )
        if request.HasField("context") and request.context.HasField("workflow")
        else {}
    )


def _fresh_socket(name: str) -> str:
    path = os.path.join(tempfile.gettempdir(), f"{name}-{os.getpid()}.sock")
    if os.path.exists(path):
        os.remove(path)
    return path


def setUpModule() -> None:
    global _runtime_server, _host_server
    global _runtime_socket, _host_socket, _provider

    _provider = _AgentRuntimeProvider()
    _runtime_socket = _fresh_socket("py-agent-runtime")
    _host_socket = _fresh_socket("py-agent-host")

    _runtime_server = grpc.server(futures.ThreadPoolExecutor(max_workers=2))
    adapter = _runtime._servable_target(_provider, runtime_kind=ProviderKind.AGENT)
    _runtime._register_services(server=_runtime_server, servable=adapter)
    _runtime_server.add_insecure_port(f"unix:{_runtime_socket}")
    _runtime_server.start()

    _host_server = grpc.server(futures.ThreadPoolExecutor(max_workers=2))
    agent_pb2_grpc.add_AgentProviderServicer_to_server(
        _AgentServicer(),
        _host_server,
    )
    _host_server.add_insecure_port(f"unix:{_host_socket}")
    _host_server.start()

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
    for server in (_runtime_server, _host_server):
        if server is not None:
            server.stop(grace=0).wait()
    for path in (_runtime_socket, _host_socket):
        if path and os.path.exists(path):
            os.remove(path)


class AgentTransportTests(unittest.TestCase):
    def setUp(self) -> None:
        _provider.configured.clear()
        _provider.context_subject_ids.clear()
        _provider.session_tools.clear()
        _manager_requests.clear()
        _manager_contexts.clear()
        _manager_workflows.clear()
        _manager_session_tools.clear()
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
            created_by_subject_id="user:session-owner",
            context=app_pb2.RequestContext(
                subject=app_pb2.SubjectContext(id="user:session")
            ),
            tools=agent_pb2.AgentToolConfig(
                catalog=agent_pb2.AgentCatalogToolConfig(
                    refs=[
                        app_pb2.AgentToolRef(
                            app="slack",
                            operation="chat.postMessage",
                        )
                    ],
                    tools=[
                        agent_pb2.ListedAgentTool(
                            id="tool-slack",
                            mcp_name="slack__chat_post_message",
                            title="Send Slack message",
                            description="Post a Slack message",
                            input_schema='{"type":"object"}',
                            ref=app_pb2.AgentToolRef(
                                app="slack",
                                operation="chat.postMessage",
                            ),
                        )
                    ],
                )
            ),
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
                timeout_seconds=120,
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
                created_by_subject_id="user:turn-owner",
                execution_ref="exec-turn-1",
                context=app_pb2.RequestContext(
                    subject=app_pb2.SubjectContext(id="user:turn")
                ),
                output=agent_pb2.AgentOutput(
                    text=agent_pb2.AgentTextOutput(),
                ),
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
        self.assertEqual(created_session.created_by_subject_id, "user:session-owner")
        self.assertEqual(
            [session.id for session in listed_sessions.sessions], ["session-1"]
        )
        self.assertEqual(fetched_session.state, agent_pb2.AGENT_SESSION_STATE_ARCHIVED)
        self.assertEqual(updated_session.client_ref, "cli-session-2")
        self.assertEqual(created_turn.id, "turn-1")
        self.assertEqual(created_turn.created_by_subject_id, "user:turn-owner")
        self.assertEqual(_provider.context_subject_ids, ["user:session", "user:turn"])
        self.assertEqual(
            _provider.session_tools,
            [
                {
                    "tool_ref_operation": "chat.postMessage",
                    "listed_tool_mcp_name": "slack__chat_post_message",
                }
            ],
        )
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

    def test_agent_roundtrip(self) -> None:
        context = app_pb2.RequestContext(
            subject=app_pb2.SubjectContext(id="user:agent-manager")
        )
        with Agent(Request(context=context)) as manager:
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
                    timeout_seconds=120,
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
                    output=agent_pb2.AgentOutput(
                        structured={"schema": {"type": "object"}},
                    ),
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
            _manager_contexts,
            [{"subject": {"id": "user:agent-manager"}}] * 11,
        )
        self.assertEqual(
            _manager_requests,
            [
                {
                    "method": "create_session",
                    "provider_name": "openai",
                    "session_id": "",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "get_session",
                    "provider_name": "",
                    "session_id": "session-managed-1",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "list_sessions",
                    "provider_name": "openai",
                    "session_id": "",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "update_session",
                    "provider_name": "",
                    "session_id": "session-managed-1",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "create_turn",
                    "provider_name": "",
                    "session_id": "session-managed-1",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                    "timeout_seconds": 120,
                    "output_kind": "structured",
                    "has_structured_schema": True,
                },
                {
                    "method": "get_turn",
                    "provider_name": "",
                    "session_id": "",
                    "turn_id": "turn-managed-1",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "list_turns",
                    "provider_name": "",
                    "session_id": "session-managed-1",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "cancel_turn",
                    "provider_name": "",
                    "session_id": "",
                    "turn_id": "turn-managed-1",
                    "interaction_id": "",
                    "reason": "user canceled",
                },
                {
                    "method": "list_turn_events",
                    "provider_name": "",
                    "session_id": "",
                    "turn_id": "turn-managed-1",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "list_interactions",
                    "provider_name": "",
                    "session_id": "",
                    "turn_id": "turn-managed-1",
                    "interaction_id": "",
                    "reason": "",
                },
                {
                    "method": "resolve_interaction",
                    "provider_name": "",
                    "session_id": "",
                    "turn_id": "turn-managed-1",
                    "interaction_id": "interaction-1",
                    "reason": "",
                },
            ],
        )

    def test_agent_create_session_accepts_direct_catalog_tools_dict(self) -> None:
        context = app_pb2.RequestContext(
            subject=app_pb2.SubjectContext(id="user:agent-manager")
        )
        with Agent(Request(context=context)) as manager:
            manager.create_session(
                AgentCreateSession(
                    provider_name="openai",
                    model="gpt-5.1",
                    tools={
                        "refs": [
                            {
                                "app": "slack",
                                "operation": "chat.postMessage",
                            },
                        ],
                        "tools": [
                            {
                                "id": "tool-slack",
                                "mcp_name": "slack__chat_post_message",
                                "title": "Send Slack message",
                                "description": "Post a Slack message",
                                "input_schema": '{"type":"object"}',
                                "ref": {
                                    "app": "slack",
                                    "operation": "chat.postMessage",
                                },
                            },
                        ],
                    },
                )
            )

        self.assertEqual(
            _manager_session_tools[-1],
            {
                "tool_ref_operation": "chat.postMessage",
                "listed_tool_mcp_name": "slack__chat_post_message",
            },
        )

    def test_request_agent_roundtrip(self) -> None:
        request = Request(
            context=app_pb2.RequestContext(
                subject=app_pb2.SubjectContext(id="user:request-agent")
            )
        )

        with request.agent() as manager:
            fetched = manager.get_session(
                agent_pb2.GetAgentProviderSessionRequest(session_id="session-managed-1")
            )

        self.assertEqual(fetched.id, "session-managed-1")
        self.assertEqual(_manager_relay_tokens, ["relay-token-py"])
        self.assertEqual(
            _manager_contexts,
            [{"subject": {"id": "user:request-agent"}}],
        )
        self.assertEqual(
            _manager_requests,
            [
                {
                    "method": "get_session",
                    "provider_name": "",
                    "session_id": "session-managed-1",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                }
            ],
        )

    def test_request_agent_forwards_workflow_context(self) -> None:
        context = app_pb2.RequestContext(
            subject=app_pb2.SubjectContext(id="user:workflow-caller")
        )
        context.workflow.update(
            {
                "runId": "run-python-agent",
                "runAs": {"id": "service_account:workflow-test"},
            }
        )
        request = Request(
            context=context,
        )

        with request.agent() as manager:
            fetched = manager.get_session(
                agent_pb2.GetAgentProviderSessionRequest(session_id="session-managed-1")
            )

        self.assertEqual(fetched.id, "session-managed-1")
        self.assertEqual(
            _manager_requests,
            [
                {
                    "method": "get_session",
                    "provider_name": "",
                    "session_id": "session-managed-1",
                    "turn_id": "",
                    "interaction_id": "",
                    "reason": "",
                }
            ],
        )
        self.assertEqual(
            _manager_contexts,
            [
                {
                    "subject": {"id": "user:workflow-caller"},
                    "workflow": {
                        "runId": "run-python-agent",
                        "runAs": {"id": "service_account:workflow-test"},
                    },
                }
            ],
        )
        self.assertEqual(
            _manager_workflows,
            [
                {
                    "runId": "run-python-agent",
                    "runAs": {"id": "service_account:workflow-test"},
                }
            ],
        )

    def test_agent_accepts_native_inputs(self) -> None:
        with Agent(Request()) as manager:
            created_session = manager.create_session(
                AgentCreateSession(
                    provider_name="openai",
                    model="gpt-5.1",
                    client_ref="cli-session-1",
                    tools=AgentCatalogToolConfig(
                        refs=[
                            AgentToolRef(
                                app="github",
                                operation="issues.get",
                                connection="default",
                            )
                        ]
                    ),
                )
            )
            created_turn = manager.create_turn(
                AgentCreateTurn(
                    session_id=created_session.id,
                    model="gpt-5.1",
                    messages=[
                        AgentMessage(
                            role="user",
                            text="Summarize this",
                            parts=[AgentMessagePart(text="Summarize this")],
                            metadata={"source": "native"},
                        )
                    ],
                    output=AgentOutput(
                        structured=AgentStructuredOutput(
                            schema={"type": "object"},
                        ),
                    ),
                    metadata={"request": "native"},
                    model_options={"temperature": 0},
                    timeout_seconds=120,
                )
            )
            resolved = manager.resolve_interaction(
                AgentResolveInteraction(
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
        self.assertEqual(_manager_relay_tokens, ["relay-token-py", "relay-token-py", "relay-token-py"])
        self.assertEqual(
            [item["method"] for item in _manager_requests],
            ["create_session", "create_turn", "resolve_interaction"],
        )
        self.assertEqual(_manager_session_tools[0]["tool_ref_operation"], "issues.get")
        self.assertEqual(_manager_requests[1]["timeout_seconds"], 120)
        self.assertEqual(_manager_requests[1]["output_kind"], "structured")
        self.assertTrue(_manager_requests[1]["has_structured_schema"])

    def test_agent_create_turn_requires_unambiguous_output(self) -> None:
        with Agent(Request()) as manager:
            with self.assertRaisesRegex(ValueError, "agent output is required"):
                manager.create_turn(
                    AgentCreateTurn(
                        timeout_seconds=120,
                        session_id="session-managed-1",
                        messages=[AgentMessage(role="user", text="Summarize this")],
                    )
                )
            with self.assertRaisesRegex(
                ValueError,
                "exactly one of output.text or output.structured is required",
            ):
                manager.create_turn(
                    AgentCreateTurn(
                        timeout_seconds=120,
                        session_id="session-managed-1",
                        messages=[AgentMessage(role="user", text="Summarize this")],
                        output=AgentOutput(
                            text=AgentTextOutput(),
                            structured=AgentStructuredOutput(
                                schema={"type": "object"}
                            ),
                        ),
                    )
                )
            with self.assertRaisesRegex(
                ValueError,
                "output.structured.schema is required",
            ):
                manager.create_turn(
                    AgentCreateTurn(
                        timeout_seconds=120,
                        session_id="session-managed-1",
                        messages=[AgentMessage(role="user", text="Summarize this")],
                        output=AgentOutput(
                            structured={"schema": None},
                        ),
                    )
                )


if __name__ == "__main__":
    unittest.main()
