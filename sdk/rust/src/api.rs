use std::collections::BTreeMap;
use std::convert::Infallible;

use tonic::codegen::async_trait;

use crate::catalog::Catalog;
use crate::error::{Error, Result};

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Identifies the caller that initiated an operation.
pub struct Subject {
    pub id: String,
    pub kind: String,
    pub display_name: String,
    pub auth_source: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Describes the resolved credential used to authorize an operation.
pub struct Credential {
    pub mode: String,
    pub subject_id: String,
    pub connection: String,
    pub instance: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Summarizes the host-side access decision attached to an operation.
pub struct Access {
    pub policy: String,
    pub role: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Carries execution-scoped metadata into typed operation handlers.
pub struct Request {
    pub token: String,
    pub connection_params: BTreeMap<String, String>,
    pub subject: Subject,
    pub credential: Credential,
    pub access: Access,
    /// Workflow callback metadata uses a JSON-style lowerCamelCase object
    /// such as `runId`, `target.plugin.pluginName`, `trigger.scheduleId`, and
    /// `trigger.event.specVersion`.
    pub workflow: serde_json::Map<String, serde_json::Value>,
    pub invocation_token: String,
}

impl Request {
    /// Returns one resolved connection parameter by name.
    pub fn connection_param(&self, name: &str) -> Option<&str> {
        self.connection_params.get(name).map(String::as_str)
    }

    pub fn invocation_token(&self) -> &str {
        &self.invocation_token
    }

    pub async fn invoker(
        &self,
    ) -> std::result::Result<crate::PluginInvoker, crate::PluginInvokerError> {
        crate::PluginInvoker::connect(self.invocation_token()).await
    }

    pub async fn workflow_manager(
        &self,
    ) -> std::result::Result<crate::WorkflowManager, crate::WorkflowManagerError> {
        crate::WorkflowManager::connect(self.invocation_token()).await
    }

    pub async fn agent_manager(
        &self,
    ) -> std::result::Result<crate::AgentManager, crate::AgentManagerError> {
        crate::AgentManager::connect(self.invocation_token()).await
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
/// Wraps a typed handler response plus an optional explicit HTTP status code.
pub struct Response<T> {
    pub status: Option<u16>,
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
    pub name: String,
    pub display_name: String,
    pub description: String,
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

    /// Reports whether this provider can derive additional operations from the
    /// current request context.
    fn supports_session_catalog(&self) -> bool {
        false
    }

    /// Returns an optional request-scoped catalog extension.
    async fn catalog_for_request(&self, _request: &Request) -> Result<Option<Catalog>> {
        Ok(None)
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
