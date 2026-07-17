//! Factory for the public Gestalt transport client.

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

/// App-only external public Gestalt client variants.
pub enum GestaltClient {
    /// REST-backed client.
    Rest(AppClient<RestTransport>),
    /// gRPC-backed client.
    Grpc(Box<AppClient<GrpcTransport>>),
}

/// REST-backed public Gestalt client (five REST-capable services).
pub struct RestGestaltClient {
    /// App service client.
    pub app: AppClient<RestTransport>,
    /// Agent service client.
    pub agent: AgentClient<RestTransport>,
    /// Workflow service client.
    pub workflow: WorkflowClient<RestTransport>,
    /// Identity service client.
    pub identity: IdentityClient<RestTransport>,
    /// Authorization service client.
    pub authorization: AuthorizationClient<RestTransport>,
}

impl RestGestaltClient {
    /// Releases transport resources when the client owns them.
    pub fn close(self) {}
}

/// gRPC-backed public Gestalt client (all seven public services).
pub struct GrpcGestaltClient {
    /// App service client.
    pub app: AppClient<GrpcTransport>,
    /// Agent service client.
    pub agent: AgentClient<GrpcTransport>,
    /// Workflow service client.
    pub workflow: WorkflowClient<GrpcTransport>,
    /// Identity service client.
    pub identity: IdentityClient<GrpcTransport>,
    /// Authorization service client.
    pub authorization: AuthorizationClient<GrpcTransport>,
    /// IndexedDB service client.
    pub indexed_db: IndexedDBClient<GrpcTransport>,
    /// External credentials service client.
    pub external_credentials: ExternalCredentialsClient<GrpcTransport>,
}

impl GrpcGestaltClient {
    /// Releases transport resources when the client owns them.
    pub fn close(self) {}
}

/// Creates an App-only public Gestalt client for the requested transport.
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

/// Creates a REST public Gestalt client.
pub async fn create_rest_gestalt_client<A: Auth + 'static>(
    address: impl Into<String>,
    auth: A,
) -> Result<RestGestaltClient, GestaltError> {
    let address = normalize_address(address.into())?;
    let auth: Arc<dyn Auth> = Arc::new(auth);
    Ok(bind_rest(RestTransport::new(address, auth)))
}

/// Creates a gRPC public Gestalt client.
pub async fn create_grpc_gestalt_client<A: Auth + 'static>(
    address: impl Into<String>,
    auth: A,
) -> Result<GrpcGestaltClient, GestaltError> {
    let address = normalize_address(address.into())?;
    let auth: Arc<dyn Auth> = Arc::new(auth);
    let channel = dial_public_grpc(&address)?;
    Ok(bind_grpc(GrpcTransport::new(channel, auth)))
}

fn bind_rest(transport: RestTransport) -> RestGestaltClient {
    RestGestaltClient {
        app: AppClient::new(transport.clone()),
        agent: AgentClient::new(transport.clone()),
        workflow: WorkflowClient::new(transport.clone()),
        identity: IdentityClient::new(transport.clone()),
        authorization: AuthorizationClient::new(transport.clone()),
    }
}

fn bind_grpc(transport: GrpcTransport) -> GrpcGestaltClient {
    GrpcGestaltClient {
        app: AppClient::new(transport.clone()),
        agent: AgentClient::new(transport.clone()),
        workflow: WorkflowClient::new(transport.clone()),
        identity: IdentityClient::new(transport.clone()),
        authorization: AuthorizationClient::new(transport.clone()),
        indexed_db: IndexedDBClient::new(transport.clone()),
        external_credentials: ExternalCredentialsClient::new(transport),
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
