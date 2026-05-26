"""Transport-backed Authorization SDK tests over real sockets."""

from __future__ import annotations

import tempfile
import unittest
from concurrent import futures
from typing import Any

import grpc
from google.protobuf import empty_pb2 as _empty_pb2

import gestalt
from gestalt import _runtime
from gestalt._gen.v1 import (
    authorization_pb2,
    authorization_pb2_grpc,
    runtime_pb2,
    runtime_pb2_grpc,
)

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


class _SDKAuthorizationProvider(gestalt.AuthorizationProvider):
    def __init__(self) -> None:
        self.requests: dict[str, Any] = {}
        self.writes: list[Any] = []

    def evaluate(
        self,
        request: gestalt.AccessEvaluationRequest,
    ) -> gestalt.AccessDecision:
        self.requests["evaluate"] = request
        return gestalt.AccessDecision(
            allowed=request.subject is not None and request.subject.id == "user:1",
            model_id="model-1",
        )

    def evaluate_many(
        self,
        request: gestalt.AccessEvaluationsRequest,
    ) -> gestalt.AccessEvaluationsResponse:
        self.requests["evaluate_many"] = request
        return gestalt.AccessEvaluationsResponse(
            decisions=[self.evaluate(item) for item in request.requests],
        )

    def search_resources(
        self,
        request: gestalt.ResourceSearchRequest,
    ) -> gestalt.ResourceSearchResponse:
        self.requests["search_resources"] = request
        return gestalt.ResourceSearchResponse(
            resources=[
                gestalt.AuthorizationResource(
                    type=request.resource_type,
                    id="doc-1",
                )
            ],
            model_id="model-1",
        )

    def search_subjects(
        self,
        request: gestalt.SubjectSearchRequest,
    ) -> gestalt.SubjectSearchResponse:
        self.requests["search_subjects"] = request
        return gestalt.SubjectSearchResponse(
            subjects=[
                gestalt.AuthorizationSubject(
                    type=request.subject_type,
                    id="user:1",
                )
            ],
            model_id="model-1",
        )

    def search_actions(
        self,
        request: gestalt.ActionSearchRequest,
    ) -> gestalt.ActionSearchResponse:
        self.requests["search_actions"] = request
        return gestalt.ActionSearchResponse(
            actions=[gestalt.AuthorizationAction(name="view")],
            model_id="model-1",
        )

    def get_metadata(self) -> gestalt.AuthorizationMetadata:
        return gestalt.AuthorizationMetadata(
            capabilities=["evaluate"],
            active_model_id="model-1",
        )

    def read_relationships(
        self,
        request: gestalt.ReadRelationshipsRequest,
    ) -> gestalt.ReadRelationshipsResponse:
        self.requests["read_relationships"] = request
        return gestalt.ReadRelationshipsResponse(model_id="model-1")

    def write_relationships(self, request: gestalt.WriteRelationshipsRequest) -> None:
        self.requests["write_relationships"] = request
        self.writes.append(request)

    def get_active_model(self) -> gestalt.GetActiveModelResponse:
        return gestalt.GetActiveModelResponse(
            model=gestalt.AuthorizationModelRef(id="model-1", version="1"),
        )

    def list_models(
        self,
        request: gestalt.ListModelsRequest,
    ) -> gestalt.ListModelsResponse:
        self.requests["list_models"] = request
        return gestalt.ListModelsResponse(
            models=[gestalt.AuthorizationModelRef(id="model-1", version="1")],
        )

    def write_model(
        self,
        request: gestalt.WriteModelRequest,
    ) -> gestalt.AuthorizationModelRef:
        self.requests["write_model"] = request
        return gestalt.AuthorizationModelRef(
            id="model-2",
            version=str(request.model.version if request.model is not None else 0),
        )


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
                client = gestalt.Authorization(f"unix://{socket_path}")
                resource_response = client.effective_search_resources(
                    gestalt.ResourceSearchRequest(
                        subject=gestalt.AuthorizationSubject(
                            type="subject",
                            id="user:shared",
                        ),
                        action=gestalt.AuthorizationAction(name="edit"),
                        resource_type="agent_session",
                    )
                )
                self.assertEqual(resource_response.resources[0].id, "session-1")

                subject_response = client.effective_search_subjects(
                    gestalt.EffectiveSubjectSearchRequest(
                        resource=gestalt.AuthorizationResource(
                            type="agent_session",
                            id="session-1",
                        ),
                        action=gestalt.AuthorizationAction(name="edit"),
                    )
                )
                self.assertTrue(subject_response.truncated)
                self.assertEqual(
                    subject_response.targets[0].subject_set,
                    gestalt.AuthorizationSubjectSet(
                        resource=gestalt.AuthorizationResource(
                            type="slack_channel",
                            id="C123",
                        ),
                        relation="member",
                    ),
                )

                expand_response = client.expand(
                    gestalt.ExpandRequest(
                        resource=gestalt.AuthorizationResource(
                            type="agent_session",
                            id="session-1",
                        ),
                        relation="editor",
                        max_depth=1,
                    )
                )
                self.assertTrue(expand_response.max_depth_reached)
                root = expand_response.root
                self.assertIsNotNone(root)
                self.assertIsNotNone(root.target)
                self.assertIsNotNone(root.target.resource)
                self.assertEqual(root.target.resource.id, "session-1")

                client.write_relationships(
                    gestalt.WriteRelationshipsRequest(
                        writes=[
                            gestalt.Relationship(
                                target=gestalt.AuthorizationRelationshipTarget(
                                    subject_set=gestalt.AuthorizationSubjectSet(
                                        resource=gestalt.AuthorizationResource(
                                            type="slack_channel",
                                            id="C123",
                                        ),
                                        relation="member",
                                    )
                                ),
                                relation="editor",
                                resource=gestalt.AuthorizationResource(
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

    def test_sdk_authorization_provider_serves_required_rpcs(self) -> None:
        provider = _SDKAuthorizationProvider()
        server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))
        _runtime._register_authorization_services(server, provider)
        with tempfile.TemporaryDirectory() as tmpdir:
            socket_path = f"{tmpdir}/authorization.sock"
            server.add_insecure_port(f"unix:{socket_path}")
            server.start()
            try:
                channel = grpc.insecure_channel(f"unix:{socket_path}")
                lifecycle = runtime_pb2_grpc.ProviderLifecycleStub(channel)
                identity = lifecycle.GetProviderIdentity(empty_pb2.Empty(), timeout=5)
                self.assertEqual(
                    identity.kind,
                    runtime_pb2.ProviderKind.PROVIDER_KIND_AUTHORIZATION,
                )

                stub = authorization_pb2_grpc.AuthorizationProviderStub(channel)
                decision = stub.Evaluate(
                    authorization_pb2.AccessEvaluationRequest(
                        subject=authorization_pb2.Subject(
                            type="subject",
                            id="user:1",
                        ),
                        action=authorization_pb2.Action(name="view"),
                        resource=authorization_pb2.Resource(
                            type="doc",
                            id="doc-1",
                        ),
                    ),
                    timeout=5,
                )
                self.assertTrue(decision.allowed)
                self.assertIsInstance(
                    provider.requests["evaluate"],
                    gestalt.AccessEvaluationRequest,
                )

                batch = stub.EvaluateMany(
                    authorization_pb2.AccessEvaluationsRequest(
                        requests=[
                            authorization_pb2.AccessEvaluationRequest(
                                subject=authorization_pb2.Subject(
                                    type="subject",
                                    id="user:1",
                                ),
                                action=authorization_pb2.Action(name="view"),
                                resource=authorization_pb2.Resource(
                                    type="doc",
                                    id="doc-1",
                                ),
                            )
                        ]
                    ),
                    timeout=5,
                )
                self.assertTrue(batch.decisions[0].allowed)
                self.assertIsInstance(
                    provider.requests["evaluate_many"],
                    gestalt.AccessEvaluationsRequest,
                )

                empty_batch = stub.EvaluateMany(
                    authorization_pb2.AccessEvaluationsRequest(),
                    timeout=5,
                )
                self.assertEqual(list(empty_batch.decisions), [])
                self.assertEqual(
                    provider.requests["evaluate_many"].requests,
                    (),
                )

                metadata = stub.GetMetadata(empty_pb2.Empty(), timeout=5)
                self.assertEqual(list(metadata.capabilities), ["evaluate"])

                subjects = stub.SearchSubjects(
                    authorization_pb2.SubjectSearchRequest(
                        resource=authorization_pb2.Resource(
                            type="doc",
                            id="doc-1",
                        ),
                        action=authorization_pb2.Action(name="view"),
                        subject_type="subject",
                    ),
                    timeout=5,
                )
                self.assertEqual(subjects.subjects[0].id, "user:1")
                self.assertIsInstance(
                    provider.requests["search_subjects"],
                    gestalt.SubjectSearchRequest,
                )

                model_ref = stub.WriteModel(
                    authorization_pb2.WriteModelRequest(
                        model=authorization_pb2.AuthorizationModel(version=2),
                    ),
                    timeout=5,
                )
                self.assertEqual(model_ref.version, "2")
                self.assertIsInstance(
                    provider.requests["write_model"],
                    gestalt.WriteModelRequest,
                )

                stub.WriteRelationships(
                    authorization_pb2.WriteRelationshipsRequest(),
                    timeout=5,
                )
                write_request = provider.requests["write_relationships"]
                self.assertEqual(write_request.writes, ())
                self.assertEqual(write_request.deletes, ())

                with self.assertRaises(grpc.RpcError) as failure:
                    stub.Expand(authorization_pb2.ExpandRequest(), timeout=5)
                rpc_error: Any = failure.exception
                self.assertEqual(
                    rpc_error.code(),
                    grpc.StatusCode.UNIMPLEMENTED,
                )
            finally:
                server.stop(grace=0)

    def test_sdk_authorization_provider_write_relationships_unimplemented(self) -> None:
        provider = gestalt.AuthorizationProvider()
        server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))
        _runtime._register_authorization_services(server, provider)
        with tempfile.TemporaryDirectory() as tmpdir:
            socket_path = f"{tmpdir}/authorization.sock"
            server.add_insecure_port(f"unix:{socket_path}")
            server.start()
            try:
                channel = grpc.insecure_channel(f"unix:{socket_path}")
                stub = authorization_pb2_grpc.AuthorizationProviderStub(channel)

                with self.assertRaises(grpc.RpcError) as failure:
                    stub.WriteRelationships(
                        authorization_pb2.WriteRelationshipsRequest(),
                        timeout=5,
                    )
                rpc_error: Any = failure.exception
                self.assertEqual(
                    rpc_error.code(),
                    grpc.StatusCode.UNIMPLEMENTED,
                )
                self.assertIn("write_relationships", rpc_error.details())
            finally:
                server.stop(grace=0)


if __name__ == "__main__":
    unittest.main()
