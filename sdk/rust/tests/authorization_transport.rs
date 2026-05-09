#[allow(dead_code)]
mod helpers;

use std::path::Path;
use std::sync::{Arc, Mutex};

use gestalt::proto::v1::authorization_provider_server::{
    AuthorizationProvider as ProtoAuthorizationProvider, AuthorizationProviderServer,
};
use gestalt::proto::v1::{
    AccessDecision as ProtoAccessDecision, AccessEvaluationRequest as ProtoAccessEvaluationRequest,
    AccessEvaluationsRequest as ProtoAccessEvaluationsRequest,
    AccessEvaluationsResponse as ProtoAccessEvaluationsResponse, Action as ProtoAction,
    ActionSearchRequest as ProtoActionSearchRequest,
    ActionSearchResponse as ProtoActionSearchResponse,
    AuthorizationMetadata as ProtoAuthorizationMetadata,
    AuthorizationModelRef as ProtoAuthorizationModelRef,
    EffectiveSubjectSearchRequest as ProtoEffectiveSubjectSearchRequest,
    EffectiveSubjectSearchResponse as ProtoEffectiveSubjectSearchResponse,
    ExpandNode as ProtoExpandNode, ExpandRequest as ProtoExpandRequest,
    ExpandResponse as ProtoExpandResponse, GetActiveModelResponse as ProtoGetActiveModelResponse,
    ListModelsRequest as ProtoListModelsRequest, ListModelsResponse as ProtoListModelsResponse,
    ReadRelationshipsRequest as ProtoReadRelationshipsRequest,
    ReadRelationshipsResponse as ProtoReadRelationshipsResponse,
    RelationshipTarget as ProtoRelationshipTarget, Resource as ProtoResource,
    ResourceSearchRequest as ProtoResourceSearchRequest,
    ResourceSearchResponse as ProtoResourceSearchResponse, Subject as ProtoSubject,
    SubjectSearchRequest as ProtoSubjectSearchRequest,
    SubjectSearchResponse as ProtoSubjectSearchResponse, SubjectSet as ProtoSubjectSet,
    WriteModelRequest as ProtoWriteModelRequest,
    WriteRelationshipsRequest as ProtoWriteRelationshipsRequest, relationship_target,
};
use gestalt::{
    Authorization, AuthorizationAction, AuthorizationRelationshipTarget, AuthorizationResource,
    AuthorizationSubjectSet, ENV_AUTHORIZATION_SOCKET, ENV_AUTHORIZATION_SOCKET_TOKEN,
    EffectiveSubjectSearchRequest, ExpandRequest, Relationship, ResourceSearchRequest,
    SubjectSearchRequest, WriteRelationshipsRequest,
};
use tokio::net::UnixListener;
use tokio_stream::wrappers::UnixListenerStream;
use tonic::transport::Server;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

#[derive(Clone, Default)]
struct TestAuthorizationServer {
    seen_tokens: Arc<Mutex<Vec<String>>>,
    searches: Arc<Mutex<Vec<ProtoSubjectSearchRequest>>>,
    writes: Arc<Mutex<Vec<ProtoWriteRelationshipsRequest>>>,
}

