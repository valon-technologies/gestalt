#![warn(rustdoc::broken_intra_doc_links)]
#![doc = include_str!("../README.md")]

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
mod provider_server;
mod router;
mod rpc_status;
/// Runtime entrypoints for serving Gestalt provider surfaces over Unix sockets.
pub mod runtime;
mod runtime_server;
/// S3-compatible client and provider helpers.
pub mod s3;
mod secrets;
mod secrets_server;
mod workflow;
mod workflow_manager;

/// Generated protobuf and gRPC bindings for the Gestalt provider protocol.
pub mod proto {
    pub use crate::generated::v1;
}

pub use api::{Access, Credential, Provider, Request, Response, RuntimeMetadata, Subject, ok};
pub use auth::{
    AuthenticatedUser, AuthenticationProvider, BeginLoginRequest, BeginLoginResponse,
    CompleteLoginRequest,
};
pub use cache::{
    Cache, CacheEntry, CacheError, CacheProvider, CacheSetOptions, ENV_CACHE_SOCKET,
    cache_socket_env,
};
pub use catalog::{Catalog, CatalogOperation};
pub use env::{CURRENT_PROTOCOL_VERSION, ENV_PROVIDER_SOCKET};
pub use error::{Error, Result};
pub use indexeddb::{Cursor, CursorDirection, IndexedDB, IndexedDBError};
pub use invoker::{
    ENV_PLUGIN_INVOKER_SOCKET, InvocationGrant, InvokeOptions, PluginInvoker, PluginInvokerError,
};
#[doc(hidden)]
pub use provider_server::{OperationResult, ProviderServer};
pub use router::{Operation, Router};
pub use s3::{S3, S3Error, S3Provider};
pub use secrets::SecretsProvider;
pub use tonic::codegen::async_trait;
pub use workflow::{ENV_WORKFLOW_HOST_SOCKET, WorkflowHost, WorkflowHostError, WorkflowProvider};
pub use workflow_manager::{ENV_WORKFLOW_MANAGER_SOCKET, WorkflowManager, WorkflowManagerError};

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

#[macro_export]
macro_rules! export_workflow_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_workflow(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_workflow_provider(provider)
        }
    };
}
