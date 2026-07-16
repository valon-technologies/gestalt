//! Bound provider gRPC client over the host-service relay.

use std::sync::Arc;

use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tower::service_fn;

use crate::api::{current_caller_bearer_token, current_request_context};
use crate::env::{ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN};
use crate::generated::v1::RequestContext;
use crate::public::auth::Auth;
use crate::public::client::GestaltGrpcClient;
use crate::public::client::grpc_clients;
use crate::rpc_support::{GestaltError, gestalt_error_code};

const RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";
const CALLER_BEARER_TOKEN_HEADER: &str = "x-gestalt-caller-bearer-token";

/// Options for [`gestalt_from_context`].
pub struct BoundOptions {
    /// Provider request context injected into outgoing RPCs.
    pub request_context: Option<RequestContext>,
    /// Caller bearer token forwarded through gRPC metadata.
    pub caller_bearer_token: String,
}

/// Returns a gRPC client bound to the host-service relay.
pub async fn gestalt_from_context(
    options: BoundOptions,
) -> Result<GestaltGrpcClient, GestaltError> {
    let target = std::env::var(ENV_HOST_SERVICE_SOCKET).map_err(|_| {
        GestaltError::new(
            gestalt_error_code::FAILED_PRECONDITION,
            format!("{ENV_HOST_SERVICE_SOCKET} is not set"),
        )
    })?;
    let token = std::env::var(ENV_HOST_SERVICE_TOKEN).unwrap_or_default();
    let channel = connect_host_service(&target).await?;
    let request_context = options.request_context.or_else(current_request_context);
    let caller_bearer_token = {
        let explicit = options.caller_bearer_token.trim();
        if explicit.is_empty() {
            current_caller_bearer_token()
        } else {
            explicit.to_string()
        }
    };
    Ok(grpc_clients(
        channel,
        Arc::new(BoundRelayAuth {
            relay_token: token,
            caller_bearer_token,
        }),
        request_context,
    ))
}

#[derive(Clone)]
struct BoundRelayAuth {
    relay_token: String,
    caller_bearer_token: String,
}

impl Auth for BoundRelayAuth {
    fn authorization_header(&self) -> Option<String> {
        None
    }

    fn extra_metadata(&self) -> Vec<(&'static str, String)> {
        let mut out = Vec::new();
        let relay = self.relay_token.trim();
        if !relay.is_empty() {
            out.push((RELAY_TOKEN_HEADER, relay.to_string()));
        }
        let caller = self.caller_bearer_token.trim();
        if !caller.is_empty() {
            out.push((CALLER_BEARER_TOKEN_HEADER, caller.to_string()));
        }
        out
    }
}

async fn connect_host_service(target: &str) -> Result<Channel, GestaltError> {
    match parse_host_service_target(target)? {
        HostServiceTarget::Unix(path) => Endpoint::try_from("http://[::]:50051")
            .map_err(transport_error)?
            .connect_with_connector(service_fn(move |_: Uri| {
                let path = path.clone();
                async move { UnixStream::connect(path).await.map(TokioIo::new) }
            }))
            .await
            .map_err(transport_error),
        HostServiceTarget::Tcp(address) => Endpoint::from_shared(format!("http://{address}"))
            .map_err(transport_error)?
            .connect()
            .await
            .map_err(transport_error),
        HostServiceTarget::Tls(address) => Endpoint::from_shared(format!("https://{address}"))
            .map_err(transport_error)?
            .tls_config(ClientTlsConfig::new().with_native_roots())
            .map_err(transport_error)?
            .connect()
            .await
            .map_err(transport_error),
    }
}

enum HostServiceTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_host_service_target(raw: &str) -> Result<HostServiceTarget, GestaltError> {
    let target = raw.trim();
    if target.is_empty() {
        return Err(GestaltError::new(
            gestalt_error_code::INVALID_ARGUMENT,
            "host service target is required",
        ));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        return Ok(HostServiceTarget::Unix(path.to_string()));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        return Ok(HostServiceTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        return Ok(HostServiceTarget::Tls(address.to_string()));
    }
    Ok(HostServiceTarget::Unix(target.to_string()))
}

fn transport_error(err: tonic::transport::Error) -> GestaltError {
    GestaltError::new(gestalt_error_code::UNAVAILABLE, err.to_string())
}

/// Dial a public gRPC endpoint from an https:// or http:// address.
pub fn dial_public_grpc(address: &str) -> Result<Channel, GestaltError> {
    let address = address.trim().trim_end_matches('/');
    let endpoint = if address.starts_with("https://") {
        Endpoint::from_shared(address.to_string())
            .map_err(transport_error)?
            .tls_config(ClientTlsConfig::new().with_native_roots())
            .map_err(transport_error)?
    } else if address.starts_with("http://") {
        Endpoint::from_shared(address.to_string()).map_err(transport_error)?
    } else {
        return Err(GestaltError::new(
            gestalt_error_code::INVALID_ARGUMENT,
            format!("invalid gRPC address {address:?}"),
        ));
    };
    Ok(endpoint.connect_lazy())
}
