"""Transport-backed Agent SDK tests over real sockets."""

from __future__ import annotations

import os
import tempfile
import unittest
from concurrent import futures
from datetime import datetime, timezone
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
    AgentInteraction,
    AgentMessage,
    AgentMessagePart,
    AgentMessagePartImageRef,
    AgentMessagePartToolCall,
    AgentMessagePartToolResult,
    AgentProvider,
    AgentProviderCapabilities,
    AgentSession,
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
from gestalt import agent as genagent
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



class _AgentRuntimeProvider(AgentProvider, MetadataProvider, WarningsProvider):
    def __init__(self) -> None:
        self.configured: list[tuple[str, dict[str, object]]] = []
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
            id="session-1",
            provider_name="py-agent",
            model=request.model,
            client_ref=request.client_ref,
            state=AGENT_SESSION_STATE_ACTIVE,
            metadata=request.metadata,
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
            output=AgentTurnOutput(
                text="echo:Plan it",
            ),
            status_message="waiting for input",
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


class _AgentServicer(agent_pb2_grpc.AgentServicer):
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
    agent_pb2_grpc.add_AgentServicer_to_server(
        _AgentServicer(),
        _host_server,
    )
    _host_server.add_insecure_port(f"unix:{_host_socket}")
    _host_server.start()

    for env_name, value in {
        ENV_HOST_SERVICE_SOCKET: _host_socket,
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
        _provider.session_tools.clear()
        _manager_requests.clear()
        _manager_contexts.clear()
        _manager_workflows.clear()
        _manager_session_tools.clear()
        _manager_relay_tokens.clear()


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

    def test_agent_provider_native_errors_map_to_grpc_statuses(self) -> None:
        channel = grpc.insecure_channel(f"unix:{_runtime_socket}")
        provider_client = agent_pb2_grpc.AgentStub(channel)

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
            provider_client = agent_pb2_grpc.AgentStub(channel)

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


    def test_request_agent_roundtrip(self) -> None:
        request = Request(
            context=app_pb2.RequestContext(
                subject=app_pb2.SubjectContext(id="user:request-agent")
            ),
            relay_token="relay-token-py",
        )

        manager = request.agent()
        fetched = manager.get_session(session_id="session-managed-1")

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

        manager = request.agent()
        fetched = manager.get_session(session_id="session-managed-1")

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

    def test_agent_accepts_native_message_metadata(self) -> None:
        manager = genagent.Agent.connect(relay_token="relay-token-py")
        created_turn = manager.create_turn(
            session_id="session-managed-1",
            model="gpt-5.1",
            messages=[
                genagent.AgentMessage(
                    role="user",
                    text="Summarize this",
                    parts=[genagent.AgentMessagePart(text="Summarize this")],
                    metadata={"source": "native"},
                )
            ],
            output=genagent.AgentOutput(
                kind=genagent.AgentOutputStructured(
                    value=genagent.AgentStructuredOutput(
                        schema={"type": "object"},
                    )
                )
            ),
            metadata={"request": "native"},
            model_options={"temperature": 0},
            timeout_seconds=120,
        )
        resolved = manager.resolve_interaction(
            turn_id="turn-managed-1",
            interaction_id="interaction-1",
            resolution={"approved": True},
        )

        self.assertEqual(created_turn.id, "turn-managed-1")
        self.assertEqual(created_turn.messages[0].role, "user")
        assert created_turn.messages[0].metadata is not None
        self.assertEqual(created_turn.messages[0].metadata["source"], "native")
        self.assertEqual(resolved.id, "interaction-1")
        self.assertEqual(_manager_relay_tokens, ["relay-token-py", "relay-token-py"])
        self.assertEqual(
            [item["method"] for item in _manager_requests],
            ["create_turn", "resolve_interaction"],
        )
        self.assertEqual(_manager_requests[0]["timeout_seconds"], 120)
        self.assertEqual(_manager_requests[0]["output_kind"], "structured")
        self.assertTrue(_manager_requests[0]["has_structured_schema"])


if __name__ == "__main__":
    unittest.main()
