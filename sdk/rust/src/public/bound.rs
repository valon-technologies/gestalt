//! Bound provider gRPC client over the host-service relay.

use crate::api::current_request_context;
use crate::codec::host_service::connect_host_service;
use crate::public::generated::app_client::AppClient;
use crate::public::generated::rpc_support::GestaltError;
use crate::public::grpc_transport::GrpcTransport;
use crate::rpc_support::gestalt_error_code;

/// App-only public client bound to the provider host-service relay.
pub struct BoundGestaltClient {
    /// App service client.
    pub app: AppClient<GrpcTransport>,
}

/// Returns a gRPC public client bound to the host-service relay.
///
/// Bound clients derive trusted runtime context from the provider environment
/// and accept no external authentication options.
pub async fn gestalt_from_context() -> Result<BoundGestaltClient, GestaltError> {
    current_request_context().ok_or_else(|| {
        GestaltError::new(
            gestalt_error_code::FAILED_PRECONDITION,
            "gestalt_from_context must be called from a provider request scope",
        )
    })?;
    let channel = connect_host_service("app", "").await?;
    let transport = GrpcTransport::from_host_service(channel);
    Ok(BoundGestaltClient {
        app: AppClient::new(transport),
    })
}
