use std::collections::BTreeMap;
use std::sync::Arc;
use std::time::SystemTime;

use prost_types::Timestamp;
use serde::Serialize;
use serde_json::Value;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::agent::{AgentToolRef, agent_tool_ref_from_proto};
use crate::api::{
    Access, ConnectedToken, Credential, ExternalIdentity, HTTPSubjectRequest, Request, Response,
    Subject,
};
use crate::catalog::{catalog_to_proto, object_map};
use crate::env::CURRENT_PROTOCOL_VERSION;
use crate::error::{Error, HTTP_INTERNAL_SERVER_ERROR, INTERNAL_ERROR_MESSAGE};
use crate::generated::v1::app_provider_server::AppProvider;
use crate::generated::v1::{
    ExecuteRequest, GetSessionCatalogRequest, GetSessionCatalogResponse, HttpSubjectRequest,
    OperationResult as ProtoOperationResult, PostConnectCredential, PostConnectRequest,
    PostConnectResponse, ProviderMetadata, ResolveHttpSubjectRequest, ResolveHttpSubjectResponse,
    StartProviderRequest, StartProviderResponse, SubjectContext,
};
use crate::protocol;
use crate::rpc_status::{require_protocol_version, rpc_status};
use crate::{Provider, Router};

const JSON_CONTENT_TYPE: &str = "application/json";

#[derive(Clone)]
/// gRPC integration-provider server used by the Rust runtime.
pub struct ProviderServer<P> {
    provider: Arc<P>,
    router: Router<P>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
/// Serialized operation result returned by the provider runtime.
pub struct OperationResult {
    /// HTTP-style status code.
    pub status: u16,
    /// HTTP response headers.
    pub headers: BTreeMap<String, Vec<String>>,
    /// JSON-encoded response body.
    pub body: String,
}

impl OperationResult {
    /// Serializes a typed handler response.
    pub fn from_response<T: Serialize>(response: Response<T>) -> Self {
        let status = response.status.unwrap_or(200);
        match serde_json::to_string(&response.body) {
            Ok(body) => Self {
                status,
                headers: json_response_headers(response.headers),
                body,
            },
            Err(error) => {
                eprintln!("internal error in Gestalt operation response: {error}");
                Self::error(HTTP_INTERNAL_SERVER_ERROR, INTERNAL_ERROR_MESSAGE)
            }
        }
    }

    /// Converts an SDK error into a serialized operation result.
    pub fn from_error(error: Error) -> Self {
        let status = error.status().unwrap_or(HTTP_INTERNAL_SERVER_ERROR);
        if !error.expose_message() {
            eprintln!("internal error in Gestalt operation: {}", error.message());
            return Self::error(HTTP_INTERNAL_SERVER_ERROR, INTERNAL_ERROR_MESSAGE);
        }
        Self::error(status, error.message().to_owned())
    }

