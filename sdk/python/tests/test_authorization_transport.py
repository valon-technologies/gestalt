"""Transport-backed Authorization SDK tests over real sockets."""

from __future__ import annotations

import tempfile
import unittest
from concurrent import futures
from typing import Any

import grpc
from google.protobuf import empty_pb2 as _empty_pb2

from gestalt import (
    AuthorizationAction,
    AuthorizationClient,
    AuthorizationRelationshipTarget,
    AuthorizationResource,
    AuthorizationSubject,
    AuthorizationSubjectSet,
    EffectiveSubjectSearchRequest,
    ExpandRequest,
    Relationship,
    ResourceSearchRequest,
    WriteRelationshipsRequest,
    agent_session_editor_relationship,
)
from gestalt.testing import authorization_pb2, authorization_pb2_grpc

empty_pb2: Any = _empty_pb2


class _AuthorizationProvider(authorization_pb2_grpc.AuthorizationProviderServicer):
    def __init__(self) -> None:
        self.writes: list[Any] = []

    def EffectiveSearchResources(
        self,
        request: Any,
        context: grpc.ServicerContext,
    ) -> Any:
        return authorization_pb2.ResourceSearchResponse(
            resources=[
                authorization_pb2.Resource(type="agent_session", id="session-1"),
            ],
            model_id="authz-model-1",
        )

    def EffectiveSearchSubjects(
        self,
        request: Any,
        context: grpc.ServicerContext,
    ) -> Any:
        return authorization_pb2.EffectiveSubjectSearchResponse(
            targets=[
                authorization_pb2.RelationshipTarget(
                    subject_set=authorization_pb2.SubjectSet(
                        resource=authorization_pb2.Resource(
                            type="slack_channel",
                            id="C123",
                        ),
                        relation="member",
                    ),
                )
            ],
            model_id="authz-model-1",
            truncated=True,
        )

    def Expand(self, request: Any, context: grpc.ServicerContext) -> Any:
        return authorization_pb2.ExpandResponse(
            root=authorization_pb2.ExpandNode(
                target=authorization_pb2.RelationshipTarget(resource=request.resource),
                relation=request.relation,
            ),
            model_id="authz-model-1",
            max_depth_reached=True,
        )

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
                resource_response = client.effective_search_resources(
                    ResourceSearchRequest(
                        subject=AuthorizationSubject(
                            type="subject",
                            id="user:shared",
                        ),
                        action=AuthorizationAction(name="edit"),
                        resource_type="agent_session",
                    )
                )
                self.assertEqual(resource_response.resources[0].id, "session-1")

                subject_response = client.effective_search_subjects(
                    EffectiveSubjectSearchRequest(
                        resource=AuthorizationResource(
                            type="agent_session",
                            id="session-1",
                        ),
                        action=AuthorizationAction(name="edit"),
                    )
                )
                self.assertTrue(subject_response.truncated)
                self.assertEqual(
                    subject_response.targets[0].subject_set.relation,
                    "member",
                )

                expand_response = client.expand(
                    ExpandRequest(
                        resource=AuthorizationResource(
                            type="agent_session",
                            id="session-1",
                        ),
                        relation="editor",
                        max_depth=1,
                    )
                )
                self.assertTrue(expand_response.max_depth_reached)
                self.assertEqual(expand_response.root.target.resource.id, "session-1")

                client.write_relationships(
                    WriteRelationshipsRequest(
                        writes=[
                            Relationship(
                                target=AuthorizationRelationshipTarget(
                                    subject_set=AuthorizationSubjectSet(
                                        resource=AuthorizationResource(
                                            type="slack_channel",
                                            id="C123",
                                        ),
                                        relation="member",
                                    )
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
                client.grant_agent_session_editor("user:shared", "session-1")
                client.close()
            finally:
                server.stop(grace=0)

        self.assertEqual(len(provider.writes), 2)
        self.assertEqual(
            provider.writes[0].writes[0],
            authorization_pb2.Relationship(
                target=authorization_pb2.RelationshipTarget(
                    subject_set=authorization_pb2.SubjectSet(
                        resource=authorization_pb2.Resource(
                            type="slack_channel",
                            id="C123",
                        ),
                        relation="member",
                    )
                ),
                relation="editor",
                resource=authorization_pb2.Resource(
                    type="agent_session",
                    id="session-1",
                ),
            ),
        )

        self.assertEqual(
            provider.writes[1].writes[0],
            agent_session_editor_relationship("user:shared", "session-1"),
        )
        self.assertEqual(
            agent_session_editor_relationship("user:shared", "session-1"),
            authorization_pb2.Relationship(
                target=authorization_pb2.RelationshipTarget(
                    subject=authorization_pb2.Subject(
                        type="subject",
                        id="user:shared",
                    ),
                ),
                relation="editor",
                resource=authorization_pb2.Resource(
                    type="agent_session",
                    id="session-1",
                ),
            ),
        )


if __name__ == "__main__":
    unittest.main()
