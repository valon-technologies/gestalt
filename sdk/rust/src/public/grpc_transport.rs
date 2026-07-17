//! tonic-based gRPC transport for the public Gestalt API.

use std::sync::Arc;
use std::time::Duration;

use prost::Message;
use tonic::service::Interceptor;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint};
use tonic::{Request, Status};

use crate::codec::host_service::HostServiceChannel;
use crate::generated::v1;
use crate::public::auth::Auth;
use crate::public::generated::metadata::{METHOD_APP_INVOKE, METHOD_APP_INVOKE_GRAPHQL, Method};
use crate::public::generated::rpc_support::GestaltError;
use crate::public::generated::unary_transport::UnaryTransport;
use crate::rpc_support::gestalt_error_code;

type AuthChannel = tonic::service::interceptor::InterceptedService<Channel, AuthInterceptor>;

#[derive(Clone)]
enum AppGrpcClient {
    Public(v1::app_client::AppClient<AuthChannel>),
    Bound(v1::app_client::AppClient<HostServiceChannel>),
}

/// gRPC transport implementing [`UnaryTransport`] for the public App surface.
#[derive(Clone)]
pub struct GrpcTransport {
    client: AppGrpcClient,
    timeout: Option<Duration>,
}

impl GrpcTransport {
    /// Creates a gRPC transport over an established public channel.
    pub fn new(channel: Channel, auth: Arc<dyn Auth>) -> Self {
        Self::from_client(AppGrpcClient::Public(v1::app_client::AppClient::new(
            auth_channel(channel, auth),
        )))
    }

    /// Creates a gRPC transport over the provider host-service relay.
    pub(crate) fn from_host_service(channel: HostServiceChannel) -> Self {
        Self::from_client(AppGrpcClient::Bound(v1::app_client::AppClient::new(
            channel,
        )))
    }

    fn from_client(client: AppGrpcClient) -> Self {
        Self {
            client,
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
        Req: Message + Send + Sync,
        Resp: Message + Default + Send,
    {
        let mut client = self.client.clone();
        let timeout = self.timeout;
        let request_bytes = request.encode_to_vec();
        let method_name = method.name.to_string();

        async move {
            let tonic_response = if method_name == METHOD_APP_INVOKE.name {
                let wire =
                    v1::AppInvokeRequest::decode(request_bytes.as_slice()).map_err(|err| {
                        GestaltError::new(gestalt_error_code::INVALID_ARGUMENT, err.to_string())
                    })?;
                let mut tonic_request = Request::new(wire);
                if let Some(timeout) = timeout {
                    tonic_request.set_timeout(timeout);
                }
                match &mut client {
                    AppGrpcClient::Public(c) => c.invoke(tonic_request).await,
                    AppGrpcClient::Bound(c) => c.invoke(tonic_request).await,
                }
                .map_err(GestaltError::from)?
                .into_inner()
            } else if method_name == METHOD_APP_INVOKE_GRAPHQL.name {
                let wire = v1::AppInvokeGraphQlRequest::decode(request_bytes.as_slice()).map_err(
                    |err| GestaltError::new(gestalt_error_code::INVALID_ARGUMENT, err.to_string()),
                )?;
                let mut tonic_request = Request::new(wire);
                if let Some(timeout) = timeout {
                    tonic_request.set_timeout(timeout);
                }
                match &mut client {
                    AppGrpcClient::Public(c) => c.invoke_graph_ql(tonic_request).await,
                    AppGrpcClient::Bound(c) => c.invoke_graph_ql(tonic_request).await,
                }
                .map_err(GestaltError::from)?
                .into_inner()
            } else {
                return Err(GestaltError::new(
                    gestalt_error_code::UNIMPLEMENTED,
                    format!("unsupported public gRPC method {method_name}"),
                ));
            };

            let bytes = tonic_response.encode_to_vec();
            *response = Resp::decode(bytes.as_slice())
                .map_err(|err| GestaltError::new(gestalt_error_code::INTERNAL, err.to_string()))?;
            Ok(())
        }
    }
}

/// Dials a public gRPC endpoint from an https:// or http:// address.
pub fn dial_public_grpc(address: &str) -> Result<Channel, GestaltError> {
    let endpoint = if let Some(rest) = address.strip_prefix("https://") {
        Endpoint::from_shared(format!("https://{rest}"))
            .map_err(transport_error)?
            .tls_config(ClientTlsConfig::new().with_native_roots())
            .map_err(transport_error)?
    } else if let Some(rest) = address.strip_prefix("http://") {
        Endpoint::from_shared(format!("http://{rest}")).map_err(transport_error)?
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

impl Interceptor for AuthInterceptor {
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
