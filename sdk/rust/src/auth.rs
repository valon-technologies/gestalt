use std::time::Duration;

use tonic::codegen::async_trait;

use crate::api::RuntimeMetadata;
use crate::error::{Error, Result};
pub use crate::generated::v1::{
    AuthenticateRequest, AuthenticatedUser, BeginAuthenticationRequest,
    BeginAuthenticationResponse, BeginLoginRequest, BeginLoginResponse,
    CompleteAuthenticationRequest, CompleteLoginRequest, HttpRequestAuthInput, TokenAuthInput,
    authenticate_request,
};

#[async_trait]
/// Lifecycle and login contract for Gestalt authentication providers.
pub trait AuthenticationProvider: Send + Sync + 'static {
    /// Configures the provider before it starts serving requests.
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> Result<()> {
        Ok(())
    }

    /// Returns runtime metadata that should augment the static manifest.
    fn metadata(&self) -> Option<RuntimeMetadata> {
        None
    }

    /// Returns non-fatal warnings the host should surface to users.
    fn warnings(&self) -> Vec<String> {
        Vec::new()
    }

    /// Performs an optional health check.
    async fn health_check(&self) -> Result<()> {
        Ok(())
    }

    /// Shuts the provider down before the runtime exits.
    async fn close(&self) -> Result<()> {
        Ok(())
    }

    /// Starts an interactive authentication flow.
    async fn begin_authentication(
        &self,
        _req: BeginAuthenticationRequest,
    ) -> Result<BeginAuthenticationResponse> {
        Err(Error::unimplemented(
            "authentication provider does not implement begin_authentication",
        ))
    }

    /// Finishes an interactive authentication flow.
    async fn complete_authentication(
        &self,
        _req: CompleteAuthenticationRequest,
    ) -> Result<AuthenticatedUser> {
        Err(Error::unimplemented(
            "authentication provider does not implement complete_authentication",
        ))
    }

    /// Validates an externally minted token or HTTP request when supported.
    async fn authenticate(&self, req: AuthenticateRequest) -> Result<Option<AuthenticatedUser>> {
        let Some(input) = req.input else {
            return Err(Error::unimplemented(
                "authentication provider does not support external authentication",
            ));
        };
        match input {
            authenticate_request::Input::Token(token) => {
                self.validate_external_token(&token.token).await
            }
            authenticate_request::Input::Http(_) => Err(Error::unimplemented(
                "authentication provider does not support external authentication",
            )),
        }
    }

    /// Deprecated: use begin_authentication.
    async fn begin_login(&self, _req: BeginLoginRequest) -> Result<BeginLoginResponse> {
        Err(Error::unimplemented(
            "authentication provider does not implement begin_login",
        ))
    }

    /// Deprecated: use complete_authentication.
    async fn complete_login(&self, _req: CompleteLoginRequest) -> Result<AuthenticatedUser> {
        Err(Error::unimplemented(
            "authentication provider does not implement complete_login",
        ))
    }

    /// Deprecated: use authenticate with TokenAuthInput.
    async fn validate_external_token(&self, _token: &str) -> Result<Option<AuthenticatedUser>> {
        Err(Error::unimplemented(
            "authentication provider does not support external token validation",
        ))
    }

    /// Returns the TTL the host should use for persisted sessions.
    fn session_ttl(&self) -> Option<Duration> {
        None
    }
}
