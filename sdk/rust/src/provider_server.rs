use std::sync::Arc;

use serde::Serialize;
use serde_json::Value;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::api::{Request, Response};
use crate::catalog::{catalog_json, object_map};
use crate::env::CURRENT_PROTOCOL_VERSION;
use crate::error::Error;
use crate::generated::v1::plugin_provider_server::PluginProvider;
use crate::generated::v1::{
    ExecuteRequest, GetSessionCatalogRequest, GetSessionCatalogResponse,
    OperationResult as ProtoOperationResult, PostConnectRequest, PostConnectResponse,
    ProviderMetadata, StartProviderRequest, StartProviderResponse,
};
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
            Err(error) => Self::error(500, error.to_string()),
        }
    }

    pub fn from_error(error: Error) -> Self {
        Self::error(error.status().unwrap_or(500), error.message().to_owned())
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
impl<P> PluginProvider for ProviderServer<P>
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
        self.provider
            .configure(&request.name, object_map(request.config))
            .await
            .map_err(|error| Status::unknown(format!("configure provider: {}", error.message())))?;

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
        };
        let catalog = self
            .provider
            .catalog_for_request(&request)
            .await
            .map_err(|error| Status::unknown(format!("session catalog: {}", error.message())))?;
        let catalog_json = catalog
            .as_ref()
            .map(catalog_json)
            .transpose()
            .map_err(|error| Status::internal(format!("encode session catalog: {}", error)))?
            .unwrap_or_default();

        Ok(GrpcResponse::new(GetSessionCatalogResponse {
            catalog_json,
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
