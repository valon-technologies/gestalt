use std::time::Duration;

use tonic::codegen::async_trait;

use crate::api::RuntimeMetadata;
use crate::error::{Error, Result};
pub use crate::generated::v1::{
    AuthenticatedUser, BeginLoginRequest, BeginLoginResponse, CompleteLoginRequest,
};

#[async_trait]
/// Lifecycle and login contract for Gestalt auth providers.
pub trait AuthProvider: Send + Sync + 'static {
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

    /// Starts an interactive login flow.
    async fn begin_login(&self, req: BeginLoginRequest) -> Result<BeginLoginResponse>;

    /// Finishes an interactive login flow.
    async fn complete_login(&self, req: CompleteLoginRequest) -> Result<AuthenticatedUser>;

    /// Validates an externally minted token when supported.
    async fn validate_external_token(&self, _token: &str) -> Result<Option<AuthenticatedUser>> {
        Err(Error::unimplemented(
            "auth provider does not support external token validation",
        ))
    }

    /// Returns the TTL the host should use for persisted sessions.
    fn session_ttl(&self) -> Option<Duration> {
        None
    }
}
