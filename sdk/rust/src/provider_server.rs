use std::collections::BTreeMap;
use std::sync::Arc;

use serde::Serialize;
use serde_json::Value;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::agent::{AgentToolRef, agent_tool_ref_from_proto};
use crate::api::{
    Access, Credential, HTTPSubjectRequest, Request, Response, Subject, scope_request_context,
};
use crate::catalog::{catalog_to_proto, object_map};
use crate::env::CURRENT_PROTOCOL_VERSION;
use crate::error::{Error, HTTP_INTERNAL_SERVER_ERROR, INTERNAL_ERROR_MESSAGE};
use crate::generated::v1::app_provider_server::AppProvider;
use crate::generated::v1::{
    ExecuteRequest, GetSessionCatalogRequest, GetSessionCatalogResponse, HttpSubjectRequest,
    OperationResult as ProtoOperationResult, ProviderMetadata, ResolveHttpSubjectRequest,
    ResolveHttpSubjectResponse, StartProviderRequest, StartProviderResponse, SubjectContext,
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
    /// Serialized response body bytes.
    pub body: Vec<u8>,
}

impl OperationResult {
    /// Returns whether the status is in the HTTP 2xx range.
    pub fn ok(&self) -> bool {
        (200..300).contains(&self.status)
    }

    /// Returns the raw response body bytes.
    pub fn bytes(&self) -> &[u8] {
        &self.body
    }

    /// Decodes the raw response body as UTF-8 text.
    pub fn text(&self) -> String {
        String::from_utf8_lossy(&self.body).into_owned()
    }

    /// Decodes the raw body as JSON without app invocation envelope handling.
    pub fn json(&self) -> std::result::Result<Value, Box<crate::InvokeError>> {
        crate::app_decode::parse_operation_result_json(&self.body).map_err(|_| {
            Box::new(crate::InvokeError {
                app: String::new(),
                operation: String::new(),
                status: None,
                code: None,
                message: "operation result body is not valid JSON".to_string(),
                body: None,
                raw_body: self.body.clone(),
            })
        })
    }

    /// Returns an invoke error when the status is outside the HTTP 2xx range.
    pub fn require_ok(&self) -> std::result::Result<(), Box<crate::InvokeError>> {
        if self.ok() {
            return Ok(());
        }
        Err(Box::new(crate::InvokeError {
            app: String::new(),
            operation: String::new(),
            status: Some(self.status),
            code: None,
            message: format!("app invoke failed with status {}", self.status),
            body: self.json().ok().map(Box::new),
            raw_body: self.body.clone(),
        }))
    }

    /// Serializes a typed handler response.
    pub fn from_response<T: Serialize>(response: Response<T>) -> Self {
        let status = response.status.unwrap_or(200);
        match serde_json::to_vec(&response.body) {
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
            body: serde_json::to_vec(&serde_json::json!({ "error": message.into() }))
                .unwrap_or_else(|_| br#"{"error":"internal error"}"#.to_vec()),
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
        let context = request.context.clone();
        let result = scope_request_context(
            context,
            self.router.execute(
                Arc::clone(&self.provider),
                &request.operation,
                Value::Object(object_map(request.params)),
                request_context(
                    request.context.as_ref(),
                    request.token,
                    request.connection_params.into_iter().collect(),
                    request.idempotency_key.trim().to_string(),
                ),
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
        let context = request.context.clone();
        let request = request_context(
            request.context.as_ref(),
            request.token,
            request.connection_params.into_iter().collect(),
            String::new(),
        );
        let catalog = scope_request_context(context, self.provider.catalog_for_request(&request))
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
        let context = request.context.clone();
        let subject = scope_request_context(
            context,
            self.provider.resolve_http_subject(
                http_subject_request(request.request.as_ref()),
                &request_context(
                    request.context.as_ref(),
                    String::new(),
                    Default::default(),
                    String::new(),
                ),
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
}

fn request_context(
    context: Option<&crate::generated::v1::RequestContext>,
    token: String,
    connection_params: std::collections::BTreeMap<String, String>,
    idempotency_key: String,
) -> Request {
    Request {
        token,
        connection_params,
        subject: request_subject(context),
        agent_subject: request_subject_field(context, "agent_subject"),
        credential: request_credential(context),
        access: request_access(context),
        host: request_host(context),
        idempotency_key,
        workflow: request_workflow(context),
        tool_refs: request_tool_refs(context),
        tool_refs_set: context
            .map(|context| context.tool_refs_set)
            .unwrap_or(false),
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
        credential_subject_id: subject.credential_subject_id.clone(),
        email: subject.email.clone(),
        display_name: subject.display_name.clone(),
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
        credential_subject_id: subject.credential_subject_id,
        email: subject.email,
        display_name: subject.display_name,
        ..Default::default()
    }
}
