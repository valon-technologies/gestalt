"""Transport-backed Authorization SDK tests over a real Unix socket."""
from __future__ import annotations

import os
import tempfile
import unittest
from concurrent import futures
from typing import Any

import grpc

from gestalt import (
    ENV_AUTHORIZATION_SOCKET,
    ENV_AUTHORIZATION_SOCKET_TOKEN,
    Authorization,
    AuthorizationClient,
)
from gestalt.gen.v1 import authorization_pb2 as _authorization_pb2
from gestalt.gen.v1 import authorization_pb2_grpc as _authorization_pb2_grpc

authorization_pb2: Any = _authorization_pb2
authorization_pb2_grpc: Any = _authorization_pb2_grpc

_server: grpc.Server | None = None
_socket_path: str = ""
_previous_socket_env: str | None = None
_previous_socket_token_env: str | None = None
_relay_tokens: list[str] = []
_subject_search_requests: list[dict[str, Any]] = []


class _AuthorizationServicer(authorization_pb2_grpc.AuthorizationProviderServicer):
    def SearchSubjects(self, request, context):
        _capture_relay_tokens(context)
        _subject_search_requests.append(
            {
                "subject_type": request.subject_type,
                "resource_type": request.resource.type,
                "resource_id": request.resource.id,
                "action_name": request.action.name,
                "page_size": request.page_size,
            }
        )
        return authorization_pb2.SubjectSearchResponse(
            subjects=[
                authorization_pb2.Subject(
                    type="user",
                    id="user:user-123",
                )
            ],
            model_id="authz-model-1",
        )

    def GetMetadata(self, request, context):
        _capture_relay_tokens(context)
        return authorization_pb2.AuthorizationMetadata(
            capabilities=["search_subjects"],
            active_model_id="authz-model-1",
        )


def _capture_relay_tokens(context) -> None:
    _relay_tokens.extend(
        value
        for key, value in context.invocation_metadata()
        if key == "x-gestalt-host-service-relay-token"
    )


def setUpModule() -> None:
    global _server, _socket_path, _previous_socket_env, _previous_socket_token_env
    _socket_path = os.path.join(
        tempfile.gettempdir(),
        f"py-authorization-test-{os.getpid()}.sock",
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
    _previous_socket_token_env = os.environ.get(ENV_AUTHORIZATION_SOCKET_TOKEN)
    os.environ[ENV_AUTHORIZATION_SOCKET] = _socket_path


def tearDownModule() -> None:
    if _previous_socket_env is None:
        os.environ.pop(ENV_AUTHORIZATION_SOCKET, None)
    else:
        os.environ[ENV_AUTHORIZATION_SOCKET] = _previous_socket_env
    if _previous_socket_token_env is None:
        os.environ.pop(ENV_AUTHORIZATION_SOCKET_TOKEN, None)
    else:
        os.environ[ENV_AUTHORIZATION_SOCKET_TOKEN] = _previous_socket_token_env
    if _server is not None:
        _server.stop(grace=0).wait()
    if _socket_path and os.path.exists(_socket_path):
        os.remove(_socket_path)


class AuthorizationTransportTests(unittest.TestCase):
    def setUp(self) -> None:
        _relay_tokens.clear()
        _subject_search_requests.clear()

    def test_authorization_and_client_fail_fast_without_socket(self) -> None:
        previous = os.environ.pop(ENV_AUTHORIZATION_SOCKET, None)
        try:
            with self.assertRaisesRegex(RuntimeError, ENV_AUTHORIZATION_SOCKET):
                Authorization()
            with self.assertRaisesRegex(RuntimeError, ENV_AUTHORIZATION_SOCKET):
                AuthorizationClient()
        finally:
            if previous is not None:
                os.environ[ENV_AUTHORIZATION_SOCKET] = previous

    def test_authorization_roundtrip_and_client_caching(self) -> None:
        os.environ[ENV_AUTHORIZATION_SOCKET_TOKEN] = "relay-token-py"

        shared = Authorization()
        self.assertIs(shared, Authorization())
        metadata = shared.get_metadata()
        response = shared.search_subjects(
            {
                "subject_type": "user",
                "resource": {
                    "type": "slack_identity",
                    "id": "team:T123:user:U456",
                },
                "action": {
                    "name": "assume",
                },
                "page_size": 1,
            }
        )

        self.assertEqual(list(metadata.capabilities), ["search_subjects"])
        self.assertEqual(metadata.active_model_id, "authz-model-1")
        self.assertEqual(response.model_id, "authz-model-1")
        self.assertEqual(len(response.subjects), 1)
        self.assertEqual(response.subjects[0].id, "user:user-123")
        self.assertEqual(
            _subject_search_requests,
            [
                {
                    "subject_type": "user",
                    "resource_type": "slack_identity",
                    "resource_id": "team:T123:user:U456",
                    "action_name": "assume",
                    "page_size": 1,
                }
            ],
        )
        self.assertEqual(_relay_tokens, ["relay-token-py", "relay-token-py"])
