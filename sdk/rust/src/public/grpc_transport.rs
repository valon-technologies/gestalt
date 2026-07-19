//! tonic-based gRPC transports for the public Gestalt API.
//!
//! [`GrpcTransport`] is async (backed by `tonic`); [`SyncGrpcTransport`] is
//! sync (backed by `tonic` driven via `tokio::runtime::Runtime::block_on`).
//! Both share the inner unary call logic via a private `unary_grpc_call` helper.

use std::sync::Arc;
use std::time::Duration;

use http::uri::PathAndQuery;
use prost::Message;
use tonic::client::Grpc;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::Channel;
use tonic::{Request, Status};
use tonic_prost::ProstCodec;

use crate::codec::host_service::HostServiceChannel;
use crate::public::auth::Auth;
use crate::public::generated::metadata::Method;
use crate::public::generated::rpc_support::GestaltError;
use crate::public::generated::unary_transport::{
    GrpcCapable, SyncGrpcCapable, SyncUnaryTransport, UnaryTransport,
};
use crate::rpc_support::gestalt_error_code;

type AuthChannel = InterceptedService<Channel, AuthInterceptor>;

#[derive(Clone)]
enum GrpcService {
    Public(AuthChannel),
    Bound(HostServiceChannel),
}

/// gRPC transport implementing [`UnaryTransport`] for the full public surface.
#[derive(Clone)]
pub struct GrpcTransport {
    service: GrpcService,
    timeout: Option<Duration>,
}

impl GrpcTransport {
    /// Creates a gRPC transport over an established public channel.
    pub fn new(channel: Channel, auth: Arc<dyn Auth>) -> Self {
        Self::from_service(GrpcService::Public(auth_channel(channel, auth)))
    }

    /// Creates a gRPC transport over the provider host-service relay.
    pub(crate) fn from_host_service(channel: HostServiceChannel) -> Self {
        Self::from_service(GrpcService::Bound(channel))
    }

    fn from_service(service: GrpcService) -> Self {
        Self {
            service,
            timeout: None,
        }
    }

    /// Applies a per-request deadline to unary calls.
    pub fn with_timeout(mut self, timeout: Duration) -> Self {
        self.timeout = Some(timeout);
        self
    }
}

impl UnaryTransport for GrpcTransport {
    fn unary<Req, Resp>(
        &self,
        method: &Method,
        request: &Req,
        response: &mut Resp,
    ) -> impl std::future::Future<Output = Result<(), GestaltError>> + Send
    where
        Req: Message + Clone + Send + Sync + 'static,
        Resp: Message + Default + Send + 'static,
    {
        let service = self.service.clone();
        let timeout = self.timeout;
        let path = method.full_method.to_string();
        let request = request.clone();

        async move { unary_grpc_call(&service, &path, request, timeout, response).await }
    }
}

impl GrpcCapable for GrpcTransport {}

/// Sync gRPC transport implementing [`SyncUnaryTransport`] and
/// [`SyncGrpcCapable`] for the full public surface.
///
/// Wraps `tonic`'s async unary calls in `tokio::runtime::Runtime::block_on`.
/// Like [`crate::public::rest_transport::SyncRestTransport`], this panics if
/// called from within an async runtime — callers in async context should use
/// [`GrpcTransport`] instead.
pub struct SyncGrpcTransport {
    service: GrpcService,
    runtime: tokio::runtime::Runtime,
    timeout: Option<Duration>,
}

impl SyncGrpcTransport {
    /// Creates a sync gRPC transport over an established public channel.
    ///
    /// The channel's background IO driver must outlive this transport — i.e.,
    /// the channel must have been created on a tokio runtime that is still
    /// running. For the common case of dialing from a sync entry point, use
    /// [`SyncGrpcTransport::from_endpoint`] instead, which dials inside the
    /// transport's own runtime.
    pub fn new(channel: Channel, auth: Arc<dyn Auth>) -> Self {
        Self::from_service(GrpcService::Public(auth_channel(channel, auth)))
    }

    /// Creates a sync gRPC transport by dialing the given endpoint inside the
    /// transport's own tokio runtime. This is the recommended entry point for
    /// sync callers: the channel's background IO driver is spawned on the
    /// transport's runtime, so it stays alive for the lifetime of the
    /// transport.
    pub fn from_endpoint(endpoint: tonic::transport::Endpoint, auth: Arc<dyn Auth>) -> Self {
        let runtime = tokio::runtime::Builder::new_multi_thread()
            .enable_all()
            .build()
            .expect("tokio runtime");
        let _guard = runtime.enter();
        let channel = endpoint.connect_lazy();
        Self {
            service: GrpcService::Public(auth_channel(channel, auth)),
            runtime,
            timeout: None,
        }
    }

