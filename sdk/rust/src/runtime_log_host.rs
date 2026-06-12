use std::time::SystemTime;

use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::Request;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tower::service_fn;

use crate::env::{ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN};
use crate::generated::v1::{
    self as pb, runtime_log_host_client::RuntimeLogHostClient as ProtoRuntimeLogHostClient,
};

type RuntimeLogHostTransport = InterceptedService<Channel, RelayTokenInterceptor>;

/// Environment variable containing the current runtime session id.
pub const ENV_RUNTIME_SESSION_ID: &str = "GESTALT_RUNTIME_SESSION_ID";
const RUNTIME_LOG_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
#[repr(i32)]
/// Runtime log stream for runtime log entries.
pub enum RuntimeLogStream {
    #[default]
    /// The `Unspecified` variant.
    Unspecified = 0,
    /// The `Stdout` variant.
    Stdout = 1,
    /// The `Stderr` variant.
    Stderr = 2,
    /// The `Runtime` variant.
    Runtime = 3,
}

impl RuntimeLogStream {
    const fn as_i32(self) -> i32 {
        self as i32
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
/// One runtime log entry.
pub struct RuntimeLogEntry {
    /// The `stream` field.
    pub stream: RuntimeLogStream,
    /// The `message` field.
    pub message: String,
    /// The `observed_at` field.
    pub observed_at: SystemTime,
    /// The `source_seq` field.
    pub source_seq: i64,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
/// Request for appending runtime logs.
pub struct AppendRuntimeLogsRequest {
    /// The `session_id` field.
    pub session_id: String,
    /// The `logs` field.
    pub logs: Vec<RuntimeLogEntry>,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
/// Response returned after appending runtime logs.
pub struct AppendRuntimeLogsResponse {
    /// The `last_seq` field.
    pub last_seq: i64,
}

fn append_logs_request_to_proto(request: AppendRuntimeLogsRequest) -> pb::AppendRuntimeLogsRequest {
    pb::AppendRuntimeLogsRequest {
        session_id: request.session_id,
        logs: request
            .logs
            .into_iter()
            .map(|entry| pb::RuntimeLogEntry {
                stream: entry.stream.as_i32(),
                message: entry.message,
                observed_at: Some(crate::protocol::timestamp_from_system_time(
                    entry.observed_at,
                )),
                source_seq: entry.source_seq,
            })
            .collect(),
    }
}

fn append_logs_response_from_proto(
    response: pb::AppendRuntimeLogsResponse,
) -> AppendRuntimeLogsResponse {
    AppendRuntimeLogsResponse {
        last_seq: response.last_seq,
    }
}

#[derive(Debug, thiserror::Error)]
/// Errors returned by [`RuntimeLogHost`].
pub enum RuntimeLogHostError {
    /// The host-service transport could not be created.
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    /// The host-service RPC returned a gRPC status.
    #[error("{0}")]
    Status(#[from] tonic::Status),
    /// Required environment or target configuration was invalid.
    #[error("{0}")]
    Env(String),
}

/// Client for appending runtime logs to the host.
pub struct RuntimeLogHost {
    client: ProtoRuntimeLogHostClient<RuntimeLogHostTransport>,
    source_seq: i64,
}

impl RuntimeLogHost {
    /// Connects to the runtime-log host service described by the environment.
    pub async fn connect() -> std::result::Result<Self, RuntimeLogHostError> {
        let socket_path = std::env::var(ENV_HOST_SERVICE_SOCKET).map_err(|_| {
            RuntimeLogHostError::Env(format!("{ENV_HOST_SERVICE_SOCKET} is not set"))
        })?;
        let relay_token = std::env::var(ENV_HOST_SERVICE_TOKEN).unwrap_or_default();
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
                    .tls_config(ClientTlsConfig::new().with_native_roots())?
                    .connect()
                    .await?
            }
        };

        Ok(Self {
            client: ProtoRuntimeLogHostClient::with_interceptor(
                channel,
                relay_token_interceptor(relay_token.trim())?,
            ),
            source_seq: 0,
        })
    }

    /// Appends logs using a raw protocol request message.
    pub async fn append_logs(
        &mut self,
        request: AppendRuntimeLogsRequest,
    ) -> std::result::Result<AppendRuntimeLogsResponse, RuntimeLogHostError> {
        Ok(append_logs_response_from_proto(
            self.client
                .append_logs(append_logs_request_to_proto(request))
                .await?
                .into_inner(),
        ))
    }

    /// Appends one log entry for an explicit session id.
    pub async fn append(
        &mut self,
        session_id: impl Into<String>,
        stream: RuntimeLogStream,
        message: impl Into<String>,
    ) -> std::result::Result<AppendRuntimeLogsResponse, RuntimeLogHostError> {
        self.source_seq += 1;
        let source_seq = self.source_seq;
        self.append_entry(session_id, stream, message, None, source_seq)
            .await
    }

    /// Appends one log entry for `GESTALT_RUNTIME_SESSION_ID`.
    pub async fn append_current(
        &mut self,
        stream: RuntimeLogStream,
        message: impl Into<String>,
    ) -> std::result::Result<AppendRuntimeLogsResponse, RuntimeLogHostError> {
        self.append(runtime_session_id()?, stream, message).await
    }

    /// Appends one log entry with an explicit timestamp and source sequence.
    pub async fn append_entry(
        &mut self,
        session_id: impl Into<String>,
        stream: RuntimeLogStream,
        message: impl Into<String>,
        observed_at: Option<SystemTime>,
        source_seq: i64,
    ) -> std::result::Result<AppendRuntimeLogsResponse, RuntimeLogHostError> {
        self.source_seq = self.source_seq.max(source_seq);
        self.append_logs(AppendRuntimeLogsRequest {
            session_id: session_id.into(),
            logs: vec![RuntimeLogEntry {
                stream,
                message: message.into(),
                observed_at: observed_at.unwrap_or_else(SystemTime::now),
                source_seq,
            }],
        })
        .await
    }

    /// Appends one explicit log entry for `GESTALT_RUNTIME_SESSION_ID`.
    pub async fn append_current_entry(
        &mut self,
        stream: RuntimeLogStream,
        message: impl Into<String>,
        observed_at: Option<SystemTime>,
        source_seq: i64,
    ) -> std::result::Result<AppendRuntimeLogsResponse, RuntimeLogHostError> {
        self.append_entry(
            runtime_session_id()?,
            stream,
            message,
            observed_at,
            source_seq,
        )
        .await
    }

    /// Appends one stdout log entry for an explicit session id.
    pub async fn append_stdout(
        &mut self,
        session_id: impl Into<String>,
        message: impl Into<String>,
    ) -> std::result::Result<AppendRuntimeLogsResponse, RuntimeLogHostError> {
        self.append(session_id, RuntimeLogStream::Stdout, message)
            .await
    }

    /// Appends one stdout log entry for `GESTALT_RUNTIME_SESSION_ID`.
    pub async fn append_current_stdout(
        &mut self,
        message: impl Into<String>,
    ) -> std::result::Result<AppendRuntimeLogsResponse, RuntimeLogHostError> {
        self.append_current(RuntimeLogStream::Stdout, message).await
    }

    /// Appends one stderr log entry for an explicit session id.
    pub async fn append_stderr(
        &mut self,
        session_id: impl Into<String>,
        message: impl Into<String>,
    ) -> std::result::Result<AppendRuntimeLogsResponse, RuntimeLogHostError> {
        self.append(session_id, RuntimeLogStream::Stderr, message)
            .await
    }

    /// Appends one stderr log entry for `GESTALT_RUNTIME_SESSION_ID`.
    pub async fn append_current_stderr(
        &mut self,
        message: impl Into<String>,
    ) -> std::result::Result<AppendRuntimeLogsResponse, RuntimeLogHostError> {
        self.append_current(RuntimeLogStream::Stderr, message).await
    }

    /// Appends one runtime log entry for an explicit session id.
    pub async fn append_runtime(
        &mut self,
        session_id: impl Into<String>,
        message: impl Into<String>,
    ) -> std::result::Result<AppendRuntimeLogsResponse, RuntimeLogHostError> {
        self.append(session_id, RuntimeLogStream::Runtime, message)
            .await
    }

    /// Appends one runtime log entry for `GESTALT_RUNTIME_SESSION_ID`.
    pub async fn append_current_runtime(
        &mut self,
        message: impl Into<String>,
    ) -> std::result::Result<AppendRuntimeLogsResponse, RuntimeLogHostError> {
        self.append_current(RuntimeLogStream::Runtime, message)
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

/// Returns the current runtime session id from `GESTALT_RUNTIME_SESSION_ID`.
pub fn runtime_session_id() -> std::result::Result<String, RuntimeLogHostError> {
    let session_id = std::env::var(ENV_RUNTIME_SESSION_ID)
        .unwrap_or_default()
        .trim()
        .to_string();
    if session_id.is_empty() {
        return Err(RuntimeLogHostError::Env(format!(
            "runtime session: {ENV_RUNTIME_SESSION_ID} is not set"
        )));
    }
    Ok(session_id)
}
