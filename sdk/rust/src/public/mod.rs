//! Public gestaltd transport client for external applications.

pub mod auth;
pub mod bound;
pub mod client;
pub mod errors;
pub mod generated;
pub mod grpc_auth;
pub(crate) mod proto_json;
#[allow(missing_docs)]
pub mod rest_mapping;
pub mod rest_transport;

pub use bound::gestalt_from_context;
pub use client::create_gestalt_client;
pub use rest_transport::RestTransport;
