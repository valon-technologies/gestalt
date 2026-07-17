//! tonic-based gRPC transport for the public Gestalt API.

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
use crate::public::generated::unary_transport::{GrpcCapable, UnaryTransport};
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

        async move {
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
                    let mut client = Grpc::new(channel);
                    client.ready().await.map_err(grpc_ready_error)?;
                    client
                        .unary(tonic_request, path, codec)
                        .await
                        .map_err(GestaltError::from)?
                        .into_inner()
                }
                GrpcService::Bound(channel) => {
                    let mut client = Grpc::new(channel);
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
    }
}

impl GrpcCapable for GrpcTransport {}

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
