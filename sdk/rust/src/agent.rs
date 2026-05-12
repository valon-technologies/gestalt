use std::sync::Arc;
use std::time::SystemTime;

use hyper_util::rt::TokioIo;
use prost_types::{Struct, Timestamp, Value};
use serde::Serialize;
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

impl AgentHostExecuteToolInput {
    /// Returns this input with JSON object arguments serialized from a typed value.
    pub fn with_arguments<T: Serialize>(mut self, arguments: T) -> ProviderResult<Self> {
        let arguments = protocol::json_from_serializable(arguments)?;
        if !arguments.is_object() {
            return Err(crate::Error::bad_request(
                "agent host tool arguments must serialize to a JSON object",
            ));
        }
        self.arguments = Some(arguments);
        Ok(self)
    }
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

/// Native JSON object used by authored agent providers.
pub type AgentJson = serde_json::Value;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Hash)]
#[repr(i32)]
pub enum AgentMessagePartType {
    #[default]
    Unspecified = 0,
    Text = 1,
    Json = 2,
    ToolCall = 3,
    ToolResult = 4,
    ImageRef = 5,
}

impl AgentMessagePartType {
    pub const fn as_i32(self) -> i32 {
        self as i32
    }

    pub const fn from_i32_lossy(value: i32) -> Self {
        match value {
            1 => Self::Text,
            2 => Self::Json,
            3 => Self::ToolCall,
            4 => Self::ToolResult,
            5 => Self::ImageRef,
            _ => Self::Unspecified,
        }
    }
}

impl TryFrom<i32> for AgentMessagePartType {
    type Error = crate::Error;

    fn try_from(value: i32) -> ProviderResult<Self> {
        match value {
            0 => Ok(Self::Unspecified),
            1 => Ok(Self::Text),
            2 => Ok(Self::Json),
            3 => Ok(Self::ToolCall),
            4 => Ok(Self::ToolResult),
            5 => Ok(Self::ImageRef),
            _ => Err(crate::Error::bad_request(format!(
                "unknown agent message part type {value}"
            ))),
        }
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Hash)]
#[repr(i32)]
pub enum AgentToolSourceMode {
    #[default]
    Unspecified = 0,
    McpCatalog = 2,
}

impl AgentToolSourceMode {
    pub const fn as_i32(self) -> i32 {
        self as i32
    }

    pub const fn from_i32_lossy(value: i32) -> Self {
        match value {
            2 => Self::McpCatalog,
            _ => Self::Unspecified,
        }
    }
}

impl TryFrom<i32> for AgentToolSourceMode {
    type Error = crate::Error;

    fn try_from(value: i32) -> ProviderResult<Self> {
        match value {
            0 => Ok(Self::Unspecified),
            2 => Ok(Self::McpCatalog),
            _ => Err(crate::Error::bad_request(format!(
                "unknown agent tool source mode {value}"
            ))),
        }
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Hash)]
#[repr(i32)]
pub enum AgentExecutionStatus {
    #[default]
    Unspecified = 0,
    Pending = 1,
    Running = 2,
    Succeeded = 3,
    Failed = 4,
    Canceled = 5,
    WaitingForInput = 6,
}

impl AgentExecutionStatus {
    pub const fn as_i32(self) -> i32 {
        self as i32
    }

    pub const fn from_i32_lossy(value: i32) -> Self {
        match value {
            1 => Self::Pending,
            2 => Self::Running,
            3 => Self::Succeeded,
            4 => Self::Failed,
            5 => Self::Canceled,
            6 => Self::WaitingForInput,
            _ => Self::Unspecified,
        }
    }
}

impl TryFrom<i32> for AgentExecutionStatus {
    type Error = crate::Error;

    fn try_from(value: i32) -> ProviderResult<Self> {
        match value {
            0 => Ok(Self::Unspecified),
            1 => Ok(Self::Pending),
            2 => Ok(Self::Running),
            3 => Ok(Self::Succeeded),
            4 => Ok(Self::Failed),
            5 => Ok(Self::Canceled),
            6 => Ok(Self::WaitingForInput),
            _ => Err(crate::Error::bad_request(format!(
                "unknown agent execution status {value}"
            ))),
        }
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Hash)]
#[repr(i32)]
pub enum AgentSessionState {
    #[default]
    Unspecified = 0,
    Active = 1,
    Archived = 2,
}

impl AgentSessionState {
    pub const fn as_i32(self) -> i32 {
        self as i32
    }

    pub const fn from_i32_lossy(value: i32) -> Self {
        match value {
            1 => Self::Active,
            2 => Self::Archived,
            _ => Self::Unspecified,
        }
    }
}

impl TryFrom<i32> for AgentSessionState {
    type Error = crate::Error;

    fn try_from(value: i32) -> ProviderResult<Self> {
        match value {
            0 => Ok(Self::Unspecified),
            1 => Ok(Self::Active),
            2 => Ok(Self::Archived),
            _ => Err(crate::Error::bad_request(format!(
                "unknown agent session state {value}"
            ))),
        }
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Hash)]
#[repr(i32)]
pub enum AgentInteractionType {
    #[default]
    Unspecified = 0,
    Approval = 1,
    Clarification = 2,
    Input = 3,
}

impl AgentInteractionType {
    pub const fn as_i32(self) -> i32 {
        self as i32
    }

    pub const fn from_i32_lossy(value: i32) -> Self {
        match value {
            1 => Self::Approval,
            2 => Self::Clarification,
            3 => Self::Input,
            _ => Self::Unspecified,
        }
    }
}

impl TryFrom<i32> for AgentInteractionType {
    type Error = crate::Error;

    fn try_from(value: i32) -> ProviderResult<Self> {
        match value {
            0 => Ok(Self::Unspecified),
            1 => Ok(Self::Approval),
            2 => Ok(Self::Clarification),
            3 => Ok(Self::Input),
            _ => Err(crate::Error::bad_request(format!(
                "unknown agent interaction type {value}"
            ))),
        }
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Hash)]
#[repr(i32)]
pub enum AgentInteractionState {
    #[default]
    Unspecified = 0,
    Pending = 1,
    Resolved = 2,
    Canceled = 3,
}

impl AgentInteractionState {
    pub const fn as_i32(self) -> i32 {
        self as i32
    }

    pub const fn from_i32_lossy(value: i32) -> Self {
        match value {
            1 => Self::Pending,
            2 => Self::Resolved,
            3 => Self::Canceled,
            _ => Self::Unspecified,
        }
    }
}

