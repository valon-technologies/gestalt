//! Factory for the public Gestalt transport client.

use std::sync::Arc;

use crate::public::auth::Auth;
use crate::public::generated::app_client::AppClient;
use crate::public::generated::rpc_support::GestaltError;
use crate::public::grpc_transport::{GrpcTransport, dial_public_grpc};
use crate::public::rest_transport::RestTransport;
use crate::rpc_support::gestalt_error_code;

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
    Grpc(Box<AppClient<GrpcTransport>>),
}

/// Creates a public Gestalt client for the requested transport.
pub async fn create_gestalt_client<A: Auth + 'static>(
    address: impl Into<String>,
    auth: A,
    transport: Transport,
) -> Result<GestaltClient, GestaltError> {
    let address = normalize_address(address.into())?;
    let auth: Arc<dyn Auth> = Arc::new(auth);
    match transport {
        Transport::Rest => Ok(GestaltClient::Rest(AppClient::new(RestTransport::new(
            address,
            Arc::clone(&auth),
        )))),
        Transport::Grpc => {
            let channel = dial_public_grpc(&address)?;
            Ok(GestaltClient::Grpc(Box::new(AppClient::new(
                GrpcTransport::new(channel, auth),
            ))))
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
