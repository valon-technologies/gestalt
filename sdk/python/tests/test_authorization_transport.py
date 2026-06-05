"""Transport-backed Authorization SDK tests over a real Unix socket."""

from __future__ import annotations

import os
import tempfile
import unittest
from concurrent import futures
from typing import Any

import grpc

from gestalt import ENV_HOST_SERVICE_SOCKET, Request
from gestalt._authorization import SetAuthorizationStateRequest
from gestalt._gen.v1 import authorization_pb2 as _authorization_pb2
from gestalt._gen.v1 import authorization_pb2_grpc as _authorization_pb2_grpc

authorization_pb2: Any = _authorization_pb2
authorization_pb2_grpc: Any = _authorization_pb2_grpc

_server: grpc.Server | None = None
_socket_path = ""
_previous_socket_env: str | None = None
_invocation_tokens: list[str] = []


class _AuthorizationServicer(authorization_pb2_grpc.AuthorizationProviderServicer):
    def SetAuthorizationState(self, request, context):
        _invocation_tokens.extend(
            value
            for key, value in context.invocation_metadata()
            if key == "x-gestalt-invocation-token"
        )
        return authorization_pb2.SetAuthorizationStateResponse()


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


class AuthorizationTransportTests(unittest.TestCase):
    def setUp(self) -> None:
        _invocation_tokens.clear()

    def test_request_helper_forwards_invocation_token(self) -> None:
        request = Request(invocation_token="invoke-authz")

        with request.authorization() as client:
            client.set_authorization_state(SetAuthorizationStateRequest())

        self.assertEqual(_invocation_tokens, ["invoke-authz"])


if __name__ == "__main__":
    unittest.main()
