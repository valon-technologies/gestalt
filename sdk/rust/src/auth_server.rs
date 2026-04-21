use std::sync::Arc;

use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::auth::AuthenticationProvider;
use crate::generated::v1::auth_provider_server::AuthProvider as LegacyAuthProviderGrpc;
use crate::generated::v1::authentication_provider_server::AuthenticationProvider as AuthenticationProviderGrpc;
use crate::generated::v1::{
    AuthSessionSettings, AuthenticatedUser, BeginLoginRequest, BeginLoginResponse,
    CompleteLoginRequest, ValidateExternalTokenRequest,
};
use crate::rpc_status::rpc_status;

pub struct AuthenticationServer<P> {
    provider: Arc<P>,
}

impl<P> AuthenticationServer<P> {
    pub fn new(provider: Arc<P>) -> Self {
        Self { provider }
    }
}

impl<P> Clone for AuthenticationServer<P> {
    fn clone(&self) -> Self {
        Self {
            provider: Arc::clone(&self.provider),
        }
    }
}

#[tonic::async_trait]
impl<P> AuthenticationProviderGrpc for AuthenticationServer<P>
where
    P: AuthenticationProvider,
{
    async fn begin_login(
        &self,
        request: GrpcRequest<BeginLoginRequest>,
    ) -> std::result::Result<GrpcResponse<BeginLoginResponse>, Status> {
        let response = self
            .provider
            .begin_login(request.into_inner())
            .await
            .map_err(|error| rpc_status("begin login", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn complete_login(
        &self,
        request: GrpcRequest<CompleteLoginRequest>,
    ) -> std::result::Result<GrpcResponse<AuthenticatedUser>, Status> {
        let user = self
            .provider
            .complete_login(request.into_inner())
            .await
            .map_err(|error| rpc_status("complete login", error))?;
        Ok(GrpcResponse::new(user))
    }

    async fn validate_external_token(
        &self,
        request: GrpcRequest<ValidateExternalTokenRequest>,
    ) -> std::result::Result<GrpcResponse<AuthenticatedUser>, Status> {
        let user = self
            .provider
            .validate_external_token(&request.into_inner().token)
            .await
            .map_err(|error| rpc_status("validate external token", error))?;
        let Some(user) = user else {
            return Err(Status::not_found("token not recognized"));
        };
        Ok(GrpcResponse::new(user))
    }

    async fn get_session_settings(
        &self,
        _request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<AuthSessionSettings>, Status> {
        let Some(ttl) = self.provider.session_ttl() else {
            return Err(Status::unimplemented(
                "authentication provider does not expose session settings",
            ));
        };
        let ttl_seconds = i64::try_from(ttl.as_secs()).unwrap_or(i64::MAX);
        Ok(GrpcResponse::new(AuthSessionSettings {
            session_ttl_seconds: ttl_seconds,
        }))
    }
}

#[tonic::async_trait]
impl<P> LegacyAuthProviderGrpc for AuthenticationServer<P>
where
    P: AuthenticationProvider,
{
    async fn begin_login(
        &self,
        request: GrpcRequest<BeginLoginRequest>,
    ) -> std::result::Result<GrpcResponse<BeginLoginResponse>, Status> {
        AuthenticationProviderGrpc::begin_login(self, request).await
    }

    async fn complete_login(
        &self,
        request: GrpcRequest<CompleteLoginRequest>,
    ) -> std::result::Result<GrpcResponse<AuthenticatedUser>, Status> {
        AuthenticationProviderGrpc::complete_login(self, request).await
    }

    async fn validate_external_token(
        &self,
        request: GrpcRequest<ValidateExternalTokenRequest>,
    ) -> std::result::Result<GrpcResponse<AuthenticatedUser>, Status> {
        AuthenticationProviderGrpc::validate_external_token(self, request).await
    }

    async fn get_session_settings(
        &self,
        request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<AuthSessionSettings>, Status> {
        AuthenticationProviderGrpc::get_session_settings(self, request).await
    }
}
