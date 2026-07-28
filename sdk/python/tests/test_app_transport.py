"""Transport-backed App SDK tests over a real Unix socket."""

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
    ENV_HOST_SERVICE_SOCKET,
    InvokeError,
    Request,
)
from gestalt._gen.v1 import app_pb2 as _app_pb2
from gestalt._gen.v1 import app_pb2_grpc as _app_pb2_grpc
from gestalt.app import (
    App,
    AppInvokeRequest,
    OperationResult,
    RequestContext,
    StringList,
    SubjectContext,
)
from gestalt.rpc_support import GestaltErrorCode

app_pb2: Any = _app_pb2
app_pb2_grpc: Any = _app_pb2_grpc

_server: grpc.Server | None = None
_socket_path: str = ""
_previous_socket_env: str | None = None
_graphql_requests: list[dict[str, Any]] = []
_invoke_contexts: list[dict[str, Any]] = []
_relay_tokens: list[str] = []


class _AppServicer(app_pb2_grpc.AppServicer):
    def Invoke(self, request, context):
        _relay_tokens.extend(
            value
            for key, value in context.invocation_metadata()
            if key == "x-gestalt-host-service-relay-token"
        )
        if request.operation == "plain_text":
            return app_pb2.OperationResult(
                status=200,
                body=b"plain response",
            )
        if request.operation == "error_envelope":
            return app_pb2.OperationResult(
                status=200,
                body=json.dumps(
                    {
                        "status": "error",
                        "error": {
                            "message": "missing credential",
                            "code": "missing_credential",
                        },
                    }
                ).encode("utf-8"),
            )

        params = (
            json_format.MessageToDict(
                request.params,
                preserving_proto_field_name=True,
            )
            if request.HasField("params")
            else {}
        )
        request_context = (
            json_format.MessageToDict(
                request.context,
                preserving_proto_field_name=True,
            )
            if request.HasField("context")
            else {}
        )
        _invoke_contexts.append(request_context)
        body = {
            "app": request.app,
            "operation": request.operation,
            "params": params,
            "params_present": request.HasField("params"),
            "connection": request.connection,
            "instance": request.instance,
            "idempotency_key": request.idempotency_key,
            "context": request_context,
        }
        if request.credential_mode:
            body["credential_mode"] = request.credential_mode
        return app_pb2.OperationResult(
            status=200,
            headers={
                "Location": app_pb2.StringList(
                    values=["https://example.test/created"]
                )
            },
            body=json.dumps(body).encode("utf-8"),
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
        request_context = (
            json_format.MessageToDict(
                request.context,
                preserving_proto_field_name=True,
            )
            if request.HasField("context")
            else {}
        )
        _graphql_requests.append(
            {
                "app": request.app,
                "document": request.document,
                "variables": variables,
                "variables_present": request.HasField("variables"),
                "connection": request.connection,
                "instance": request.instance,
                "idempotency_key": request.idempotency_key,
                "context": request_context,
            }
        )
        return app_pb2.OperationResult(
            status=208,
            body=json.dumps(
                {
                    "app": request.app,
                    "document": request.document,
                    "variables": variables,
                    "variables_present": request.HasField("variables"),
                    "connection": request.connection,
                    "instance": request.instance,
                    "idempotency_key": request.idempotency_key,
                    "context": request_context,
                }
            ).encode("utf-8"),
        )


def setUpModule() -> None:
    global _server, _socket_path, _previous_socket_env
    _socket_path = os.path.join(
        tempfile.gettempdir(), f"py-plugin-app-test-{os.getpid()}.sock"
    )
    if os.path.exists(_socket_path):
        os.remove(_socket_path)

    _server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))
    app_pb2_grpc.add_AppServicer_to_server(
        _AppServicer(),
        _server,
    )
    _server.add_insecure_port(f"unix:{_socket_path}")
    _server.start()
    _previous_socket_env = os.environ.get(ENV_HOST_SERVICE_SOCKET)
    os.environ[ENV_HOST_SERVICE_SOCKET] = _socket_path


