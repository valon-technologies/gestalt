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
use crate::public::generated::grpc_dispatch;
use crate::public::generated::metadata::Method;
use crate::public::generated::rpc_support::GestaltError;
use crate::public::generated::unary_transport::UnaryTransport;
use crate::rpc_support::gestalt_error_code;

pub(crate) type AuthChannel =
    tonic::service::interceptor::InterceptedService<Channel, AuthInterceptor>;

/// tonic clients for every public gRPC service.
#[derive(Clone)]
#[allow(missing_docs)]
pub(crate) struct PublicGrpcClients {
    pub agent: v1::agent_client::AgentClient<AuthChannel>,
    pub app: v1::app_client::AppClient<AuthChannel>,
    pub authorization: v1::authorization_client::AuthorizationClient<AuthChannel>,
    pub external_credentials:
        v1::external_credentials_client::ExternalCredentialsClient<AuthChannel>,
    pub identity: v1::identity_client::IdentityClient<AuthChannel>,
    pub indexed_db: v1::indexed_db_client::IndexedDbClient<AuthChannel>,
    pub workflow: v1::workflow_client::WorkflowClient<AuthChannel>,
}

impl PublicGrpcClients {
    pub(crate) fn new(channel: Channel, auth: Arc<dyn Auth>) -> Self {
        let ch = auth_channel(channel, auth);
        Self {
            agent: v1::agent_client::AgentClient::new(ch.clone()),
            app: v1::app_client::AppClient::new(ch.clone()),
            authorization: v1::authorization_client::AuthorizationClient::new(ch.clone()),
            external_credentials: v1::external_credentials_client::ExternalCredentialsClient::new(
                ch.clone(),
            ),
            identity: v1::identity_client::IdentityClient::new(ch.clone()),
            indexed_db: v1::indexed_db_client::IndexedDbClient::new(ch.clone()),
            workflow: v1::workflow_client::WorkflowClient::new(ch),
        }
    }
}

#[derive(Clone)]
pub(crate) enum AppGrpcClient {
    Public(Box<PublicGrpcClients>),
    Bound(Box<v1::app_client::AppClient<HostServiceChannel>>),
}

/// gRPC transport implementing [`UnaryTransport`] for the public surface.
#[derive(Clone)]
pub struct GrpcTransport {
    client: AppGrpcClient,
    timeout: Option<Duration>,
}

impl GrpcTransport {
    /// Creates a gRPC transport over an established public channel.
    pub fn new(channel: Channel, auth: Arc<dyn Auth>) -> Self {
        Self::from_client(AppGrpcClient::Public(Box::new(PublicGrpcClients::new(
            channel, auth,
        ))))
    }

    /// Creates a gRPC transport over the provider host-service relay.
    pub(crate) fn from_host_service(channel: HostServiceChannel) -> Self {
        Self::from_client(AppGrpcClient::Bound(Box::new(
            v1::app_client::AppClient::new(channel),
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

        async move {
            let bytes =
                grpc_dispatch::dispatch_unary(&mut client, method, &request_bytes, timeout).await?;
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
pub(crate) struct AuthInterceptor {
    auth: Arc<dyn Auth>,
}

impl Interceptor for AuthInterceptor {
    fn call(&mut self, mut request: Request<()>) -> Result<Request<()>, Status> {
        if let Some(authorization) = self.auth.authorization_header() {
            request.metadata_mut().insert(
                "authorization",
                authorization
                    .parse()
                    .map_err(|_| Status::invalid_argument("invalid authorization header"))?,
            );
        }
        Ok(request)
    }
}

fn transport_error(err: tonic::transport::Error) -> GestaltError {
    GestaltError::new(gestalt_error_code::UNAVAILABLE, err.to_string())
}
