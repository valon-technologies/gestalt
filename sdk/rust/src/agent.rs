use std::sync::Arc;

use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::codegen::async_trait;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};
use tower::service_fn;

use crate::api::RuntimeMetadata;
use crate::error::Result as ProviderResult;
use crate::generated::v1::{
    self as pb, agent_host_client::AgentHostClient as ProtoAgentHostClient,
};
use crate::protocol;
use crate::rpc_status::rpc_status;

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
    /// Plain input could not be converted into the protocol request shape.
    #[error("{0}")]
    Input(#[from] crate::Error),
    /// Required environment or target configuration was invalid.
    #[error("{0}")]
    Env(String),
}

/// Plain input for listing tools available to one agent turn.
#[derive(Debug, Clone, Default)]
pub struct AgentHostListToolsInput {
    /// Agent session ID.
    pub session_id: String,
    /// Agent turn ID.
    pub turn_id: String,
    /// Optional run grant scoped to this turn.
    pub run_grant: String,
    /// Maximum number of tools to return.
    pub page_size: i32,
    /// Opaque page token returned by a previous list call.
    pub page_token: String,
    /// Optional server-side tool search query.
    pub query: String,
}

/// Plain input for executing a host tool during one agent turn.
#[derive(Debug, Clone, Default)]
pub struct AgentHostExecuteToolInput {
    /// Agent session ID.
    pub session_id: String,
    /// Agent turn ID.
    pub turn_id: String,
    /// Tool call ID from the agent message.
    pub tool_call_id: String,
    /// Host tool ID to execute.
    pub tool_id: String,
    /// JSON object to pass as tool arguments.
    pub arguments: Option<serde_json::Value>,
    /// Optional run grant scoped to this turn.
    pub run_grant: String,
    /// Caller-supplied idempotency key for retries.
    pub idempotency_key: String,
}

/// Plain input for resolving a configured connection during one agent turn.
#[derive(Debug, Clone, Default)]
pub struct AgentHostResolveConnectionInput {
    /// Agent session ID.
    pub session_id: String,
    /// Agent turn ID.
    pub turn_id: String,
    /// Connection name to resolve.
    pub connection: String,
    /// Optional connection instance.
    pub instance: String,
    /// Optional run grant scoped to this turn.
    pub run_grant: String,
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
                    .tls_config(ClientTlsConfig::new().with_native_roots())?
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

    /// Executes a host tool using plain Rust request fields.
    pub async fn execute_tool_for_turn(
        &mut self,
        input: AgentHostExecuteToolInput,
    ) -> std::result::Result<pb::ExecuteAgentToolResponse, AgentHostError> {
        self.execute_tool(pb::ExecuteAgentToolRequest {
            session_id: input.session_id,
            turn_id: input.turn_id,
            tool_call_id: input.tool_call_id,
            tool_id: input.tool_id,
            arguments: input
                .arguments
                .map(protocol::struct_from_json)
                .transpose()?,
            run_grant: input.run_grant,
            idempotency_key: input.idempotency_key,
        })
        .await
    }

    /// Lists host tools visible to the current agent request.
    pub async fn list_tools(
        &mut self,
        request: pb::ListAgentToolsRequest,
    ) -> std::result::Result<pb::ListAgentToolsResponse, AgentHostError> {
        Ok(self.client.list_tools(request).await?.into_inner())
    }

    /// Lists host tools using plain Rust request fields.
    pub async fn list_tools_for_turn(
        &mut self,
        input: AgentHostListToolsInput,
    ) -> std::result::Result<pb::ListAgentToolsResponse, AgentHostError> {
        self.list_tools(pb::ListAgentToolsRequest {
            session_id: input.session_id,
            turn_id: input.turn_id,
            run_grant: input.run_grant,
            page_size: input.page_size,
            page_token: input.page_token,
            query: input.query,
        })
        .await
    }

    /// Resolves a configured agent connection for the current turn.
    pub async fn resolve_connection(
        &mut self,
        request: pb::ResolveAgentConnectionRequest,
    ) -> std::result::Result<pb::ResolvedAgentConnection, AgentHostError> {
        Ok(self.client.resolve_connection(request).await?.into_inner())
    }

