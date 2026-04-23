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
_runtime_socket: str = ""
_host_socket: str = ""
_manager_socket: str = ""
_previous_envs: dict[str, str | None] = {}
_provider: "_AgentRuntimeProvider"
_host_events: list[dict[str, Any]] = []
_host_relay_tokens: list[str] = []
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

    def StartRun(self, request: Any, context: grpc.ServicerContext) -> Any:
        return agent_pb2.BoundAgentRun(
            id=request.run_id or request.idempotency_key,
            provider_name=request.provider_name,
            model=request.model,
            status=agent_pb2.AGENT_RUN_STATUS_PENDING,
            messages=request.messages,
            session_ref=request.session_ref,
            execution_ref=request.execution_ref,
        )


class _AgentHostServicer(agent_pb2_grpc.AgentHostServicer):
    def ExecuteTool(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_host_relay_tokens(context)
        return agent_pb2.ExecuteAgentToolResponse(
            status=207,
            body=f"{request.run_id}:{request.tool_call_id}:{request.tool_id}",
        )

    def EmitEvent(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_host_relay_tokens(context)
        _host_events.append(
            {
                "run_id": request.run_id,
                "type": request.type,
                "visibility": request.visibility,
                "data": json_format.MessageToDict(request.data),
            }
        )
        return empty_pb2.Empty()


class _AgentManagerServicer(agent_pb2_grpc.AgentManagerHostServicer):
    def Run(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "run",
                "invocation_token": request.invocation_token,
                "provider_name": request.provider_name,
                "run_id": "",
                "reason": "",
            }
        )
        return agent_pb2.ManagedAgentRun(
            provider_name=request.provider_name,
            run=agent_pb2.BoundAgentRun(
                id="run-managed-1",
                provider_name=request.provider_name,
                model=request.model,
                status=agent_pb2.AGENT_RUN_STATUS_RUNNING,
                messages=request.messages,
            ),
        )

    def GetRun(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "get",
                "invocation_token": request.invocation_token,
                "provider_name": "",
                "run_id": request.run_id,
                "reason": "",
            }
        )
        return agent_pb2.ManagedAgentRun(
            provider_name="openai",
            run=agent_pb2.BoundAgentRun(
                id=request.run_id,
                provider_name="openai",
                model="gpt-5.1",
                status=agent_pb2.AGENT_RUN_STATUS_SUCCEEDED,
            ),
        )

    def ListRuns(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "list",
                "invocation_token": request.invocation_token,
                "provider_name": "",
                "run_id": "",
                "reason": "",
            }
        )
        return agent_pb2.AgentManagerListRunsResponse(
            runs=[
                agent_pb2.ManagedAgentRun(
                    provider_name="openai",
                    run=agent_pb2.BoundAgentRun(
                        id="run-managed-1",
                        provider_name="openai",
                        model="gpt-5.1",
                        status=agent_pb2.AGENT_RUN_STATUS_RUNNING,
                    ),
                )
            ]
        )

    def CancelRun(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "cancel",
                "invocation_token": request.invocation_token,
                "provider_name": "",
                "run_id": request.run_id,
                "reason": request.reason,
            }
        )
        return agent_pb2.ManagedAgentRun(
            provider_name="openai",
            run=agent_pb2.BoundAgentRun(
                id=request.run_id,
                provider_name="openai",
                model="gpt-5.1",
                status=agent_pb2.AGENT_RUN_STATUS_CANCELED,
                status_message=request.reason,
            ),
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
        _host_events.clear()
        _host_relay_tokens.clear()
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
        started = provider_client.StartRun(
            agent_pb2.StartAgentProviderRunRequest(
                run_id="run-42",
                idempotency_key="idem-42",
                provider_name="openai",
                model="gpt-5.1",
                messages=[agent_pb2.AgentMessage(role="user", text="Plan it")],
                session_ref="sess-1",
                execution_ref="exec-1",
            )
        )

        self.assertEqual(identity.kind, runtime_pb2.ProviderKind.PROVIDER_KIND_AGENT)
        self.assertEqual(identity.name, "py-agent")
        self.assertEqual(list(identity.warnings), ["set OPENAI_API_KEY"])
        self.assertEqual(
            configured.protocol_version,
            _runtime.CURRENT_PROTOCOL_VERSION,
        )
        self.assertEqual(
            _provider.configured,
            [("agent-runtime", {"tenant": "acme"})],
        )
        self.assertEqual(started.id, "run-42")
        self.assertEqual(started.provider_name, "openai")
        self.assertEqual(started.model, "gpt-5.1")
        self.assertEqual(started.status, agent_pb2.AGENT_RUN_STATUS_PENDING)
        self.assertEqual(
            [(message.role, message.text) for message in started.messages],
            [("user", "Plan it")],
        )
        self.assertEqual(started.session_ref, "sess-1")
        self.assertEqual(started.execution_ref, "exec-1")

    def test_agent_host_roundtrip(self) -> None:
        arguments = struct_pb2.Struct()
        arguments.update({"query": "Ada Lovelace"})

        with AgentHost() as host:
            response = host.execute_tool(
                agent_pb2.ExecuteAgentToolRequest(
                    run_id="run-42",
                    tool_call_id="call-7",
                    tool_id="lookup",
                    arguments=arguments,
                )
            )
            data = struct_pb2.Struct()
            data.update({"phase": "tool_call", "attempt": 1})
            host.emit_event(
                agent_pb2.EmitAgentEventRequest(
                    run_id="run-42",
                    type="agent.tool_call.started",
                    visibility="public",
                    data=data,
                )
            )

        self.assertEqual(response.status, 207)
        self.assertEqual(response.body, "run-42:call-7:lookup")
        self.assertEqual(
            _host_events,
            [
                {
                    "run_id": "run-42",
                    "type": "agent.tool_call.started",
                    "visibility": "public",
                    "data": {"phase": "tool_call", "attempt": 1.0},
                }
            ],
        )
        self.assertEqual(_host_relay_tokens, ["relay-token-py"] * 2)

    def test_agent_manager_roundtrip(self) -> None:
        with AgentManager("token-123") as manager:
            started = manager.run(
                agent_pb2.AgentManagerRunRequest(
                    provider_name="openai",
                    model="gpt-5.1",
                    messages=[agent_pb2.AgentMessage(role="user", text="Summarize this")],
                    tool_source=agent_pb2.AGENT_TOOL_SOURCE_MODE_EXPLICIT,
                )
            )
            fetched = manager.get_run(
                agent_pb2.AgentManagerGetRunRequest(run_id="run-managed-1")
            )
            listed = manager.list_runs(agent_pb2.AgentManagerListRunsRequest())
            canceled = manager.cancel_run(
                agent_pb2.AgentManagerCancelRunRequest(
                    run_id="run-managed-1",
                    reason="user canceled",
                )
            )

        self.assertEqual(started.provider_name, "openai")
        self.assertEqual(started.run.id, "run-managed-1")
        self.assertEqual(fetched.run.id, "run-managed-1")
        self.assertEqual(len(listed.runs), 1)
        self.assertEqual(listed.runs[0].run.id, "run-managed-1")
        self.assertEqual(canceled.run.status_message, "user canceled")
        self.assertEqual(_manager_relay_tokens, ["relay-token-py"] * 4)
        self.assertEqual(
            _manager_requests,
            [
                {
                    "method": "run",
                    "invocation_token": "token-123",
                    "provider_name": "openai",
                    "run_id": "",
                    "reason": "",
                },
                {
                    "method": "get",
                    "invocation_token": "token-123",
                    "provider_name": "",
                    "run_id": "run-managed-1",
                    "reason": "",
                },
                {
                    "method": "list",
                    "invocation_token": "token-123",
                    "provider_name": "",
                    "run_id": "",
                    "reason": "",
                },
                {
                    "method": "cancel",
                    "invocation_token": "token-123",
                    "provider_name": "",
                    "run_id": "run-managed-1",
                    "reason": "user canceled",
                },
            ],
        )

    def test_request_agent_manager_roundtrip(self) -> None:
        request = Request(invocation_token="token-embedded")

        with request.agent_manager() as manager:
            fetched = manager.get_run(
                agent_pb2.AgentManagerGetRunRequest(run_id="run-managed-1")
            )

        self.assertEqual(fetched.run.id, "run-managed-1")
        self.assertEqual(_manager_relay_tokens, ["relay-token-py"])
        self.assertEqual(
            _manager_requests,
            [
                {
                    "method": "get",
                    "invocation_token": "token-embedded",
                    "provider_name": "",
                    "run_id": "run-managed-1",
                    "reason": "",
                }
            ],
        )


if __name__ == "__main__":
    unittest.main()
