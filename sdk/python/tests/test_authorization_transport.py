"""Transport-backed authorization SDK tests over a real Unix socket."""

from __future__ import annotations

import os
import tempfile
import unittest
from concurrent import futures
from typing import Any

import grpc

from gestalt import (
    ENV_AUTHORIZATION_SOCKET,
    Authorization,
    AuthorizationClient,
    Request,
)
from gestalt.gen.v1 import authorization_pb2 as _authorization_pb2
from gestalt.gen.v1 import authorization_pb2_grpc as _authorization_pb2_grpc

authorization_pb2: Any = _authorization_pb2
authorization_pb2_grpc: Any = _authorization_pb2_grpc

_server: grpc.Server | None = None
_socket_path: str = ""
_previous_socket_env: str | None = None
_search_requests: list[dict[str, Any]] = []


class _AuthorizationServicer(authorization_pb2_grpc.AuthorizationProviderServicer):
    def SearchSubjects(self, request, context):
        _search_requests.append(
            {
                "resource_type": request.resource.type,
                "resource_id": request.resource.id,
                "action": request.action.name,
                "subject_type": request.subject_type,
                "page_size": request.page_size,
            }
        )
        return authorization_pb2.SubjectSearchResponse(
            subjects=[
                authorization_pb2.Subject(
                    type="user",
                    id="user:usr_42",
                )
            ],
            model_id="model-123",
        )

    def GetMetadata(self, request, context):
        del request, context
        return authorization_pb2.AuthorizationMetadata(
            capabilities=["search_subjects"],
            active_model_id="model-123",
        )


def setUpModule() -> None:
    global _server, _socket_path, _previous_socket_env
    _socket_path = os.path.join(
        tempfile.gettempdir(), f"py-authorization-test-{os.getpid()}.sock"
    )
    if os.path.exists(_socket_path):
        os.remove(_socket_path)

    _server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))
    authorization_pb2_grpc.add_AuthorizationProviderServicer_to_server(
        _AuthorizationServicer(),
        _server,
    )
    _server.add_insecure_port(f"unix:{_socket_path}")
    _server.start()

    _previous_socket_env = os.environ.get(ENV_AUTHORIZATION_SOCKET)
    os.environ[ENV_AUTHORIZATION_SOCKET] = _socket_path


def tearDownModule() -> None:
    if _previous_socket_env is None:
        os.environ.pop(ENV_AUTHORIZATION_SOCKET, None)
    else:
        os.environ[ENV_AUTHORIZATION_SOCKET] = _previous_socket_env

    if _server is not None:
        _server.stop(grace=0).wait()
    if _socket_path and os.path.exists(_socket_path):
        os.remove(_socket_path)


class AuthorizationTransportTests(unittest.TestCase):
    def setUp(self) -> None:
        _search_requests.clear()

    def test_authorization_helper_roundtrip(self) -> None:
        client = Authorization()
        self.assertIs(client, Authorization())

        metadata = client.get_metadata()
        response = client.search_subjects(
            {
                "resource": {
                    "type": "slack_identity",
                    "id": "team:T123:user:U456",
                },
                "action": {"name": "assume"},
                "subject_type": "user",
                "page_size": 2,
            }
        )

        self.assertEqual(metadata.active_model_id, "model-123")
        self.assertEqual(list(metadata.capabilities), ["search_subjects"])
        self.assertEqual(len(response.subjects), 1)
        self.assertEqual(response.subjects[0].type, "user")
        self.assertEqual(response.subjects[0].id, "user:usr_42")
        self.assertEqual(
            _search_requests,
            [
                {
                    "resource_type": "slack_identity",
                    "resource_id": "team:T123:user:U456",
                    "action": "assume",
                    "subject_type": "user",
                    "page_size": 2,
                }
            ],
        )

    def test_request_authorization_helper_uses_shared_client(self) -> None:
        request = Request()
        self.assertIsInstance(request.authorization(), AuthorizationClient)
        self.assertIs(request.authorization(), Authorization())
