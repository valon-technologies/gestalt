//! Factory for the public Gestalt transport client.

use std::ops::Deref;
use std::sync::Arc;

use crate::public::auth::Auth;
use crate::public::generated::app_client::{
    AgentClient, AppClient, AuthorizationClient, ExternalCredentialsClient, IdentityClient,
    IndexedDBClient, WorkflowClient,
};
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

/// REST-backed aggregate public client.
#[allow(missing_docs)]
pub struct RestGestaltClient {
    pub app: AppClient<RestTransport>,
    pub agent: AgentClient<RestTransport>,
    pub authorization: AuthorizationClient<RestTransport>,
    pub identity: IdentityClient<RestTransport>,
    pub workflow: WorkflowClient<RestTransport>,
}

impl Deref for RestGestaltClient {
    type Target = AppClient<RestTransport>;

    fn deref(&self) -> &Self::Target {
        &self.app
    }
}

/// gRPC-backed aggregate public client.
#[allow(missing_docs)]
pub struct GrpcGestaltClient {
    pub app: AppClient<GrpcTransport>,
    pub agent: AgentClient<GrpcTransport>,
    pub authorization: AuthorizationClient<GrpcTransport>,
    pub external_credentials: ExternalCredentialsClient<GrpcTransport>,
    pub identity: IdentityClient<GrpcTransport>,
    pub indexed_db: IndexedDBClient<GrpcTransport>,
    pub workflow: WorkflowClient<GrpcTransport>,
}

impl Deref for GrpcGestaltClient {
    type Target = AppClient<GrpcTransport>;

    fn deref(&self) -> &Self::Target {
        &self.app
    }
}

/// External public Gestalt client variants.
pub enum GestaltClient {
    /// REST-backed client.
    Rest(Box<RestGestaltClient>),
    /// gRPC-backed client.
    Grpc(Box<GrpcGestaltClient>),
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
        Transport::Rest => {
            let rest = RestTransport::new(address, Arc::clone(&auth));
            Ok(GestaltClient::Rest(Box::new(RestGestaltClient {
                app: AppClient::new(rest.clone()),
                agent: AgentClient::new(rest.clone()),
                authorization: AuthorizationClient::new(rest.clone()),
                identity: IdentityClient::new(rest.clone()),
                workflow: WorkflowClient::new(rest),
            })))
        }
        Transport::Grpc => {
            let channel = dial_public_grpc(&address)?;
            let grpc = GrpcTransport::new(channel, auth);
            Ok(GestaltClient::Grpc(Box::new(GrpcGestaltClient {
                app: AppClient::new(grpc.clone()),
                agent: AgentClient::new(grpc.clone()),
                authorization: AuthorizationClient::new(grpc.clone()),
                external_credentials: ExternalCredentialsClient::new(grpc.clone()),
                identity: IdentityClient::new(grpc.clone()),
                indexed_db: IndexedDBClient::new(grpc.clone()),
                workflow: WorkflowClient::new(grpc),
            })))
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
