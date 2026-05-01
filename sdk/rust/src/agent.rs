use std::sync::Arc;

use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::codegen::async_trait;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, Endpoint, Uri};
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};
use tower::service_fn;

use crate::api::RuntimeMetadata;
use crate::error::Result as ProviderResult;
use crate::generated::v1::{
    self as pb, agent_host_client::AgentHostClient as ProtoAgentHostClient,
};

type AgentHostTransport = InterceptedService<Channel, AgentHostRelayTokenInterceptor>;

/// Environment variable containing the agent-host service target.
pub const ENV_AGENT_HOST_SOCKET: &str = "GESTALT_AGENT_HOST_SOCKET";
/// Environment variable containing the optional agent-host relay token.
pub const ENV_AGENT_HOST_SOCKET_TOKEN: &str = "GESTALT_AGENT_HOST_SOCKET_TOKEN";
const AGENT_HOST_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

#[derive(Debug, thiserror::Error)]
/// Errors returned by [`AgentHost`].
pub enum AgentHostError {
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

/// Client for the agent host service available inside agent providers.
pub struct AgentHost {
    client: ProtoAgentHostClient<AgentHostTransport>,
}

impl AgentHost {
    /// Connects to the agent host service described by the environment.
    pub async fn connect() -> std::result::Result<Self, AgentHostError> {
        let target = std::env::var(ENV_AGENT_HOST_SOCKET)
            .map_err(|_| AgentHostError::Env(format!("{ENV_AGENT_HOST_SOCKET} is not set")))?;
        let relay_token = std::env::var(ENV_AGENT_HOST_SOCKET_TOKEN).unwrap_or_default();
        let channel = match parse_agent_host_target(&target)? {
            AgentHostTarget::Unix(path) => connect_unix(path).await?,
            AgentHostTarget::Tcp(address) => {
                Endpoint::from_shared(format!("http://{address}"))?
                    .connect()
                    .await?
            }
            AgentHostTarget::Tls(address) => {
                Endpoint::from_shared(format!("https://{address}"))?
                    .connect()
                    .await?
            }
        };
        Ok(Self {
            client: ProtoAgentHostClient::with_interceptor(
                channel,
                agent_host_relay_token_interceptor(relay_token.trim())?,
            ),
        })
    }

    /// Executes a host tool using an agent protocol request message.
    pub async fn execute_tool(
        &mut self,
        request: pb::ExecuteAgentToolRequest,
    ) -> std::result::Result<pb::ExecuteAgentToolResponse, AgentHostError> {
        Ok(self.client.execute_tool(request).await?.into_inner())
    }

    /// Lists host tools visible to the current agent request.
    pub async fn list_tools(
        &mut self,
        request: pb::ListAgentToolsRequest,
    ) -> std::result::Result<pb::ListAgentToolsResponse, AgentHostError> {
        Ok(self.client.list_tools(request).await?.into_inner())
    }
}

async fn connect_unix(
    socket_path: String,
) -> std::result::Result<Channel, tonic::transport::Error> {
    Endpoint::try_from("http://[::]:50051")?
        .connect_with_connector(service_fn(move |_: Uri| {
            let path = socket_path.clone();
            async move { UnixStream::connect(path).await.map(TokioIo::new) }
        }))
        .await
}

#[derive(Clone)]
struct AgentHostRelayTokenInterceptor {
    token: Option<MetadataValue<tonic::metadata::Ascii>>,
}

impl Interceptor for AgentHostRelayTokenInterceptor {
    fn call(
        &mut self,
        mut request: tonic::Request<()>,
    ) -> std::result::Result<tonic::Request<()>, tonic::Status> {
        if let Some(token) = self.token.clone() {
            request
                .metadata_mut()
                .insert(AGENT_HOST_RELAY_TOKEN_HEADER, token);
        }
        Ok(request)
    }
}

fn agent_host_relay_token_interceptor(
    token: &str,
) -> std::result::Result<AgentHostRelayTokenInterceptor, AgentHostError> {
    let trimmed = token.trim();
    let token = if trimmed.is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(trimmed).map_err(|err| {
            AgentHostError::Env(format!("agent host: invalid relay token metadata: {err}"))
        })?)
    };
    Ok(AgentHostRelayTokenInterceptor { token })
}

