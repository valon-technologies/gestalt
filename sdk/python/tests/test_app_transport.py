"""Transport-backed App SDK tests over a real Unix socket."""

from __future__ import annotations

import json
import os
import tempfile
import unittest
from concurrent import futures
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any

import grpc
from google.protobuf import json_format

from gestalt import (
    ENV_HOST_SERVICE_SOCKET,
    ENV_HOST_SERVICE_TOKEN,
    Request,
)
from gestalt._app_access import _AppClient
from gestalt._gen.v1 import app_pb2 as _app_pb2
from gestalt._gen.v1 import app_pb2_grpc as _app_pb2_grpc

app_pb2: Any = _app_pb2
app_pb2_grpc: Any = _app_pb2_grpc


@dataclass
class IssueParams:
    repo: str
    issue_number: int


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
        request_context = (
            json_format.MessageToDict(
                request.context,
                preserving_proto_field_name=True,
            )
            if request.HasField("context")
            else {}
        )
        _invoke_contexts.append(request_context)
        return app_pb2.OperationResult(
            status=200,
            headers={
                "Location": app_pb2.StringList(
                    values=["https://example.test/created"]
                )
            },
            body=json.dumps(
                {
                    "app": request.app,
                    "operation": request.operation,
                    "params": params,
                    "params_present": request.HasField("params"),
                    "connection": request.connection,
                    "instance": request.instance,
                    "idempotency_key": request.idempotency_key,
                    "context": request_context,
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
            ),
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

        with request.app() as client:
            response = client.invoke(
                "github",
                "get_issue",
                IssueParams(repo="valon-technologies/gestalt", issue_number=1026),
                connection="work",
                instance="prod",
                idempotency_key=" issue-1026-create ",
            )

        self.assertEqual(response.status, 200)
        self.assertEqual(
            response.headers,
            {"Location": ["https://example.test/created"]},
        )
        self.assertEqual(
            json.loads(response.body),
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

    def test_invoke_rejects_non_json_dataclass_values(self) -> None:
        @dataclass
        class BadParams:
            created_at: datetime

        with _AppClient(Request()) as client:
            with self.assertRaisesRegex(TypeError, "timestamp helpers"):
                client.invoke(
                    "github",
                    "bad",
                    BadParams(created_at=datetime(2026, 5, 8, tzinfo=timezone.utc)),
                )

    def test_invoke_rejects_dataclass_types(self) -> None:
        with _AppClient(Request()) as client:
            with self.assertRaisesRegex(TypeError, "dataclass instance"):
                client.invoke("github", "bad", IssueParams)

    def test_invoke_graphql_roundtrip(self) -> None:
        context = app_pb2.RequestContext(
            subject=app_pb2.SubjectContext(id="user:graphql")
        )
        with _AppClient(Request(context=context)) as client:
            response = client.invoke_graphql(
                "linear",
                "  query Viewer($team: String!) { viewer(team: $team) { id } }  ",
                {"team": "eng"},
                connection="workspace",
                idempotency_key=" graphql-call-123 ",
            )

        self.assertEqual(response.status, 208)
        self.assertEqual(
            json.loads(response.body),
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
        with _AppClient(Request()) as client:
            response = client.invoke("slack", "plain_text")

        self.assertEqual(response.status, 200)
        self.assertEqual(response.body, "plain response")

    def test_invoke_graphql_requires_nonempty_document(self) -> None:
        with _AppClient(Request()) as client:
            with self.assertRaisesRegex(
                RuntimeError, "app: graphql document is required"
            ):
                client.invoke_graphql("linear", "   ")

    def test_empty_dict_params_are_preserved_as_present(self) -> None:
        with _AppClient(Request()) as client:
            response = client.invoke("github", "get_issue", {})

        self.assertEqual(response.status, 200)
        self.assertEqual(
            json.loads(response.body),
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

    def test_request_app_forwards_empty_context(self) -> None:
        request = Request(
            context=app_pb2.RequestContext(
                subject=app_pb2.SubjectContext(id="user:empty-context")
            )
        )

        with request.app() as client:
            response = client.invoke("github", "get_issue", {})

        self.assertEqual(response.status, 200)
        self.assertEqual(
            _invoke_contexts,
            [
                {
                    "subject": {"id": "user:empty-context"},
                }
            ],
        )

    def test_tcp_target_token_env_is_forwarded(self) -> None:
        tcp_server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))
        app_pb2_grpc.add_AppServicer_to_server(
            _AppServicer(),
            tcp_server,
        )
        port = tcp_server.add_insecure_port("127.0.0.1:0")
        tcp_server.start()
        previous_socket = os.environ.get(ENV_HOST_SERVICE_SOCKET)
        previous_token = os.environ.get(ENV_HOST_SERVICE_TOKEN)
        os.environ[ENV_HOST_SERVICE_SOCKET] = f"tcp://127.0.0.1:{port}"
        os.environ[ENV_HOST_SERVICE_TOKEN] = "relay-token-python"
        try:
            with _AppClient(Request()) as client:
                response = client.invoke("github", "plain_text")

            self.assertEqual(response.status, 200)
            self.assertEqual(response.body, "plain response")
            self.assertEqual(_relay_tokens, ["relay-token-python"])
        finally:
            if previous_socket is None:
                os.environ.pop(ENV_HOST_SERVICE_SOCKET, None)
            else:
                os.environ[ENV_HOST_SERVICE_SOCKET] = previous_socket
            if previous_token is None:
                os.environ.pop(ENV_HOST_SERVICE_TOKEN, None)
            else:
                os.environ[ENV_HOST_SERVICE_TOKEN] = previous_token
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
        previous_token = os.environ.get(ENV_HOST_SERVICE_TOKEN)
        previous_http_proxy = os.environ.get("http_proxy")
        previous_https_proxy = os.environ.get("https_proxy")
        os.environ[ENV_HOST_SERVICE_SOCKET] = f"tcp://127.0.0.1:{port}"
        os.environ[ENV_HOST_SERVICE_TOKEN] = "relay-token-python"
        os.environ["http_proxy"] = "http://127.0.0.1:1"
        os.environ["https_proxy"] = "http://127.0.0.1:1"
        try:
            with _AppClient(Request()) as client:
                response = client.invoke("github", "plain_text")

            self.assertEqual(response.status, 200)
            self.assertEqual(response.body, "plain response")
            self.assertEqual(_relay_tokens, ["relay-token-python"])
        finally:
            if previous_socket is None:
                os.environ.pop(ENV_HOST_SERVICE_SOCKET, None)
            else:
                os.environ[ENV_HOST_SERVICE_SOCKET] = previous_socket
            if previous_token is None:
                os.environ.pop(ENV_HOST_SERVICE_TOKEN, None)
            else:
                os.environ[ENV_HOST_SERVICE_TOKEN] = previous_token
            if previous_http_proxy is None:
                os.environ.pop("http_proxy", None)
            else:
                os.environ["http_proxy"] = previous_http_proxy
            if previous_https_proxy is None:
                os.environ.pop("https_proxy", None)
            else:
                os.environ["https_proxy"] = previous_https_proxy
            tcp_server.stop(grace=0).wait()
