//! Public gestaltd transport client for external applications.

pub mod auth;
pub mod bound;
pub mod client;
pub mod errors;
pub mod generated;
pub mod grpc_transport;
pub(crate) mod proto_json;
pub mod rest_mapping;
pub(crate) mod rest_request;
pub mod rest_transport;

pub use auth::{Auth, BearerAuth, NoAuth};
pub use bound::{BoundGestaltClient, gestalt_from_context};
pub use client::{
    GestaltClient, GrpcGestaltClient, RestGestaltClient, Transport, create_gestalt_client,
    create_grpc_gestalt_client, create_rest_gestalt_client, grpc, rest,
};
pub use grpc_transport::{GrpcTransport, dial_public_grpc};
pub use rest_transport::{RestTransport, SyncRestTransport};
