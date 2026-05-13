use std::sync::Arc;

use serde::Serialize;
use serde_json::Value;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::api::{
    Access, Credential, ExternalIdentity, HTTPSubjectRequest, Request, Response, Subject,
};
use crate::catalog::{catalog_to_proto, object_map};
use crate::env::CURRENT_PROTOCOL_VERSION;
use crate::error::{Error, HTTP_INTERNAL_SERVER_ERROR, INTERNAL_ERROR_MESSAGE};
use crate::generated::v1::integration_provider_server::IntegrationProvider;
use crate::generated::v1::{
    ExecuteRequest, GetSessionCatalogRequest, GetSessionCatalogResponse, HttpSubjectRequest,
    OperationResult as ProtoOperationResult, PostConnectRequest, PostConnectResponse,
    ProviderMetadata, ResolveHttpSubjectRequest, ResolveHttpSubjectResponse, StartProviderRequest,
    StartProviderResponse, SubjectContext,
};
use crate::rpc_status::{require_protocol_version, rpc_status};
use crate::{Provider, Router};

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
    /// JSON-encoded response body.
    pub body: String,
}

impl OperationResult {
    /// Serializes a typed handler response.
    pub fn from_response<T: Serialize>(response: Response<T>) -> Self {
        let status = response.status.unwrap_or(200);
        match serde_json::to_string(&response.body) {
            Ok(body) => Self { status, body },
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
            body: serde_json::json!({ "error": message.into() }).to_string(),
        }
    }
}

impl<P> ProviderServer<P> {
    /// Creates a server from a provider and operation router.
    pub fn new(provider: Arc<P>, router: Router<P>) -> Self {
        Self { provider, router }
    }
}

#[tonic::async_trait]
impl<P> IntegrationProvider for ProviderServer<P>
where
    P: Provider,
{
    async fn get_metadata(
        &self,
        _request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<ProviderMetadata>, Status> {
        Ok(GrpcResponse::new(ProviderMetadata {
            supports_session_catalog: self.provider.supports_session_catalog(),
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
        _request: GrpcRequest<PostConnectRequest>,
    ) -> std::result::Result<GrpcResponse<PostConnectResponse>, Status> {
        Err(Status::unimplemented(
            "provider does not support post connect",
        ))
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
        invocation_token,
    }
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

fn http_subject_request(request: Option<&HttpSubjectRequest>) -> HTTPSubjectRequest {
    let Some(request) = request else {
        return HTTPSubjectRequest::default();
    };
    HTTPSubjectRequest {
        binding: request.binding.clone(),
        method: request.method.clone(),
        path: request.path.clone(),
        content_type: request.content_type.clone(),
        headers: string_lists(&request.headers),
        query: string_lists(&request.query),
        params: object_map(request.params.clone()),
        raw_body: request.raw_body.clone(),
        security_scheme: request.security_scheme.clone(),
        verified_subject: request.verified_subject.clone(),
        verified_claims: request.verified_claims.clone(),
    }
}

fn string_lists(
    values: &std::collections::BTreeMap<String, crate::generated::v1::StringList>,
) -> std::collections::BTreeMap<String, Vec<String>> {
    values
        .iter()
        .map(|(key, value)| (key.clone(), value.values.clone()))
        .collect()
}

fn subject_to_proto(subject: Subject) -> SubjectContext {
    SubjectContext {
        id: subject.id,
        kind: subject.kind,
        display_name: subject.display_name,
        auth_source: subject.auth_source,
        email: subject.email,
    }
}