    /// Resolves an agent connection using plain Rust request fields.
    pub async fn resolve_connection_for_turn(
        &mut self,
        input: AgentHostResolveConnectionInput,
    ) -> std::result::Result<pb::ResolvedAgentConnection, AgentHostError> {
        self.resolve_connection(pb::ResolveAgentConnectionRequest {
            session_id: input.session_id,
            turn_id: input.turn_id,
            connection: input.connection,
            instance: input.instance,
            run_grant: input.run_grant,
        })
        .await
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
pub trait AgentProvider: Send + Sync + 'static {
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

    async fn create_session(
        &self,
        _request: pb::CreateAgentProviderSessionRequest,
    ) -> ProviderResult<pb::AgentSession> {
        Err(crate::Error::unimplemented(
            "agent create session is not implemented",
        ))
    }

    async fn get_session(
        &self,
        _request: pb::GetAgentProviderSessionRequest,
    ) -> ProviderResult<pb::AgentSession> {
        Err(crate::Error::unimplemented(
            "agent get session is not implemented",
        ))
    }

    async fn list_sessions(
        &self,
        _request: pb::ListAgentProviderSessionsRequest,
    ) -> ProviderResult<pb::ListAgentProviderSessionsResponse> {
        Err(crate::Error::unimplemented(
            "agent list sessions is not implemented",
        ))
    }

    async fn update_session(
        &self,
        _request: pb::UpdateAgentProviderSessionRequest,
    ) -> ProviderResult<pb::AgentSession> {
        Err(crate::Error::unimplemented(
            "agent update session is not implemented",
        ))
    }

    async fn create_turn(
        &self,
        _request: pb::CreateAgentProviderTurnRequest,
    ) -> ProviderResult<pb::AgentTurn> {
        Err(crate::Error::unimplemented(
            "agent create turn is not implemented",
        ))
    }

    async fn get_turn(
        &self,
        _request: pb::GetAgentProviderTurnRequest,
    ) -> ProviderResult<pb::AgentTurn> {
        Err(crate::Error::unimplemented(
            "agent get turn is not implemented",
        ))
    }

    async fn list_turns(
        &self,
        _request: pb::ListAgentProviderTurnsRequest,
    ) -> ProviderResult<pb::ListAgentProviderTurnsResponse> {
        Err(crate::Error::unimplemented(
            "agent list turns is not implemented",
        ))
    }

    async fn cancel_turn(
        &self,
        _request: pb::CancelAgentProviderTurnRequest,
    ) -> ProviderResult<pb::AgentTurn> {
        Err(crate::Error::unimplemented(
            "agent cancel turn is not implemented",
        ))
    }

    async fn list_turn_events(
        &self,
        _request: pb::ListAgentProviderTurnEventsRequest,
    ) -> ProviderResult<pb::ListAgentProviderTurnEventsResponse> {
        Err(crate::Error::unimplemented(
            "agent list turn events is not implemented",
        ))
    }

    async fn get_interaction(
        &self,
        _request: pb::GetAgentProviderInteractionRequest,
    ) -> ProviderResult<pb::AgentInteraction> {
        Err(crate::Error::unimplemented(
            "agent get interaction is not implemented",
        ))
    }

    async fn list_interactions(
        &self,
        _request: pb::ListAgentProviderInteractionsRequest,
    ) -> ProviderResult<pb::ListAgentProviderInteractionsResponse> {
        Err(crate::Error::unimplemented(
            "agent list interactions is not implemented",
        ))
    }

    async fn resolve_interaction(
        &self,
        _request: pb::ResolveAgentProviderInteractionRequest,
    ) -> ProviderResult<pb::AgentInteraction> {
        Err(crate::Error::unimplemented(
            "agent resolve interaction is not implemented",
        ))
    }

    async fn get_capabilities(
        &self,
        _request: pb::GetAgentProviderCapabilitiesRequest,
    ) -> ProviderResult<pb::AgentProviderCapabilities> {
        Err(crate::Error::unimplemented(
            "agent get capabilities is not implemented",
        ))
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
        let session = self
            .provider
            .create_session(request.into_inner())
            .await
            .map_err(|error| rpc_status("agent create session", error))?;
        Ok(GrpcResponse::new(session))
    }

    async fn get_session(
        &self,
        request: GrpcRequest<pb::GetAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentSession>, Status> {
        let session = self
            .provider
            .get_session(request.into_inner())
            .await
            .map_err(|error| rpc_status("agent get session", error))?;
        Ok(GrpcResponse::new(session))
    }

    async fn list_sessions(
        &self,
        request: GrpcRequest<pb::ListAgentProviderSessionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderSessionsResponse>, Status> {
        let response = self
            .provider
            .list_sessions(request.into_inner())
            .await
            .map_err(|error| rpc_status("agent list sessions", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn update_session(
        &self,
        request: GrpcRequest<pb::UpdateAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentSession>, Status> {
        let session = self
            .provider
            .update_session(request.into_inner())
            .await
            .map_err(|error| rpc_status("agent update session", error))?;
        Ok(GrpcResponse::new(session))
    }

    async fn create_turn(
        &self,
        request: GrpcRequest<pb::CreateAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        let turn = self
            .provider
            .create_turn(request.into_inner())
            .await
            .map_err(|error| rpc_status("agent create turn", error))?;
        Ok(GrpcResponse::new(turn))
    }

    async fn get_turn(
        &self,
        request: GrpcRequest<pb::GetAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        let turn = self
            .provider
            .get_turn(request.into_inner())
            .await
            .map_err(|error| rpc_status("agent get turn", error))?;
        Ok(GrpcResponse::new(turn))
    }

    async fn list_turns(
        &self,
        request: GrpcRequest<pb::ListAgentProviderTurnsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderTurnsResponse>, Status> {
        let response = self
            .provider
            .list_turns(request.into_inner())
            .await
            .map_err(|error| rpc_status("agent list turns", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn cancel_turn(
        &self,
        request: GrpcRequest<pb::CancelAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        let turn = self
            .provider
            .cancel_turn(request.into_inner())
            .await
            .map_err(|error| rpc_status("agent cancel turn", error))?;
        Ok(GrpcResponse::new(turn))
    }

    async fn list_turn_events(
        &self,
        request: GrpcRequest<pb::ListAgentProviderTurnEventsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderTurnEventsResponse>, Status> {
        let response = self
            .provider
            .list_turn_events(request.into_inner())
            .await
            .map_err(|error| rpc_status("agent list turn events", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn get_interaction(
        &self,
        request: GrpcRequest<pb::GetAgentProviderInteractionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentInteraction>, Status> {
        let interaction = self
            .provider
            .get_interaction(request.into_inner())
            .await
            .map_err(|error| rpc_status("agent get interaction", error))?;
        Ok(GrpcResponse::new(interaction))
    }

    async fn list_interactions(
        &self,
        request: GrpcRequest<pb::ListAgentProviderInteractionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderInteractionsResponse>, Status> {
        let response = self
            .provider
            .list_interactions(request.into_inner())
            .await
            .map_err(|error| rpc_status("agent list interactions", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn resolve_interaction(
        &self,
        request: GrpcRequest<pb::ResolveAgentProviderInteractionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentInteraction>, Status> {
        let interaction = self
            .provider
            .resolve_interaction(request.into_inner())
            .await
            .map_err(|error| rpc_status("agent resolve interaction", error))?;
        Ok(GrpcResponse::new(interaction))
    }

    async fn get_capabilities(
        &self,
        request: GrpcRequest<pb::GetAgentProviderCapabilitiesRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentProviderCapabilities>, Status> {
        let capabilities = self
            .provider
            .get_capabilities(request.into_inner())
            .await
            .map_err(|error| rpc_status("agent get capabilities", error))?;
        Ok(GrpcResponse::new(capabilities))
    }
}
