#![doc = include_str!("../README.md")]

mod api;
mod auth;
mod auth_server;
mod catalog;
mod env;
mod error;
pub mod indexeddb;
mod provider_server;
mod router;
mod rpc_status;
pub mod runtime;
mod runtime_server;
mod secrets;
mod secrets_server;

/// Generated protobuf and gRPC bindings compiled from `sdk/proto/v1/*.proto`.
mod generated {
    pub mod v1 {
        tonic::include_proto!("gestalt.provider.v1");
    }
}

pub mod proto {
    pub use crate::generated::v1;
}

pub use api::RuntimeMetadata;
pub use api::{Provider, Request, Response, ok};
pub use auth::{
    AuthProvider, AuthenticatedUser, BeginLoginRequest, BeginLoginResponse, CompleteLoginRequest,
};
pub use catalog::{Catalog, CatalogOperation};
pub use env::{CURRENT_PROTOCOL_VERSION, ENV_PROVIDER_SOCKET};
pub use error::{Error, Result};
pub use indexeddb::{IndexedDB, IndexedDBError};
#[doc(hidden)]
pub use provider_server::{OperationResult, ProviderServer};
pub use router::{Operation, Router};
pub use secrets::SecretsProvider;
pub use tonic::codegen::async_trait;

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
pub fn into_router_result<P, R>(router: R) -> Result<Router<P>>
where
    R: IntoRouterResult<P>,
{
    router.into_router_result()
}

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

#[macro_export]
macro_rules! export_auth_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_auth(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_auth_provider(provider)
        }
    };
}

#[macro_export]
macro_rules! export_secrets_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_secrets(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_secrets_provider(provider)
        }
    };
}
