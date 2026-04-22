"""Transport-backed PluginInvoker SDK tests over a real Unix socket."""
from __future__ import annotations

import json
import os
import tempfile
import unittest
from concurrent import futures
from typing import Any

import grpc
from google.protobuf import json_format

from gestalt import (
    ENV_PLUGIN_INVOKER_SOCKET,
    ENV_PLUGIN_INVOKER_SOCKET_TOKEN,
    PluginInvoker,
    Request,
)
from gestalt.gen.v1 import plugin_pb2 as _plugin_pb2
from gestalt.gen.v1 import plugin_pb2_grpc as _plugin_pb2_grpc

plugin_pb2: Any = _plugin_pb2
plugin_pb2_grpc: Any = _plugin_pb2_grpc

_server: grpc.Server | None = None
_socket_path: str = ""
_previous_socket_env: str | None = None
_exchange_requests: list[dict[str, Any]] = []
_graphql_requests: list[dict[str, Any]] = []
_relay_tokens: list[str] = []


class _PluginInvokerServicer(plugin_pb2_grpc.PluginInvokerServicer):
    def ExchangeInvocationToken(self, request, context):
        _exchange_requests.append(
            {
                "parent_invocation_token": request.parent_invocation_token,
                "grants": [
                    {
                        "plugin": grant.plugin,
                        "operations": list(grant.operations),
                        "surfaces": list(grant.surfaces),
                        "all_operations": grant.all_operations,
                    }
                    for grant in request.grants
                ],
                "ttl_seconds": request.ttl_seconds,
            }
        )
        return plugin_pb2.ExchangeInvocationTokenResponse(
            invocation_token=f"{request.parent_invocation_token}:child"
        )

    def Invoke(self, request, context):
        _relay_tokens.extend(
            value
            for key, value in context.invocation_metadata()
            if key == "x-gestalt-host-service-relay-token"
        )
        if request.operation == "plain_text":
            return plugin_pb2.OperationResult(
                status=200,
                body="plain response",
            )

        params = (
            json_format.MessageToDict(
                request.params,
                preserving_proto_field_name=True,
            )
            if request.HasField("params")
            else {}
        )
        return plugin_pb2.OperationResult(
            status=200,
            body=json.dumps(
                {
                    "invocation_token": request.invocation_token,
                    "plugin": request.plugin,
                    "operation": request.operation,
                    "params": params,
                    "params_present": request.HasField("params"),
                    "connection": request.connection,
                    "instance": request.instance,
                }
            ),
        )

    def InvokeGraphQL(self, request, context):
        variables = (
            json_format.MessageToDict(
                request.variables,
                preserving_proto_field_name=True,
            )
            if request.HasField("variables")
            else {}
        )
        _graphql_requests.append(
            {
                "invocation_token": request.invocation_token,
                "plugin": request.plugin,
                "document": request.document,
                "variables": variables,
                "variables_present": request.HasField("variables"),
                "connection": request.connection,
                "instance": request.instance,
            }
        )
        return plugin_pb2.OperationResult(
            status=208,
            body=json.dumps(
                {
                    "invocation_token": request.invocation_token,
                    "plugin": request.plugin,
                    "document": request.document,
                    "variables": variables,
                    "variables_present": request.HasField("variables"),
                    "connection": request.connection,
                    "instance": request.instance,
                }
            ),
        )


def setUpModule() -> None:
    global _server, _socket_path, _previous_socket_env
    _socket_path = os.path.join(
        tempfile.gettempdir(), f"py-plugin-invoker-test-{os.getpid()}.sock"
    )
    if os.path.exists(_socket_path):
        os.remove(_socket_path)

    _server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))
    plugin_pb2_grpc.add_PluginInvokerServicer_to_server(
        _PluginInvokerServicer(),
        _server,
    )
    _server.add_insecure_port(f"unix:{_socket_path}")
    _server.start()
    _previous_socket_env = os.environ.get(ENV_PLUGIN_INVOKER_SOCKET)
    os.environ[ENV_PLUGIN_INVOKER_SOCKET] = _socket_path


def tearDownModule() -> None:
    if _previous_socket_env is None:
        os.environ.pop(ENV_PLUGIN_INVOKER_SOCKET, None)
    else:
        os.environ[ENV_PLUGIN_INVOKER_SOCKET] = _previous_socket_env
    if _server is not None:
        _server.stop(grace=0).wait()
    if _socket_path and os.path.exists(_socket_path):
        os.remove(_socket_path)


