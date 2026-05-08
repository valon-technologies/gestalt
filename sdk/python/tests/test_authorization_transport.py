"""Transport-backed Authorization SDK tests over real sockets."""

from __future__ import annotations

import tempfile
import unittest
from concurrent import futures
from typing import Any

import grpc
from google.protobuf import empty_pb2 as _empty_pb2

from gestalt import (
    AuthorizationClient,
    AuthorizationResource,
    AuthorizationSubject,
    Relationship,
    WriteRelationshipsRequest,
)
from gestalt.testing import authorization_pb2, authorization_pb2_grpc

empty_pb2: Any = _empty_pb2


class _AuthorizationProvider(authorization_pb2_grpc.AuthorizationProviderServicer):
    def __init__(self) -> None:
        self.writes: list[Any] = []

    def WriteRelationships(self, request: Any, context: grpc.ServicerContext) -> Any:
        self.writes.append(request)
        return empty_pb2.Empty()


class AuthorizationTransportTest(unittest.TestCase):
    def test_write_relationships_sends_request_over_transport(self) -> None:
        provider = _AuthorizationProvider()
        server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))
        authorization_pb2_grpc.add_AuthorizationProviderServicer_to_server(
            provider,
            server,
        )
        with tempfile.TemporaryDirectory() as tmpdir:
            socket_path = f"{tmpdir}/authorization.sock"
            server.add_insecure_port(f"unix:{socket_path}")
            server.start()
            try:
                client = AuthorizationClient(f"unix://{socket_path}")
                client.write_relationships(
                    WriteRelationshipsRequest(
                        writes=[
                            Relationship(
                                subject=AuthorizationSubject(
                                    type="subject",
                                    id="user:shared",
                                ),
                                relation="editor",
                                resource=AuthorizationResource(
                                    type="agent_session",
                                    id="session-1",
                                ),
                            )
                        ]
                    )
                )
                client.close()
            finally:
                server.stop(grace=0)

        self.assertEqual(len(provider.writes), 1)
        self.assertEqual(
            provider.writes[0].writes[0],
            authorization_pb2.Relationship(
                subject=authorization_pb2.Subject(type="subject", id="user:shared"),
                relation="editor",
                resource=authorization_pb2.Resource(
                    type="agent_session",
                    id="session-1",
                ),
            ),
        )


if __name__ == "__main__":
    unittest.main()
