#![doc = include_str!("../README.md")]

mod api;
mod auth;
mod auth_server;
mod catalog;
mod datastore;
mod datastore_server;
mod env;
mod error;
mod provider_server;
mod router;
mod rpc_status;
mod runtime_server;
mod runtime_types;

pub mod runtime;

/// The shared protobuf package name used by the Gestalt SDK protocol surface.
pub const PROTO_PACKAGE: &str = "gestalt.plugin.v1";

/// Generated protobuf and gRPC bindings compiled from `sdk/proto/v1/*.proto`.
mod generated {
    pub mod v1 {
        tonic::include_proto!("gestalt.plugin.v1");
    }
}

pub mod proto {
    pub use crate::generated::v1;
}

pub use api::{IntoResponse, Provider, Request, Response, ok};
pub use async_trait::async_trait;
pub use auth::{
    AuthProvider, AuthenticatedUser, BeginLoginRequest, BeginLoginResponse, CompleteLoginRequest,
};
pub use catalog::{
    Catalog, CatalogOperation, CatalogParameter, OperationAnnotations, write_catalog,
};
pub use datastore::{
    DatastoreProvider, OAuthRegistration, StoredApiToken, StoredIntegrationToken, StoredUser,
};
pub use env::{
    CURRENT_PROTOCOL_VERSION, ENV_PLUGIN_NAME, ENV_PLUGIN_PARENT_PID, ENV_PLUGIN_SOCKET,
    ENV_WRITE_CATALOG,
};
pub use error::{Error, Result};
pub use provider_server::{OperationResult, ProviderServer};
pub use router::{Operation, Router};
pub use runtime_types::RuntimeMetadata;

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
macro_rules! export_datastore_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_datastore(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_datastore_provider(provider)
        }
    };
}