class PluginInvokerTransportTests(unittest.TestCase):
    def setUp(self) -> None:
        _exchange_requests.clear()
        _graphql_requests.clear()
        _relay_tokens.clear()

    def test_request_helper_roundtrip(self) -> None:
        request = Request(invocation_token="invoke-123")

        with request.invoker() as client:
            child_token = client.exchange_invocation_token(
                grants=[
                    {"plugin": "github", "operations": ["get_issue", " "]},
                    {"plugin": "linear", "surfaces": [" GraphQL ", " "]},
                    {"plugin": "google_sheets", "all_operations": True},
                    {"plugin": "   ", "operations": ["ignored"]},
                ],
                ttl_seconds=45,
            )
            response = client.invoke(
                "github",
                "get_issue",
                {"repo": "valon-technologies/gestalt", "issue_number": 1026},
                connection="work",
                instance="prod",
            )

        self.assertEqual(child_token, "invoke-123:child")
        self.assertEqual(
            _exchange_requests,
            [
                {
                    "parent_invocation_token": "invoke-123",
                    "grants": [
                        {
                            "plugin": "github",
                            "operations": ["get_issue"],
                            "surfaces": [],
                            "all_operations": False,
                        },
                        {
                            "plugin": "linear",
                            "operations": [],
                            "surfaces": ["graphql"],
                            "all_operations": False,
                        },
                        {
                            "plugin": "google_sheets",
                            "operations": [],
                            "surfaces": [],
                            "all_operations": True,
                        }
                    ],
                    "ttl_seconds": 45,
                }
            ],
        )
        self.assertEqual(response.status, 200)
        self.assertEqual(
            json.loads(response.body),
            {
                "invocation_token": "invoke-123",
                "plugin": "github",
                "operation": "get_issue",
                "params": {
                    "repo": "valon-technologies/gestalt",
                    "issue_number": 1026.0,
                },
                "params_present": True,
                "connection": "work",
                "instance": "prod",
            },
        )

    def test_invoke_graphql_roundtrip(self) -> None:
        with PluginInvoker("invoke-graphql") as client:
            response = client.invoke_graphql(
                "linear",
                "  query Viewer($team: String!) { viewer(team: $team) { id } }  ",
                {"team": "eng"},
                connection="workspace",
            )

        self.assertEqual(response.status, 208)
        self.assertEqual(
            json.loads(response.body),
            {
                "invocation_token": "invoke-graphql",
                "plugin": "linear",
                "document": "query Viewer($team: String!) { viewer(team: $team) { id } }",
                "variables": {
                    "team": "eng",
                },
                "variables_present": True,
                "connection": "workspace",
                "instance": "",
            },
        )
        self.assertEqual(
            _graphql_requests,
            [
                {
                    "invocation_token": "invoke-graphql",
                    "plugin": "linear",
                    "document": "query Viewer($team: String!) { viewer(team: $team) { id } }",
                    "variables": {
                        "team": "eng",
                    },
                    "variables_present": True,
                    "connection": "workspace",
                    "instance": "",
                }
            ],
        )

    def test_invocation_token_constructor_roundtrip(self) -> None:
        with PluginInvoker("invoke-456") as client:
            response = client.invoke("slack", "plain_text")

        self.assertEqual(response.status, 200)
        self.assertEqual(response.body, "plain response")

    def test_invoke_graphql_requires_nonempty_document(self) -> None:
        with PluginInvoker("invoke-graphql-empty") as client:
            with self.assertRaisesRegex(
                RuntimeError, "plugin invoker: graphql document is required"
            ):
                client.invoke_graphql("linear", "   ")

    def test_empty_dict_params_are_preserved_as_present(self) -> None:
        with PluginInvoker("invoke-789") as client:
            response = client.invoke("github", "get_issue", {})

        self.assertEqual(response.status, 200)
        self.assertEqual(
            json.loads(response.body),
            {
                "invocation_token": "invoke-789",
                "plugin": "github",
                "operation": "get_issue",
                "params": {},
                "params_present": True,
                "connection": "",
                "instance": "",
            },
        )

    def test_whitespace_only_invocation_token_is_rejected(self) -> None:
        with self.assertRaisesRegex(
            RuntimeError, "plugin invoker: invocation token is not available"
        ):
            PluginInvoker("   ")

    def test_tcp_target_token_env_is_forwarded(self) -> None:
        tcp_server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))
        plugin_pb2_grpc.add_PluginInvokerServicer_to_server(
            _PluginInvokerServicer(),
            tcp_server,
        )
        port = tcp_server.add_insecure_port("127.0.0.1:0")
        tcp_server.start()
        previous_socket = os.environ.get(ENV_PLUGIN_INVOKER_SOCKET)
        previous_token = os.environ.get(ENV_PLUGIN_INVOKER_SOCKET_TOKEN)
        os.environ[ENV_PLUGIN_INVOKER_SOCKET] = f"tcp://127.0.0.1:{port}"
        os.environ[ENV_PLUGIN_INVOKER_SOCKET_TOKEN] = "relay-token-python"
        try:
            with PluginInvoker("invoke-tcp") as client:
                response = client.invoke("github", "plain_text")

            self.assertEqual(response.status, 200)
            self.assertEqual(response.body, "plain response")
            self.assertEqual(_relay_tokens, ["relay-token-python"])
        finally:
            if previous_socket is None:
                os.environ.pop(ENV_PLUGIN_INVOKER_SOCKET, None)
            else:
                os.environ[ENV_PLUGIN_INVOKER_SOCKET] = previous_socket
            if previous_token is None:
                os.environ.pop(ENV_PLUGIN_INVOKER_SOCKET_TOKEN, None)
            else:
                os.environ[ENV_PLUGIN_INVOKER_SOCKET_TOKEN] = previous_token
            tcp_server.stop(grace=0).wait()
