#![warn(rustdoc::broken_intra_doc_links)]
#![doc = include_str!("../README.md")]

mod agent;
mod agent_manager;
mod api;
mod auth;
mod auth_server;
mod cache;
mod cache_server;
mod catalog;
mod env;
mod error;
mod generated;
/// IndexedDB-style datastore client and provider helpers.
pub mod indexeddb;
mod invoker;
mod plugin_runtime;
mod provider_server;
mod router;
mod rpc_status;
/// Runtime entrypoints for serving Gestalt provider surfaces over Unix sockets.
pub mod runtime;
mod runtime_log_host;
mod runtime_server;
/// S3-compatible client and provider helpers.
pub mod s3;
mod secrets;
mod secrets_server;
/// OpenTelemetry helpers for provider-authored GenAI instrumentation.
pub mod telemetry;
mod workflow;
mod workflow_manager;

/// Generated protobuf and gRPC bindings for the Gestalt provider protocol.
pub mod proto {
    pub use crate::generated::v1;
}

pub use agent::{
    AgentHost, AgentHostError, AgentProvider, ENV_AGENT_HOST_SOCKET, ENV_AGENT_HOST_SOCKET_TOKEN,
};
pub use agent_manager::{
    AgentManager, AgentManagerError, ENV_AGENT_MANAGER_SOCKET, ENV_AGENT_MANAGER_SOCKET_TOKEN,
};
pub use api::{Access, Credential, Provider, Request, Response, RuntimeMetadata, Subject, ok};
pub use auth::{
    AuthenticatedUser, AuthenticationProvider, BeginLoginRequest, BeginLoginResponse,
    CompleteLoginRequest,
};
pub use cache::{
    Cache, CacheEntry, CacheError, CacheProvider, CacheSetOptions, ENV_CACHE_SOCKET,
    ENV_CACHE_SOCKET_TOKEN, cache_socket_env, cache_socket_token_env,
};
pub use catalog::{Catalog, CatalogOperation};
pub use env::{CURRENT_PROTOCOL_VERSION, ENV_PROVIDER_SOCKET};
pub use error::{Error, Result};
pub use indexeddb::{
    Cursor, CursorDirection, ENV_INDEXEDDB_SOCKET, IndexedDB, IndexedDBError, Transaction,
    TransactionDurabilityHint, TransactionIndexClient, TransactionMode, TransactionObjectStore,
    TransactionOptions, indexeddb_socket_env, indexeddb_socket_token_env,
};
pub use invoker::{
    ENV_PLUGIN_INVOKER_SOCKET, ENV_PLUGIN_INVOKER_SOCKET_TOKEN, InvocationGrant, InvokeOptions,
    PluginInvoker, PluginInvokerError,
};
pub use plugin_runtime::PluginRuntimeProvider;
#[doc(hidden)]
pub use provider_server::{OperationResult, ProviderServer};
pub use router::{Operation, Router};
pub use runtime_log_host::{
    ENV_RUNTIME_LOG_HOST_SOCKET, ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN, RuntimeLogHost,
    RuntimeLogHostError, RuntimeLogStream,
};
pub use s3::{
    ENV_S3_SOCKET, ENV_S3_SOCKET_TOKEN, S3, S3Error, S3Provider, s3_socket_env, s3_socket_token_env,
};
pub use secrets::SecretsProvider;
pub use tonic::codegen::async_trait;
pub use workflow::{ENV_WORKFLOW_HOST_SOCKET, WorkflowHost, WorkflowHostError, WorkflowProvider};
pub use workflow_manager::{
    ENV_WORKFLOW_MANAGER_SOCKET, ENV_WORKFLOW_MANAGER_SOCKET_TOKEN, WorkflowManager,
    WorkflowManagerError,
};

#[doc(hidden)]
pub trait IntoRouterResult<P> {
    fn into_router_result(self) -> Result<Router<P>>;
}

impl<P> IntoRouterResult<P> for Router<P> {
    fn into_router_result(self) -> Result<Router<P>> {
        Ok(self)
    }
}

impl<P> IntoRouterResult<P> for Result<Router<P>> {
    fn into_router_result(self) -> Result<Router<P>> {
        self
    }
}

#[doc(hidden)]
/// Converts router-like values used by the export macros into a [`Router`].
pub fn into_router_result<P, R>(router: R) -> Result<Router<P>>
where
    R: IntoRouterResult<P>,
{
    router.into_router_result()
}

/// Exports the integration-provider entrypoints expected by `gestaltd`.
#[macro_export]
macro_rules! export_provider {
    (constructor = $constructor:path, router = $router:path $(,)?) => {
        pub fn __gestalt_serve(name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            let router = $crate::into_router_result($router())?.with_name(name);
            $crate::runtime::run_provider(provider, router)
        }

        pub fn __gestalt_write_catalog(name: &str, path: &str) -> $crate::Result<()> {
            let router = $crate::into_router_result($router())?.with_name(name);
            $crate::runtime::write_catalog_path(&router, path)
        }
    };
}

/// Exports the authentication-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_authentication_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_authentication(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_authentication_provider(provider)
        }
    };
}

/// Exports the cache-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_cache_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_cache(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_cache_provider(provider)
        }
    };
}

/// Exports the secrets-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_secrets_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_secrets(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_secrets_provider(provider)
        }
    };
}

/// Exports the S3-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_s3_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_s3(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_s3_provider(provider)
        }
    };
}

/// Exports the plugin-runtime-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_plugin_runtime_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_runtime(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_plugin_runtime_provider(provider)
        }
    };
}

#[macro_export]
macro_rules! export_workflow_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_workflow(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_workflow_provider(provider)
        }
    };
}

#[macro_export]
macro_rules! export_agent_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_agent(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_agent_provider(provider)
        }
    };
}
