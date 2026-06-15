use std::sync::Arc;

use tonic::codegen::async_trait;

use crate::authentication::{
    AuthorizeRequest, AuthorizeResponse, GetGrantRequest, GetGrantResponse, IntrospectRequest,
    IntrospectResponse, ListGrantsRequest, ListGrantsResponse, RevokeGrantRequest,
    RevokeGrantResponse, TokenRequest, TokenResponse,
};
use crate::error::Result;

pub const CALLER_BEARER_TOKEN_METADATA_KEY: &str = "x-gestalt-caller-bearer-token";

pub const GRANT_TYPE_AUTHORIZATION_CODE: &str = "authorization_code";
pub const GRANT_TYPE_TOKEN_EXCHANGE: &str = "urn:ietf:params:oauth:grant-type:token-exchange";
pub const SUBJECT_TOKEN_TYPE_ACCESS_TOKEN: &str = "urn:ietf:params:oauth:token-type:access_token";

/// Caller-scoped authentication metadata for grant-management RPCs.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct AuthCallContext {
    /// The caller bearer token from gRPC metadata.
    pub caller_bearer_token: String,
}

#[async_trait]
/// Lifecycle and authentication contract for Gestalt authentication providers.
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
    fn metadata(&self) -> Option<crate::api::RuntimeMetadata> {
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

    /// Starts provider-owned background work after configuration.
    async fn start(&self) -> Result<()> {
        Ok(())
    }

    /// Shuts the provider down before the runtime exits.
    async fn close(&self) -> Result<()> {
        Ok(())
    }

    /// Starts an RFC 6749 authorization flow.
    async fn authorize(&self, req: AuthorizeRequest) -> Result<AuthorizeResponse>;

    /// Issues or exchanges tokens via the RFC 6749 token endpoint.
    async fn token(&self, req: TokenRequest) -> Result<TokenResponse>;

    /// Introspects a bearer token via RFC 7662.
    async fn introspect(&self, req: IntrospectRequest) -> Result<IntrospectResponse>;

    /// Lists grant IDs visible to the caller.
    async fn list_grants(
        &self,
        call: AuthCallContext,
        req: ListGrantsRequest,
    ) -> Result<ListGrantsResponse>;

    /// Returns one grant owned by the caller.
    async fn get_grant(
        &self,
        call: AuthCallContext,
        req: GetGrantRequest,
    ) -> Result<GetGrantResponse>;

    /// Revokes one grant owned by the caller.
    async fn revoke_grant(
        &self,
        call: AuthCallContext,
        req: RevokeGrantRequest,
    ) -> Result<RevokeGrantResponse>;
}

pub(crate) fn caller_bearer_token_from_metadata(
    metadata: &tonic::metadata::MetadataMap,
) -> String {
    metadata
        .get(CALLER_BEARER_TOKEN_METADATA_KEY)
        .and_then(|value| value.to_str().ok())
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(str::to_owned)
        .unwrap_or_default()
}

pub(crate) struct AuthProviderState<P> {
    pub provider: Arc<P>,
    pub call: AuthCallContext,
}

impl<P> AuthProviderState<P> {
    pub fn new(provider: Arc<P>, call: AuthCallContext) -> Self {
        Self { provider, call }
    }
}
