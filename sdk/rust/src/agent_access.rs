use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::Request as GrpcRequest;
use tonic::codegen::async_trait;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tower::service_fn;

use crate::generated::v1::{
    self as pb, agent_provider_client::AgentProviderClient as ProtoAgentProviderClient,
};
use crate::{
    agent::{
        AgentExecutionStatus, AgentInteraction, AgentMessage, AgentOutput, AgentSession,
        AgentSessionState, AgentToolConfig, AgentTurn, AgentTurnEvent, AgentWorkspace,
        agent_output_to_proto, event_from_proto, interaction_from_proto, new_agent_messages,
        new_agent_tool_config, new_agent_workspace, session_from_proto, turn_from_proto,
    },
    api::{Request, current_request_context},
    env::{ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN},
    protocol,
};

type AgentTransport = InterceptedService<Channel, RelayTokenInterceptor>;

const AGENT_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

#[derive(Debug, thiserror::Error)]
/// Errors returned by [`Agent`].
pub enum AgentError {
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

/// Input for creating an agent session.
#[derive(Clone, Debug, Default)]
pub struct AgentCreateSession {
    pub provider_name: String,
    pub model: String,
    pub client_ref: String,
    pub metadata: Option<serde_json::Value>,
    pub idempotency_key: String,
    pub workspace: Option<AgentWorkspace>,
    pub tools: Option<AgentToolConfig>,
}

/// Input for fetching an agent session.
#[derive(Clone, Debug, Default)]
pub struct AgentGetSession {
    pub provider_name: String,
    pub session_id: String,
}

/// Input for listing agent sessions.
#[derive(Clone, Debug)]
pub struct AgentListSessions {
    pub provider_name: String,
    pub state: AgentSessionState,
    pub limit: i32,
    pub summary_only: bool,
}

impl Default for AgentListSessions {
    fn default() -> Self {
        Self {
            provider_name: String::new(),
            state: AgentSessionState::Unspecified,
            limit: 0,
            summary_only: false,
        }
    }
}

/// Input for updating an agent session.
#[derive(Clone, Debug)]
pub struct AgentUpdateSession {
    pub provider_name: String,
    pub session_id: String,
    pub client_ref: String,
    pub state: AgentSessionState,
    pub metadata: Option<serde_json::Value>,
}

impl Default for AgentUpdateSession {
    fn default() -> Self {
        Self {
            provider_name: String::new(),
            session_id: String::new(),
            client_ref: String::new(),
            state: AgentSessionState::Unspecified,
            metadata: None,
        }
    }
}

/// Input for creating an agent turn.
#[derive(Clone, Debug)]
pub struct AgentCreateTurn {
    pub provider_name: String,
    pub session_id: String,
    pub model: String,
    pub messages: Vec<AgentMessage>,
    pub output: AgentOutput,
    pub metadata: Option<serde_json::Value>,
    pub idempotency_key: String,
    pub model_options: Option<serde_json::Value>,
    pub timeout_seconds: i32,
}

/// Input for fetching an agent turn.
#[derive(Clone, Debug, Default)]
pub struct AgentGetTurn {
    pub provider_name: String,
    pub turn_id: String,
}

/// Input for listing agent turns.
#[derive(Clone, Debug)]
pub struct AgentListTurns {
    pub provider_name: String,
    pub session_id: String,
    pub status: AgentExecutionStatus,
    pub limit: i32,
    pub summary_only: bool,
}

impl Default for AgentListTurns {
    fn default() -> Self {
        Self {
            provider_name: String::new(),
            session_id: String::new(),
            status: AgentExecutionStatus::Unspecified,
            limit: 0,
            summary_only: false,
        }
    }
}

/// Input for canceling an agent turn.
#[derive(Clone, Debug, Default)]
pub struct AgentCancelTurn {
    pub provider_name: String,
    pub turn_id: String,
    pub reason: String,
}

/// Input for listing agent turn events.
#[derive(Clone, Debug, Default)]
pub struct AgentListTurnEvents {
    pub provider_name: String,
    pub turn_id: String,
    pub after_seq: i64,
    pub limit: i32,
}

/// Input for listing agent interactions.
#[derive(Clone, Debug, Default)]
pub struct AgentListInteractions {
    pub provider_name: String,
    pub turn_id: String,
}

/// Input for resolving an agent interaction.
#[derive(Clone, Debug, Default)]
pub struct AgentResolveInteraction {
    pub provider_name: String,
    pub turn_id: String,
    pub interaction_id: String,
    pub resolution: Option<serde_json::Value>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct AgentListSessionsResponse {
    pub sessions: Vec<AgentSession>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct AgentListTurnsResponse {
    pub turns: Vec<AgentTurn>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct AgentListTurnEventsResponse {
    pub events: Vec<AgentTurnEvent>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct AgentListInteractionsResponse {
    pub interactions: Vec<AgentInteraction>,
}

#[async_trait]
/// Fakeable client contract for managing agent sessions and turns.
pub trait AgentContract: Send {
    async fn create_session(
        &mut self,
        input: AgentCreateSession,
    ) -> std::result::Result<AgentSession, AgentError>;
    async fn get_session(
        &mut self,
        input: AgentGetSession,
    ) -> std::result::Result<AgentSession, AgentError>;
    async fn list_sessions(
        &mut self,
        input: AgentListSessions,
    ) -> std::result::Result<AgentListSessionsResponse, AgentError>;
    async fn update_session(
        &mut self,
        input: AgentUpdateSession,
    ) -> std::result::Result<AgentSession, AgentError>;
    async fn create_turn(
        &mut self,
        input: AgentCreateTurn,
    ) -> std::result::Result<AgentTurn, AgentError>;
    async fn get_turn(&mut self, input: AgentGetTurn)
    -> std::result::Result<AgentTurn, AgentError>;
    async fn list_turns(
        &mut self,
        input: AgentListTurns,
    ) -> std::result::Result<AgentListTurnsResponse, AgentError>;
    async fn cancel_turn(
        &mut self,
        input: AgentCancelTurn,
    ) -> std::result::Result<AgentTurn, AgentError>;
    async fn list_turn_events(
        &mut self,
        input: AgentListTurnEvents,
    ) -> std::result::Result<AgentListTurnEventsResponse, AgentError>;
    async fn list_interactions(
        &mut self,
        input: AgentListInteractions,
    ) -> std::result::Result<AgentListInteractionsResponse, AgentError>;
    async fn resolve_interaction(
        &mut self,
        input: AgentResolveInteraction,
    ) -> std::result::Result<AgentInteraction, AgentError>;
}

/// Creates a protocol create-session request.
pub(crate) fn new_agent_create_session_request(
    input: AgentCreateSession,
) -> crate::Result<pb::CreateAgentProviderSessionRequest> {
    Ok(pb::CreateAgentProviderSessionRequest {
        provider_name: input.provider_name,
        model: input.model,
        client_ref: input.client_ref,
        metadata: input.metadata.map(protocol::struct_from_json).transpose()?,
        idempotency_key: input.idempotency_key,
        workspace: input.workspace.map(new_agent_workspace),
        tools: input.tools.map(new_agent_tool_config),
        ..Default::default()
    })
}

pub(crate) fn new_agent_get_session_request(
    input: AgentGetSession,
) -> pb::GetAgentProviderSessionRequest {
    pb::GetAgentProviderSessionRequest {
        provider_name: input.provider_name,
        session_id: input.session_id,
        ..Default::default()
    }
}

pub(crate) fn new_agent_list_sessions_request(
    input: AgentListSessions,
) -> pb::ListAgentProviderSessionsRequest {
    pb::ListAgentProviderSessionsRequest {
        provider_name: input.provider_name,
        state: input.state.as_i32(),
        limit: input.limit,
        summary_only: input.summary_only,
        ..Default::default()
    }
}

pub(crate) fn new_agent_update_session_request(
    input: AgentUpdateSession,
) -> crate::Result<pb::UpdateAgentProviderSessionRequest> {
    Ok(pb::UpdateAgentProviderSessionRequest {
        provider_name: input.provider_name,
        session_id: input.session_id,
        client_ref: input.client_ref,
        state: input.state.as_i32(),
        metadata: input.metadata.map(protocol::struct_from_json).transpose()?,
        ..Default::default()
    })
}

pub(crate) fn new_agent_create_turn_request(
    input: AgentCreateTurn,
) -> crate::Result<pb::CreateAgentProviderTurnRequest> {
    if input.timeout_seconds < 0 {
        return Err(crate::Error::bad_request(
            "agent create turn timeout_seconds must not be negative",
        ));
    }
    Ok(pb::CreateAgentProviderTurnRequest {
        provider_name: input.provider_name,
        session_id: input.session_id,
        model: input.model,
        messages: new_agent_messages(input.messages)?,
        output: agent_output_to_proto(Some(input.output))?,
        metadata: input.metadata.map(protocol::struct_from_json).transpose()?,
        idempotency_key: input.idempotency_key,
        model_options: input
            .model_options
            .map(protocol::struct_from_json)
            .transpose()?,
        timeout_seconds: input.timeout_seconds,
        ..Default::default()
    })
}

pub(crate) fn new_agent_get_turn_request(input: AgentGetTurn) -> pb::GetAgentProviderTurnRequest {
    pb::GetAgentProviderTurnRequest {
        provider_name: input.provider_name,
        turn_id: input.turn_id,
        ..Default::default()
    }
}

pub(crate) fn new_agent_list_turns_request(
    input: AgentListTurns,
) -> pb::ListAgentProviderTurnsRequest {
    pb::ListAgentProviderTurnsRequest {
        provider_name: input.provider_name,
        session_id: input.session_id,
        status: input.status.as_i32(),
        limit: input.limit,
        summary_only: input.summary_only,
        ..Default::default()
    }
}

pub(crate) fn new_agent_cancel_turn_request(
    input: AgentCancelTurn,
) -> pb::CancelAgentProviderTurnRequest {
    pb::CancelAgentProviderTurnRequest {
        provider_name: input.provider_name,
        turn_id: input.turn_id,
        reason: input.reason,
        ..Default::default()
    }
}

pub(crate) fn new_agent_list_turn_events_request(
    input: AgentListTurnEvents,
) -> pb::ListAgentProviderTurnEventsRequest {
    pb::ListAgentProviderTurnEventsRequest {
        provider_name: input.provider_name,
        turn_id: input.turn_id,
        after_seq: input.after_seq,
        limit: input.limit,
        ..Default::default()
    }
}

pub(crate) fn new_agent_list_interactions_request(
    input: AgentListInteractions,
) -> pb::ListAgentProviderInteractionsRequest {
    pb::ListAgentProviderInteractionsRequest {
        provider_name: input.provider_name,
        turn_id: input.turn_id,
        ..Default::default()
    }
}

pub(crate) fn new_agent_resolve_interaction_request(
    input: AgentResolveInteraction,
) -> crate::Result<pb::ResolveAgentProviderInteractionRequest> {
    Ok(pb::ResolveAgentProviderInteractionRequest {
        provider_name: input.provider_name,
        turn_id: input.turn_id,
        interaction_id: input.interaction_id,
        resolution: input
            .resolution
            .map(protocol::struct_from_json)
            .transpose()?,
        ..Default::default()
    })
}

/// Client for managing agent sessions, turns, events, and interactions.
pub struct Agent {
    client: ProtoAgentProviderClient<AgentTransport>,
    context: Option<pb::RequestContext>,
}

impl Agent {
    /// Connects to the agent service with host request context from Gestalt.
    pub async fn connect(_request: &Request) -> std::result::Result<Self, AgentError> {
        let context = current_request_context();
        let socket_path = std::env::var(ENV_HOST_SERVICE_SOCKET)
            .map_err(|_| AgentError::Env(format!("{ENV_HOST_SERVICE_SOCKET} is not set")))?;
        let relay_token = std::env::var(ENV_HOST_SERVICE_TOKEN).unwrap_or_default();
        let channel = match parse_agent_target(&socket_path)? {
            AgentTarget::Unix(path) => {
                Endpoint::try_from("http://[::]:50051")?
                    .connect_with_connector(service_fn(move |_: Uri| {
                        let path = path.clone();
                        async move { UnixStream::connect(path).await.map(TokioIo::new) }
                    }))
                    .await?
            }
            AgentTarget::Tcp(address) => {
                Endpoint::from_shared(format!("http://{address}"))?
                    .connect()
                    .await?
            }
            AgentTarget::Tls(address) => {
                Endpoint::from_shared(format!("https://{address}"))?
                    .tls_config(ClientTlsConfig::new().with_native_roots())?
                    .connect()
                    .await?
            }
        };

        Ok(Self {
            client: ProtoAgentProviderClient::with_interceptor(
                channel,
                relay_token_interceptor(relay_token.trim())?,
            ),
            context,
        })
    }

    /// Creates an agent session.
    pub async fn create_session(
        &mut self,
        input: AgentCreateSession,
    ) -> std::result::Result<AgentSession, AgentError> {
        let mut request = new_agent_create_session_request(input)?;
        self.attach_context(&mut request);
        Ok(session_from_proto(
            self.client.create_session(request).await?.into_inner(),
        )?)
    }

    /// Fetches one agent session.
    pub async fn get_session(
        &mut self,
        input: AgentGetSession,
    ) -> std::result::Result<AgentSession, AgentError> {
        let mut request = new_agent_get_session_request(input);
        self.attach_context(&mut request);
        Ok(session_from_proto(
            self.client.get_session(request).await?.into_inner(),
        )?)
    }

    /// Lists agent sessions visible to the request context.
    pub async fn list_sessions(
        &mut self,
        input: AgentListSessions,
    ) -> std::result::Result<AgentListSessionsResponse, AgentError> {
        let mut request = new_agent_list_sessions_request(input);
        self.attach_context(&mut request);
        let response = self.client.list_sessions(request).await?.into_inner();
        Ok(AgentListSessionsResponse {
            sessions: response
                .sessions
                .into_iter()
                .map(session_from_proto)
                .collect::<std::result::Result<Vec<_>, _>>()?,
        })
    }

    /// Updates mutable fields on an agent session.
    pub async fn update_session(
        &mut self,
        input: AgentUpdateSession,
    ) -> std::result::Result<AgentSession, AgentError> {
        let mut request = new_agent_update_session_request(input)?;
        self.attach_context(&mut request);
        Ok(session_from_proto(
            self.client.update_session(request).await?.into_inner(),
        )?)
    }

    /// Creates an agent turn.
    pub async fn create_turn(
        &mut self,
        input: AgentCreateTurn,
    ) -> std::result::Result<AgentTurn, AgentError> {
        let mut request = new_agent_create_turn_request(input)?;
        self.attach_context(&mut request);
        Ok(turn_from_proto(
            self.client.create_turn(request).await?.into_inner(),
        )?)
    }

    /// Fetches one agent turn.
    pub async fn get_turn(
        &mut self,
        input: AgentGetTurn,
    ) -> std::result::Result<AgentTurn, AgentError> {
        let mut request = new_agent_get_turn_request(input);
        self.attach_context(&mut request);
        Ok(turn_from_proto(
            self.client.get_turn(request).await?.into_inner(),
        )?)
    }

    /// Lists turns for an agent session.
    pub async fn list_turns(
        &mut self,
        input: AgentListTurns,
    ) -> std::result::Result<AgentListTurnsResponse, AgentError> {
        let mut request = new_agent_list_turns_request(input);
        self.attach_context(&mut request);
        let response = self.client.list_turns(request).await?.into_inner();
        Ok(AgentListTurnsResponse {
            turns: response
                .turns
                .into_iter()
                .map(turn_from_proto)
                .collect::<std::result::Result<Vec<_>, _>>()?,
        })
    }

    /// Cancels an in-progress agent turn.
    pub async fn cancel_turn(
        &mut self,
        input: AgentCancelTurn,
    ) -> std::result::Result<AgentTurn, AgentError> {
        let mut request = new_agent_cancel_turn_request(input);
        self.attach_context(&mut request);
        Ok(turn_from_proto(
            self.client.cancel_turn(request).await?.into_inner(),
        )?)
    }

    /// Lists events emitted for an agent turn.
    pub async fn list_turn_events(
        &mut self,
        input: AgentListTurnEvents,
    ) -> std::result::Result<AgentListTurnEventsResponse, AgentError> {
        let mut request = new_agent_list_turn_events_request(input);
        self.attach_context(&mut request);
        let response = self.client.list_turn_events(request).await?.into_inner();
        Ok(AgentListTurnEventsResponse {
            events: response
                .events
                .into_iter()
                .map(event_from_proto)
                .collect::<std::result::Result<Vec<_>, _>>()?,
        })
    }

    /// Lists pending or completed agent interactions.
    pub async fn list_interactions(
        &mut self,
        input: AgentListInteractions,
    ) -> std::result::Result<AgentListInteractionsResponse, AgentError> {
        let mut request = new_agent_list_interactions_request(input);
        self.attach_context(&mut request);
        let response = self.client.list_interactions(request).await?.into_inner();
        Ok(AgentListInteractionsResponse {
            interactions: response
                .interactions
                .into_iter()
                .map(interaction_from_proto)
                .collect::<std::result::Result<Vec<_>, _>>()?,
        })
    }

    /// Resolves an agent interaction with a host response.
    pub async fn resolve_interaction(
        &mut self,
        input: AgentResolveInteraction,
    ) -> std::result::Result<AgentInteraction, AgentError> {
        let mut request = new_agent_resolve_interaction_request(input)?;
        self.attach_context(&mut request);
        Ok(interaction_from_proto(
            self.client.resolve_interaction(request).await?.into_inner(),
        )?)
    }

    fn attach_context<T: HasAgentRequestContext>(&self, request: &mut T) {
        request.set_context(self.context.clone());
    }
}

trait HasAgentRequestContext {
    fn set_context(&mut self, context: Option<pb::RequestContext>);
}

macro_rules! impl_agent_request_context {
    ($($ty:ty),+ $(,)?) => {
        $(
            impl HasAgentRequestContext for $ty {
                fn set_context(&mut self, context: Option<pb::RequestContext>) {
                    self.context = context;
                }
            }
        )+
    };
}

impl_agent_request_context!(
    pb::CreateAgentProviderSessionRequest,
    pb::GetAgentProviderSessionRequest,
    pb::ListAgentProviderSessionsRequest,
    pb::UpdateAgentProviderSessionRequest,
    pb::CreateAgentProviderTurnRequest,
    pb::GetAgentProviderTurnRequest,
    pb::ListAgentProviderTurnsRequest,
    pb::CancelAgentProviderTurnRequest,
    pb::ListAgentProviderTurnEventsRequest,
    pb::ListAgentProviderInteractionsRequest,
    pb::ResolveAgentProviderInteractionRequest,
);

#[async_trait]
impl AgentContract for Agent {
    async fn create_session(
        &mut self,
        input: AgentCreateSession,
    ) -> std::result::Result<AgentSession, AgentError> {
        Agent::create_session(self, input).await
    }

    async fn get_session(
        &mut self,
        input: AgentGetSession,
    ) -> std::result::Result<AgentSession, AgentError> {
        Agent::get_session(self, input).await
    }

    async fn list_sessions(
        &mut self,
        input: AgentListSessions,
    ) -> std::result::Result<AgentListSessionsResponse, AgentError> {
        Agent::list_sessions(self, input).await
    }

    async fn update_session(
        &mut self,
        input: AgentUpdateSession,
    ) -> std::result::Result<AgentSession, AgentError> {
        Agent::update_session(self, input).await
    }

    async fn create_turn(
        &mut self,
        input: AgentCreateTurn,
    ) -> std::result::Result<AgentTurn, AgentError> {
        Agent::create_turn(self, input).await
    }

    async fn get_turn(
        &mut self,
        input: AgentGetTurn,
    ) -> std::result::Result<AgentTurn, AgentError> {
        Agent::get_turn(self, input).await
    }

    async fn list_turns(
        &mut self,
        input: AgentListTurns,
    ) -> std::result::Result<AgentListTurnsResponse, AgentError> {
        Agent::list_turns(self, input).await
    }

    async fn cancel_turn(
        &mut self,
        input: AgentCancelTurn,
    ) -> std::result::Result<AgentTurn, AgentError> {
        Agent::cancel_turn(self, input).await
    }

    async fn list_turn_events(
        &mut self,
        input: AgentListTurnEvents,
    ) -> std::result::Result<AgentListTurnEventsResponse, AgentError> {
        Agent::list_turn_events(self, input).await
    }

    async fn list_interactions(
        &mut self,
        input: AgentListInteractions,
    ) -> std::result::Result<AgentListInteractionsResponse, AgentError> {
        Agent::list_interactions(self, input).await
    }

    async fn resolve_interaction(
        &mut self,
        input: AgentResolveInteraction,
    ) -> std::result::Result<AgentInteraction, AgentError> {
        Agent::resolve_interaction(self, input).await
    }
}

#[derive(Clone)]
struct RelayTokenInterceptor {
    token: Option<MetadataValue<tonic::metadata::Ascii>>,
}

impl Interceptor for RelayTokenInterceptor {
    fn call(
        &mut self,
        mut request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcRequest<()>, tonic::Status> {
        if let Some(token) = self.token.clone() {
            request
                .metadata_mut()
                .insert(AGENT_RELAY_TOKEN_HEADER, token);
        }
        Ok(request)
    }
}

fn relay_token_interceptor(token: &str) -> std::result::Result<RelayTokenInterceptor, AgentError> {
    let trimmed = token.trim();
    let token = if trimmed.is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(trimmed).map_err(|err| {
            AgentError::Env(format!("agent: invalid relay token metadata: {err}"))
        })?)
    };
    Ok(RelayTokenInterceptor { token })
}

enum AgentTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_agent_target(raw: &str) -> std::result::Result<AgentTarget, AgentError> {
    let target = raw.trim();
    if target.is_empty() {
        return Err(AgentError::Env(
            "agent: transport target is required".to_string(),
        ));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(AgentError::Env(format!(
                "agent: tcp target {raw:?} is missing host:port"
            )));
        }
        return Ok(AgentTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(AgentError::Env(format!(
                "agent: tls target {raw:?} is missing host:port"
            )));
        }
        return Ok(AgentTarget::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(AgentError::Env(format!(
                "agent: unix target {raw:?} is missing a socket path"
            )));
        }
        return Ok(AgentTarget::Unix(path.to_string()));
    }
    if target.contains("://") {
        return Err(AgentError::Env(format!(
            "agent: unsupported target scheme in {raw:?}"
        )));
    }
    Ok(AgentTarget::Unix(target.to_string()))
}
