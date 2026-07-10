//! Crate-private transport for generated host-service clients: resolves the
//! host-service target from the environment, dials it, and attaches the relay
//! token and binding-name metadata to every request.

use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tower::service_fn;

use crate::env::{
    ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN, HOST_SERVICE_BINDING_HEADER,
    host_service_configured,
};
use crate::rpc_support::{GestaltError, gestalt_error_code};

/// gRPC metadata header carrying the host-service relay token.
const HOST_SERVICE_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

/// Transport used by generated host-service clients: an established channel
/// plus the host-service metadata interceptor.
pub(crate) type HostServiceChannel = InterceptedService<Channel, HostServiceHeaders>;

/// Interceptor attaching the relay token and binding name to every request.
/// The default value attaches nothing.
#[derive(Clone, Default)]
pub(crate) struct HostServiceHeaders {
    relay_token: Option<MetadataValue<tonic::metadata::Ascii>>,
    binding: Option<MetadataValue<tonic::metadata::Ascii>>,
}

impl Interceptor for HostServiceHeaders {
    fn call(
        &mut self,
        mut request: tonic::Request<()>,
    ) -> Result<tonic::Request<()>, tonic::Status> {
        if let Some(token) = self.relay_token.clone() {
            request
                .metadata_mut()
                .insert(HOST_SERVICE_RELAY_TOKEN_HEADER, token);
        }
        if let Some(binding) = self.binding.clone() {
            request
                .metadata_mut()
                .insert(HOST_SERVICE_BINDING_HEADER, binding);
        }
        Ok(request)
    }
}

/// Wraps an established channel without attaching host-service metadata.
pub(crate) fn plain_channel(channel: Channel) -> HostServiceChannel {
    InterceptedService::new(channel, HostServiceHeaders::default())
}

/// Connects to the host service described by the environment, attaching the
/// relay token and the optional binding name to every request.
pub(crate) async fn connect_host_service(
    service: &str,
    name: &str,
) -> Result<HostServiceChannel, GestaltError> {
    if let Err(message) = host_service_configured(service) {
        return Err(env_error(message));
    }
    let target = std::env::var(ENV_HOST_SERVICE_SOCKET).map_err(|_| {
        env_error(format!("{service}: {ENV_HOST_SERVICE_SOCKET} is not set"))
    })?;
    let token = std::env::var(ENV_HOST_SERVICE_TOKEN).unwrap_or_default();
    let channel = match parse_host_service_target(service, &target)? {
        HostServiceTarget::Unix(path) => Endpoint::try_from("http://[::]:50051")
            .map_err(|err| transport_error(service, &err))?
            .connect_with_connector(service_fn(move |_: Uri| {
                let path = path.clone();
                async move { UnixStream::connect(path).await.map(TokioIo::new) }
            }))
            .await
            .map_err(|err| transport_error(service, &err))?,
        HostServiceTarget::Tcp(address) => Endpoint::from_shared(format!("http://{address}"))
            .map_err(|err| transport_error(service, &err))?
            .connect()
            .await
            .map_err(|err| transport_error(service, &err))?,
        HostServiceTarget::Tls(address) => Endpoint::from_shared(format!("https://{address}"))
            .map_err(|err| transport_error(service, &err))?
            .tls_config(ClientTlsConfig::new().with_native_roots())
            .map_err(|err| transport_error(service, &err))?
            .connect()
            .await
            .map_err(|err| transport_error(service, &err))?,
    };
    Ok(InterceptedService::new(
        channel,
        host_service_headers(service, token.trim(), name.trim())?,
    ))
}

fn env_error(message: String) -> GestaltError {
    GestaltError::new(gestalt_error_code::FAILED_PRECONDITION, message)
}

fn transport_error(service: &str, err: &tonic::transport::Error) -> GestaltError {
    GestaltError::new(
        gestalt_error_code::UNAVAILABLE,
        format!("{service}: {err}"),
    )
}

fn host_service_headers(
    service: &str,
    token: &str,
    binding: &str,
) -> Result<HostServiceHeaders, GestaltError> {
    let relay_token = if token.is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(token).map_err(|err| {
            env_error(format!("{service}: invalid relay token metadata: {err}"))
        })?)
    };
    let binding = if binding.is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(binding).map_err(|err| {
            env_error(format!("{service}: invalid binding metadata: {err}"))
        })?)
    };
    Ok(HostServiceHeaders {
        relay_token,
        binding,
    })
}

enum HostServiceTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_host_service_target(
    service: &str,
    raw_target: &str,
) -> Result<HostServiceTarget, GestaltError> {
    let target = raw_target.trim();
    if target.is_empty() {
        return Err(env_error(format!(
            "{service}: transport target is required"
        )));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(env_error(format!(
                "{service}: tcp target {raw_target:?} is missing host:port"
            )));
        }
        return Ok(HostServiceTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(env_error(format!(
                "{service}: tls target {raw_target:?} is missing host:port"
            )));
        }
        return Ok(HostServiceTarget::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(env_error(format!(
                "{service}: unix target {raw_target:?} is missing a socket path"
            )));
        }
        return Ok(HostServiceTarget::Unix(path.to_string()));
    }
    if target.contains("://") {
        let scheme = target.split("://").next().unwrap_or_default();
        return Err(env_error(format!(
            "{service}: unsupported target scheme {scheme:?}"
        )));
    }
    Ok(HostServiceTarget::Unix(target.to_string()))
}
