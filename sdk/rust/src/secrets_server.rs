use std::sync::Arc;

use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::generated::v1::secrets_provider_server::SecretsProvider as SecretsProviderGrpc;
use crate::generated::v1::{GetSecretRequest, GetSecretResponse};
use crate::rpc_status::rpc_status;
use crate::secrets::SecretsProvider;

#[derive(Clone)]
pub struct SecretsServer<P> {
    secrets: Arc<P>,
}

impl<P> SecretsServer<P> {
    pub fn new(secrets: Arc<P>) -> Self {
        Self { secrets }
    }
}

#[tonic::async_trait]
impl<P> SecretsProviderGrpc for SecretsServer<P>
where
    P: SecretsProvider,
{
    async fn get_secret(
        &self,
        request: GrpcRequest<GetSecretRequest>,
    ) -> std::result::Result<GrpcResponse<GetSecretResponse>, Status> {
        let request = request.into_inner();
        let value = self
            .secrets
            .get_secret(&request.name)
            .await
            .map_err(|error| rpc_status("get secret", error))?;
        Ok(GrpcResponse::new(GetSecretResponse { value }))
    }
}
