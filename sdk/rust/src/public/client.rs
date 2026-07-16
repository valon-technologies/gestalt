//! Factory for the public Gestalt transport client.

use std::sync::Arc;

use tonic::transport::Channel;

use crate::generated::v1::RequestContext;
use crate::public::auth::Auth;
use crate::public::bound::dial_public_grpc;
use crate::public::generated::{
    agent::{AgentGRPC, AgentREST},
    app::{AppGRPC, AppREST},
    authorization::{AuthorizationGRPC, AuthorizationREST},
    external_credential::ExternalCredentialsGRPC,
    identity::{IdentityGRPC, IdentityREST},
    indexeddb::IndexedDBGRPC,
    workflow::{WorkflowGRPC, WorkflowREST},
};
use crate::public::rest_transport::RestTransport;
use crate::rpc_support::GestaltError;

/// REST service clients backed by one transport.
pub struct GestaltRestClient {
    /// Shared REST transport used by all service clients.
    pub transport: Arc<RestTransport>,
    /// App service REST client.
    pub app: AppREST,
    /// Agent service REST client.
    pub agent: AgentREST,
    /// Workflow service REST client.
    pub workflow: WorkflowREST,
    /// Identity service REST client.
    pub identity: IdentityREST,
    /// Authorization service REST client.
    pub authorization: AuthorizationREST,
}

/// gRPC service clients backed by one connection.
pub struct GestaltGrpcClient {
    /// App service gRPC client.
    pub app: AppGRPC,
    /// Agent service gRPC client.
    pub agent: AgentGRPC,
    /// Workflow service gRPC client.
    pub workflow: WorkflowGRPC,
    /// Identity service gRPC client.
    pub identity: IdentityGRPC,
    /// Authorization service gRPC client.
    pub authorization: AuthorizationGRPC,
    /// IndexedDB service gRPC client.
    pub indexed_db: IndexedDBGRPC,
    /// External credentials service gRPC client.
    pub external_credentials: ExternalCredentialsGRPC,
}

/// Transport selector for [`create_gestalt_client`].
pub enum Transport {
    /// REST over `/api/v2`.
    Rest,
    /// gRPC over the configured address.
    Grpc,
}

/// Public Gestalt client variants.
pub enum Client {
    /// REST-backed client.
    Rest(GestaltRestClient),
    /// gRPC-backed client.
    Grpc(Box<GestaltGrpcClient>),
}

/// Creates a public Gestalt client for the requested transport.
pub fn create_gestalt_client(
    address: impl Into<String>,
    auth: Arc<dyn Auth>,
    transport: Transport,
) -> Result<Client, GestaltError> {
    let address = normalize_address(address.into())?;
    match transport {
        Transport::Rest => {
            let rest = Arc::new(RestTransport::new(address, Arc::clone(&auth)));
            Ok(Client::Rest(GestaltRestClient {
                app: AppREST::new(Arc::clone(&rest)),
                agent: AgentREST::new(Arc::clone(&rest)),
                workflow: WorkflowREST::new(Arc::clone(&rest)),
                identity: IdentityREST::new(Arc::clone(&rest)),
                authorization: AuthorizationREST::new(Arc::clone(&rest)),
                transport: rest,
            }))
        }
        Transport::Grpc => {
            let channel = dial_public_grpc(&address)?;
            Ok(Client::Grpc(Box::new(grpc_clients(channel, auth, None))))
        }
    }
}

pub(crate) fn grpc_clients(
    channel: Channel,
    auth: Arc<dyn Auth>,
    request_context: Option<RequestContext>,
) -> GestaltGrpcClient {
    GestaltGrpcClient {
        app: AppGRPC::with_request_context(
            channel.clone(),
            Arc::clone(&auth),
            request_context.clone(),
        ),
        agent: AgentGRPC::with_request_context(
            channel.clone(),
            Arc::clone(&auth),
            request_context.clone(),
        ),
        workflow: WorkflowGRPC::with_request_context(
            channel.clone(),
            Arc::clone(&auth),
            request_context.clone(),
        ),
        identity: IdentityGRPC::new(channel.clone(), Arc::clone(&auth)),
        authorization: AuthorizationGRPC::new(channel.clone(), Arc::clone(&auth)),
        indexed_db: IndexedDBGRPC::new(channel.clone(), Arc::clone(&auth)),
        external_credentials: ExternalCredentialsGRPC::new(channel, auth),
    }
}

fn normalize_address(address: String) -> Result<String, GestaltError> {
    let address = address.trim();
    if address.is_empty() {
        return Err(GestaltError::new(
            crate::rpc_support::gestalt_error_code::INVALID_ARGUMENT,
            "address is required for external clients (use gestalt_from_context for bound provider access)",
        ));
    }
    Ok(address.trim_end_matches('/').to_string())
}
