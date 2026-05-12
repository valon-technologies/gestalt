"""Transport-backed Authorization SDK tests over real sockets."""

from __future__ import annotations

import tempfile
import unittest
from concurrent import futures
from typing import Any

import grpc
from google.protobuf import empty_pb2 as _empty_pb2

from gestalt import (
    AccessDecision,
    AccessEvaluationsResponse,
    ActionSearchResponse,
    AuthorizationAction,
    AuthorizationClient,
    AuthorizationMetadata,
    AuthorizationModel,
    AuthorizationModelAction,
    AuthorizationModelAllowedTarget,
    AuthorizationModelComputedUserset,
    AuthorizationModelRef,
    AuthorizationModelRelation,
    AuthorizationModelResourceType,
    AuthorizationModelRewrite,
    AuthorizationModelRewriteThis,
    AuthorizationModelRewriteUnion,
    AuthorizationModelSubjectSetTarget,
    AuthorizationModelTupleToUserset,
    AuthorizationProvider,
    AuthorizationRelationshipTarget,
    AuthorizationResource,
    AuthorizationSubject,
    AuthorizationSubjectSet,
    EffectiveSubjectSearchRequest,
    ExpandRequest,
    GetActiveModelResponse,
    ListModelsResponse,
    ReadRelationshipsResponse,
    Relationship,
    ResourceSearchRequest,
    ResourceSearchResponse,
    SubjectSearchResponse,
    WriteModelRequest,
    WriteRelationshipsRequest,
    _runtime,
)
from gestalt.testing import (
    authorization_pb2,
    authorization_pb2_grpc,
    runtime_pb2,
    runtime_pb2_grpc,
)

empty_pb2: Any = _empty_pb2


class _AuthorizationProvider(authorization_pb2_grpc.AuthorizationProviderServicer):
    def __init__(self) -> None:
        self.writes: list[Any] = []
        self.models: list[Any] = []

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

    def WriteModel(self, request: Any, context: grpc.ServicerContext) -> Any:
        self.models.append(request.model)
        return authorization_pb2.AuthorizationModelRef(id="model-2", version="2")


