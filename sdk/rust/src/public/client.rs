//! Factory for the public Gestalt transport client.

use std::sync::Arc;
use std::time::Duration;

use crate::public::auth::{Auth, NoAuth};
use crate::public::generated::app_client::AppClient;
use crate::public::generated::rpc_support::GestaltError;
use crate::public::grpc_transport::{GrpcTransport, dial_public_grpc};
use crate::public::rest_transport::RestTransport;
use crate::rpc_support::gestalt_error_code;

/// REST transport selector for [`create_gestalt_client`].
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct Rest;

/// gRPC transport selector for [`create_gestalt_client`].
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct Grpc;

/// Transport selector for [`create_gestalt_client`].
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Transport {
    /// REST over `/api/v2`.
    Rest,
    /// gRPC over the configured address.
    Grpc,
}

/// Returns the REST transport selector.
pub fn rest() -> Transport {
    Transport::Rest
}

/// Returns the gRPC transport selector.
pub fn grpc() -> Transport {
    Transport::Grpc
}

/// External public Gestalt client variants.
pub enum GestaltClient {
    /// REST-backed client.
    Rest(AppClient<RestTransport>),
    /// gRPC-backed client.
    Grpc(AppClient<GrpcTransport>),
}

impl GestaltClient {
    /// Returns the generated App client for either transport.
    pub fn app(&self) -> AppClientRef<'_> {
        match self {
            Self::Rest(client) => AppClientRef::Rest(client),
            Self::Grpc(client) => AppClientRef::Grpc(client),
        }
    }
}

/// Borrowed App client reference across transport variants.
pub enum AppClientRef<'a> {
    /// REST-backed App client.
    Rest(&'a AppClient<RestTransport>),
    /// gRPC-backed App client.
    Grpc(&'a AppClient<GrpcTransport>),
}

/// Creates a public Gestalt client for the requested transport.
pub async fn create_gestalt_client<A: Auth + 'static>(
    address: impl Into<String>,
    auth: A,
    transport: Transport,
) -> Result<GestaltClient, GestaltError> {
    create_gestalt_client_with_timeout(address, auth, transport, None).await
}

/// Creates a public Gestalt client with an optional per-request timeout.
pub async fn create_gestalt_client_with_timeout<A: Auth + 'static>(
    address: impl Into<String>,
    auth: A,
    transport: Transport,
    timeout: Option<Duration>,
) -> Result<GestaltClient, GestaltError> {
    let address = normalize_address(address.into())?;
    let auth: Arc<dyn Auth> = Arc::new(auth);
    match transport {
        Transport::Rest => {
            let mut rest = RestTransport::new(address, Arc::clone(&auth));
            if let Some(timeout) = timeout {
                rest = rest.with_timeout(timeout);
            }
            Ok(GestaltClient::Rest(AppClient::new(rest)))
        }
        Transport::Grpc => {
            let channel = dial_public_grpc(&address)?;
            let mut grpc = GrpcTransport::new(channel, auth);
            if let Some(timeout) = timeout {
                grpc = grpc.with_timeout(timeout);
            }
            Ok(GestaltClient::Grpc(AppClient::new(grpc)))
        }
    }
}

fn normalize_address(address: String) -> Result<String, GestaltError> {
    let address = address.trim();
    if address.is_empty() {
        return Err(GestaltError::new(
            gestalt_error_code::INVALID_ARGUMENT,
            "address is required for external clients (use gestalt_from_context for bound provider access)",
        ));
    }
    Ok(address.trim_end_matches('/').to_string())
}

/// Creates an unauthenticated REST client for testing.
pub async fn create_unauthenticated_rest_client(
    address: impl Into<String>,
) -> Result<GestaltClient, GestaltError> {
    create_gestalt_client(address, NoAuth, Transport::Rest).await
}
