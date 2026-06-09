use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::Request as GrpcRequest;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tower::service_fn;

use crate::env::HOST_SERVICE_BINDING_HEADER;

const RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

pub(crate) type Transport = InterceptedService<Channel, RelayTokenInterceptor>;

#[derive(Debug, thiserror::Error)]
pub(crate) enum HostServiceError {
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    #[error("{0}")]
    Env(String),
}

pub(crate) async fn connect(
    service_name: &str,
    raw_target: &str,
    relay_token: &str,
    binding: Option<&str>,
) -> Result<Transport, HostServiceError> {
    let channel = match parse_target(service_name, raw_target)? {
        Target::Unix(path) => {
            Endpoint::try_from("http://[::]:50051")?
                .connect_with_connector(service_fn(move |_: Uri| {
                    let path = path.clone();
                    async move { UnixStream::connect(path).await.map(TokioIo::new) }
                }))
                .await?
        }
        Target::Tcp(address) => {
            Endpoint::from_shared(format!("http://{address}"))?
                .connect()
                .await?
        }
        Target::Tls(address) => {
            Endpoint::from_shared(format!("https://{address}"))?
                .tls_config(ClientTlsConfig::new().with_native_roots())?
                .connect()
                .await?
        }
    };

    Ok(InterceptedService::new(
        channel,
        relay_token_interceptor(service_name, relay_token, binding)?,
    ))
}

enum Target {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_target(service_name: &str, raw_target: &str) -> Result<Target, HostServiceError> {
    let target = raw_target.trim();
    if target.is_empty() {
        return Err(HostServiceError::Env(format!(
            "{service_name}: transport target is required"
        )));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(HostServiceError::Env(format!(
                "{service_name}: tcp target {raw_target:?} is missing host:port"
            )));
        }
        return Ok(Target::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(HostServiceError::Env(format!(
                "{service_name}: tls target {raw_target:?} is missing host:port"
            )));
        }
        return Ok(Target::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(HostServiceError::Env(format!(
                "{service_name}: unix target {raw_target:?} is missing a socket path"
            )));
        }
        return Ok(Target::Unix(path.to_string()));
    }
    if target.contains("://") {
        let scheme = target.split("://").next().unwrap_or_default();
        return Err(HostServiceError::Env(format!(
            "{service_name}: unsupported target scheme {scheme:?}"
        )));
    }
    Ok(Target::Unix(target.to_string()))
}

fn relay_token_interceptor(
    service_name: &str,
    token: &str,
    binding: Option<&str>,
) -> Result<RelayTokenInterceptor, HostServiceError> {
    let relay_token = if token.trim().is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(token.to_string()).map_err(|err| {
            HostServiceError::Env(format!(
                "{service_name}: invalid relay token metadata: {err}"
            ))
        })?)
    };
    let binding = binding
        .filter(|value| !value.trim().is_empty())
        .map(|value| {
            MetadataValue::try_from(value.to_string()).map_err(|err| {
                HostServiceError::Env(format!("{service_name}: invalid binding metadata: {err}"))
            })
        })
        .transpose()?;

    Ok(RelayTokenInterceptor {
        relay_token,
        binding,
    })
}

#[derive(Clone)]
pub(crate) struct RelayTokenInterceptor {
    relay_token: Option<MetadataValue<tonic::metadata::Ascii>>,
    binding: Option<MetadataValue<tonic::metadata::Ascii>>,
}

impl Interceptor for RelayTokenInterceptor {
    fn call(&mut self, mut request: GrpcRequest<()>) -> Result<GrpcRequest<()>, tonic::Status> {
        if let Some(header) = self.relay_token.clone() {
            request.metadata_mut().insert(RELAY_TOKEN_HEADER, header);
        }
        if let Some(header) = self.binding.clone() {
            request
                .metadata_mut()
                .insert(HOST_SERVICE_BINDING_HEADER, header);
        }
        Ok(request)
    }
}
