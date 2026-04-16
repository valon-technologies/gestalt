use std::sync::Arc;

use serde::Serialize;
use serde_json::Value;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::api::{Access, Credential, Request, Response, Subject};
use crate::catalog::object_map;
use crate::env::CURRENT_PROTOCOL_VERSION;
use crate::error::{Error, HTTP_INTERNAL_SERVER_ERROR, INTERNAL_ERROR_MESSAGE};
use crate::generated::v1::integration_provider_server::IntegrationProvider;
use crate::generated::v1::{
    ExecuteRequest, GetSessionCatalogRequest, GetSessionCatalogResponse,
    OperationResult as ProtoOperationResult, PostConnectRequest, PostConnectResponse,
    ProviderMetadata, StartProviderRequest, StartProviderResponse,
};
use crate::rpc_status::{require_protocol_version, rpc_status};
use crate::{Provider, Router};

#[derive(Clone)]
pub struct ProviderServer<P> {
    provider: Arc<P>,
    router: Router<P>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct OperationResult {
    pub status: u16,
    pub body: String,
}

impl OperationResult {
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

    pub fn from_error(error: Error) -> Self {
        let status = error.status().unwrap_or(HTTP_INTERNAL_SERVER_ERROR);
        if !error.expose_message() {
            eprintln!("internal error in Gestalt operation: {}", error.message());
            return Self::error(HTTP_INTERNAL_SERVER_ERROR, INTERNAL_ERROR_MESSAGE);
        }
        Self::error(status, error.message().to_owned())
    }

    pub fn error(status: u16, message: impl Into<String>) -> Self {
        Self {
            status,
            body: serde_json::json!({ "error": message.into() }).to_string(),
        }
    }
}

impl<P> ProviderServer<P> {
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
                Request {
                    token: request.token,
                    connection_params: request.connection_params.into_iter().collect(),
                    subject: request_subject(request.context.as_ref()),
                    credential: request_credential(request.context.as_ref()),
                    access: request_access(request.context.as_ref()),
                    workflow: request_workflow(request.context.as_ref()),
                    request_handle: request.request_handle,
                },
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
        let request = Request {
            token: request.token,
            connection_params: request.connection_params.into_iter().collect(),
            subject: request_subject(request.context.as_ref()),
            credential: request_credential(request.context.as_ref()),
            access: request_access(request.context.as_ref()),
            workflow: request_workflow(request.context.as_ref()),
            request_handle: String::new(),
        };
        let catalog = self
            .provider
            .catalog_for_request(&request)
            .await
            .map_err(|error| rpc_status("session catalog", error))?;

        Ok(GrpcResponse::new(GetSessionCatalogResponse { catalog }))
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

fn request_subject(context: Option<&crate::generated::v1::RequestContext>) -> Subject {
    let Some(context) = context else {
        return Subject::default();
    };
    let Some(subject) = context.subject.as_ref() else {
        return Subject::default();
    };
    Subject {
        id: subject.id.clone(),
        kind: subject.kind.clone(),
        display_name: subject.display_name.clone(),
        auth_source: subject.auth_source.clone(),
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

fn request_workflow(
    context: Option<&crate::generated::v1::RequestContext>,
) -> serde_json::Map<String, serde_json::Value> {
    let Some(context) = context else {
        return serde_json::Map::new();
    };
    crate::catalog::object_map(context.workflow.clone())
}
