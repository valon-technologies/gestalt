use std::sync::Arc;

use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::auth::AuthenticationProvider;
use crate::error::{Error, HTTP_NOT_IMPLEMENTED};
use crate::generated::v1::authentication_provider_server::AuthenticationProvider as AuthenticationProviderGrpc;
use crate::generated::v1::{
    AuthSessionSettings, AuthenticateRequest, AuthenticatedUser, BeginAuthenticationRequest,
    BeginAuthenticationResponse, BeginLoginRequest, BeginLoginResponse,
    CompleteAuthenticationRequest, CompleteLoginRequest, TokenAuthInput,
    ValidateExternalTokenRequest, authenticate_request,
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

fn is_unimplemented(error: &Error) -> bool {
    error.status() == Some(HTTP_NOT_IMPLEMENTED)
}

fn begin_auth_request_from_login(request: BeginLoginRequest) -> BeginAuthenticationRequest {
    BeginAuthenticationRequest {
        callback_url: request.callback_url,
        host_state: request.host_state,
        scopes: request.scopes,
        options: request.options,
    }
}

fn begin_login_request_from_auth(request: BeginAuthenticationRequest) -> BeginLoginRequest {
    BeginLoginRequest {
        callback_url: request.callback_url,
        host_state: request.host_state,
        scopes: request.scopes,
        options: request.options,
    }
}

fn begin_auth_response_from_login(response: BeginLoginResponse) -> BeginAuthenticationResponse {
    BeginAuthenticationResponse {
        authorization_url: response.authorization_url,
        provider_state: response.provider_state,
    }
}

fn begin_login_response_from_auth(response: BeginAuthenticationResponse) -> BeginLoginResponse {
    BeginLoginResponse {
        authorization_url: response.authorization_url,
        provider_state: response.provider_state,
    }
}

fn complete_auth_request_from_login(
    request: CompleteLoginRequest,
) -> CompleteAuthenticationRequest {
    CompleteAuthenticationRequest {
        query: request.query,
        provider_state: request.provider_state,
        callback_url: request.callback_url,
    }
}

fn complete_login_request_from_auth(
    request: CompleteAuthenticationRequest,
) -> CompleteLoginRequest {
    CompleteLoginRequest {
        query: request.query,
        provider_state: request.provider_state,
        callback_url: request.callback_url,
    }
}

fn token_auth_request(token: String) -> AuthenticateRequest {
    AuthenticateRequest {
        options: Default::default(),
        input: Some(authenticate_request::Input::Token(TokenAuthInput { token })),
    }
}

#[tonic::async_trait]
impl<P> AuthenticationProviderGrpc for AuthenticationServer<P>
where
    P: AuthenticationProvider,
{
    async fn begin_authentication(
        &self,
        request: GrpcRequest<BeginAuthenticationRequest>,
    ) -> std::result::Result<GrpcResponse<BeginAuthenticationResponse>, Status> {
        let request = request.into_inner();
        let response = match self.provider.begin_authentication(request.clone()).await {
            Ok(response) => response,
            Err(error) if is_unimplemented(&error) => begin_auth_response_from_login(
                self.provider
                    .begin_login(begin_login_request_from_auth(request))
                    .await
                    .map_err(|fallback| rpc_status("begin authentication", fallback))?,
            ),
            Err(error) => return Err(rpc_status("begin authentication", error)),
        };
        Ok(GrpcResponse::new(response))
    }

    async fn complete_authentication(
        &self,
        request: GrpcRequest<CompleteAuthenticationRequest>,
    ) -> std::result::Result<GrpcResponse<AuthenticatedUser>, Status> {
        let request = request.into_inner();
        let user = match self.provider.complete_authentication(request.clone()).await {
            Ok(user) => user,
            Err(error) if is_unimplemented(&error) => self
                .provider
                .complete_login(complete_login_request_from_auth(request))
                .await
                .map_err(|fallback| rpc_status("complete authentication", fallback))?,
            Err(error) => return Err(rpc_status("complete authentication", error)),
        };
        Ok(GrpcResponse::new(user))
    }

    async fn authenticate(
        &self,
        request: GrpcRequest<AuthenticateRequest>,
    ) -> std::result::Result<GrpcResponse<AuthenticatedUser>, Status> {
        let request = request.into_inner();
        let user = match self.provider.authenticate(request.clone()).await {
            Ok(user) => user,
            Err(error) if is_unimplemented(&error) => {
                let Some(authenticate_request::Input::Token(token)) = request.input else {
                    return Err(rpc_status("authenticate", error));
                };
                self.provider
                    .validate_external_token(&token.token)
                    .await
                    .map_err(|fallback| rpc_status("authenticate", fallback))?
            }
            Err(error) => return Err(rpc_status("authenticate", error)),
        };
        let Some(user) = user else {
            return Err(Status::not_found("authentication input not recognized"));
        };
        Ok(GrpcResponse::new(user))
    }

    async fn begin_login(
        &self,
        request: GrpcRequest<BeginLoginRequest>,
    ) -> std::result::Result<GrpcResponse<BeginLoginResponse>, Status> {
        let request = request.into_inner();
        let response = match self.provider.begin_login(request.clone()).await {
            Ok(response) => response,
            Err(error) if is_unimplemented(&error) => begin_login_response_from_auth(
                self.provider
                    .begin_authentication(begin_auth_request_from_login(request))
                    .await
                    .map_err(|fallback| rpc_status("begin login", fallback))?,
            ),
            Err(error) => return Err(rpc_status("begin login", error)),
        };
        Ok(GrpcResponse::new(response))
    }

    async fn complete_login(
        &self,
        request: GrpcRequest<CompleteLoginRequest>,
    ) -> std::result::Result<GrpcResponse<AuthenticatedUser>, Status> {
        let request = request.into_inner();
        let user = match self.provider.complete_login(request.clone()).await {
            Ok(user) => user,
            Err(error) if is_unimplemented(&error) => self
                .provider
                .complete_authentication(complete_auth_request_from_login(request))
                .await
                .map_err(|fallback| rpc_status("complete login", fallback))?,
            Err(error) => return Err(rpc_status("complete login", error)),
        };
        Ok(GrpcResponse::new(user))
    }

    async fn validate_external_token(
        &self,
        request: GrpcRequest<ValidateExternalTokenRequest>,
    ) -> std::result::Result<GrpcResponse<AuthenticatedUser>, Status> {
        let token = request.into_inner().token;
        let user = match self.provider.validate_external_token(&token).await {
            Ok(user) => user,
            Err(error) if is_unimplemented(&error) => self
                .provider
                .authenticate(token_auth_request(token))
                .await
                .map_err(|fallback| rpc_status("validate external token", fallback))?,
            Err(error) => return Err(rpc_status("validate external token", error)),
        };
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
