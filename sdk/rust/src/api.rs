use std::collections::BTreeMap;
use std::convert::Infallible;
use std::time::SystemTime;

use tonic::codegen::async_trait;

use crate::agent::AgentToolRef;
use crate::catalog::Catalog;
use crate::error::{Error, Result};

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Identifies the caller that initiated an operation.
pub struct Subject {
    /// Stable subject id.
    pub id: String,
    /// Subject kind, such as user or service account.
    pub kind: String,
    /// Subject id used for credential lookup, when different from the actor.
    pub credential_subject_id: String,
    /// Human-readable display name.
    pub display_name: String,
    /// Authentication source that produced the subject.
    pub auth_source: String,
    /// Email address resolved by the Gestalt host for user subjects.
    pub email: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Provider-owned identity attached to an incoming provider request.
pub struct ExternalIdentity {
    /// Provider identity namespace.
    pub r#type: String,
    /// Provider-owned identity id.
    pub id: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Describes the resolved credential used to authorize an operation.
pub struct Credential {
    /// Credential mode used by the host.
    pub mode: String,
    /// Subject id associated with the credential.
    pub subject_id: String,
    /// Connection id or name associated with the credential.
    pub connection: String,
    /// Provider instance id or name associated with the credential.
    pub instance: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Summarizes the host-side access decision attached to an operation.
pub struct Access {
    /// Policy name or id applied to the request.
    pub policy: String,
    /// Effective role granted to the request.
    pub role: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Describes public host metadata attached to a request.
pub struct Host {
    /// Public base URL for the Gestalt host.
    pub public_base_url: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Carries execution-scoped metadata into typed operation handlers.
pub struct Request {
    /// Request token supplied to hosted HTTP operation handlers.
    pub token: String,
    /// Connection parameters resolved by the host.
    pub connection_params: BTreeMap<String, String>,
    /// Subject that initiated the request.
    pub subject: Subject,
    /// Original agent caller when an agent tool runs as a delegated subject.
    pub agent_subject: Subject,
    /// Provider-owned external identity this request is authorized to assume.
    pub external_identity: ExternalIdentity,
    /// Original agent caller's provider-owned external identity, when known.
    pub agent_external_identity: ExternalIdentity,
    /// Credential used to authorize the request.
    pub credential: Credential,
    /// Access decision attached to the request.
    pub access: Access,
    /// Public host metadata attached to the request.
    pub host: Host,
    /// Idempotency key supplied by the host.
    pub idempotency_key: String,
    /// Workflow callback metadata uses a JSON-style lowerCamelCase object
    /// such as `runId`, `target.plugin.pluginName`, `trigger.scheduleId`, and
    /// `trigger.event.specVersion`.
    pub workflow: serde_json::Map<String, serde_json::Value>,
    /// Agent tool refs granted to the current operation request.
    pub tool_refs: Vec<AgentToolRef>,
    /// Whether the host attached a tool-ref context to this request.
    pub tool_refs_set: bool,
    /// Invocation token used to call host services.
    pub invocation_token: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Host-managed connection payload passed into post-connect hooks.
pub struct ConnectedToken {
    /// Stable connection token id.
    pub id: String,
    /// Subject id associated with the connected credential.
    pub subject_id: String,
    /// Provider integration name.
    pub integration: String,
    /// Connection id or name associated with the credential.
    pub connection: String,
    /// Provider instance id or name associated with the credential.
    pub instance: String,
    /// Access token stored by the host for the connected credential.
    pub access_token: String,
    /// Refresh token stored by the host for the connected credential.
    pub refresh_token: String,
    /// Space- or provider-formatted scope string returned by the upstream provider.
    pub scopes: String,
    /// Access-token expiration timestamp, when known.
    pub expires_at: Option<SystemTime>,
    /// Last refresh timestamp, when known.
    pub last_refreshed_at: Option<SystemTime>,
    /// Consecutive refresh failure count tracked by the host.
    pub refresh_error_count: i32,
    /// Raw JSON metadata stored with the connected credential.
    pub metadata_json: String,
    /// String-valued metadata parsed from [`Self::metadata_json`].
    pub metadata: BTreeMap<String, String>,
    /// Credential creation timestamp, when known.
    pub created_at: Option<SystemTime>,
    /// Credential update timestamp, when known.
    pub updated_at: Option<SystemTime>,
}

impl Request {
    /// Returns one resolved connection parameter by name.
    pub fn connection_param(&self, name: &str) -> Option<&str> {
        self.connection_params.get(name).map(String::as_str)
    }

    /// Returns the invocation token used to call host services.
    pub fn invocation_token(&self) -> &str {
        &self.invocation_token
    }