#[gestalt::async_trait]
impl ProtoAuthorizationProvider for TestAuthorizationServer {
    async fn evaluate(
        &self,
        _request: GrpcRequest<ProtoAccessEvaluationRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoAccessDecision>, Status> {
        Ok(GrpcResponse::new(ProtoAccessDecision {
            allowed: true,
            context: None,
            model_id: "authz-model-1".to_string(),
        }))
    }

    async fn evaluate_many(
        &self,
        request: GrpcRequest<ProtoAccessEvaluationsRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoAccessEvaluationsResponse>, Status> {
        let decisions = request
            .into_inner()
            .requests
            .into_iter()
            .map(|_| ProtoAccessDecision {
                allowed: true,
                context: None,
                model_id: "authz-model-1".to_string(),
            })
            .collect();
        Ok(GrpcResponse::new(ProtoAccessEvaluationsResponse {
            decisions,
        }))
    }

    async fn search_resources(
        &self,
        _request: GrpcRequest<ProtoResourceSearchRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoResourceSearchResponse>, Status> {
        Ok(GrpcResponse::new(ProtoResourceSearchResponse {
            resources: vec![ProtoResource {
                r#type: "agent_session".to_string(),
                id: "session-123".to_string(),
                properties: None,
            }],
            next_page_token: String::new(),
            model_id: "authz-model-1".to_string(),
        }))
    }

    async fn search_subjects(
        &self,
        request: GrpcRequest<ProtoSubjectSearchRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoSubjectSearchResponse>, Status> {
        maybe_record_relay_token(&self.seen_tokens, request.metadata());
        self.searches
            .lock()
            .expect("searches lock")
            .push(request.into_inner());
        Ok(GrpcResponse::new(ProtoSubjectSearchResponse {
            subjects: vec![ProtoSubject {
                r#type: "subject".to_string(),
                id: "user:user-123".to_string(),
                properties: None,
            }],
            next_page_token: String::new(),
            model_id: "authz-model-1".to_string(),
        }))
    }

    async fn effective_search_resources(
        &self,
        request: GrpcRequest<ProtoResourceSearchRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoResourceSearchResponse>, Status> {
        maybe_record_relay_token(&self.seen_tokens, request.metadata());
        Ok(GrpcResponse::new(ProtoResourceSearchResponse {
            resources: vec![ProtoResource {
                r#type: "agent_session".to_string(),
                id: "session-123".to_string(),
                properties: None,
            }],
            next_page_token: String::new(),
            model_id: "authz-model-1".to_string(),
        }))
    }

    async fn effective_search_subjects(
        &self,
        request: GrpcRequest<ProtoEffectiveSubjectSearchRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoEffectiveSubjectSearchResponse>, Status> {
        maybe_record_relay_token(&self.seen_tokens, request.metadata());
        Ok(GrpcResponse::new(ProtoEffectiveSubjectSearchResponse {
            targets: vec![ProtoRelationshipTarget {
                kind: Some(relationship_target::Kind::SubjectSet(ProtoSubjectSet {
                    resource: Some(ProtoResource {
                        r#type: "slack_channel".to_string(),
                        id: "C123".to_string(),
                        properties: None,
                    }),
                    relation: "member".to_string(),
                })),
            }],
            next_page_token: String::new(),
            model_id: "authz-model-1".to_string(),
            truncated: true,
        }))
    }

    async fn search_actions(
        &self,
        _request: GrpcRequest<ProtoActionSearchRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoActionSearchResponse>, Status> {
        Ok(GrpcResponse::new(ProtoActionSearchResponse {
            actions: vec![ProtoAction {
                name: "edit".to_string(),
                properties: None,
            }],
            next_page_token: String::new(),
            model_id: "authz-model-1".to_string(),
        }))
    }

    async fn expand(
        &self,
        request: GrpcRequest<ProtoExpandRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoExpandResponse>, Status> {
        maybe_record_relay_token(&self.seen_tokens, request.metadata());
        let request = request.into_inner();
        Ok(GrpcResponse::new(ProtoExpandResponse {
            root: Some(ProtoExpandNode {
                target: Some(ProtoRelationshipTarget {
                    kind: request.resource.map(relationship_target::Kind::Resource),
                }),
                relation: request.relation,
                children: Vec::new(),
            }),
            truncated: false,
            cycle_detected: false,
            max_depth_reached: true,
            model_id: "authz-model-1".to_string(),
        }))
    }

    async fn get_metadata(
        &self,
        _request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<ProtoAuthorizationMetadata>, Status> {
        Ok(GrpcResponse::new(ProtoAuthorizationMetadata {
            capabilities: vec![
                "search_subjects".to_string(),
                "write_relationships".to_string(),
            ],
            active_model_id: "authz-model-1".to_string(),
        }))
    }

    async fn read_relationships(
        &self,
        _request: GrpcRequest<ProtoReadRelationshipsRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoReadRelationshipsResponse>, Status> {
        Ok(GrpcResponse::new(ProtoReadRelationshipsResponse {
            relationships: Vec::new(),
            next_page_token: String::new(),
            model_id: "authz-model-1".to_string(),
        }))
    }

    async fn write_relationships(
        &self,
        request: GrpcRequest<ProtoWriteRelationshipsRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        maybe_record_relay_token(&self.seen_tokens, request.metadata());
        self.writes
            .lock()
            .expect("writes lock")
            .push(request.into_inner());
        Ok(GrpcResponse::new(()))
    }

    async fn get_active_model(
        &self,
        _request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<ProtoGetActiveModelResponse>, Status> {
        Ok(GrpcResponse::new(ProtoGetActiveModelResponse {
            model: Some(ProtoAuthorizationModelRef {
                id: "authz-model-1".to_string(),
                version: "1".to_string(),
                created_at: None,
            }),
        }))
    }

    async fn list_models(
        &self,
        _request: GrpcRequest<ProtoListModelsRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoListModelsResponse>, Status> {
        Ok(GrpcResponse::new(ProtoListModelsResponse {
            models: Vec::new(),
            next_page_token: String::new(),
        }))
    }

    async fn write_model(
        &self,
        _request: GrpcRequest<ProtoWriteModelRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoAuthorizationModelRef>, Status> {
        Ok(GrpcResponse::new(ProtoAuthorizationModelRef {
            id: "authz-model-1".to_string(),
            version: "1".to_string(),
            created_at: None,
        }))
    }
}

#[tokio::test]
async fn authorization_client_uses_public_sdk_types_for_search_and_writes() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("authorization.sock");
    let server = TestAuthorizationServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task =
        tokio::spawn(async move { serve_authorization(serve_server, &serve_socket).await });
    helpers::wait_for_socket(&socket).await;

    let _socket_guard = helpers::EnvGuard::set(ENV_AUTHORIZATION_SOCKET, &socket);
    let _token_guard = helpers::EnvGuard::set(ENV_AUTHORIZATION_SOCKET_TOKEN, "relay-token-rust");

    let mut authorization = Authorization::connect()
        .await
        .expect("connect authorization");
    let metadata = authorization.get_metadata().await.expect("metadata");
    assert_eq!(
        metadata.capabilities,
        vec![
            "search_subjects".to_string(),
            "write_relationships".to_string()
        ]
    );

    let subjects = authorization
        .search_subjects(SubjectSearchRequest {
            resource: AuthorizationResource::new("slack_identity", "team:T123:user:U456"),
            action: AuthorizationAction::new("assume"),
            subject_type: "subject".to_string(),
            page_size: 1,
            ..Default::default()
        })
        .await
        .expect("search subjects");
    assert_eq!(subjects.model_id, "authz-model-1");
    assert_eq!(subjects.subjects[0].id, "user:user-123");

    let resources = authorization
        .effective_search_resources(ResourceSearchRequest {
            subject: gestalt::AuthorizationSubject::new("subject", "user:user-123"),
            action: AuthorizationAction::new("edit"),
            resource_type: "agent_session".to_string(),
            ..Default::default()
        })
        .await
        .expect("effective search resources");
    assert_eq!(resources.resources[0].id, "session-123");

    let targets = authorization
        .effective_search_subjects(EffectiveSubjectSearchRequest {
            resource: AuthorizationResource::new("agent_session", "session-123"),
            action: AuthorizationAction::new("edit"),
            ..Default::default()
        })
        .await
        .expect("effective search subjects");
    assert!(targets.truncated);
    assert!(matches!(
        &targets.targets[0],
        AuthorizationRelationshipTarget::SubjectSet(subject_set)
            if subject_set.resource.r#type == "slack_channel" && subject_set.relation == "member"
    ));

    let expanded = authorization
        .expand(ExpandRequest {
            resource: AuthorizationResource::new("agent_session", "session-123"),
            relation: "editor".to_string(),
            max_depth: 1,
            ..Default::default()
        })
        .await
        .expect("expand");
    assert!(expanded.max_depth_reached);
    assert!(matches!(
        expanded.root.and_then(|root| root.target),
        Some(AuthorizationRelationshipTarget::Resource(resource)) if resource.id == "session-123"
    ));

    authorization
        .write_relationships(WriteRelationshipsRequest::writes([
            Relationship::with_target(
                AuthorizationRelationshipTarget::SubjectSet(AuthorizationSubjectSet::new(
                    AuthorizationResource::new("slack_channel", "C123"),
                    "member",
                )),
                "editor",
                AuthorizationResource::new("agent_session", "session-123"),
            ),
        ]))
        .await
        .expect("write relationships");

    let searches = server.searches.lock().expect("searches lock").clone();
    assert_eq!(searches.len(), 1);
    assert_eq!(
        searches[0].resource.as_ref().expect("resource").r#type,
        "slack_identity"
    );
    assert_eq!(searches[0].action.as_ref().expect("action").name, "assume");

    let writes = server.writes.lock().expect("writes lock").clone();
    assert_eq!(writes.len(), 1);
    let write = writes[0].writes[0].clone();
    assert!(write.subject.is_none());
    let target = write.target.expect("target");
    assert!(matches!(
        target.kind,
        Some(relationship_target::Kind::SubjectSet(subject_set))
            if subject_set.resource.as_ref().expect("resource").r#type == "slack_channel"
                && subject_set.relation == "member"
    ));
    assert_eq!(write.relation, "editor");
    assert_eq!(
        write.resource.as_ref().expect("resource").r#type,
        "agent_session"
    );
    assert_eq!(write.resource.as_ref().expect("resource").id, "session-123");

    let tokens = server.seen_tokens.lock().expect("seen tokens lock").clone();
    assert_eq!(
        tokens,
        vec![
            "relay-token-rust".to_string(),
            "relay-token-rust".to_string(),
            "relay-token-rust".to_string(),
            "relay-token-rust".to_string(),
            "relay-token-rust".to_string()
        ]
    );

    serve_task.abort();
    let _ = serve_task.await;
}

async fn serve_authorization(
    server: TestAuthorizationServer,
    socket: &Path,
) -> std::result::Result<(), tonic::transport::Error> {
    let _ = std::fs::remove_file(socket);
    let listener = UnixListener::bind(socket).expect("bind authorization socket");
    Server::builder()
        .add_service(AuthorizationProviderServer::new(server))
        .serve_with_incoming(UnixListenerStream::new(listener))
        .await
}

fn maybe_record_relay_token(
    tokens: &Arc<Mutex<Vec<String>>>,
    metadata: &tonic::metadata::MetadataMap,
) {
    if let Some(token) = metadata.get("x-gestalt-host-service-relay-token") {
        tokens
            .lock()
            .expect("tokens lock")
            .push(token.to_str().expect("relay token ascii").to_string());
    }
}