    /// Creates a serialized error response.
    pub fn error(status: u16, message: impl Into<String>) -> Self {
        Self {
            status,
            headers: json_response_headers(BTreeMap::new()),
            body: serde_json::json!({ "error": message.into() }).to_string(),
        }
    }
}

fn json_response_headers(
    mut headers: BTreeMap<String, Vec<String>>,
) -> BTreeMap<String, Vec<String>> {
    if !headers
        .keys()
        .any(|name| name.eq_ignore_ascii_case("content-type"))
    {
        headers.insert(
            "Content-Type".to_owned(),
            vec![JSON_CONTENT_TYPE.to_owned()],
        );
    }
    headers
}

impl<P> ProviderServer<P> {
    /// Creates a server from a provider and operation router.
    pub fn new(provider: Arc<P>, router: Router<P>) -> Self {
        Self { provider, router }
    }
}

#[tonic::async_trait]
impl<P> AppProvider for ProviderServer<P>
where
    P: Provider,
{
    async fn get_metadata(
        &self,
        _request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<ProviderMetadata>, Status> {
        Ok(GrpcResponse::new(ProviderMetadata {
            supports_session_catalog: self.provider.supports_session_catalog(),
            supports_post_connect: self.provider.supports_post_connect(),
            min_protocol_version: CURRENT_PROTOCOL_VERSION,
            max_protocol_version: CURRENT_PROTOCOL_VERSION,
            ..ProviderMetadata::default()
        }))
    }

    async fn start_provider(
        &self,
        request: GrpcRequest<StartProviderRequest>,
    ) -> std::result::Result<GrpcResponse<StartProviderResponse>, Status> {
        let request = request.into_inner();
        require_protocol_version(request.protocol_version, CURRENT_PROTOCOL_VERSION)?;
        self.provider
            .configure(&request.name, object_map(request.config))
            .await
            .map_err(|error| rpc_status("configure provider", error))?;

        Ok(GrpcResponse::new(StartProviderResponse {
            protocol_version: CURRENT_PROTOCOL_VERSION,
        }))
    }

    async fn execute(
        &self,
        request: GrpcRequest<ExecuteRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoOperationResult>, Status> {
        let request = request.into_inner();
        let result = self
            .router
            .execute(
                Arc::clone(&self.provider),
                &request.operation,
                Value::Object(object_map(request.params)),
                request_context(
                    request.context.as_ref(),
                    request.token,
                    request.connection_params.into_iter().collect(),
                    request.idempotency_key.trim().to_string(),
                    request.invocation_token,
                ),
            )
            .await;

        Ok(GrpcResponse::new(ProtoOperationResult {
            status: i32::from(result.status),
            headers: protocol::string_lists_to_proto(result.headers),
            body: result.body,
        }))
    }

    async fn get_session_catalog(
        &self,
        request: GrpcRequest<GetSessionCatalogRequest>,
    ) -> std::result::Result<GrpcResponse<GetSessionCatalogResponse>, Status> {
        if !self.provider.supports_session_catalog() {
            return Err(Status::unimplemented(
                "provider does not support session catalogs",
            ));
        }

        let request = request.into_inner();
        let request = request_context(
            request.context.as_ref(),
            request.token,
            request.connection_params.into_iter().collect(),
            String::new(),
            String::new(),
        );
        let catalog = self
            .provider
            .catalog_for_request(&request)
            .await
            .map_err(|error| rpc_status("session catalog", error))?;

        Ok(GrpcResponse::new(GetSessionCatalogResponse {
            catalog: catalog.as_ref().map(catalog_to_proto),
        }))
    }

    async fn resolve_http_subject(
        &self,
        request: GrpcRequest<ResolveHttpSubjectRequest>,
    ) -> std::result::Result<GrpcResponse<ResolveHttpSubjectResponse>, Status> {
        let request = request.into_inner();
        let subject = self
            .provider
            .resolve_http_subject(
                http_subject_request(request.request.as_ref()),
                &request_context(
                    request.context.as_ref(),
                    String::new(),
                    Default::default(),
                    String::new(),
                    String::new(),
                ),
            )
            .await;

        let subject = match subject {
            Ok(subject) => subject,
            Err(error) if error.status().is_some() && error.expose_message() => {
                return Ok(GrpcResponse::new(ResolveHttpSubjectResponse {
                    reject_status: i32::from(error.status().unwrap_or_default()),
                    reject_message: error.message().to_owned(),
                    ..Default::default()
                }));
            }
            Err(error) => return Err(rpc_status("resolve http subject", error)),
        };

        Ok(GrpcResponse::new(ResolveHttpSubjectResponse {
            subject: subject.map(subject_to_proto),
            ..Default::default()
        }))
    }

    async fn post_connect(
        &self,
        request: GrpcRequest<PostConnectRequest>,
    ) -> std::result::Result<GrpcResponse<PostConnectResponse>, Status> {
        if !self.provider.supports_post_connect() {
            return Err(Status::unimplemented(
                "provider does not support post connect",
            ));
        }

        let request = request.into_inner();
        let token = connected_token_from_proto(request.token.as_ref())
            .map_err(|error| rpc_status("post connect", error))?;
        let metadata = self
            .provider
            .post_connect(&token)
            .await
            .map_err(|error| rpc_status("post connect", error))?;

        Ok(GrpcResponse::new(PostConnectResponse { metadata }))
    }
}

fn request_context(
    context: Option<&crate::generated::v1::RequestContext>,
    token: String,
    connection_params: std::collections::BTreeMap<String, String>,
    idempotency_key: String,
    invocation_token: String,
) -> Request {
    Request {
        token,
        connection_params,
        subject: request_subject(context),
        agent_subject: request_subject_field(context, "agent_subject"),
        external_identity: request_external_identity_field(context, "external_identity"),
        agent_external_identity: request_external_identity_field(
            context,
            "agent_external_identity",
        ),
        credential: request_credential(context),
        access: request_access(context),
        host: request_host(context),
        idempotency_key,
        workflow: request_workflow(context),
        tool_refs: request_tool_refs(context),
        tool_refs_set: context
            .map(|context| context.tool_refs_set)
            .unwrap_or(false),
        invocation_token,
    }
}

fn connected_token_from_proto(
    token: Option<&PostConnectCredential>,
) -> crate::Result<ConnectedToken> {
    let Some(token) = token else {
        return Ok(ConnectedToken::default());
    };
    let metadata_json = token.metadata_json.clone();
    Ok(ConnectedToken {
        id: token.id.clone(),
        subject_id: token.subject_id.clone(),
        integration: token.integration.clone(),
        connection: token.connection.clone(),
        instance: token.instance.clone(),
        access_token: token.access_token.clone(),
        refresh_token: token.refresh_token.clone(),
        scopes: token.scopes.clone(),
        expires_at: time_from_timestamp(token.expires_at.as_ref())?,
        last_refreshed_at: time_from_timestamp(token.last_refreshed_at.as_ref())?,
        refresh_error_count: token.refresh_error_count,
        metadata_json: metadata_json.clone(),
        metadata: metadata_from_json(&metadata_json),
        created_at: time_from_timestamp(token.created_at.as_ref())?,
        updated_at: time_from_timestamp(token.updated_at.as_ref())?,
    })
}

fn time_from_timestamp(value: Option<&Timestamp>) -> crate::Result<Option<SystemTime>> {
    value.map(protocol::system_time_from_timestamp).transpose()
}

fn metadata_from_json(value: &str) -> BTreeMap<String, String> {
    if value.trim().is_empty() {
        return BTreeMap::new();
    }
    let Ok(Value::Object(object)) = serde_json::from_str::<Value>(value) else {
        return BTreeMap::new();
    };
    object
        .into_iter()
        .filter_map(|(key, value)| match value {
            Value::String(value) => Some((key, value)),
            _ => None,
        })
        .collect()
}

fn request_subject(context: Option<&crate::generated::v1::RequestContext>) -> Subject {
    request_subject_field(context, "subject")
}

fn request_subject_field(
    context: Option<&crate::generated::v1::RequestContext>,
    field_name: &str,
) -> Subject {
    let Some(context) = context else {
        return Subject::default();
    };
    let subject = match field_name {
        "agent_subject" => context.agent_subject.as_ref(),
        _ => context.subject.as_ref(),
    };
    let Some(subject) = subject else {
        return Subject::default();
    };
    Subject {
        id: subject.id.clone(),
        kind: subject.kind.clone(),
        credential_subject_id: subject.credential_subject_id.clone(),
        display_name: subject.display_name.clone(),
        auth_source: subject.auth_source.clone(),
        email: subject.email.clone(),
    }
}

fn request_external_identity_field(
    context: Option<&crate::generated::v1::RequestContext>,
    field_name: &str,
) -> ExternalIdentity {
    let Some(context) = context else {
        return ExternalIdentity::default();
    };
    let identity = match field_name {
        "agent_external_identity" => context.agent_external_identity.as_ref(),
        _ => context.external_identity.as_ref(),
    };
    let Some(identity) = identity else {
        return ExternalIdentity::default();
    };
    ExternalIdentity {
        r#type: identity.r#type.clone(),
        id: identity.id.clone(),
    }
}

fn request_credential(context: Option<&crate::generated::v1::RequestContext>) -> Credential {
    let Some(context) = context else {
        return Credential::default();
    };
    let Some(credential) = context.credential.as_ref() else {
        return Credential::default();
    };
    Credential {
        mode: credential.mode.clone(),
        subject_id: credential.subject_id.clone(),
        connection: credential.connection.clone(),
        instance: credential.instance.clone(),
    }
}

fn request_access(context: Option<&crate::generated::v1::RequestContext>) -> Access {
    let Some(context) = context else {
        return Access::default();
    };
    let Some(access) = context.access.as_ref() else {
        return Access::default();
    };
    Access {
        policy: access.policy.clone(),
        role: access.role.clone(),
    }
}

fn request_host(context: Option<&crate::generated::v1::RequestContext>) -> crate::Host {
    let Some(context) = context else {
        return crate::Host::default();
    };
    let Some(host) = context.host.as_ref() else {
        return crate::Host::default();
    };
    crate::Host {
        public_base_url: host.public_base_url.clone(),
    }
}

fn request_workflow(
    context: Option<&crate::generated::v1::RequestContext>,
) -> serde_json::Map<String, serde_json::Value> {
    let Some(context) = context else {
        return serde_json::Map::new();
    };
    crate::catalog::object_map(context.workflow.clone())
}

fn request_tool_refs(context: Option<&crate::generated::v1::RequestContext>) -> Vec<AgentToolRef> {
    let Some(context) = context else {
        return Vec::new();
    };
    context
        .tool_refs
        .iter()
        .cloned()
        .map(agent_tool_ref_from_proto)
        .collect()
}

fn http_subject_request(request: Option<&HttpSubjectRequest>) -> HTTPSubjectRequest {
    let Some(request) = request else {
        return HTTPSubjectRequest::default();
    };
    HTTPSubjectRequest {
        binding: request.binding.clone(),
        method: request.method.clone(),
        path: request.path.clone(),
        content_type: request.content_type.clone(),
        headers: protocol::string_lists_from_proto(&request.headers),
        query: protocol::string_lists_from_proto(&request.query),
        params: object_map(request.params.clone()),
        raw_body: request.raw_body.clone(),
        security_scheme: request.security_scheme.clone(),
        verified_subject: request.verified_subject.clone(),
        verified_claims: request.verified_claims.clone(),
    }
}

fn subject_to_proto(subject: Subject) -> SubjectContext {
    SubjectContext {
        id: subject.id,
        kind: subject.kind,
        credential_subject_id: subject.credential_subject_id,
        display_name: subject.display_name,
        auth_source: subject.auth_source,
        email: subject.email,
    }
}
