use std::sync::Arc;

use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::datastore::DatastoreProvider;
use crate::generated::v1::datastore_provider_server::DatastoreProvider as DatastoreProviderGrpc;
use crate::generated::v1::{
    DeleteOAuthRegistrationRequest, DeleteStoredIntegrationTokenRequest, GetApiTokenByHashRequest,
    GetOAuthRegistrationRequest, GetStoredIntegrationTokenRequest, GetUserRequest,
    ListApiTokensRequest, ListApiTokensResponse, ListStoredIntegrationTokensRequest,
    ListStoredIntegrationTokensResponse, OAuthRegistration, RevokeAllApiTokensRequest,
    RevokeAllApiTokensResponse, RevokeApiTokenRequest, StoredApiToken, StoredIntegrationToken,
    StoredUser,
};
use crate::rpc_status::rpc_status;

#[derive(Clone)]
pub struct DatastoreServer<P> {
    store: Arc<P>,
}

impl<P> DatastoreServer<P> {
    pub fn new(store: Arc<P>) -> Self {
        Self { store }
    }
}

#[tonic::async_trait]
impl<P> DatastoreProviderGrpc for DatastoreServer<P>
where
    P: DatastoreProvider,
{
    async fn migrate(
        &self,
        _request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.store
            .migrate()
            .await
            .map_err(|error| rpc_status("migrate", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn get_user(
        &self,
        request: GrpcRequest<GetUserRequest>,
    ) -> std::result::Result<GrpcResponse<StoredUser>, Status> {
        let request = request.into_inner();
        let user = self
            .store
            .get_user(&request.id)
            .await
            .map_err(|error| rpc_status("get user", error))?;
        let Some(user) = user else {
            return Err(Status::not_found("user not found"));
        };
        Ok(GrpcResponse::new(user))
    }

    async fn find_or_create_user(
        &self,
        request: GrpcRequest<crate::generated::v1::FindOrCreateUserRequest>,
    ) -> std::result::Result<GrpcResponse<StoredUser>, Status> {
        let request = request.into_inner();
        let user = self
            .store
            .find_or_create_user(&request.email)
            .await
            .map_err(|error| rpc_status("find or create user", error))?;
        Ok(GrpcResponse::new(user))
    }

    async fn put_stored_integration_token(
        &self,
        request: GrpcRequest<StoredIntegrationToken>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.store
            .put_integration_token(request.into_inner())
            .await
            .map_err(|error| rpc_status("put integration token", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn get_stored_integration_token(
        &self,
        request: GrpcRequest<GetStoredIntegrationTokenRequest>,
    ) -> std::result::Result<GrpcResponse<StoredIntegrationToken>, Status> {
        let request = request.into_inner();
        let token = self
            .store
            .get_integration_token(
                &request.user_id,
                &request.integration,
                &request.connection,
                &request.instance,
            )
            .await
            .map_err(|error| rpc_status("get integration token", error))?;
        let Some(token) = token else {
            return Err(Status::not_found("integration token not found"));
        };
        Ok(GrpcResponse::new(token))
    }

    async fn list_stored_integration_tokens(
        &self,
        request: GrpcRequest<ListStoredIntegrationTokensRequest>,
    ) -> std::result::Result<GrpcResponse<ListStoredIntegrationTokensResponse>, Status> {
        let request = request.into_inner();
        let tokens = self
            .store
            .list_integration_tokens(&request.user_id, &request.integration, &request.connection)
            .await
            .map_err(|error| rpc_status("list integration tokens", error))?;
        Ok(GrpcResponse::new(ListStoredIntegrationTokensResponse {
            tokens,
        }))
    }

    async fn delete_stored_integration_token(
        &self,
        request: GrpcRequest<DeleteStoredIntegrationTokenRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        let request = request.into_inner();
        self.store
            .delete_integration_token(&request.id)
            .await
            .map_err(|error| rpc_status("delete integration token", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn put_api_token(
        &self,
        request: GrpcRequest<StoredApiToken>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.store
            .put_api_token(request.into_inner())
            .await
            .map_err(|error| rpc_status("put api token", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn get_api_token_by_hash(
        &self,
        request: GrpcRequest<GetApiTokenByHashRequest>,
    ) -> std::result::Result<GrpcResponse<StoredApiToken>, Status> {
        let request = request.into_inner();
        let token = self
            .store
            .get_api_token_by_hash(&request.hashed_token)
            .await
            .map_err(|error| rpc_status("get api token by hash", error))?;
        let Some(token) = token else {
            return Err(Status::not_found("api token not found"));
        };
        Ok(GrpcResponse::new(token))
    }

    async fn list_api_tokens(
        &self,
        request: GrpcRequest<ListApiTokensRequest>,
    ) -> std::result::Result<GrpcResponse<ListApiTokensResponse>, Status> {
        let request = request.into_inner();
        let tokens = self
            .store
            .list_api_tokens(&request.user_id)
            .await
            .map_err(|error| rpc_status("list api tokens", error))?;
        Ok(GrpcResponse::new(ListApiTokensResponse { tokens }))
    }

    async fn revoke_api_token(
        &self,
        request: GrpcRequest<RevokeApiTokenRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        let request = request.into_inner();
        self.store
            .revoke_api_token(&request.user_id, &request.id)
            .await
            .map_err(|error| rpc_status("revoke api token", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn revoke_all_api_tokens(
        &self,
        request: GrpcRequest<RevokeAllApiTokensRequest>,
    ) -> std::result::Result<GrpcResponse<RevokeAllApiTokensResponse>, Status> {
        let request = request.into_inner();
        let revoked = self
            .store
            .revoke_all_api_tokens(&request.user_id)
            .await
            .map_err(|error| rpc_status("revoke all api tokens", error))?;
        Ok(GrpcResponse::new(RevokeAllApiTokensResponse { revoked }))
    }

    async fn get_o_auth_registration(
        &self,
        request: GrpcRequest<GetOAuthRegistrationRequest>,
    ) -> std::result::Result<GrpcResponse<OAuthRegistration>, Status> {
        let request = request.into_inner();
        let registration = self
            .store
            .get_oauth_registration(&request.auth_server_url, &request.redirect_uri)
            .await
            .map_err(|error| rpc_status("get oauth registration", error))?;
        let Some(registration) = registration else {
            return Err(Status::not_found("oauth registration not found"));
        };
        Ok(GrpcResponse::new(registration))
    }

    async fn put_o_auth_registration(
        &self,
        request: GrpcRequest<OAuthRegistration>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.store
            .put_oauth_registration(request.into_inner())
            .await
            .map_err(|error| rpc_status("put oauth registration", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn delete_o_auth_registration(
        &self,
        request: GrpcRequest<DeleteOAuthRegistrationRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        let request = request.into_inner();
        self.store
            .delete_oauth_registration(&request.auth_server_url, &request.redirect_uri)
            .await
            .map_err(|error| rpc_status("delete oauth registration", error))?;
        Ok(GrpcResponse::new(()))
    }
}
