use std::sync::Arc;

use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::auth::{
    AuthSessionSettings, AuthenticationProvider, auth_session_settings_to_proto,
    authenticated_user_to_proto, begin_login_request_from_proto, begin_login_response_to_proto,
    complete_login_request_from_proto,
};
use crate::generated::v1::authentication_server::Authentication as AuthenticationProviderGrpc;
use crate::generated::v1::{
    AuthSessionSettings as ProtoAuthSessionSettings, AuthenticatedUser as ProtoAuthenticatedUser,
    BeginLoginRequest as ProtoBeginLoginRequest, BeginLoginResponse as ProtoBeginLoginResponse,
    CompleteLoginRequest as ProtoCompleteLoginRequest, ValidateExternalTokenRequest,
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
        request: GrpcRequest<ProtoBeginLoginRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoBeginLoginResponse>, Status> {
        let response = self
            .provider
            .begin_login(begin_login_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("begin login", error))?;
        Ok(GrpcResponse::new(begin_login_response_to_proto(response)))
    }

    async fn complete_login(
        &self,
        request: GrpcRequest<ProtoCompleteLoginRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoAuthenticatedUser>, Status> {
        let user = self
            .provider
            .complete_login(complete_login_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("complete login", error))?;
        Ok(GrpcResponse::new(authenticated_user_to_proto(user)))
    }

    async fn validate_external_token(
        &self,
        request: GrpcRequest<ValidateExternalTokenRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoAuthenticatedUser>, Status> {
        let user = self
            .provider
            .validate_external_token(&request.into_inner().token)
            .await
            .map_err(|error| rpc_status("validate external token", error))?;
        let Some(user) = user else {
            return Err(Status::not_found("token not recognized"));
        };
        Ok(GrpcResponse::new(authenticated_user_to_proto(user)))
    }

    async fn get_session_settings(
        &self,
        _request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<ProtoAuthSessionSettings>, Status> {
        let settings = self.provider.session_settings().or_else(|| {
            self.provider
                .session_ttl()
                .map(|session_ttl| AuthSessionSettings { session_ttl })
        });
        let Some(settings) = settings else {
            return Err(Status::unimplemented(
                "authentication provider does not expose session settings",
            ));
        };
        Ok(GrpcResponse::new(auth_session_settings_to_proto(settings)))
    }
}