    /// Creates a plugin invoker using this request's invocation token.
    pub async fn invoker(
        &self,
    ) -> std::result::Result<crate::PluginInvoker, crate::PluginInvokerError> {
        crate::PluginInvoker::connect(self.invocation_token()).await
    }

    /// Creates a workflow manager using this request's invocation token.
    pub async fn workflow_manager(
        &self,
    ) -> std::result::Result<crate::WorkflowManager, crate::WorkflowManagerError> {
        crate::WorkflowManager::connect_with_idempotency_key(
            self.invocation_token(),
            self.idempotency_key.trim(),
        )
        .await
    }

    /// Creates an agent manager using this request's invocation token.
    pub async fn agent_manager(
        &self,
    ) -> std::result::Result<crate::AgentManager, crate::AgentManagerError> {
        crate::AgentManager::connect(self.invocation_token()).await
    }
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Carries one verified hosted HTTP request into a provider subject resolver.
pub struct HTTPSubjectRequest {
    /// Hosted HTTP binding name from the plugin manifest.
    pub binding: String,
    /// HTTP method used for the inbound request.
    pub method: String,
    /// Request path received by the hosted HTTP binding.
    pub path: String,
    /// Request content type.
    pub content_type: String,
    /// Request headers after host-side verification.
    pub headers: BTreeMap<String, Vec<String>>,
    /// Request query parameters.
    pub query: BTreeMap<String, Vec<String>>,
    /// Decoded request parameters.
    pub params: serde_json::Map<String, serde_json::Value>,
    /// Raw request body bytes.
    pub raw_body: Vec<u8>,
    /// Security scheme used to verify the request.
    pub security_scheme: String,
    /// Subject string verified by the security scheme, when available.
    pub verified_subject: String,
    /// Claims verified by the security scheme.
    pub verified_claims: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
/// Wraps a typed handler response plus an optional explicit HTTP status code.
pub struct Response<T> {
    /// Optional explicit HTTP-style status code.
    pub status: Option<u16>,
    /// Typed response body returned by the handler.
    pub body: T,
}

impl<T> Response<T> {
    /// Creates a response with an explicit HTTP status code.
    pub fn new(status: u16, body: T) -> Self {
        Self {
            status: Some(status),
            body,
        }
    }
}

/// Returns a successful JSON response with status code `200`.
pub fn ok<T>(body: T) -> Response<T> {
    Response::new(200, body)
}

/// Converts handler return values into a typed [`Response`].
pub trait IntoResponse<T> {
    /// Converts a handler return value into a typed response wrapper.
    fn into_response(self) -> Response<T>;
}

impl<T> IntoResponse<T> for Response<T> {
    fn into_response(self) -> Response<T> {
        self
    }
}

impl<T> IntoResponse<T> for T {
    fn into_response(self) -> Response<T> {
        ok(self)
    }
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Describes provider metadata that should be surfaced by the runtime.
pub struct RuntimeMetadata {
    /// Provider name to report to the host.
    pub name: String,
    /// Human-readable provider display name.
    pub display_name: String,
    /// Human-readable provider description.
    pub description: String,
    /// Provider version string.
    pub version: String,
}

#[async_trait]
/// Shared lifecycle contract for Gestalt integration providers.
pub trait Provider: Send + Sync + 'static {
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

    /// Reports whether this provider can derive additional operations from the
    /// current request context.
    fn supports_session_catalog(&self) -> bool {
        false
    }

    /// Returns an optional request-scoped catalog extension.
    async fn catalog_for_request(&self, _request: &Request) -> Result<Option<Catalog>> {
        Ok(None)
    }

    /// Resolves a hosted HTTP request to a concrete subject before dispatch.
    async fn resolve_http_subject(
        &self,
        _request: HTTPSubjectRequest,
        _context: &Request,
    ) -> Result<Option<Subject>> {
        Ok(None)
    }

    /// Reports whether this provider can derive additional connection metadata
    /// after the host completes the credential exchange.
    fn supports_post_connect(&self) -> bool {
        false
    }

    /// Returns provider-defined, non-secret connection metadata to persist after
    /// a successful credential exchange.
    async fn post_connect(&self, _token: &ConnectedToken) -> Result<BTreeMap<String, String>> {
        Ok(BTreeMap::new())
    }

    /// Shuts the provider down before the runtime exits.
    async fn close(&self) -> Result<()> {
        Ok(())
    }
}

impl From<Infallible> for Error {
    fn from(_value: Infallible) -> Self {
        Error::internal("unreachable infallible error")
    }
}
