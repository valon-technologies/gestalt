use std::time::Duration;

use tonic::codegen::async_trait;

use crate::api::RuntimeMetadata;
use crate::error::{Error, Result};
pub use crate::generated::v1::{
    AuthenticatedUser, BeginLoginRequest, BeginLoginResponse, CompleteLoginRequest,
};

#[async_trait]
pub trait AuthProvider: Send + Sync + 'static {
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> Result<()> {
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        None
    }

    fn warnings(&self) -> Vec<String> {
        Vec::new()
    }

    async fn health_check(&self) -> Result<()> {
        Ok(())
    }

    async fn close(&self) -> Result<()> {
        Ok(())
    }

    async fn begin_login(&self, req: BeginLoginRequest) -> Result<BeginLoginResponse>;

    async fn complete_login(&self, req: CompleteLoginRequest) -> Result<AuthenticatedUser>;

    async fn validate_external_token(&self, _token: &str) -> Result<Option<AuthenticatedUser>> {
        Err(Error::unimplemented(
            "auth provider does not support external token validation",
        ))
    }

    fn session_ttl(&self) -> Option<Duration> {
        None
    }
}
