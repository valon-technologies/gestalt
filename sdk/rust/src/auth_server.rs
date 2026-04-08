use std::sync::Arc;

use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::auth::AuthProvider;
use crate::generated::v1::auth_plugin_server::AuthPlugin;
use crate::generated::v1::{
    AuthSessionSettings, AuthenticatedUser, BeginLoginRequest, BeginLoginResponse,
    CompleteLoginRequest, ValidateExternalTokenRequest,
};
use crate::rpc_status::rpc_status;

#[derive(Clone)]
pub struct AuthServer<P> {
    auth: Arc<P>,
}

impl<P> AuthServer<P> {
    pub fn new(auth: Arc<P>) -> Self {
        Self { auth }
    }
}

#[tonic::async_trait]
impl<P> AuthPlugin for AuthServer<P>
where
    P: AuthProvider,
{
    async fn begin_login(
        &self,
        request: GrpcRequest<BeginLoginRequest>,
    ) -> std::result::Result<GrpcResponse<BeginLoginResponse>, Status> {
        let response = self
            .auth
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
            .auth
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
            .auth
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
        let Some(ttl) = self.auth.session_ttl() else {
            return Err(Status::unimplemented(
                "auth provider does not expose session settings",
            ));
        };
        let ttl_seconds = i64::try_from(ttl.as_secs()).unwrap_or(i64::MAX);
        Ok(GrpcResponse::new(AuthSessionSettings {
            session_ttl_seconds: ttl_seconds,
        }))
    }
}