    /// Creates a sync gRPC transport over the provider host-service relay.
    #[allow(dead_code)]
    pub(crate) fn from_host_service(channel: HostServiceChannel) -> Self {
        Self::from_service(GrpcService::Bound(channel))
    }

    fn from_service(service: GrpcService) -> Self {
        Self {
            service,
            runtime: tokio::runtime::Builder::new_multi_thread()
                .enable_all()
                .build()
                .expect("tokio runtime"),
            timeout: None,
        }
    }

    /// Applies a per-request deadline to unary calls.
    pub fn with_timeout(mut self, timeout: Duration) -> Self {
        self.timeout = Some(timeout);
        self
    }
}

impl SyncUnaryTransport for SyncGrpcTransport {
    fn unary<Req, Resp>(
        &self,
        method: &Method,
        request: &Req,
        response: &mut Resp,
    ) -> Result<(), GestaltError>
    where
        Req: Message + Clone + Send + Sync + 'static,
        Resp: Message + Default + Send + 'static,
    {
        let service = self.service.clone();
        let timeout = self.timeout;
        let path = method.full_method.to_string();
        let request = request.clone();
        let _guard = self.runtime.enter();
        self.runtime
            .block_on(unary_grpc_call(&service, &path, request, timeout, response))
    }
}

impl SyncGrpcCapable for SyncGrpcTransport {}

fn grpc_ready_error(err: tonic::transport::Error) -> GestaltError {
    GestaltError::new(gestalt_error_code::UNAVAILABLE, err.to_string())
}

/// Dials a public gRPC endpoint from an https:// or http:// address.
pub fn dial_public_grpc(address: &str) -> Result<Channel, GestaltError> {
    let endpoint = if let Some(rest) = address.strip_prefix("https://") {
        tonic::transport::Endpoint::from_shared(format!("https://{rest}"))
            .map_err(transport_error)?
            .tls_config(tonic::transport::ClientTlsConfig::new().with_native_roots())
            .map_err(transport_error)?
    } else if let Some(rest) = address.strip_prefix("http://") {
        tonic::transport::Endpoint::from_shared(format!("http://{rest}"))
            .map_err(transport_error)?
    } else {
        return Err(GestaltError::new(
            gestalt_error_code::INVALID_ARGUMENT,
            format!("invalid gRPC address {address:?}"),
        ));
    };
    Ok(endpoint.connect_lazy())
}

fn auth_channel(channel: Channel, auth: Arc<dyn Auth>) -> AuthChannel {
    tonic::service::interceptor::InterceptedService::new(channel, AuthInterceptor { auth })
}

#[derive(Clone)]
struct AuthInterceptor {
    auth: Arc<dyn Auth>,
}

impl tonic::service::Interceptor for AuthInterceptor {
    fn call(&mut self, mut request: Request<()>) -> Result<Request<()>, Status> {
        if let Some(authorization) = self.auth.authorization_header() {
            request.metadata_mut().insert(
                "authorization",
                authorization.parse().map_err(
                    |err: tonic::metadata::errors::InvalidMetadataValue| {
                        Status::invalid_argument(err.to_string())
                    },
                )?,
            );
        }
        Ok(request)
    }
}

fn transport_error(err: tonic::transport::Error) -> GestaltError {
    GestaltError::new(gestalt_error_code::UNAVAILABLE, err.to_string())
}

/// Shared inner async unary call logic for both [`GrpcTransport`] (async) and
/// [`SyncGrpcTransport`] (sync via `block_on`).
async fn unary_grpc_call<Req, Resp>(
    service: &GrpcService,
    path: &str,
    request: Req,
    timeout: Option<Duration>,
    response: &mut Resp,
) -> Result<(), GestaltError>
where
    Req: Message + Send + Sync + 'static,
    Resp: Message + Default + Send + 'static,
{
    let path: PathAndQuery = path.parse().map_err(|err: http::uri::InvalidUri| {
        GestaltError::new(gestalt_error_code::INVALID_ARGUMENT, err.to_string())
    })?;
    let mut tonic_request = Request::new(request);
    if let Some(timeout) = timeout {
        tonic_request.set_timeout(timeout);
    }

    let codec = ProstCodec::<Req, Resp>::default();
    let wire_response = match service {
        GrpcService::Public(channel) => {
            let mut client = Grpc::new(channel.clone());
            client.ready().await.map_err(grpc_ready_error)?;
            client
                .unary(tonic_request, path, codec)
                .await
                .map_err(GestaltError::from)?
                .into_inner()
        }
        GrpcService::Bound(channel) => {
            let mut client = Grpc::new(channel.clone());
            client.ready().await.map_err(grpc_ready_error)?;
            client
                .unary(tonic_request, path, codec)
                .await
                .map_err(GestaltError::from)?
                .into_inner()
        }
    };

    *response = wire_response;
    Ok(())
}