def tearDownModule() -> None:
    if _previous_socket_env is None:
        os.environ.pop(ENV_HOST_SERVICE_SOCKET, None)
    else:
        os.environ[ENV_HOST_SERVICE_SOCKET] = _previous_socket_env
    if _server is not None:
        _server.stop(grace=0).wait()
    if _socket_path and os.path.exists(_socket_path):
        os.remove(_socket_path)


def _decode_json(result: OperationResult) -> Any:
    return json.loads(result.body)


class AppTransportTests(unittest.TestCase):
    def setUp(self) -> None:
        _graphql_requests.clear()
        _invoke_contexts.clear()
        _relay_tokens.clear()

    def test_request_helper_forwards_context(self) -> None:
        context = app_pb2.RequestContext(
            subject=app_pb2.SubjectContext(
                id="user:ada",
                email="ada@example.com",
            ),
        )
        context.workflow.update(
            {
                "runId": "run-python-app",
                "runAs": {"id": "service_account:workflow-test"},
            }
        )
        request = Request(context=context)

        response = request.app().invoke_raw(
            AppInvokeRequest(
                app="github",
                operation="get_issue",
                params={
                    "repo": "valon-technologies/gestalt",
                    "issue_number": 1026,
                },
                connection="work",
                instance="prod",
                idempotency_key="issue-1026-create",
                credential_mode="subject",
            )
        )

        self.assertEqual(response.status, 200)
        self.assertEqual(
            response.headers,
            {"Location": StringList(values=["https://example.test/created"])},
        )
        self.assertEqual(
            _decode_json(response),
            {
                "app": "github",
                "operation": "get_issue",
                "params": {
                    "repo": "valon-technologies/gestalt",
                    "issue_number": 1026.0,
                },
                "params_present": True,
                "connection": "work",
                "instance": "prod",
                "idempotency_key": "issue-1026-create",
                "credential_mode": "subject",
                "context": {
                    "subject": {
                        "id": "user:ada",
                        "email": "ada@example.com",
                    },
                    "workflow": {
                        "runId": "run-python-app",
                        "runAs": {"id": "service_account:workflow-test"},
                    },
                },
            },
        )

    def test_invoke_graphql_roundtrip(self) -> None:
        client = App.connect(
            context=RequestContext(subject=SubjectContext(id="user:graphql"))
        )
        response = client.invoke_graphql(
            app="linear",
            document="query Viewer($team: String!) { viewer(team: $team) { id } }",
            variables={"team": "eng"},
            connection="workspace",
            idempotency_key="graphql-call-123",
        )

        self.assertEqual(response.status, 208)
        self.assertEqual(
            _decode_json(response),
            {
                "app": "linear",
                "document": "query Viewer($team: String!) { viewer(team: $team) { id } }",
                "variables": {
                    "team": "eng",
                },
                "variables_present": True,
                "connection": "workspace",
                "instance": "",
                "idempotency_key": "graphql-call-123",
                "context": {
                    "subject": {"id": "user:graphql"},
                },
            },
        )
        self.assertEqual(
            _graphql_requests,
            [
                {
                    "app": "linear",
                    "document": "query Viewer($team: String!) { viewer(team: $team) { id } }",
                    "variables": {
                        "team": "eng",
                    },
                    "variables_present": True,
                    "connection": "workspace",
                    "instance": "",
                    "idempotency_key": "graphql-call-123",
                    "context": {
                        "subject": {"id": "user:graphql"},
                    },
                }
            ],
        )

    def test_direct_client_roundtrip(self) -> None:
        client = App.connect()
        response = client.invoke_raw(AppInvokeRequest(app="slack", operation="plain_text"))

        self.assertEqual(response.status, 200)
        self.assertEqual(response.body, b"plain response")

    def test_invoke_decodes_json_result(self) -> None:
        client = App.connect()
        decoded = client.invoke(app="github", operation="get_issue", params={})

        self.assertEqual(
            decoded,
            {
                "app": "github",
                "operation": "get_issue",
                "params": {},
                "params_present": True,
                "connection": "",
                "instance": "",
                "idempotency_key": "",
                "context": {},
            },
        )

    def test_invoke_request_object_decodes_json_result(self) -> None:
        client = App.connect()
        decoded = client.invoke(AppInvokeRequest(app="github", operation="get_issue"))

        self.assertEqual(decoded["app"], "github")
        self.assertEqual(decoded["operation"], "get_issue")
        self.assertFalse(decoded["params_present"])

    def test_invoke_raises_on_error_envelope(self) -> None:
        client = App.connect()
        with self.assertRaises(InvokeError) as error:
            client.invoke(app="github", operation="error_envelope")

        self.assertEqual(error.exception.app, "github")
        self.assertEqual(error.exception.operation, "error_envelope")
        self.assertEqual(error.exception.reason, "missing_credential")
        self.assertEqual(error.exception.code, GestaltErrorCode.UNKNOWN)
        self.assertEqual(str(error.exception), "missing credential")

    def test_request_app_forwards_empty_context(self) -> None:
        request = Request(
            context=app_pb2.RequestContext(
                subject=app_pb2.SubjectContext(id="user:empty-context")
            )
        )

        decoded = request.app().invoke(
            app="github", operation="get_issue", params={}
        )

        self.assertEqual(decoded["operation"], "get_issue")
        self.assertEqual(
            _invoke_contexts,
            [
                {
                    "subject": {"id": "user:empty-context"},
                }
            ],
        )

    def test_tcp_target_forwards_relay_token(self) -> None:
        tcp_server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))
        app_pb2_grpc.add_AppServicer_to_server(
            _AppServicer(),
            tcp_server,
        )
        port = tcp_server.add_insecure_port("127.0.0.1:0")
        tcp_server.start()
        previous_socket = os.environ.get(ENV_HOST_SERVICE_SOCKET)
        os.environ[ENV_HOST_SERVICE_SOCKET] = f"tcp://127.0.0.1:{port}"
        try:
            client = App.connect(relay_token="relay-token-python")
            response = client.invoke_raw(
                AppInvokeRequest(app="github", operation="plain_text")
            )

            self.assertEqual(response.status, 200)
            self.assertEqual(response.body, b"plain response")
            self.assertEqual(_relay_tokens, ["relay-token-python"])
        finally:
            if previous_socket is None:
                os.environ.pop(ENV_HOST_SERVICE_SOCKET, None)
            else:
                os.environ[ENV_HOST_SERVICE_SOCKET] = previous_socket
            tcp_server.stop(grace=0).wait()

    def test_tcp_target_ignores_proxy_env(self) -> None:
        tcp_server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))
        app_pb2_grpc.add_AppServicer_to_server(
            _AppServicer(),
            tcp_server,
        )
        port = tcp_server.add_insecure_port("127.0.0.1:0")
        tcp_server.start()
        previous_socket = os.environ.get(ENV_HOST_SERVICE_SOCKET)
        previous_http_proxy = os.environ.get("http_proxy")
        previous_https_proxy = os.environ.get("https_proxy")
        os.environ[ENV_HOST_SERVICE_SOCKET] = f"tcp://127.0.0.1:{port}"
        os.environ["http_proxy"] = "http://127.0.0.1:1"
        os.environ["https_proxy"] = "http://127.0.0.1:1"
        try:
            client = App.connect(relay_token="relay-token-python")
            response = client.invoke_raw(
                AppInvokeRequest(app="github", operation="plain_text")
            )

            self.assertEqual(response.status, 200)
            self.assertEqual(response.body, b"plain response")
            self.assertEqual(_relay_tokens, ["relay-token-python"])
        finally:
            if previous_socket is None:
                os.environ.pop(ENV_HOST_SERVICE_SOCKET, None)
            else:
                os.environ[ENV_HOST_SERVICE_SOCKET] = previous_socket
            if previous_http_proxy is None:
                os.environ.pop("http_proxy", None)
            else:
                os.environ["http_proxy"] = previous_http_proxy
            if previous_https_proxy is None:
                os.environ.pop("https_proxy", None)
            else:
                os.environ["https_proxy"] = previous_https_proxy
            tcp_server.stop(grace=0).wait()


if __name__ == "__main__":
    unittest.main()