class _SDKAuthorizationProvider(AuthorizationProvider):
    def __init__(self) -> None:
        self.writes: list[Any] = []

    def evaluate(self, request: Any) -> Any:
        return AccessDecision(
            allowed=request.subject.id == "user:1",
            model_id="model-1",
        )

    def evaluate_many(self, request: Any) -> Any:
        return AccessEvaluationsResponse(
            decisions=[self.evaluate(item) for item in request.requests],
        )

    def search_resources(self, request: Any) -> Any:
        return ResourceSearchResponse(
            resources=[
                AuthorizationResource(
                    type=request.resource_type,
                    id="doc-1",
                )
            ],
            model_id="model-1",
        )

    def search_subjects(self, request: Any) -> Any:
        return SubjectSearchResponse(
            subjects=[
                AuthorizationSubject(
                    type=request.subject_type,
                    id="user:1",
                )
            ],
            model_id="model-1",
        )

    def search_actions(self, request: Any) -> Any:
        return ActionSearchResponse(
            actions=[AuthorizationAction(name="view")],
            model_id="model-1",
        )

    def get_metadata(self) -> Any:
        return AuthorizationMetadata(
            capabilities=["evaluate"],
            active_model_id="model-1",
        )

    def read_relationships(self, request: Any) -> Any:
        return ReadRelationshipsResponse(model_id="model-1")

    def write_relationships(self, request: Any) -> None:
        self.writes.append(request)

    def get_active_model(self) -> Any:
        return GetActiveModelResponse(
            model=AuthorizationModelRef(id="model-1", version="1"),
        )

    def list_models(self, request: Any) -> Any:
        return ListModelsResponse(
            models=[AuthorizationModelRef(id="model-1", version="1")],
        )

    def write_model(self, request: Any) -> Any:
        return AuthorizationModelRef(
            id="model-2",
            version=str(request.model.version),
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
            authorization_pb2.Relationship(
                subject=authorization_pb2.Subject(
                    type="subject",
                    id="user:shared",
                ),
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

    def test_write_model_sends_oneof_model_fields_over_transport(self) -> None:
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
                model_ref = client.write_model(
                    WriteModelRequest(
                        model=AuthorizationModel(
                            version=3,
                            resource_types=[
                                AuthorizationModelResourceType(
                                    name="document",
                                    relations=[
                                        AuthorizationModelRelation(
                                            name="viewer",
                                            allowed_targets=[
                                                AuthorizationModelAllowedTarget(
                                                    subject_type="user",
                                                ),
                                                AuthorizationModelAllowedTarget(
                                                    resource_type="team",
                                                ),
                                                AuthorizationModelAllowedTarget(
                                                    subject_set=AuthorizationModelSubjectSetTarget(
                                                        resource_type="team",
                                                        relation="member",
                                                    ),
                                                ),
                                            ],
                                            rewrite=AuthorizationModelRewrite(
                                                this=AuthorizationModelRewriteThis(),
                                            ),
                                        ),
                                    ],
                                    actions=[
                                        AuthorizationModelAction(
                                            name="view",
                                            rewrite=AuthorizationModelRewrite(
                                                computed_userset=AuthorizationModelComputedUserset(
                                                    relation="viewer",
                                                ),
                                            ),
                                        ),
                                        AuthorizationModelAction(
                                            name="share",
                                            rewrite=AuthorizationModelRewrite(
                                                tuple_to_userset=AuthorizationModelTupleToUserset(
                                                    tupleset_relation="parent",
                                                    computed_relation="owner",
                                                ),
                                            ),
                                        ),
                                        AuthorizationModelAction(
                                            name="admin",
                                            rewrite=AuthorizationModelRewrite(
                                                union=AuthorizationModelRewriteUnion(
                                                    children=[
                                                        AuthorizationModelRewrite(
                                                            this=AuthorizationModelRewriteThis(),
                                                        ),
                                                    ],
                                                ),
                                            ),
                                        ),
                                    ],
                                ),
                            ],
                        )
                    )
                )
                self.assertEqual(model_ref.version, "2")
                client.close()
            finally:
                server.stop(grace=0)

        self.assertEqual(
            provider.models,
            [
                authorization_pb2.AuthorizationModel(
                    version=3,
                    resource_types=[
                        authorization_pb2.AuthorizationModelResourceType(
                            name="document",
                            relations=[
                                authorization_pb2.AuthorizationModelRelation(
                                    name="viewer",
                                    allowed_targets=[
                                        authorization_pb2.AuthorizationModelAllowedTarget(
                                            subject_type="user",
                                        ),
                                        authorization_pb2.AuthorizationModelAllowedTarget(
                                            resource_type="team",
                                        ),
                                        authorization_pb2.AuthorizationModelAllowedTarget(
                                            subject_set=authorization_pb2.AuthorizationModelSubjectSetTarget(
                                                resource_type="team",
                                                relation="member",
                                            ),
                                        ),
                                    ],
                                    rewrite=authorization_pb2.AuthorizationModelRewrite(
                                        this=authorization_pb2.AuthorizationModelRewriteThis(),
                                    ),
                                ),
                            ],
                            actions=[
                                authorization_pb2.AuthorizationModelAction(
                                    name="view",
                                    rewrite=authorization_pb2.AuthorizationModelRewrite(
                                        computed_userset=authorization_pb2.AuthorizationModelComputedUserset(
                                            relation="viewer",
                                        ),
                                    ),
                                ),
                                authorization_pb2.AuthorizationModelAction(
                                    name="share",
                                    rewrite=authorization_pb2.AuthorizationModelRewrite(
                                        tuple_to_userset=authorization_pb2.AuthorizationModelTupleToUserset(
                                            tupleset_relation="parent",
                                            computed_relation="owner",
                                        ),
                                    ),
                                ),
                                authorization_pb2.AuthorizationModelAction(
                                    name="admin",
                                    rewrite=authorization_pb2.AuthorizationModelRewrite(
                                        union=authorization_pb2.AuthorizationModelRewriteUnion(
                                            children=[
                                                authorization_pb2.AuthorizationModelRewrite(
                                                    this=authorization_pb2.AuthorizationModelRewriteThis(),
                                                ),
                                            ],
                                        ),
                                    ),
                                ),
                            ],
                        ),
                    ],
                ),
            ],
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
                        resource=authorization_pb2.Resource(type="doc", id="doc-1"),
                    ),
                    timeout=5,
                )
                self.assertTrue(decision.allowed)

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

                metadata = stub.GetMetadata(empty_pb2.Empty(), timeout=5)
                self.assertEqual(list(metadata.capabilities), ["evaluate"])

                model_ref = stub.WriteModel(
                    authorization_pb2.WriteModelRequest(
                        model=authorization_pb2.AuthorizationModel(version=2),
                    ),
                    timeout=5,
                )
                self.assertEqual(model_ref.version, "2")

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
        provider = AuthorizationProvider()
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
