use std::time::{SystemTime, UNIX_EPOCH};

use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::Request;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, Endpoint, Uri};
use tower::service_fn;

use crate::generated::v1::{
    self as pb,
    plugin_runtime_log_host_client::PluginRuntimeLogHostClient as ProtoPluginRuntimeLogHostClient,
};

type RuntimeLogHostTransport = InterceptedService<Channel, RelayTokenInterceptor>;

pub const ENV_RUNTIME_LOG_HOST_SOCKET: &str = "GESTALT_RUNTIME_LOG_SOCKET";
pub const ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN: &str = "GESTALT_RUNTIME_LOG_SOCKET_TOKEN";
const RUNTIME_LOG_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

pub type RuntimeLogStream = pb::PluginRuntimeLogStream;

#[derive(Debug, thiserror::Error)]
pub enum RuntimeLogHostError {
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    #[error("{0}")]
    Status(#[from] tonic::Status),
    #[error("{0}")]
    Env(String),
}

pub struct RuntimeLogHost {
    client: ProtoPluginRuntimeLogHostClient<RuntimeLogHostTransport>,
    source_seq: i64,
}

impl RuntimeLogHost {
    pub async fn connect() -> std::result::Result<Self, RuntimeLogHostError> {
        let socket_path = std::env::var(ENV_RUNTIME_LOG_HOST_SOCKET).map_err(|_| {
            RuntimeLogHostError::Env(format!("{ENV_RUNTIME_LOG_HOST_SOCKET} is not set"))
        })?;
        let relay_token = std::env::var(ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN).unwrap_or_default();
        let channel = match parse_runtime_log_host_target(&socket_path)? {
            RuntimeLogHostTarget::Unix(path) => {
                Endpoint::try_from("http://[::]:50051")?
                    .connect_with_connector(service_fn(move |_: Uri| {
                        let path = path.clone();
                        async move { UnixStream::connect(path).await.map(TokioIo::new) }
                    }))
                    .await?
            }
            RuntimeLogHostTarget::Tcp(address) => {
                Endpoint::from_shared(format!("http://{address}"))?
                    .connect()
                    .await?
            }
            RuntimeLogHostTarget::Tls(address) => {
                Endpoint::from_shared(format!("https://{address}"))?
                    .connect()
                    .await?
            }
        };

        Ok(Self {
            client: ProtoPluginRuntimeLogHostClient::with_interceptor(
                channel,
                relay_token_interceptor(relay_token.trim())?,
            ),
            source_seq: 0,
        })
    }

    pub async fn append_logs(
        &mut self,
        request: pb::AppendPluginRuntimeLogsRequest,
    ) -> std::result::Result<pb::AppendPluginRuntimeLogsResponse, RuntimeLogHostError> {
        Ok(self.client.append_logs(request).await?.into_inner())
    }

    pub async fn append(
        &mut self,
        session_id: impl Into<String>,
        stream: RuntimeLogStream,
        message: impl Into<String>,
    ) -> std::result::Result<pb::AppendPluginRuntimeLogsResponse, RuntimeLogHostError> {
        self.source_seq += 1;
        let source_seq = self.source_seq;
        self.append_entry(session_id, stream, message, None, source_seq)
            .await
    }

    pub async fn append_entry(
        &mut self,
        session_id: impl Into<String>,
        stream: RuntimeLogStream,
        message: impl Into<String>,
        observed_at: Option<prost_types::Timestamp>,
        source_seq: i64,
    ) -> std::result::Result<pb::AppendPluginRuntimeLogsResponse, RuntimeLogHostError> {
        self.source_seq = self.source_seq.max(source_seq);
        self.append_logs(pb::AppendPluginRuntimeLogsRequest {
            session_id: session_id.into(),
            logs: vec![pb::PluginRuntimeLogEntry {
                stream: stream as i32,
                message: message.into(),
                observed_at: Some(observed_at.unwrap_or_else(timestamp_now)),
                source_seq,
            }],
        })
        .await
    }

    pub async fn append_stdout(
        &mut self,
        session_id: impl Into<String>,
        message: impl Into<String>,
    ) -> std::result::Result<pb::AppendPluginRuntimeLogsResponse, RuntimeLogHostError> {
        self.append(session_id, RuntimeLogStream::Stdout, message)
            .await
    }

    pub async fn append_stderr(
        &mut self,
        session_id: impl Into<String>,
        message: impl Into<String>,
    ) -> std::result::Result<pb::AppendPluginRuntimeLogsResponse, RuntimeLogHostError> {
        self.append(session_id, RuntimeLogStream::Stderr, message)
            .await
    }

    pub async fn append_runtime(
        &mut self,
        session_id: impl Into<String>,
        message: impl Into<String>,
    ) -> std::result::Result<pb::AppendPluginRuntimeLogsResponse, RuntimeLogHostError> {
        self.append(session_id, RuntimeLogStream::Runtime, message)
            .await
    }
}

#[derive(Clone)]
struct RelayTokenInterceptor {
    token: Option<MetadataValue<tonic::metadata::Ascii>>,
}

impl Interceptor for RelayTokenInterceptor {
    fn call(
        &mut self,
        mut request: Request<()>,
    ) -> std::result::Result<Request<()>, tonic::Status> {
        if let Some(token) = self.token.clone() {
            request
                .metadata_mut()
                .insert(RUNTIME_LOG_RELAY_TOKEN_HEADER, token);
        }
        Ok(request)
    }
}

fn relay_token_interceptor(
    token: &str,
) -> std::result::Result<RelayTokenInterceptor, RuntimeLogHostError> {
    let trimmed = token.trim();
    let token = if trimmed.is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(trimmed).map_err(|err| {
            RuntimeLogHostError::Env(format!(
                "runtime log host: invalid relay token metadata: {err}"
            ))
        })?)
    };
    Ok(RelayTokenInterceptor { token })
}

enum RuntimeLogHostTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_runtime_log_host_target(
    raw: &str,
) -> std::result::Result<RuntimeLogHostTarget, RuntimeLogHostError> {
    let target = raw.trim();
    if target.is_empty() {
        return Err(RuntimeLogHostError::Env(
            "runtime log host: transport target is required".to_string(),
        ));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(RuntimeLogHostError::Env(format!(
                "runtime log host: tcp target {raw:?} is missing host:port"
            )));
        }
        return Ok(RuntimeLogHostTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(RuntimeLogHostError::Env(format!(
                "runtime log host: tls target {raw:?} is missing host:port"
            )));
        }
        return Ok(RuntimeLogHostTarget::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(RuntimeLogHostError::Env(format!(
                "runtime log host: unix target {raw:?} is missing a socket path"
            )));
        }
        return Ok(RuntimeLogHostTarget::Unix(path.to_string()));
    }
    if target.contains("://") {
        return Err(RuntimeLogHostError::Env(format!(
            "runtime log host: unsupported target scheme in {raw:?}"
        )));
    }
    Ok(RuntimeLogHostTarget::Unix(target.to_string()))
}

fn timestamp_now() -> prost_types::Timestamp {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("unix epoch");
    prost_types::Timestamp {
        seconds: now.as_secs() as i64,
        nanos: now.subsec_nanos() as i32,
    }
}
