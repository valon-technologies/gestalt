use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::Request;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, Endpoint, Uri};
use tower::service_fn;

use crate::generated::v1::{
    self as pb, agent_manager_host_client::AgentManagerHostClient as ProtoAgentManagerHostClient,
};

type AgentManagerTransport = InterceptedService<Channel, RelayTokenInterceptor>;

pub const ENV_AGENT_MANAGER_SOCKET: &str = "GESTALT_AGENT_MANAGER_SOCKET";
pub const ENV_AGENT_MANAGER_SOCKET_TOKEN: &str = "GESTALT_AGENT_MANAGER_SOCKET_TOKEN";
const AGENT_MANAGER_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

#[derive(Debug, thiserror::Error)]
pub enum AgentManagerError {
    #[error("agent manager: invocation token is not available")]
    MissingInvocationToken,
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    #[error("{0}")]
    Status(#[from] tonic::Status),
    #[error("{0}")]
    Env(String),
}

pub struct AgentManager {
    client: ProtoAgentManagerHostClient<AgentManagerTransport>,
    invocation_token: String,
}

impl AgentManager {
    pub async fn connect(
        invocation_token: impl AsRef<str>,
    ) -> std::result::Result<Self, AgentManagerError> {
        let invocation_token = invocation_token.as_ref().trim().to_owned();
        if invocation_token.is_empty() {
            return Err(AgentManagerError::MissingInvocationToken);
        }

        let socket_path = std::env::var(ENV_AGENT_MANAGER_SOCKET).map_err(|_| {
            AgentManagerError::Env(format!("{ENV_AGENT_MANAGER_SOCKET} is not set"))
        })?;
        let relay_token = std::env::var(ENV_AGENT_MANAGER_SOCKET_TOKEN).unwrap_or_default();
        let channel = match parse_agent_manager_target(&socket_path)? {
            AgentManagerTarget::Unix(path) => {
                Endpoint::try_from("http://[::]:50051")?
                    .connect_with_connector(service_fn(move |_: Uri| {
                        let path = path.clone();
                        async move { UnixStream::connect(path).await.map(TokioIo::new) }
                    }))
                    .await?
            }
            AgentManagerTarget::Tcp(address) => {
                Endpoint::from_shared(format!("http://{address}"))?
                    .connect()
                    .await?
            }
            AgentManagerTarget::Tls(address) => {
                Endpoint::from_shared(format!("https://{address}"))?
                    .connect()
                    .await?
            }
        };

        Ok(Self {
            client: ProtoAgentManagerHostClient::with_interceptor(
                channel,
                relay_token_interceptor(relay_token.trim())?,
            ),
            invocation_token,
        })
    }

    pub async fn run(
        &mut self,
        mut request: pb::AgentManagerRunRequest,
    ) -> std::result::Result<pb::ManagedAgentRun, AgentManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self.client.run(request).await?.into_inner())
    }

    pub async fn get_run(
        &mut self,
        mut request: pb::AgentManagerGetRunRequest,
    ) -> std::result::Result<pb::ManagedAgentRun, AgentManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self.client.get_run(request).await?.into_inner())
    }

    pub async fn list_runs(
        &mut self,
        mut request: pb::AgentManagerListRunsRequest,
    ) -> std::result::Result<pb::AgentManagerListRunsResponse, AgentManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self.client.list_runs(request).await?.into_inner())
    }

    pub async fn cancel_run(
        &mut self,
        mut request: pb::AgentManagerCancelRunRequest,
    ) -> std::result::Result<pb::ManagedAgentRun, AgentManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self.client.cancel_run(request).await?.into_inner())
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
                .insert(AGENT_MANAGER_RELAY_TOKEN_HEADER, token);
        }
        Ok(request)
    }
}

fn relay_token_interceptor(
    token: &str,
) -> std::result::Result<RelayTokenInterceptor, AgentManagerError> {
    let trimmed = token.trim();
    let token = if trimmed.is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(trimmed).map_err(|err| {
            AgentManagerError::Env(format!(
                "agent manager: invalid relay token metadata: {err}"
            ))
        })?)
    };
    Ok(RelayTokenInterceptor { token })
}

enum AgentManagerTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_agent_manager_target(
    raw: &str,
) -> std::result::Result<AgentManagerTarget, AgentManagerError> {
    let target = raw.trim();
    if target.is_empty() {
        return Err(AgentManagerError::Env(
            "agent manager: transport target is required".to_string(),
        ));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(AgentManagerError::Env(format!(
                "agent manager: tcp target {raw:?} is missing host:port"
            )));
        }
        return Ok(AgentManagerTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(AgentManagerError::Env(format!(
                "agent manager: tls target {raw:?} is missing host:port"
            )));
        }
        return Ok(AgentManagerTarget::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(AgentManagerError::Env(format!(
                "agent manager: unix target {raw:?} is missing a socket path"
            )));
        }
        return Ok(AgentManagerTarget::Unix(path.to_string()));
    }
    if target.contains("://") {
        return Err(AgentManagerError::Env(format!(
            "agent manager: unsupported target scheme in {raw:?}"
        )));
    }
    Ok(AgentManagerTarget::Unix(target.to_string()))
}
