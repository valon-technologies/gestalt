use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::Request;
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
        AgentExecutionStatus, AgentInteraction, AgentMessage, AgentSession, AgentSessionState,
        AgentToolRef, AgentToolSourceMode, AgentTurn, AgentTurnEvent, AgentWorkspace,
        event_from_proto, interaction_from_proto, new_agent_messages, new_agent_tool_refs,
        new_agent_workspace, session_from_proto, turn_from_proto,
    },
    protocol,
};

type AgentManagerTransport = InterceptedService<Channel, RelayTokenInterceptor>;

/// Environment variable containing the agent-manager host-service target.
pub const ENV_AGENT_MANAGER_SOCKET: &str = "GESTALT_HOST_SERVICE_SOCKET";
/// Environment variable containing the optional agent-manager relay token.
pub const ENV_AGENT_MANAGER_SOCKET_TOKEN: &str = "GESTALT_HOST_SERVICE_TOKEN";
const AGENT_MANAGER_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

#[derive(Debug, thiserror::Error)]
/// Errors returned by [`AgentManager`].
pub enum AgentManagerError {
    /// The invocation token was empty.
    #[error("agent manager: invocation token is not available")]
    MissingInvocationToken,
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
pub struct AgentManagerCreateSession {
    pub provider_name: String,
    pub model: String,
    pub client_ref: String,
    pub metadata: Option<serde_json::Value>,
    pub idempotency_key: String,
    pub workspace: Option<AgentWorkspace>,
}

/// Input for fetching an agent session.
#[derive(Clone, Debug, Default)]
pub struct AgentManagerGetSession {
    pub session_id: String,
}

/// Input for listing agent sessions.
#[derive(Clone, Debug)]
pub struct AgentManagerListSessions {
    pub provider_name: String,
    pub state: AgentSessionState,
    pub limit: i32,
    pub summary_only: bool,
}

impl Default for AgentManagerListSessions {
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
pub struct AgentManagerUpdateSession {
    pub session_id: String,
    pub client_ref: String,
    pub state: AgentSessionState,
    pub metadata: Option<serde_json::Value>,
}

impl Default for AgentManagerUpdateSession {
    fn default() -> Self {
        Self {
            session_id: String::new(),
            client_ref: String::new(),
            state: AgentSessionState::Unspecified,
            metadata: None,
        }
    }
}

/// Input for creating an agent turn.
#[derive(Clone, Debug)]
pub struct AgentManagerCreateTurn {
    pub session_id: String,
    pub model: String,
    pub messages: Vec<AgentMessage>,
    pub tool_refs: Vec<AgentToolRef>,
    pub tool_refs_set: bool,
    pub tool_source: AgentToolSourceMode,
    pub response_schema: Option<serde_json::Value>,
    pub metadata: Option<serde_json::Value>,
    pub idempotency_key: String,
    pub model_options: Option<serde_json::Value>,
    pub timeout_seconds: i32,
}

impl Default for AgentManagerCreateTurn {
    fn default() -> Self {
        Self {
            session_id: String::new(),
            model: String::new(),
            messages: Vec::new(),
            tool_refs: Vec::new(),
            tool_refs_set: false,
            tool_source: AgentToolSourceMode::Unspecified,
            response_schema: None,
            metadata: None,
            idempotency_key: String::new(),
            model_options: None,
            timeout_seconds: 0,
        }
    }
}

/// Input for fetching an agent turn.
#[derive(Clone, Debug, Default)]
pub struct AgentManagerGetTurn {
    pub turn_id: String,
}

/// Input for listing agent turns.
#[derive(Clone, Debug)]
pub struct AgentManagerListTurns {
    pub session_id: String,
    pub status: AgentExecutionStatus,
    pub limit: i32,
    pub summary_only: bool,
}

impl Default for AgentManagerListTurns {
    fn default() -> Self {
        Self {
            session_id: String::new(),
            status: AgentExecutionStatus::Unspecified,
            limit: 0,
            summary_only: false,
        }
    }
}

/// Input for canceling an agent turn.
#[derive(Clone, Debug, Default)]
pub struct AgentManagerCancelTurn {
    pub turn_id: String,
    pub reason: String,
}

/// Input for listing agent turn events.
#[derive(Clone, Debug, Default)]
pub struct AgentManagerListTurnEvents {
    pub turn_id: String,
    pub after_seq: i64,
    pub limit: i32,
}

/// Input for listing agent interactions.
#[derive(Clone, Debug, Default)]
pub struct AgentManagerListInteractions {
    pub turn_id: String,
}

/// Input for resolving an agent interaction.
#[derive(Clone, Debug, Default)]
pub struct AgentManagerResolveInteraction {
    pub turn_id: String,
    pub interaction_id: String,
    pub resolution: Option<serde_json::Value>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct AgentManagerListSessionsResponse {
    pub sessions: Vec<AgentSession>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct AgentManagerListTurnsResponse {
    pub turns: Vec<AgentTurn>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct AgentManagerListTurnEventsResponse {
    pub events: Vec<AgentTurnEvent>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct AgentManagerListInteractionsResponse {
    pub interactions: Vec<AgentInteraction>,
}

/// Creates a protocol create-session request.
pub(crate) fn new_agent_manager_create_session_request(
    input: AgentManagerCreateSession,
) -> crate::Result<pb::CreateAgentProviderSessionRequest> {
    Ok(pb::CreateAgentProviderSessionRequest {
        provider_name: input.provider_name,
        model: input.model,
        client_ref: input.client_ref,
        metadata: input.metadata.map(protocol::struct_from_json).transpose()?,
        idempotency_key: input.idempotency_key,
        invocation_token: String::new(),
        workspace: input.workspace.map(new_agent_workspace),
        ..Default::default()
    })
}

pub(crate) fn new_agent_manager_get_session_request(
    input: AgentManagerGetSession,
) -> pb::GetAgentProviderSessionRequest {
    pb::GetAgentProviderSessionRequest {
        session_id: input.session_id,
        invocation_token: String::new(),
        ..Default::default()
    }
}

pub(crate) fn new_agent_manager_list_sessions_request(
    input: AgentManagerListSessions,
) -> pb::ListAgentProviderSessionsRequest {
    pb::ListAgentProviderSessionsRequest {
        provider_name: input.provider_name,
        invocation_token: String::new(),
        state: input.state.as_i32(),
        limit: input.limit,
        summary_only: input.summary_only,
        ..Default::default()
    }
}

pub(crate) fn new_agent_manager_update_session_request(
    input: AgentManagerUpdateSession,
) -> crate::Result<pb::UpdateAgentProviderSessionRequest> {
    Ok(pb::UpdateAgentProviderSessionRequest {
        session_id: input.session_id,
        client_ref: input.client_ref,
        state: input.state.as_i32(),
        metadata: input.metadata.map(protocol::struct_from_json).transpose()?,
        invocation_token: String::new(),
        ..Default::default()
    })
}

pub(crate) fn new_agent_manager_create_turn_request(
    input: AgentManagerCreateTurn,
) -> crate::Result<pb::CreateAgentProviderTurnRequest> {
    Ok(pb::CreateAgentProviderTurnRequest {
        session_id: input.session_id,
        model: input.model,
        messages: new_agent_messages(input.messages)?,
        tool_refs_set: input.tool_refs_set || !input.tool_refs.is_empty(),
        tool_refs: new_agent_tool_refs(input.tool_refs),
        tool_source: input.tool_source.as_i32(),
        response_schema: input
            .response_schema
            .map(protocol::struct_from_json)
            .transpose()?,
        metadata: input.metadata.map(protocol::struct_from_json).transpose()?,
        idempotency_key: input.idempotency_key,
        invocation_token: String::new(),
        model_options: input
            .model_options
            .map(protocol::struct_from_json)
            .transpose()?,
        timeout_seconds: input.timeout_seconds,
        ..Default::default()
    })
}

pub(crate) fn new_agent_manager_get_turn_request(
    input: AgentManagerGetTurn,
) -> pb::GetAgentProviderTurnRequest {
    pb::GetAgentProviderTurnRequest {
        turn_id: input.turn_id,
        invocation_token: String::new(),
        ..Default::default()
    }
}

pub(crate) fn new_agent_manager_list_turns_request(
    input: AgentManagerListTurns,
) -> pb::ListAgentProviderTurnsRequest {
    pb::ListAgentProviderTurnsRequest {
        session_id: input.session_id,
        invocation_token: String::new(),
        status: input.status.as_i32(),
        limit: input.limit,
        summary_only: input.summary_only,
        ..Default::default()
    }
}

pub(crate) fn new_agent_manager_cancel_turn_request(
    input: AgentManagerCancelTurn,
) -> pb::CancelAgentProviderTurnRequest {
    pb::CancelAgentProviderTurnRequest {
        turn_id: input.turn_id,
        reason: input.reason,
        invocation_token: String::new(),
        ..Default::default()
    }
}

pub(crate) fn new_agent_manager_list_turn_events_request(
    input: AgentManagerListTurnEvents,
) -> pb::ListAgentProviderTurnEventsRequest {
    pb::ListAgentProviderTurnEventsRequest {
        turn_id: input.turn_id,
        after_seq: input.after_seq,
        limit: input.limit,
        invocation_token: String::new(),
        ..Default::default()
    }
}

pub(crate) fn new_agent_manager_list_interactions_request(
    input: AgentManagerListInteractions,
) -> pb::ListAgentProviderInteractionsRequest {
    pb::ListAgentProviderInteractionsRequest {
        turn_id: input.turn_id,
        invocation_token: String::new(),
        ..Default::default()
    }
}

pub(crate) fn new_agent_manager_resolve_interaction_request(
    input: AgentManagerResolveInteraction,
) -> crate::Result<pb::ResolveAgentProviderInteractionRequest> {
    Ok(pb::ResolveAgentProviderInteractionRequest {
        turn_id: input.turn_id,
        interaction_id: input.interaction_id,
        resolution: input
            .resolution
            .map(protocol::struct_from_json)
            .transpose()?,
        invocation_token: String::new(),
        ..Default::default()
    })
}

/// Client for managing agent sessions, turns, events, and interactions.
pub struct AgentManager {
    client: ProtoAgentProviderClient<AgentManagerTransport>,
    invocation_token: String,
}

impl AgentManager {
    /// Connects to the agent manager with an invocation token from the host.
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
            invocation_token,
        })
    }

    /// Creates an agent session.
    pub async fn create_session(
        &mut self,
        input: AgentManagerCreateSession,
    ) -> std::result::Result<AgentSession, AgentManagerError> {
        let mut request = new_agent_manager_create_session_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(session_from_proto(
            self.client.create_session(request).await?.into_inner(),
        )?)
    }

    /// Fetches one agent session.
    pub async fn get_session(
        &mut self,
        input: AgentManagerGetSession,
    ) -> std::result::Result<AgentSession, AgentManagerError> {
        let mut request = new_agent_manager_get_session_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(session_from_proto(
            self.client.get_session(request).await?.into_inner(),
        )?)
    }

    /// Lists agent sessions visible to the invocation token.
    pub async fn list_sessions(
        &mut self,
        input: AgentManagerListSessions,
    ) -> std::result::Result<AgentManagerListSessionsResponse, AgentManagerError> {
        let mut request = new_agent_manager_list_sessions_request(input);
        request.invocation_token = self.invocation_token.clone();
        let response = self.client.list_sessions(request).await?.into_inner();
        Ok(AgentManagerListSessionsResponse {
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
        input: AgentManagerUpdateSession,
    ) -> std::result::Result<AgentSession, AgentManagerError> {
        let mut request = new_agent_manager_update_session_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(session_from_proto(
            self.client.update_session(request).await?.into_inner(),
        )?)
    }

    /// Creates an agent turn.
    pub async fn create_turn(
        &mut self,
        input: AgentManagerCreateTurn,
    ) -> std::result::Result<AgentTurn, AgentManagerError> {
        let mut request = new_agent_manager_create_turn_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(turn_from_proto(
            self.client.create_turn(request).await?.into_inner(),
        )?)
    }

    /// Fetches one agent turn.
    pub async fn get_turn(
        &mut self,
        input: AgentManagerGetTurn,
    ) -> std::result::Result<AgentTurn, AgentManagerError> {
        let mut request = new_agent_manager_get_turn_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(turn_from_proto(
            self.client.get_turn(request).await?.into_inner(),
        )?)
    }

    /// Lists turns for an agent session.
    pub async fn list_turns(
        &mut self,
        input: AgentManagerListTurns,
    ) -> std::result::Result<AgentManagerListTurnsResponse, AgentManagerError> {
        let mut request = new_agent_manager_list_turns_request(input);
        request.invocation_token = self.invocation_token.clone();
        let response = self.client.list_turns(request).await?.into_inner();
        Ok(AgentManagerListTurnsResponse {
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
        input: AgentManagerCancelTurn,
    ) -> std::result::Result<AgentTurn, AgentManagerError> {
        let mut request = new_agent_manager_cancel_turn_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(turn_from_proto(
            self.client.cancel_turn(request).await?.into_inner(),
        )?)
    }

    /// Lists events emitted for an agent turn.
    pub async fn list_turn_events(
        &mut self,
        input: AgentManagerListTurnEvents,
    ) -> std::result::Result<AgentManagerListTurnEventsResponse, AgentManagerError> {
        let mut request = new_agent_manager_list_turn_events_request(input);
        request.invocation_token = self.invocation_token.clone();
        let response = self.client.list_turn_events(request).await?.into_inner();
        Ok(AgentManagerListTurnEventsResponse {
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
        input: AgentManagerListInteractions,
    ) -> std::result::Result<AgentManagerListInteractionsResponse, AgentManagerError> {
        let mut request = new_agent_manager_list_interactions_request(input);
        request.invocation_token = self.invocation_token.clone();
        let response = self.client.list_interactions(request).await?.into_inner();
        Ok(AgentManagerListInteractionsResponse {
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
        input: AgentManagerResolveInteraction,
    ) -> std::result::Result<AgentInteraction, AgentManagerError> {
        let mut request = new_agent_manager_resolve_interaction_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(interaction_from_proto(
            self.client.resolve_interaction(request).await?.into_inner(),
        )?)
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