enum AgentHostTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_agent_host_target(raw: &str) -> std::result::Result<AgentHostTarget, AgentHostError> {
    let target = raw.trim();
    if target.is_empty() {
        return Err(AgentHostError::Env(
            "agent host: transport target is required".to_string(),
        ));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(AgentHostError::Env(format!(
                "agent host: tcp target {raw:?} is missing host:port"
            )));
        }
        return Ok(AgentHostTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(AgentHostError::Env(format!(
                "agent host: tls target {raw:?} is missing host:port"
            )));
        }
        return Ok(AgentHostTarget::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(AgentHostError::Env(format!(
                "agent host: unix target {raw:?} is missing a socket path"
            )));
        }
        return Ok(AgentHostTarget::Unix(path.to_string()));
    }
    if target.contains("://") {
        return Err(AgentHostError::Env(format!(
            "agent host: unsupported target scheme in {raw:?}"
        )));
    }
    Ok(AgentHostTarget::Unix(target.to_string()))
}

#[async_trait]
/// Provider trait for serving the Gestalt agent-provider protocol.
pub trait AgentProvider: pb::agent_provider_server::AgentProvider + Send + Sync + 'static {
    /// Configures the provider before it starts serving requests.
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> ProviderResult<()> {
        Ok(())
    }

    /// Returns runtime metadata that should augment the static manifest.
    fn metadata(&self) -> Option<RuntimeMetadata> {
        None
    }

    /// Returns non-fatal warnings the host should surface to users.
    fn warnings(&self) -> Vec<String> {
        Vec::new()
    }

    /// Performs an optional health check.
    async fn health_check(&self) -> ProviderResult<()> {
        Ok(())
    }

    /// Starts provider-owned background work after configuration.
    async fn start(&self) -> ProviderResult<()> {
        Ok(())
    }

    /// Shuts the provider down before the runtime exits.
    async fn close(&self) -> ProviderResult<()> {
        Ok(())
    }
}

#[derive(Clone)]
pub(crate) struct AgentServer<P> {
    provider: Arc<P>,
}

impl<P> AgentServer<P> {
    pub(crate) fn new(provider: Arc<P>) -> Self {
        Self { provider }
    }
}

#[async_trait]
impl<P> pb::agent_provider_server::AgentProvider for AgentServer<P>
where
    P: AgentProvider,
{
    async fn create_session(
        &self,
        request: GrpcRequest<pb::CreateAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentSession>, Status> {
        self.provider.create_session(request).await
    }

    async fn get_session(
        &self,
        request: GrpcRequest<pb::GetAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentSession>, Status> {
        self.provider.get_session(request).await
    }

    async fn list_sessions(
        &self,
        request: GrpcRequest<pb::ListAgentProviderSessionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderSessionsResponse>, Status> {
        self.provider.list_sessions(request).await
    }

    async fn update_session(
        &self,
        request: GrpcRequest<pb::UpdateAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentSession>, Status> {
        self.provider.update_session(request).await
    }

    async fn create_turn(
        &self,
        request: GrpcRequest<pb::CreateAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        self.provider.create_turn(request).await
    }

    async fn get_turn(
        &self,
        request: GrpcRequest<pb::GetAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        self.provider.get_turn(request).await
    }

    async fn list_turns(
        &self,
        request: GrpcRequest<pb::ListAgentProviderTurnsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderTurnsResponse>, Status> {
        self.provider.list_turns(request).await
    }

    async fn cancel_turn(
        &self,
        request: GrpcRequest<pb::CancelAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        self.provider.cancel_turn(request).await
    }

    async fn list_turn_events(
        &self,
        request: GrpcRequest<pb::ListAgentProviderTurnEventsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderTurnEventsResponse>, Status> {
        self.provider.list_turn_events(request).await
    }

    async fn get_interaction(
        &self,
        request: GrpcRequest<pb::GetAgentProviderInteractionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentInteraction>, Status> {
        self.provider.get_interaction(request).await
    }

    async fn list_interactions(
        &self,
        request: GrpcRequest<pb::ListAgentProviderInteractionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderInteractionsResponse>, Status> {
        self.provider.list_interactions(request).await
    }

    async fn resolve_interaction(
        &self,
        request: GrpcRequest<pb::ResolveAgentProviderInteractionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentInteraction>, Status> {
        self.provider.resolve_interaction(request).await
    }

    async fn get_capabilities(
        &self,
        request: GrpcRequest<pb::GetAgentProviderCapabilitiesRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentProviderCapabilities>, Status> {
        self.provider.get_capabilities(request).await
    }
}
