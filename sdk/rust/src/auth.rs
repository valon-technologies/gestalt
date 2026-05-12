use std::time::Duration;

use tonic::codegen::async_trait;

use crate::api::RuntimeMetadata;
use crate::error::{Error, Result};

/// Normalized user identity returned by an authentication provider.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct AuthenticatedUser {
    pub subject: String,
    pub email: String,
    pub email_verified: bool,
    pub display_name: String,
    pub avatar_url: String,
    pub claims: std::collections::BTreeMap<String, String>,
}

/// Starts an interactive login flow.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct BeginLoginRequest {
    pub callback_url: String,
    pub host_state: String,
    pub scopes: Vec<String>,
    pub options: std::collections::BTreeMap<String, String>,
}

/// Provider-managed authorization URL and opaque state for login completion.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct BeginLoginResponse {
    pub authorization_url: String,
    pub provider_state: Vec<u8>,
}

/// Finishes an interactive login flow.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct CompleteLoginRequest {
    pub query: std::collections::BTreeMap<String, String>,
    pub provider_state: Vec<u8>,
    pub callback_url: String,
}

/// Host persistence settings for authenticated sessions.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct AuthSessionSettings {
    pub session_ttl: Duration,
}

pub(crate) fn begin_login_request_from_proto(
    value: crate::generated::v1::BeginLoginRequest,
) -> BeginLoginRequest {
    BeginLoginRequest {
        callback_url: value.callback_url,
        host_state: value.host_state,
        scopes: value.scopes,
        options: value.options,
    }
}

pub(crate) fn begin_login_response_to_proto(
    value: BeginLoginResponse,
) -> crate::generated::v1::BeginLoginResponse {
    crate::generated::v1::BeginLoginResponse {
        authorization_url: value.authorization_url,
        provider_state: value.provider_state,
    }
}

pub(crate) fn complete_login_request_from_proto(
    value: crate::generated::v1::CompleteLoginRequest,
) -> CompleteLoginRequest {
    CompleteLoginRequest {
        query: value.query,
        provider_state: value.provider_state,
        callback_url: value.callback_url,
    }
}

pub(crate) fn authenticated_user_to_proto(
    value: AuthenticatedUser,
) -> crate::generated::v1::AuthenticatedUser {
    crate::generated::v1::AuthenticatedUser {
        subject: value.subject,
        email: value.email,
        email_verified: value.email_verified,
        display_name: value.display_name,
        avatar_url: value.avatar_url,
        claims: value.claims,
    }
}

pub(crate) fn auth_session_settings_to_proto(
    value: AuthSessionSettings,
) -> crate::generated::v1::AuthSessionSettings {
    crate::generated::v1::AuthSessionSettings {
        session_ttl_seconds: i64::try_from(value.session_ttl.as_secs()).unwrap_or(i64::MAX),
    }
}

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

    /// Starts provider-owned background work after configuration.
    async fn start(&self) -> Result<()> {
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
            "authentication provider does not support external token validation",
        ))
    }

    /// Returns host persistence settings for authenticated sessions.
    fn session_settings(&self) -> Option<AuthSessionSettings> {
        None
    }

    /// Returns the TTL the host should use for persisted sessions.
    ///
    /// Prefer overriding [`AuthenticationProvider::session_settings`] for new providers.
    fn session_ttl(&self) -> Option<Duration> {
        None
    }
}
