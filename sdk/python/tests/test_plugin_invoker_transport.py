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

from gestalt import ENV_PLUGIN_INVOKER_SOCKET, PluginInvoker, Request
from gestalt.gen.v1 import plugin_pb2 as _plugin_pb2
from gestalt.gen.v1 import plugin_pb2_grpc as _plugin_pb2_grpc

plugin_pb2: Any = _plugin_pb2
plugin_pb2_grpc: Any = _plugin_pb2_grpc

_server: grpc.Server | None = None
_socket_path: str = ""
_previous_socket_env: str | None = None


class _PluginInvokerServicer(plugin_pb2_grpc.PluginInvokerServicer):
    def Invoke(self, request, context):
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
                    "request_handle": request.request_handle,
                    "plugin": request.plugin,
                    "operation": request.operation,
                    "params": params,
                    "params_present": request.HasField("params"),
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
    def test_request_helper_roundtrip(self) -> None:
        request = Request(request_handle="req-123")

        with request.invoker() as client:
            response = client.invoke(
                "github",
                "get_issue",
                {"repo": "valon-technologies/gestalt", "issue_number": 1026},
                connection="work",
                instance="prod",
            )

        self.assertEqual(response.status, 200)
        self.assertEqual(
            json.loads(response.body),
            {
                "request_handle": "req-123",
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

    def test_request_handle_constructor_roundtrip(self) -> None:
        with PluginInvoker("req-456") as client:
            response = client.invoke("slack", "plain_text")

        self.assertEqual(response.status, 200)
        self.assertEqual(response.body, "plain response")

    def test_empty_dict_params_are_preserved_as_present(self) -> None:
        with PluginInvoker("req-789") as client:
            response = client.invoke("github", "get_issue", {})

        self.assertEqual(response.status, 200)
        self.assertEqual(
            json.loads(response.body),
            {
                "request_handle": "req-789",
                "plugin": "github",
                "operation": "get_issue",
                "params": {},
                "params_present": True,
                "connection": "",
                "instance": "",
            },
        )

    def test_whitespace_only_request_handle_is_rejected(self) -> None:
        with self.assertRaisesRegex(
            RuntimeError, "plugin invoker: request handle is not available"
        ):
            PluginInvoker("   ")