impl TryFrom<i32> for AgentInteractionState {
    type Error = crate::Error;

    fn try_from(value: i32) -> ProviderResult<Self> {
        match value {
            0 => Ok(Self::Unspecified),
            1 => Ok(Self::Pending),
            2 => Ok(Self::Resolved),
            3 => Ok(Self::Canceled),
            _ => Err(crate::Error::bad_request(format!(
                "unknown agent interaction state {value}"
            ))),
        }
    }
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct AgentMessage {
    pub role: String,
    pub text: String,
    pub parts: Vec<AgentMessagePart>,
    pub metadata: Option<AgentJson>,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct AgentMessagePartToolCall {
    pub id: String,
    pub tool_id: String,
    pub arguments: Option<AgentJson>,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct AgentMessagePartToolResult {
    pub tool_call_id: String,
    pub status: i32,
    pub content: String,
    pub output: Option<AgentJson>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AgentMessagePartImageRef {
    pub uri: String,
    pub mime_type: String,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct AgentMessagePart {
    pub r#type: AgentMessagePartType,
    pub text: String,
    pub json: Option<AgentJson>,
    pub tool_call: Option<AgentMessagePartToolCall>,
    pub tool_result: Option<AgentMessagePartToolResult>,
    pub image_ref: Option<AgentMessagePartImageRef>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AgentActor {
    pub subject_id: String,
    pub subject_kind: String,
    pub display_name: String,
    pub auth_source: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AgentSubjectContext {
    pub subject_id: String,
    pub subject_kind: String,
    pub credential_subject_id: String,
    pub display_name: String,
    pub auth_source: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AgentPreparedWorkspace {
    pub root: String,
    pub cwd: String,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct ResolvedAgentTool {
    pub id: String,
    pub name: String,
    pub description: String,
    pub parameters_schema: Option<AgentJson>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AgentToolRef {
    pub plugin: String,
    pub operation: String,
    pub connection: String,
    pub instance: String,
    pub title: String,
    pub description: String,
    pub system: String,
}

impl AgentMessage {
    /// Sets message metadata from any JSON-object-like serializable value.
    pub fn with_metadata<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.metadata = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }

    /// Creates a user text message.
    pub fn user_text(text: impl Into<String>) -> Self {
        Self {
            role: "user".to_string(),
            text: text.into(),
            ..Default::default()
        }
    }
}

impl AgentMessagePart {
    /// Creates a JSON message part from any JSON-object-like serializable value.
    pub fn json<T: Serialize>(value: T) -> ProviderResult<Self> {
        Ok(Self {
            r#type: AgentMessagePartType::Json,
            json: Some(protocol::json_from_serializable(value)?),
            ..Default::default()
        })
    }
}

impl AgentMessagePartToolCall {
    /// Sets tool-call arguments from any JSON-object-like serializable value.
    pub fn with_arguments<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.arguments = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }
}

impl AgentMessagePartToolResult {
    /// Sets tool-result output from any JSON-object-like serializable value.
    pub fn with_output<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.output = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }
}

/// Native agent workspace request data.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct AgentWorkspace {
    pub checkouts: Vec<AgentWorkspaceGitCheckout>,
    pub cwd: String,
}

/// Native agent workspace git checkout data.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct AgentWorkspaceGitCheckout {
    pub url: String,
    pub reference: String,
    pub path: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AgentProviderCapabilities {
    pub streaming_text: bool,
    pub tool_calls: bool,
    pub parallel_tool_calls: bool,
    pub structured_output: bool,
    pub interactions: bool,
    pub resumable_turns: bool,
    pub reasoning_summaries: bool,
    pub bounded_list_hydration: bool,
    pub supported_tool_sources: Vec<AgentToolSourceMode>,
    pub supports_session_start: bool,
    pub supports_prepared_workspace: bool,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct GetAgentProviderCapabilitiesRequest {}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct AgentInteraction {
    pub id: String,
    pub r#type: AgentInteractionType,
    pub state: AgentInteractionState,
    pub title: String,
    pub prompt: String,
    pub request: Option<AgentJson>,
    pub resolution: Option<AgentJson>,
    pub created_at: Option<SystemTime>,
    pub resolved_at: Option<SystemTime>,
    pub turn_id: String,
    pub session_id: String,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct AgentSession {
    pub id: String,
    pub provider_name: String,
    pub model: String,
    pub client_ref: String,
    pub state: AgentSessionState,
    pub metadata: Option<AgentJson>,
    pub created_by: Option<AgentActor>,
    pub created_at: Option<SystemTime>,
    pub updated_at: Option<SystemTime>,
    pub last_turn_at: Option<SystemTime>,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct CreateAgentProviderSessionRequest {
    pub session_id: String,
    pub idempotency_key: String,
    pub model: String,
    pub client_ref: String,
    pub metadata: Option<AgentJson>,
    pub created_by: Option<AgentActor>,
    pub subject: Option<AgentSubjectContext>,
    pub session_start: Option<AgentSessionStartConfig>,
    pub prepared_workspace: Option<AgentPreparedWorkspace>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AgentSessionStartConfig {
    pub hooks: Vec<AgentSessionStartHook>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AgentSessionStartHook {
    pub id: String,
    pub r#type: String,
    pub command: Vec<String>,
    pub cwd: String,
    pub timeout: String,
    pub env: std::collections::HashMap<String, String>,
    pub output: Option<AgentSessionStartHookOutput>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AgentSessionStartHookOutput {
    pub additional_context: bool,
    pub metadata: bool,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct GetAgentProviderSessionRequest {
    pub session_id: String,
    pub subject: Option<AgentSubjectContext>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ListAgentProviderSessionsRequest {
    pub subject: Option<AgentSubjectContext>,
    pub session_ids: Vec<String>,
    pub state: AgentSessionState,
    pub limit: i32,
    pub summary_only: bool,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct ListAgentProviderSessionsResponse {
    pub sessions: Vec<AgentSession>,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct UpdateAgentProviderSessionRequest {
    pub session_id: String,
    pub client_ref: String,
    pub state: AgentSessionState,
    pub metadata: Option<AgentJson>,
    pub subject: Option<AgentSubjectContext>,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct AgentTurn {
    pub id: String,
    pub session_id: String,
    pub provider_name: String,
    pub model: String,
    pub status: AgentExecutionStatus,
    pub messages: Vec<AgentMessage>,
    pub output_text: String,
    pub structured_output: Option<AgentJson>,
    pub status_message: String,
    pub created_by: Option<AgentActor>,
    pub created_at: Option<SystemTime>,
    pub started_at: Option<SystemTime>,
    pub completed_at: Option<SystemTime>,
    pub execution_ref: String,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct AgentTurnDisplay {
    pub kind: String,
    pub phase: String,
    pub text: String,
    pub label: String,
    pub r#ref: String,
    pub parent_ref: String,
    pub input: Option<AgentJson>,
    pub output: Option<AgentJson>,
    pub error: Option<AgentJson>,
    pub action: String,
    pub format: String,
    pub language: String,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct CreateAgentProviderTurnRequest {
    pub turn_id: String,
    pub session_id: String,
    pub idempotency_key: String,
    pub model: String,
    pub messages: Vec<AgentMessage>,
    pub tools: Vec<ResolvedAgentTool>,
    pub response_schema: Option<AgentJson>,
    pub metadata: Option<AgentJson>,
    pub created_by: Option<AgentActor>,
    pub execution_ref: String,
    pub tool_refs: Vec<AgentToolRef>,
    pub tool_source: AgentToolSourceMode,
    pub subject: Option<AgentSubjectContext>,
    pub model_options: Option<AgentJson>,
    pub run_grant: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct GetAgentProviderTurnRequest {
    pub turn_id: String,
    pub subject: Option<AgentSubjectContext>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ListAgentProviderTurnsRequest {
    pub session_id: String,
    pub subject: Option<AgentSubjectContext>,
    pub turn_ids: Vec<String>,
    pub status: AgentExecutionStatus,
    pub limit: i32,
    pub summary_only: bool,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct ListAgentProviderTurnsResponse {
    pub turns: Vec<AgentTurn>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct CancelAgentProviderTurnRequest {
    pub turn_id: String,
    pub reason: String,
    pub subject: Option<AgentSubjectContext>,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct AgentTurnEvent {
    pub id: String,
    pub turn_id: String,
    pub seq: i64,
    pub r#type: String,
    pub source: String,
    pub visibility: String,
    pub data: Option<AgentJson>,
    pub created_at: Option<SystemTime>,
    pub display: Option<AgentTurnDisplay>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ListAgentProviderTurnEventsRequest {
    pub turn_id: String,
    pub after_seq: i64,
    pub limit: i32,
    pub subject: Option<AgentSubjectContext>,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct ListAgentProviderTurnEventsResponse {
    pub events: Vec<AgentTurnEvent>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct GetAgentProviderInteractionRequest {
    pub interaction_id: String,
    pub subject: Option<AgentSubjectContext>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ListAgentProviderInteractionsRequest {
    pub turn_id: String,
    pub subject: Option<AgentSubjectContext>,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct ListAgentProviderInteractionsResponse {
    pub interactions: Vec<AgentInteraction>,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct ResolveAgentProviderInteractionRequest {
    pub interaction_id: String,
    pub resolution: Option<AgentJson>,
    pub subject: Option<AgentSubjectContext>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ExecuteAgentToolResponse {
    pub status: i32,
    pub body: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AgentToolAnnotations {
    pub read_only_hint: Option<bool>,
    pub idempotent_hint: Option<bool>,
    pub destructive_hint: Option<bool>,
    pub open_world_hint: Option<bool>,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct ListedAgentTool {
    pub id: String,
    pub mcp_name: String,
    pub title: String,
    pub description: String,
    pub input_schema: String,
    pub output_schema: String,
    pub annotations: Option<AgentToolAnnotations>,
    pub r#ref: Option<AgentToolRef>,
    pub tags: Vec<String>,
    pub search_text: String,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct ListAgentToolsResponse {
    pub tools: Vec<ListedAgentTool>,
    pub next_page_token: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ResolvedAgentConnection {
    pub connection_id: String,
    pub connection: String,
    pub instance: String,
    pub mode: String,
    pub headers: std::collections::HashMap<String, String>,
    pub params: std::collections::HashMap<String, String>,
    pub expires_at: Option<SystemTime>,
}

/// Creates a native agent message.
pub fn new_agent_message(input: AgentMessage) -> ProviderResult<AgentMessage> {
    Ok(AgentMessage {
        role: input.role,
        text: input.text,
        parts: input
            .parts
            .into_iter()
            .map(new_agent_message_part)
            .collect::<ProviderResult<Vec<_>>>()?,
        metadata: input.metadata,
    })
}

/// Creates a native agent message part.
pub fn new_agent_message_part(input: AgentMessagePart) -> ProviderResult<AgentMessagePart> {
    let mut part_type = input.r#type;
    if part_type == AgentMessagePartType::Unspecified {
        part_type = infer_agent_message_part_type(&input);
    }
    Ok(AgentMessagePart {
        r#type: part_type,
        text: input.text,
        json: input.json,
        tool_call: input.tool_call.map(new_agent_tool_call).transpose()?,
        tool_result: input.tool_result.map(new_agent_tool_result).transpose()?,
        image_ref: input.image_ref.map(new_agent_image_ref),
    })
}

/// Creates a native agent tool-call payload.
pub fn new_agent_tool_call(
    input: AgentMessagePartToolCall,
) -> ProviderResult<AgentMessagePartToolCall> {
    Ok(AgentMessagePartToolCall {
        id: input.id,
        tool_id: input.tool_id,
        arguments: input.arguments,
    })
}

/// Creates a native agent tool-result payload.
pub fn new_agent_tool_result(
    input: AgentMessagePartToolResult,
) -> ProviderResult<AgentMessagePartToolResult> {
    Ok(AgentMessagePartToolResult {
        tool_call_id: input.tool_call_id,
        status: input.status,
        content: input.content,
        output: input.output,
    })
}

/// Creates a native agent image-reference payload.
pub fn new_agent_image_ref(input: AgentMessagePartImageRef) -> AgentMessagePartImageRef {
    AgentMessagePartImageRef {
        uri: input.uri,
        mime_type: input.mime_type,
    }
}

/// Creates a native agent tool reference.
pub fn new_agent_tool_ref(input: AgentToolRef) -> AgentToolRef {
    AgentToolRef {
        plugin: input.plugin,
        operation: input.operation,
        connection: input.connection,
        instance: input.instance,
        title: input.title,
        description: input.description,
        system: input.system,
    }
}

/// Creates an agent workspace request payload for the manager host service.
pub(crate) fn new_agent_workspace(input: AgentWorkspace) -> pb::AgentWorkspace {
    pb::AgentWorkspace {
        checkouts: input
            .checkouts
            .into_iter()
            .map(|checkout| pb::AgentWorkspaceGitCheckout {
                url: checkout.url,
                r#ref: checkout.reference,
                path: checkout.path,
            })
            .collect(),
        cwd: input.cwd,
    }
}

pub(crate) fn new_agent_messages(
    values: Vec<AgentMessage>,
) -> ProviderResult<Vec<pb::AgentMessage>> {
    values
        .into_iter()
        .map(new_agent_message)
        .map(|value| value.and_then(message_to_proto))
        .collect()
}

pub(crate) fn new_agent_tool_refs(values: Vec<AgentToolRef>) -> Vec<pb::AgentToolRef> {
    values
        .into_iter()
        .map(new_agent_tool_ref)
        .map(agent_tool_ref_to_proto)
        .collect()
}

fn infer_agent_message_part_type(input: &AgentMessagePart) -> AgentMessagePartType {
    if input.tool_call.is_some() {
        AgentMessagePartType::ToolCall
    } else if input.tool_result.is_some() {
        AgentMessagePartType::ToolResult
    } else if input.image_ref.is_some() {
        AgentMessagePartType::ImageRef
    } else if input.json.is_some() {
        AgentMessagePartType::Json
    } else if !input.text.is_empty() {
        AgentMessagePartType::Text
    } else {
        AgentMessagePartType::Unspecified
    }
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

    /// Executes a host tool using native request fields.
    pub async fn execute_tool(
        &mut self,
        input: AgentHostExecuteToolInput,
    ) -> std::result::Result<ExecuteAgentToolResponse, AgentHostError> {
        let request = pb::ExecuteAgentToolRequest {
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
        };
        Ok(execute_tool_response_from_proto(
            self.client.execute_tool(request).await?.into_inner(),
        ))
    }

    /// Executes a host tool using plain Rust request fields.
    pub async fn execute_tool_for_turn(
        &mut self,
        input: AgentHostExecuteToolInput,
    ) -> std::result::Result<ExecuteAgentToolResponse, AgentHostError> {
        self.execute_tool(input).await
    }

    /// Lists host tools visible to the current agent request.
    pub async fn list_tools(
        &mut self,
        input: AgentHostListToolsInput,
    ) -> std::result::Result<ListAgentToolsResponse, AgentHostError> {
        let request = pb::ListAgentToolsRequest {
            session_id: input.session_id,
            turn_id: input.turn_id,
            run_grant: input.run_grant,
            page_size: input.page_size,
            page_token: input.page_token,
            query: input.query,
        };
        Ok(list_tools_response_from_proto(
            self.client.list_tools(request).await?.into_inner(),
        ))
    }

    /// Lists host tools using plain Rust request fields.
    pub async fn list_tools_for_turn(
        &mut self,
        input: AgentHostListToolsInput,
    ) -> std::result::Result<ListAgentToolsResponse, AgentHostError> {
        self.list_tools(input).await
    }

    /// Resolves a configured agent connection for the current turn.
    pub async fn resolve_connection(
        &mut self,
        input: AgentHostResolveConnectionInput,
    ) -> std::result::Result<ResolvedAgentConnection, AgentHostError> {
        let request = pb::ResolveAgentConnectionRequest {
            session_id: input.session_id,
            turn_id: input.turn_id,
            connection: input.connection,
            instance: input.instance,
            run_grant: input.run_grant,
        };
        resolved_connection_from_proto(self.client.resolve_connection(request).await?.into_inner())
            .map_err(AgentHostError::Input)
    }

    /// Resolves an agent connection using plain Rust request fields.
    pub async fn resolve_connection_for_turn(
        &mut self,
        input: AgentHostResolveConnectionInput,
    ) -> std::result::Result<ResolvedAgentConnection, AgentHostError> {
        self.resolve_connection(input).await
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

fn json_from_struct(value: Option<Struct>) -> Option<AgentJson> {
    value.map(|value| protocol::json_from_struct(&value))
}

fn struct_from_json(value: Option<AgentJson>) -> ProviderResult<Option<Struct>> {
    value.map(protocol::struct_from_json).transpose()
}

fn value_from_json(value: Option<AgentJson>) -> Option<Value> {
    value.map(protocol::value_from_json)
}

fn time_from_timestamp(value: Option<Timestamp>) -> ProviderResult<Option<SystemTime>> {
    value
        .as_ref()
        .map(protocol::system_time_from_timestamp)
        .transpose()
}

fn timestamp_from_time(value: Option<SystemTime>) -> Option<Timestamp> {
    value.map(protocol::timestamp_from_system_time)
}

fn agent_actor_from_proto(value: Option<pb::AgentActor>) -> Option<AgentActor> {
    value.map(|value| AgentActor {
        subject_id: value.subject_id,
        subject_kind: value.subject_kind,
        display_name: value.display_name,
        auth_source: value.auth_source,
    })
}

fn agent_actor_to_proto(value: Option<AgentActor>) -> Option<pb::AgentActor> {
    value.map(|value| pb::AgentActor {
        subject_id: value.subject_id,
        subject_kind: value.subject_kind,
        display_name: value.display_name,
        auth_source: value.auth_source,
    })
}

fn agent_subject_from_proto(value: Option<pb::AgentSubjectContext>) -> Option<AgentSubjectContext> {
    value.map(|value| AgentSubjectContext {
        subject_id: value.subject_id,
        subject_kind: value.subject_kind,
        credential_subject_id: value.credential_subject_id,
        display_name: value.display_name,
        auth_source: value.auth_source,
    })
}

pub(crate) fn agent_tool_ref_from_proto(value: pb::AgentToolRef) -> AgentToolRef {
    AgentToolRef {
        plugin: value.plugin,
        operation: value.operation,
        connection: value.connection,
        instance: value.instance,
        title: value.title,
        description: value.description,
        system: value.system,
    }
}

pub(crate) fn agent_tool_ref_to_proto(value: AgentToolRef) -> pb::AgentToolRef {
    pb::AgentToolRef {
        plugin: value.plugin,
        operation: value.operation,
        connection: value.connection,
        instance: value.instance,
        title: value.title,
        description: value.description,
        system: value.system,
    }
}

pub(crate) fn message_from_proto(value: pb::AgentMessage) -> AgentMessage {
    AgentMessage {
        role: value.role,
        text: value.text,
        parts: value
            .parts
            .into_iter()
            .map(message_part_from_proto)
            .collect(),
        metadata: json_from_struct(value.metadata),
    }
}

pub(crate) fn message_to_proto(value: AgentMessage) -> ProviderResult<pb::AgentMessage> {
    Ok(pb::AgentMessage {
        role: value.role,
        text: value.text,
        parts: value
            .parts
            .into_iter()
            .map(message_part_to_proto)
            .collect::<ProviderResult<Vec<_>>>()?,
        metadata: struct_from_json(value.metadata)?,
    })
}

fn message_part_from_proto(value: pb::AgentMessagePart) -> AgentMessagePart {
    AgentMessagePart {
        r#type: AgentMessagePartType::from_i32_lossy(value.r#type),
        text: value.text,
        json: json_from_struct(value.json),
        tool_call: value.tool_call.map(|value| AgentMessagePartToolCall {
            id: value.id,
            tool_id: value.tool_id,
            arguments: json_from_struct(value.arguments),
        }),
        tool_result: value.tool_result.map(|value| AgentMessagePartToolResult {
            tool_call_id: value.tool_call_id,
            status: value.status,
            content: value.content,
            output: json_from_struct(value.output),
        }),
        image_ref: value.image_ref.map(|value| AgentMessagePartImageRef {
            uri: value.uri,
            mime_type: value.mime_type,
        }),
    }
}

fn message_part_to_proto(value: AgentMessagePart) -> ProviderResult<pb::AgentMessagePart> {
    Ok(pb::AgentMessagePart {
        r#type: value.r#type.as_i32(),
        text: value.text,
        json: struct_from_json(value.json)?,
        tool_call: value
            .tool_call
            .map(|value| -> ProviderResult<pb::AgentMessagePartToolCall> {
                Ok(pb::AgentMessagePartToolCall {
                    id: value.id,
                    tool_id: value.tool_id,
                    arguments: struct_from_json(value.arguments)?,
                })
            })
            .transpose()?,
        tool_result: value
            .tool_result
            .map(|value| -> ProviderResult<pb::AgentMessagePartToolResult> {
                Ok(pb::AgentMessagePartToolResult {
                    tool_call_id: value.tool_call_id,
                    status: value.status,
                    content: value.content,
                    output: struct_from_json(value.output)?,
                })
            })
            .transpose()?,
        image_ref: value.image_ref.map(|value| pb::AgentMessagePartImageRef {
            uri: value.uri,
            mime_type: value.mime_type,
        }),
    })
}

fn session_to_proto(value: AgentSession) -> ProviderResult<pb::AgentSession> {
    Ok(pb::AgentSession {
        id: value.id,
        provider_name: value.provider_name,
        model: value.model,
        client_ref: value.client_ref,
        state: value.state.as_i32(),
        metadata: struct_from_json(value.metadata)?,
        created_by: agent_actor_to_proto(value.created_by),
        created_at: timestamp_from_time(value.created_at),
        updated_at: timestamp_from_time(value.updated_at),
        last_turn_at: timestamp_from_time(value.last_turn_at),
    })
}

pub(crate) fn session_from_proto(value: pb::AgentSession) -> ProviderResult<AgentSession> {
    Ok(AgentSession {
        id: value.id,
        provider_name: value.provider_name,
        model: value.model,
        client_ref: value.client_ref,
        state: AgentSessionState::try_from(value.state)?,
        metadata: json_from_struct(value.metadata),
        created_by: agent_actor_from_proto(value.created_by),
        created_at: time_from_timestamp(value.created_at)?,
        updated_at: time_from_timestamp(value.updated_at)?,
        last_turn_at: time_from_timestamp(value.last_turn_at)?,
    })
}

fn turn_to_proto(value: AgentTurn) -> ProviderResult<pb::AgentTurn> {
    Ok(pb::AgentTurn {
        id: value.id,
        session_id: value.session_id,
        provider_name: value.provider_name,
        model: value.model,
        status: value.status.as_i32(),
        messages: value
            .messages
            .into_iter()
            .map(message_to_proto)
            .collect::<ProviderResult<Vec<_>>>()?,
        output_text: value.output_text,
        structured_output: struct_from_json(value.structured_output)?,
        status_message: value.status_message,
        created_by: agent_actor_to_proto(value.created_by),
        created_at: timestamp_from_time(value.created_at),
        started_at: timestamp_from_time(value.started_at),
        completed_at: timestamp_from_time(value.completed_at),
        execution_ref: value.execution_ref,
    })
}

pub(crate) fn turn_from_proto(value: pb::AgentTurn) -> ProviderResult<AgentTurn> {
    Ok(AgentTurn {
        id: value.id,
        session_id: value.session_id,
        provider_name: value.provider_name,
        model: value.model,
        status: AgentExecutionStatus::try_from(value.status)?,
        messages: value.messages.into_iter().map(message_from_proto).collect(),
        output_text: value.output_text,
        structured_output: json_from_struct(value.structured_output),
        status_message: value.status_message,
        created_by: agent_actor_from_proto(value.created_by),
        created_at: time_from_timestamp(value.created_at)?,
        started_at: time_from_timestamp(value.started_at)?,
        completed_at: time_from_timestamp(value.completed_at)?,
        execution_ref: value.execution_ref,
    })
}

fn display_to_proto(value: AgentTurnDisplay) -> pb::AgentTurnDisplay {
    pb::AgentTurnDisplay {
        kind: value.kind,
        phase: value.phase,
        text: value.text,
        label: value.label,
        r#ref: value.r#ref,
        parent_ref: value.parent_ref,
        input: value_from_json(value.input),
        output: value_from_json(value.output),
        error: value_from_json(value.error),
        action: value.action,
        format: value.format,
        language: value.language,
    }
}

fn event_to_proto(value: AgentTurnEvent) -> ProviderResult<pb::AgentTurnEvent> {
    Ok(pb::AgentTurnEvent {
        id: value.id,
        turn_id: value.turn_id,
        seq: value.seq,
        r#type: value.r#type,
        source: value.source,
        visibility: value.visibility,
        data: struct_from_json(value.data)?,
        created_at: timestamp_from_time(value.created_at),
        display: value.display.map(display_to_proto),
    })
}

pub(crate) fn event_from_proto(value: pb::AgentTurnEvent) -> ProviderResult<AgentTurnEvent> {
    Ok(AgentTurnEvent {
        id: value.id,
        turn_id: value.turn_id,
        seq: value.seq,
        r#type: value.r#type,
        source: value.source,
        visibility: value.visibility,
        data: json_from_struct(value.data),
        created_at: time_from_timestamp(value.created_at)?,
        display: value.display.map(|display| AgentTurnDisplay {
            kind: display.kind,
            phase: display.phase,
            text: display.text,
            label: display.label,
            r#ref: display.r#ref,
            parent_ref: display.parent_ref,
            input: display.input.map(|value| protocol::json_from_value(&value)),
            output: display
                .output
                .map(|value| protocol::json_from_value(&value)),
            error: display.error.map(|value| protocol::json_from_value(&value)),
            action: display.action,
            format: display.format,
            language: display.language,
        }),
    })
}

fn interaction_to_proto(value: AgentInteraction) -> ProviderResult<pb::AgentInteraction> {
    Ok(pb::AgentInteraction {
        id: value.id,
        r#type: value.r#type.as_i32(),
        state: value.state.as_i32(),
        title: value.title,
        prompt: value.prompt,
        request: struct_from_json(value.request)?,
        resolution: struct_from_json(value.resolution)?,
        created_at: timestamp_from_time(value.created_at),
        resolved_at: timestamp_from_time(value.resolved_at),
        turn_id: value.turn_id,
        session_id: value.session_id,
    })
}

pub(crate) fn interaction_from_proto(
    value: pb::AgentInteraction,
) -> ProviderResult<AgentInteraction> {
    Ok(AgentInteraction {
        id: value.id,
        r#type: AgentInteractionType::try_from(value.r#type)?,
        state: AgentInteractionState::try_from(value.state)?,
        title: value.title,
        prompt: value.prompt,
        request: json_from_struct(value.request),
        resolution: json_from_struct(value.resolution),
        created_at: time_from_timestamp(value.created_at)?,
        resolved_at: time_from_timestamp(value.resolved_at)?,
        turn_id: value.turn_id,
        session_id: value.session_id,
    })
}

fn capabilities_to_proto(value: AgentProviderCapabilities) -> pb::AgentProviderCapabilities {
    pb::AgentProviderCapabilities {
        streaming_text: value.streaming_text,
        tool_calls: value.tool_calls,
        parallel_tool_calls: value.parallel_tool_calls,
        structured_output: value.structured_output,
        interactions: value.interactions,
        resumable_turns: value.resumable_turns,
        reasoning_summaries: value.reasoning_summaries,
        bounded_list_hydration: value.bounded_list_hydration,
        supported_tool_sources: value
            .supported_tool_sources
            .into_iter()
            .map(AgentToolSourceMode::as_i32)
            .collect(),
        supports_session_start: value.supports_session_start,
        supports_prepared_workspace: value.supports_prepared_workspace,
    }
}

fn create_session_request_from_proto(
    value: pb::CreateAgentProviderSessionRequest,
) -> CreateAgentProviderSessionRequest {
    CreateAgentProviderSessionRequest {
        session_id: value.session_id,
        idempotency_key: value.idempotency_key,
        model: value.model,
        client_ref: value.client_ref,
        metadata: json_from_struct(value.metadata),
        created_by: agent_actor_from_proto(value.created_by),
        subject: agent_subject_from_proto(value.subject),
        session_start: value.session_start.map(|value| AgentSessionStartConfig {
            hooks: value
                .hooks
                .into_iter()
                .map(|hook| AgentSessionStartHook {
                    id: hook.id,
                    r#type: hook.r#type,
                    command: hook.command,
                    cwd: hook.cwd,
                    timeout: hook.timeout,
                    env: hook.env.into_iter().collect(),
                    output: hook.output.map(|output| AgentSessionStartHookOutput {
                        additional_context: output.additional_context,
                        metadata: output.metadata,
                    }),
                })
                .collect(),
        }),
        prepared_workspace: value
            .prepared_workspace
            .map(|value| AgentPreparedWorkspace {
                root: value.root,
                cwd: value.cwd,
            }),
    }
}

fn create_turn_request_from_proto(
    value: pb::CreateAgentProviderTurnRequest,
) -> CreateAgentProviderTurnRequest {
    CreateAgentProviderTurnRequest {
        turn_id: value.turn_id,
        session_id: value.session_id,
        idempotency_key: value.idempotency_key,
        model: value.model,
        messages: value.messages.into_iter().map(message_from_proto).collect(),
        tools: value
            .tools
            .into_iter()
            .map(|tool| ResolvedAgentTool {
                id: tool.id,
                name: tool.name,
                description: tool.description,
                parameters_schema: json_from_struct(tool.parameters_schema),
            })
            .collect(),
        response_schema: json_from_struct(value.response_schema),
        metadata: json_from_struct(value.metadata),
        created_by: agent_actor_from_proto(value.created_by),
        execution_ref: value.execution_ref,
        tool_refs: value
            .tool_refs
            .into_iter()
            .map(agent_tool_ref_from_proto)
            .collect(),
        tool_source: AgentToolSourceMode::from_i32_lossy(value.tool_source),
        subject: agent_subject_from_proto(value.subject),
        model_options: json_from_struct(value.model_options),
        run_grant: value.run_grant,
    }
}

fn execute_tool_response_from_proto(
    value: pb::ExecuteAgentToolResponse,
) -> ExecuteAgentToolResponse {
    ExecuteAgentToolResponse {
        status: value.status,
        body: value.body,
    }
}

fn listed_tool_from_proto(value: pb::ListedAgentTool) -> ListedAgentTool {
    ListedAgentTool {
        id: value.id,
        mcp_name: value.mcp_name,
        title: value.title,
        description: value.description,
        input_schema: value.input_schema,
        output_schema: value.output_schema,
        annotations: value.annotations.map(|annotations| AgentToolAnnotations {
            read_only_hint: annotations.read_only_hint,
            idempotent_hint: annotations.idempotent_hint,
            destructive_hint: annotations.destructive_hint,
            open_world_hint: annotations.open_world_hint,
        }),
        r#ref: value.r#ref.map(agent_tool_ref_from_proto),
        tags: value.tags,
        search_text: value.search_text,
    }
}

fn list_tools_response_from_proto(value: pb::ListAgentToolsResponse) -> ListAgentToolsResponse {
    ListAgentToolsResponse {
        tools: value
            .tools
            .into_iter()
            .map(listed_tool_from_proto)
            .collect(),
        next_page_token: value.next_page_token,
    }
}

fn resolved_connection_from_proto(
    value: pb::ResolvedAgentConnection,
) -> ProviderResult<ResolvedAgentConnection> {
    Ok(ResolvedAgentConnection {
        connection_id: value.connection_id,
        connection: value.connection,
        instance: value.instance,
        mode: value.mode,
        headers: value.headers.into_iter().collect(),
        params: value.params.into_iter().collect(),
        expires_at: time_from_timestamp(value.expires_at)?,
    })
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

    /// Creates or idempotently returns an agent session.
    async fn create_session(
        &self,
        _request: CreateAgentProviderSessionRequest,
    ) -> ProviderResult<AgentSession> {
        Err(crate::Error::unimplemented(
            "agent create session is not implemented",
        ))
    }

    /// Returns one agent session by ID.
    async fn get_session(
        &self,
        _request: GetAgentProviderSessionRequest,
    ) -> ProviderResult<AgentSession> {
        Err(crate::Error::unimplemented(
            "agent get session is not implemented",
        ))
    }

    /// Lists agent sessions visible to the request subject.
    async fn list_sessions(
        &self,
        _request: ListAgentProviderSessionsRequest,
    ) -> ProviderResult<ListAgentProviderSessionsResponse> {
        Err(crate::Error::unimplemented(
            "agent list sessions is not implemented",
        ))
    }

    /// Updates mutable agent session metadata or state.
    async fn update_session(
        &self,
        _request: UpdateAgentProviderSessionRequest,
    ) -> ProviderResult<AgentSession> {
        Err(crate::Error::unimplemented(
            "agent update session is not implemented",
        ))
    }

    /// Starts or idempotently returns an agent turn.
    async fn create_turn(
        &self,
        _request: CreateAgentProviderTurnRequest,
    ) -> ProviderResult<AgentTurn> {
        Err(crate::Error::unimplemented(
            "agent create turn is not implemented",
        ))
    }

    /// Returns one agent turn by ID.
    async fn get_turn(&self, _request: GetAgentProviderTurnRequest) -> ProviderResult<AgentTurn> {
        Err(crate::Error::unimplemented(
            "agent get turn is not implemented",
        ))
    }

    /// Lists turns for a session or query.
    async fn list_turns(
        &self,
        _request: ListAgentProviderTurnsRequest,
    ) -> ProviderResult<ListAgentProviderTurnsResponse> {
        Err(crate::Error::unimplemented(
            "agent list turns is not implemented",
        ))
    }

    /// Requests cancellation of a running or pending turn.
    async fn cancel_turn(
        &self,
        _request: CancelAgentProviderTurnRequest,
    ) -> ProviderResult<AgentTurn> {
        Err(crate::Error::unimplemented(
            "agent cancel turn is not implemented",
        ))
    }

    /// Lists ordered events emitted by a turn.
    async fn list_turn_events(
        &self,
        _request: ListAgentProviderTurnEventsRequest,
    ) -> ProviderResult<ListAgentProviderTurnEventsResponse> {
        Err(crate::Error::unimplemented(
            "agent list turn events is not implemented",
        ))
    }

    /// Returns one pending or resolved interaction.
    async fn get_interaction(
        &self,
        _request: GetAgentProviderInteractionRequest,
    ) -> ProviderResult<AgentInteraction> {
        Err(crate::Error::unimplemented(
            "agent get interaction is not implemented",
        ))
    }

    /// Lists interactions associated with a turn.
    async fn list_interactions(
        &self,
        _request: ListAgentProviderInteractionsRequest,
    ) -> ProviderResult<ListAgentProviderInteractionsResponse> {
        Err(crate::Error::unimplemented(
            "agent list interactions is not implemented",
        ))
    }

    /// Records a response to a pending interaction.
    async fn resolve_interaction(
        &self,
        _request: ResolveAgentProviderInteractionRequest,
    ) -> ProviderResult<AgentInteraction> {
        Err(crate::Error::unimplemented(
            "agent resolve interaction is not implemented",
        ))
    }

    /// Returns the provider's supported agent features.
    async fn get_capabilities(
        &self,
        _request: GetAgentProviderCapabilitiesRequest,
    ) -> ProviderResult<AgentProviderCapabilities> {
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
            .create_session(create_session_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("agent create session", error))?;
        Ok(GrpcResponse::new(session_to_proto(session).map_err(
            |error| rpc_status("agent create session", error),
        )?))
    }

    async fn get_session(
        &self,
        request: GrpcRequest<pb::GetAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentSession>, Status> {
        let session = self
            .provider
            .get_session({
                let request = request.into_inner();
                GetAgentProviderSessionRequest {
                    session_id: request.session_id,
                    subject: agent_subject_from_proto(request.subject),
                }
            })
            .await
            .map_err(|error| rpc_status("agent get session", error))?;
        Ok(GrpcResponse::new(
            session_to_proto(session).map_err(|error| rpc_status("agent get session", error))?,
        ))
    }

    async fn list_sessions(
        &self,
        request: GrpcRequest<pb::ListAgentProviderSessionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderSessionsResponse>, Status> {
        let response = self
            .provider
            .list_sessions({
                let request = request.into_inner();
                ListAgentProviderSessionsRequest {
                    subject: agent_subject_from_proto(request.subject),
                    session_ids: request.session_ids,
                    state: AgentSessionState::from_i32_lossy(request.state),
                    limit: request.limit,
                    summary_only: request.summary_only,
                }
            })
            .await
            .map_err(|error| rpc_status("agent list sessions", error))?;
        Ok(GrpcResponse::new(pb::ListAgentProviderSessionsResponse {
            sessions: response
                .sessions
                .into_iter()
                .map(session_to_proto)
                .collect::<ProviderResult<Vec<_>>>()
                .map_err(|error| rpc_status("agent list sessions", error))?,
        }))
    }

    async fn update_session(
        &self,
        request: GrpcRequest<pb::UpdateAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentSession>, Status> {
        let session = self
            .provider
            .update_session({
                let request = request.into_inner();
                UpdateAgentProviderSessionRequest {
                    session_id: request.session_id,
                    client_ref: request.client_ref,
                    state: AgentSessionState::from_i32_lossy(request.state),
                    metadata: json_from_struct(request.metadata),
                    subject: agent_subject_from_proto(request.subject),
                }
            })
            .await
            .map_err(|error| rpc_status("agent update session", error))?;
        Ok(GrpcResponse::new(session_to_proto(session).map_err(
            |error| rpc_status("agent update session", error),
        )?))
    }

    async fn create_turn(
        &self,
        request: GrpcRequest<pb::CreateAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        let turn = self
            .provider
            .create_turn(create_turn_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("agent create turn", error))?;
        Ok(GrpcResponse::new(
            turn_to_proto(turn).map_err(|error| rpc_status("agent create turn", error))?,
        ))
    }

    async fn get_turn(
        &self,
        request: GrpcRequest<pb::GetAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        let turn = self
            .provider
            .get_turn({
                let request = request.into_inner();
                GetAgentProviderTurnRequest {
                    turn_id: request.turn_id,
                    subject: agent_subject_from_proto(request.subject),
                }
            })
            .await
            .map_err(|error| rpc_status("agent get turn", error))?;
        Ok(GrpcResponse::new(
            turn_to_proto(turn).map_err(|error| rpc_status("agent get turn", error))?,
        ))
    }

    async fn list_turns(
        &self,
        request: GrpcRequest<pb::ListAgentProviderTurnsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderTurnsResponse>, Status> {
        let response = self
            .provider
            .list_turns({
                let request = request.into_inner();
                ListAgentProviderTurnsRequest {
                    session_id: request.session_id,
                    subject: agent_subject_from_proto(request.subject),
                    turn_ids: request.turn_ids,
                    status: AgentExecutionStatus::from_i32_lossy(request.status),
                    limit: request.limit,
                    summary_only: request.summary_only,
                }
            })
            .await
            .map_err(|error| rpc_status("agent list turns", error))?;
        Ok(GrpcResponse::new(pb::ListAgentProviderTurnsResponse {
            turns: response
                .turns
                .into_iter()
                .map(turn_to_proto)
                .collect::<ProviderResult<Vec<_>>>()
                .map_err(|error| rpc_status("agent list turns", error))?,
        }))
    }

    async fn cancel_turn(
        &self,
        request: GrpcRequest<pb::CancelAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        let turn = self
            .provider
            .cancel_turn({
                let request = request.into_inner();
                CancelAgentProviderTurnRequest {
                    turn_id: request.turn_id,
                    reason: request.reason,
                    subject: agent_subject_from_proto(request.subject),
                }
            })
            .await
            .map_err(|error| rpc_status("agent cancel turn", error))?;
        Ok(GrpcResponse::new(
            turn_to_proto(turn).map_err(|error| rpc_status("agent cancel turn", error))?,
        ))
    }

    async fn list_turn_events(
        &self,
        request: GrpcRequest<pb::ListAgentProviderTurnEventsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderTurnEventsResponse>, Status> {
        let response = self
            .provider
            .list_turn_events({
                let request = request.into_inner();
                ListAgentProviderTurnEventsRequest {
                    turn_id: request.turn_id,
                    after_seq: request.after_seq,
                    limit: request.limit,
                    subject: agent_subject_from_proto(request.subject),
                }
            })
            .await
            .map_err(|error| rpc_status("agent list turn events", error))?;
        Ok(GrpcResponse::new(pb::ListAgentProviderTurnEventsResponse {
            events: response
                .events
                .into_iter()
                .map(event_to_proto)
                .collect::<ProviderResult<Vec<_>>>()
                .map_err(|error| rpc_status("agent list turn events", error))?,
        }))
    }

    async fn get_interaction(
        &self,
        request: GrpcRequest<pb::GetAgentProviderInteractionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentInteraction>, Status> {
        let interaction = self
            .provider
            .get_interaction({
                let request = request.into_inner();
                GetAgentProviderInteractionRequest {
                    interaction_id: request.interaction_id,
                    subject: agent_subject_from_proto(request.subject),
                }
            })
            .await
            .map_err(|error| rpc_status("agent get interaction", error))?;
        Ok(GrpcResponse::new(
            interaction_to_proto(interaction)
                .map_err(|error| rpc_status("agent get interaction", error))?,
        ))
    }

    async fn list_interactions(
        &self,
        request: GrpcRequest<pb::ListAgentProviderInteractionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderInteractionsResponse>, Status> {
        let response = self
            .provider
            .list_interactions({
                let request = request.into_inner();
                ListAgentProviderInteractionsRequest {
                    turn_id: request.turn_id,
                    subject: agent_subject_from_proto(request.subject),
                }
            })
            .await
            .map_err(|error| rpc_status("agent list interactions", error))?;
        Ok(GrpcResponse::new(
            pb::ListAgentProviderInteractionsResponse {
                interactions: response
                    .interactions
                    .into_iter()
                    .map(interaction_to_proto)
                    .collect::<ProviderResult<Vec<_>>>()
                    .map_err(|error| rpc_status("agent list interactions", error))?,
            },
        ))
    }

    async fn resolve_interaction(
        &self,
        request: GrpcRequest<pb::ResolveAgentProviderInteractionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentInteraction>, Status> {
        let interaction = self
            .provider
            .resolve_interaction({
                let request = request.into_inner();
                ResolveAgentProviderInteractionRequest {
                    interaction_id: request.interaction_id,
                    resolution: json_from_struct(request.resolution),
                    subject: agent_subject_from_proto(request.subject),
                }
            })
            .await
            .map_err(|error| rpc_status("agent resolve interaction", error))?;
        Ok(GrpcResponse::new(
            interaction_to_proto(interaction)
                .map_err(|error| rpc_status("agent resolve interaction", error))?,
        ))
    }

    async fn get_capabilities(
        &self,
        _request: GrpcRequest<pb::GetAgentProviderCapabilitiesRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentProviderCapabilities>, Status> {
        let capabilities = self
            .provider
            .get_capabilities(GetAgentProviderCapabilitiesRequest {})
            .await
            .map_err(|error| rpc_status("agent get capabilities", error))?;
        Ok(GrpcResponse::new(capabilities_to_proto(capabilities)))
    }
}
