use std::sync::Arc;
use std::time::SystemTime;

use prost_types::{Struct, Timestamp, Value};
use serde::Serialize;
use tonic::codegen::async_trait;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::api::{RuntimeMetadata, Subject, scope_request_context};
use crate::error::Result as ProviderResult;
use crate::generated::v1::{self as pb};
use crate::protocol;
use crate::rpc_status::rpc_status;

/// Native JSON object used by authored agent providers.
pub type AgentJson = serde_json::Value;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Hash)]
#[repr(i32)]
/// Native enum for `gestalt.provider.v1.AgentMessagePartType`.
pub enum AgentMessagePartType {
    #[default]
    /// The `Unspecified` variant.
    Unspecified = 0,
    /// The `Text` variant.
    Text = 1,
    /// The `Json` variant.
    Json = 2,
    /// The `ToolCall` variant.
    ToolCall = 3,
    /// The `ToolResult` variant.
    ToolResult = 4,
    /// The `ImageRef` variant.
    ImageRef = 5,
}

impl AgentMessagePartType {
    /// Returns the wire integer for this value.
    pub const fn as_i32(self) -> i32 {
        self as i32
    }

    /// Converts a wire integer, mapping unknown values to the unspecified
    /// variant.
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
/// Native enum for `gestalt.provider.v1.AgentToolSourceMode`.
pub enum AgentToolSourceMode {
    #[default]
    /// The `Unspecified` variant.
    Unspecified = 0,
    /// The `Catalog` variant.
    Catalog = 2,
    /// The `None` variant.
    None = 3,
}

impl AgentToolSourceMode {
    /// Returns the wire integer for this value.
    pub const fn as_i32(self) -> i32 {
        self as i32
    }

    /// Converts a wire integer, mapping unknown values to the unspecified
    /// variant.
    pub const fn from_i32_lossy(value: i32) -> Self {
        match value {
            2 => Self::Catalog,
            3 => Self::None,
            _ => Self::Unspecified,
        }
    }
}

impl TryFrom<i32> for AgentToolSourceMode {
    type Error = crate::Error;

    fn try_from(value: i32) -> ProviderResult<Self> {
        match value {
            0 => Ok(Self::Unspecified),
            2 => Ok(Self::Catalog),
            3 => Ok(Self::None),
            _ => Err(crate::Error::bad_request(format!(
                "unknown agent tool source mode {value}"
            ))),
        }
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Hash)]
#[repr(i32)]
/// Native enum for `gestalt.provider.v1.AgentExecutionStatus`.
pub enum AgentExecutionStatus {
    #[default]
    /// The `Unspecified` variant.
    Unspecified = 0,
    /// The `Pending` variant.
    Pending = 1,
    /// The `Running` variant.
    Running = 2,
    /// The `Succeeded` variant.
    Succeeded = 3,
    /// The `Failed` variant.
    Failed = 4,
    /// The `Canceled` variant.
    Canceled = 5,
    /// The `WaitingForInput` variant.
    WaitingForInput = 6,
}

impl AgentExecutionStatus {
    /// Returns the wire integer for this value.
    pub const fn as_i32(self) -> i32 {
        self as i32
    }

    /// Converts a wire integer, mapping unknown values to the unspecified
    /// variant.
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
/// Native enum for `gestalt.provider.v1.AgentSessionState`.
pub enum AgentSessionState {
    #[default]
    /// The `Unspecified` variant.
    Unspecified = 0,
    /// The `Active` variant.
    Active = 1,
    /// The `Archived` variant.
    Archived = 2,
}

impl AgentSessionState {
    /// Returns the wire integer for this value.
    pub const fn as_i32(self) -> i32 {
        self as i32
    }

    /// Converts a wire integer, mapping unknown values to the unspecified
    /// variant.
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
/// Native enum for `gestalt.provider.v1.AgentInteractionType`.
pub enum AgentInteractionType {
    #[default]
    /// The `Unspecified` variant.
    Unspecified = 0,
    /// The `Approval` variant.
    Approval = 1,
    /// The `Clarification` variant.
    Clarification = 2,
    /// The `Input` variant.
    Input = 3,
}

impl AgentInteractionType {
    /// Returns the wire integer for this value.
    pub const fn as_i32(self) -> i32 {
        self as i32
    }

    /// Converts a wire integer, mapping unknown values to the unspecified
    /// variant.
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
/// Native enum for `gestalt.provider.v1.AgentInteractionState`.
pub enum AgentInteractionState {
    #[default]
    /// The `Unspecified` variant.
    Unspecified = 0,
    /// The `Pending` variant.
    Pending = 1,
    /// The `Resolved` variant.
    Resolved = 2,
    /// The `Canceled` variant.
    Canceled = 3,
}

impl AgentInteractionState {
    /// Returns the wire integer for this value.
    pub const fn as_i32(self) -> i32 {
        self as i32
    }

    /// Converts a wire integer, mapping unknown values to the unspecified
    /// variant.
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
/// Native message type for `gestalt.provider.v1.AgentMessage`.
pub struct AgentMessage {
    /// The `role` field.
    pub role: String,
    /// The `text` field.
    pub text: String,
    /// The `parts` field.
    pub parts: Vec<AgentMessagePart>,
    /// The `metadata` field.
    pub metadata: Option<AgentJson>,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.AgentMessagePartToolCall`.
pub struct AgentMessagePartToolCall {
    /// The `id` field.
    pub id: String,
    /// The `tool_id` field.
    pub tool_id: String,
    /// The `arguments` field.
    pub arguments: Option<AgentJson>,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.AgentMessagePartToolResult`.
pub struct AgentMessagePartToolResult {
    /// The `tool_call_id` field.
    pub tool_call_id: String,
    /// The `status` field.
    pub status: i32,
    /// The `content` field.
    pub content: String,
    /// The `output` field.
    pub output: Option<AgentJson>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.AgentMessagePartImageRef`.
pub struct AgentMessagePartImageRef {
    /// The `uri` field.
    pub uri: String,
    /// The `mime_type` field.
    pub mime_type: String,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.AgentMessagePart`.
pub struct AgentMessagePart {
    /// The `type` field.
    pub r#type: AgentMessagePartType,
    /// The `text` field.
    pub text: String,
    /// The `json` field.
    pub json: Option<AgentJson>,
    /// The `tool_call` field.
    pub tool_call: Option<AgentMessagePartToolCall>,
    /// The `tool_result` field.
    pub tool_result: Option<AgentMessagePartToolResult>,
    /// The `image_ref` field.
    pub image_ref: Option<AgentMessagePartImageRef>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Describes the workspace a provider prepared for a session.
pub struct AgentPreparedWorkspace {
    /// The `root` field.
    pub root: String,
    /// The `cwd` field.
    pub cwd: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.AgentToolRef`.
pub struct AgentToolRef {
    /// The `app` field.
    pub app: String,
    /// The `operation` field.
    pub operation: String,
    /// The `connection` field.
    pub connection: String,
    /// The `instance` field.
    pub instance: String,
    /// The `title` field.
    pub title: String,
    /// The `description` field.
    pub description: String,
    /// The `credential_mode` field.
    pub credential_mode: String,
    /// The `system` field.
    pub system: String,
    /// The `run_as` field.
    pub run_as: Option<Subject>,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.AgentToolConfig`.
pub struct AgentToolConfig {
    /// The `source` field.
    pub source: Option<AgentToolConfigSource>,
}

#[derive(Debug, Clone, PartialEq)]
/// Selects where a session's tools come from.
pub enum AgentToolConfigSource {
    /// The `None` variant.
    None(AgentNoTools),
    /// The `Catalog` variant.
    Catalog(AgentCatalogToolConfig),
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AgentNoTools {}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.AgentCatalogToolConfig`.
pub struct AgentCatalogToolConfig {
    /// The `refs` field.
    pub refs: Vec<AgentToolRef>,
    /// The `tools` field.
    pub tools: Vec<ListedAgentTool>,
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
    /// The `checkouts` field.
    pub checkouts: Vec<AgentWorkspaceGitCheckout>,
    /// The `cwd` field.
    pub cwd: String,
}

/// Native agent workspace git checkout data.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct AgentWorkspaceGitCheckout {
    /// The `url` field.
    pub url: String,
    /// The `reference` field.
    pub reference: String,
    /// The `path` field.
    pub path: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.AgentProviderCapabilities`.
pub struct AgentProviderCapabilities {
    /// The `streaming_text` field.
    pub streaming_text: bool,
    /// The `tool_calls` field.
    pub tool_calls: bool,
    /// The `parallel_tool_calls` field.
    pub parallel_tool_calls: bool,
    /// The `interactions` field.
    pub interactions: bool,
    /// The `resumable_turns` field.
    pub resumable_turns: bool,
    /// The `reasoning_summaries` field.
    pub reasoning_summaries: bool,
    /// The `bounded_list_hydration` field.
    pub bounded_list_hydration: bool,
    /// The `supported_tool_sources` field.
    pub supported_tool_sources: Vec<AgentToolSourceMode>,
    /// The `supports_session_start` field.
    pub supports_session_start: bool,
    /// The `supports_prepared_workspace` field.
    pub supports_prepared_workspace: bool,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.GetAgentProviderCapabilitiesRequest`.
pub struct GetAgentProviderCapabilitiesRequest {}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.AgentInteraction`.
pub struct AgentInteraction {
    /// The `id` field.
    pub id: String,
    /// The `type` field.
    pub r#type: AgentInteractionType,
    /// The `state` field.
    pub state: AgentInteractionState,
    /// The `title` field.
    pub title: String,
    /// The `prompt` field.
    pub prompt: String,
    /// The `request` field.
    pub request: Option<AgentJson>,
    /// The `resolution` field.
    pub resolution: Option<AgentJson>,
    /// The `created_at` field.
    pub created_at: Option<SystemTime>,
    /// The `resolved_at` field.
    pub resolved_at: Option<SystemTime>,
    /// The `turn_id` field.
    pub turn_id: String,
    /// The `session_id` field.
    pub session_id: String,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.AgentSession`.
pub struct AgentSession {
    /// The `id` field.
    pub id: String,
    /// The `provider_name` field.
    pub provider_name: String,
    /// The `model` field.
    pub model: String,
    /// The `client_ref` field.
    pub client_ref: String,
    /// The `state` field.
    pub state: AgentSessionState,
    /// The `metadata` field.
    pub metadata: Option<AgentJson>,
    /// The `created_by_subject_id` field.
    pub created_by_subject_id: Option<String>,
    /// The `created_at` field.
    pub created_at: Option<SystemTime>,
    /// The `updated_at` field.
    pub updated_at: Option<SystemTime>,
    /// The `last_turn_at` field.
    pub last_turn_at: Option<SystemTime>,
}

/// Request passed to [`AgentProvider::create_session`].
#[derive(Debug, Clone, Default, PartialEq)]
pub struct CreateAgentProviderSessionRequest {
    /// The `idempotency_key` field.
    pub idempotency_key: String,
    /// The `model` field.
    pub model: String,
    /// The `client_ref` field.
    pub client_ref: String,
    /// The `metadata` field.
    pub metadata: Option<AgentJson>,
    /// The `session_start` field.
    pub session_start: Option<AgentSessionStartConfig>,
    /// The `prepared_workspace` field.
    pub prepared_workspace: Option<AgentPreparedWorkspace>,
    /// The `tools` field.
    pub tools: Option<AgentToolConfig>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.AgentSessionStartConfig`.
pub struct AgentSessionStartConfig {
    /// The `hooks` field.
    pub hooks: Vec<AgentSessionStartHook>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.AgentSessionStartHook`.
pub struct AgentSessionStartHook {
    /// The `id` field.
    pub id: String,
    /// The `type` field.
    pub r#type: String,
    /// The `command` field.
    pub command: Vec<String>,
    /// The `cwd` field.
    pub cwd: String,
    /// The `timeout` field.
    pub timeout: String,
    /// The `env` field.
    pub env: std::collections::HashMap<String, String>,
    /// The `output` field.
    pub output: Option<AgentSessionStartHookOutput>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.AgentSessionStartHookOutput`.
pub struct AgentSessionStartHookOutput {
    /// The `additional_context` field.
    pub additional_context: bool,
    /// The `metadata` field.
    pub metadata: bool,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.GetAgentProviderSessionRequest`.
pub struct GetAgentProviderSessionRequest {
    /// The `provider_name` field.
    pub provider_name: String,
    /// The `session_id` field.
    pub session_id: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.ListAgentProviderSessionsRequest`.
pub struct ListAgentProviderSessionsRequest {
    /// The `provider_name` field.
    pub provider_name: String,
    /// The `session_ids` field.
    pub session_ids: Vec<String>,
    /// The `state` field.
    pub state: AgentSessionState,
    /// The `limit` field.
    pub limit: i32,
    /// The `summary_only` field.
    pub summary_only: bool,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.ListAgentProviderSessionsResponse`.
pub struct ListAgentProviderSessionsResponse {
    /// The `sessions` field.
    pub sessions: Vec<AgentSession>,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.UpdateAgentProviderSessionRequest`.
pub struct UpdateAgentProviderSessionRequest {
    /// The `provider_name` field.
    pub provider_name: String,
    /// The `session_id` field.
    pub session_id: String,
    /// The `client_ref` field.
    pub client_ref: String,
    /// The `state` field.
    pub state: AgentSessionState,
    /// The `metadata` field.
    pub metadata: Option<AgentJson>,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.AgentTurn`.
pub struct AgentTurn {
    /// The `id` field.
    pub id: String,
    /// The `session_id` field.
    pub session_id: String,
    /// The `provider_name` field.
    pub provider_name: String,
    /// The `model` field.
    pub model: String,
    /// The `status` field.
    pub status: AgentExecutionStatus,
    /// The `messages` field.
    pub messages: Vec<AgentMessage>,
    /// The `output` field.
    pub output: Option<AgentTurnOutput>,
    /// The `status_message` field.
    pub status_message: String,
    /// The `created_by_subject_id` field.
    pub created_by_subject_id: Option<String>,
    /// The `created_at` field.
    pub created_at: Option<SystemTime>,
    /// The `started_at` field.
    pub started_at: Option<SystemTime>,
    /// The `completed_at` field.
    pub completed_at: Option<SystemTime>,
    /// The `execution_ref` field.
    pub execution_ref: String,
}

#[derive(Debug, Clone, PartialEq)]
/// The structured-or-text output of a finished turn.
pub enum AgentTurnOutput {
    /// The `Text` variant.
    Text(AgentTurnTextOutput),
    /// The `Structured` variant.
    Structured(AgentTurnStructuredOutput),
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.AgentTurnTextOutput`.
pub struct AgentTurnTextOutput {
    /// The `text` field.
    pub text: String,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.AgentTurnStructuredOutput`.
pub struct AgentTurnStructuredOutput {
    /// The `text` field.
    pub text: String,
    /// The `value` field.
    pub value: Option<AgentJson>,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.AgentTurnDisplay`.
pub struct AgentTurnDisplay {
    /// The `kind` field.
    pub kind: String,
    /// The `phase` field.
    pub phase: String,
    /// The `text` field.
    pub text: String,
    /// The `label` field.
    pub label: String,
    /// The `ref` field.
    pub r#ref: String,
    /// The `parent_ref` field.
    pub parent_ref: String,
    /// The `input` field.
    pub input: Option<AgentJson>,
    /// The `output` field.
    pub output: Option<AgentJson>,
    /// The `error` field.
    pub error: Option<AgentJson>,
    /// The `action` field.
    pub action: String,
    /// The `format` field.
    pub format: String,
    /// The `language` field.
    pub language: String,
}

#[derive(Debug, Clone, PartialEq)]
/// Native message type for `gestalt.provider.v1.CreateAgentProviderTurnRequest`.
pub struct CreateAgentProviderTurnRequest {
    /// The `provider_name` field.
    pub provider_name: String,
    /// The `turn_id` field.
    pub turn_id: String,
    /// The `session_id` field.
    pub session_id: String,
    /// The `idempotency_key` field.
    pub idempotency_key: String,
    /// The `model` field.
    pub model: String,
    /// The `messages` field.
    pub messages: Vec<AgentMessage>,
    /// The `output` field.
    pub output: AgentOutput,
    /// The `metadata` field.
    pub metadata: Option<AgentJson>,
    /// The `execution_ref` field.
    pub execution_ref: String,
    /// The `model_options` field.
    pub model_options: Option<AgentJson>,
    /// The `timeout_seconds` field.
    pub timeout_seconds: i32,
    /// The `context` field.
    pub context: Option<pb::RequestContext>,
}

#[derive(Debug, Clone, PartialEq)]
/// Native enum for `gestalt.provider.v1.AgentOutput`.
pub enum AgentOutput {
    /// The `Text` variant.
    Text(AgentTextOutput),
    /// The `Structured` variant.
    Structured(AgentStructuredOutput),
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.AgentTextOutput`.
pub struct AgentTextOutput {}

#[derive(Debug, Clone, PartialEq)]
/// Native message type for `gestalt.provider.v1.AgentStructuredOutput`.
pub struct AgentStructuredOutput {
    /// The `schema` field.
    pub schema: AgentJson,
}

impl AgentOutput {
    /// Requests an unstructured text turn.
    pub fn text() -> Self {
        Self::Text(AgentTextOutput {})
    }

    /// Requests a structured turn with the supplied JSON Schema object.
    pub fn structured_schema<T: Serialize>(schema: T) -> ProviderResult<Self> {
        Ok(Self::Structured(AgentStructuredOutput {
            schema: protocol::json_from_serializable(schema)?,
        }))
    }
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.GetAgentProviderTurnRequest`.
pub struct GetAgentProviderTurnRequest {
    /// The `provider_name` field.
    pub provider_name: String,
    /// The `turn_id` field.
    pub turn_id: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.ListAgentProviderTurnsRequest`.
pub struct ListAgentProviderTurnsRequest {
    /// The `provider_name` field.
    pub provider_name: String,
    /// The `session_id` field.
    pub session_id: String,
    /// The `turn_ids` field.
    pub turn_ids: Vec<String>,
    /// The `status` field.
    pub status: AgentExecutionStatus,
    /// The `limit` field.
    pub limit: i32,
    /// The `summary_only` field.
    pub summary_only: bool,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.ListAgentProviderTurnsResponse`.
pub struct ListAgentProviderTurnsResponse {
    /// The `turns` field.
    pub turns: Vec<AgentTurn>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.CancelAgentProviderTurnRequest`.
pub struct CancelAgentProviderTurnRequest {
    /// The `provider_name` field.
    pub provider_name: String,
    /// The `turn_id` field.
    pub turn_id: String,
    /// The `reason` field.
    pub reason: String,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.AgentTurnEvent`.
pub struct AgentTurnEvent {
    /// The `id` field.
    pub id: String,
    /// The `turn_id` field.
    pub turn_id: String,
    /// The `seq` field.
    pub seq: i64,
    /// The `type` field.
    pub r#type: String,
    /// The `source` field.
    pub source: String,
    /// The `visibility` field.
    pub visibility: String,
    /// The `data` field.
    pub data: Option<AgentJson>,
    /// The `created_at` field.
    pub created_at: Option<SystemTime>,
    /// The `display` field.
    pub display: Option<AgentTurnDisplay>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.ListAgentProviderTurnEventsRequest`.
pub struct ListAgentProviderTurnEventsRequest {
    /// The `provider_name` field.
    pub provider_name: String,
    /// The `turn_id` field.
    pub turn_id: String,
    /// The `after_seq` field.
    pub after_seq: i64,
    /// The `limit` field.
    pub limit: i32,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.ListAgentProviderTurnEventsResponse`.
pub struct ListAgentProviderTurnEventsResponse {
    /// The `events` field.
    pub events: Vec<AgentTurnEvent>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.GetAgentProviderInteractionRequest`.
pub struct GetAgentProviderInteractionRequest {
    /// The `interaction_id` field.
    pub interaction_id: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// Native message type for `gestalt.provider.v1.ListAgentProviderInteractionsRequest`.
pub struct ListAgentProviderInteractionsRequest {
    /// The `provider_name` field.
    pub provider_name: String,
    /// The `turn_id` field.
    pub turn_id: String,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.ListAgentProviderInteractionsResponse`.
pub struct ListAgentProviderInteractionsResponse {
    /// The `interactions` field.
    pub interactions: Vec<AgentInteraction>,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.ResolveAgentProviderInteractionRequest`.
pub struct ResolveAgentProviderInteractionRequest {
    /// The `provider_name` field.
    pub provider_name: String,
    /// The `interaction_id` field.
    pub interaction_id: String,
    /// The `resolution` field.
    pub resolution: Option<AgentJson>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
/// MCP-style behavior hints of a tool.
pub struct AgentToolAnnotations {
    /// The `read_only_hint` field.
    pub read_only_hint: Option<bool>,
    /// The `idempotent_hint` field.
    pub idempotent_hint: Option<bool>,
    /// The `destructive_hint` field.
    pub destructive_hint: Option<bool>,
    /// The `open_world_hint` field.
    pub open_world_hint: Option<bool>,
}

#[derive(Debug, Clone, Default, PartialEq)]
/// Native message type for `gestalt.provider.v1.ListedAgentTool`.
pub struct ListedAgentTool {
    /// The `id` field.
    pub id: String,
    /// The `mcp_name` field.
    pub mcp_name: String,
    /// The `title` field.
    pub title: String,
    /// The `description` field.
    pub description: String,
    /// The `input_schema` field.
    pub input_schema: String,
    /// The `output_schema` field.
    pub output_schema: String,
    /// The `annotations` field.
    pub annotations: Option<AgentToolAnnotations>,
    /// The `ref` field.
    pub r#ref: Option<AgentToolRef>,
    /// The `tags` field.
    pub tags: Vec<String>,
    /// The `search_text` field.
    pub search_text: String,
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
        app: input.app,
        operation: input.operation,
        connection: input.connection,
        instance: input.instance,
        title: input.title,
        description: input.description,
        credential_mode: input.credential_mode,
        system: input.system,
        run_as: input.run_as,
    }
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

fn json_from_struct(value: Option<Struct>) -> Option<AgentJson> {
    value.map(|value| protocol::json_from_struct(&value))
}

fn struct_from_json(value: Option<AgentJson>) -> ProviderResult<Option<Struct>> {
    value.map(protocol::struct_from_json).transpose()
}

fn value_from_json(value: Option<AgentJson>) -> Option<Value> {
    value.map(protocol::value_from_json)
}

fn timestamp_from_time(value: Option<SystemTime>) -> Option<Timestamp> {
    value.map(protocol::timestamp_from_system_time)
}

pub(crate) fn agent_tool_ref_from_proto(value: pb::AgentToolRef) -> AgentToolRef {
    AgentToolRef {
        app: value.app,
        operation: value.operation,
        connection: value.connection,
        instance: value.instance,
        title: value.title,
        description: value.description,
        credential_mode: value.credential_mode,
        system: value.system,
        run_as: agent_run_as_context_from_proto(value.run_as),
    }
}

fn agent_run_as_context_from_proto(value: Option<pb::SubjectContext>) -> Option<Subject> {
    value.map(|value| Subject {
        id: value.id,
        email: value.email,
        display_name: value.display_name,
    })
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
        created_by_subject_id: value.created_by_subject_id.clone().unwrap_or_default(),
        created_at: timestamp_from_time(value.created_at),
        updated_at: timestamp_from_time(value.updated_at),
        last_turn_at: timestamp_from_time(value.last_turn_at),
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
        output: agent_turn_output_to_proto(value.output)?,
        status_message: value.status_message,
        created_by_subject_id: value.created_by_subject_id.clone().unwrap_or_default(),
        created_at: timestamp_from_time(value.created_at),
        started_at: timestamp_from_time(value.started_at),
        completed_at: timestamp_from_time(value.completed_at),
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

fn agent_turn_output_to_proto(
    value: Option<AgentTurnOutput>,
) -> ProviderResult<Option<pb::agent_turn::Output>> {
    match value {
        Some(AgentTurnOutput::Text(output)) => Ok(Some(pb::agent_turn::Output::Text(
            pb::AgentTurnTextOutput { text: output.text },
        ))),
        Some(AgentTurnOutput::Structured(output)) => Ok(Some(pb::agent_turn::Output::Structured(
            pb::AgentTurnStructuredOutput {
                text: output.text,
                value: struct_from_json(output.value)?,
            },
        ))),
        None => Ok(None),
    }
}

fn agent_output_from_proto(value: Option<pb::AgentOutput>) -> ProviderResult<Option<AgentOutput>> {
    match value.and_then(|output| output.kind) {
        Some(pb::agent_output::Kind::Text(_)) => Ok(Some(AgentOutput::Text(AgentTextOutput {}))),
        Some(pb::agent_output::Kind::Structured(output)) => {
            let schema = json_from_struct(output.schema)
                .ok_or_else(|| crate::Error::bad_request("output.structured.schema is required"))?;
            Ok(Some(AgentOutput::Structured(AgentStructuredOutput {
                schema,
            })))
        }
        None => Ok(None),
    }
}

fn required_agent_output_from_proto(value: Option<pb::AgentOutput>) -> ProviderResult<AgentOutput> {
    agent_output_from_proto(value)?
        .ok_or_else(|| crate::Error::bad_request("create turn output is required"))
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

fn capabilities_to_proto(value: AgentProviderCapabilities) -> pb::AgentProviderCapabilities {
    pb::AgentProviderCapabilities {
        streaming_text: value.streaming_text,
        tool_calls: value.tool_calls,
        parallel_tool_calls: value.parallel_tool_calls,
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
        idempotency_key: value.idempotency_key,
        model: value.model,
        client_ref: value.client_ref,
        metadata: json_from_struct(value.metadata),
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
        tools: value.tools.map(agent_tool_config_from_proto),
    }
}

fn create_turn_request_from_proto(
    value: pb::CreateAgentProviderTurnRequest,
) -> ProviderResult<CreateAgentProviderTurnRequest> {
    if value.timeout_seconds < 0 {
        return Err(crate::Error::bad_request(
            "agent create turn timeout_seconds must not be negative",
        ));
    }
    Ok(CreateAgentProviderTurnRequest {
        provider_name: value.provider_name,
        turn_id: value.turn_id,
        session_id: value.session_id,
        idempotency_key: value.idempotency_key,
        model: value.model,
        messages: value.messages.into_iter().map(message_from_proto).collect(),
        output: required_agent_output_from_proto(value.output)?,
        metadata: json_from_struct(value.metadata),
        execution_ref: value.execution_ref,
        model_options: json_from_struct(value.model_options),
        timeout_seconds: value.timeout_seconds,
        context: value.context,
    })
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

fn agent_tool_config_from_proto(value: pb::AgentToolConfig) -> AgentToolConfig {
    let source = match value.source {
        Some(pb::agent_tool_config::Source::None(_)) => {
            Some(AgentToolConfigSource::None(AgentNoTools {}))
        }
        Some(pb::agent_tool_config::Source::Catalog(catalog)) => {
            Some(AgentToolConfigSource::Catalog(AgentCatalogToolConfig {
                refs: catalog
                    .refs
                    .into_iter()
                    .map(agent_tool_ref_from_proto)
                    .collect(),
                tools: catalog
                    .tools
                    .into_iter()
                    .map(listed_tool_from_proto)
                    .collect(),
            }))
        }
        None => None,
    };
    AgentToolConfig { source }
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
    ///
    /// Mints the session id returned on the [`AgentSession`]. Must be
    /// idempotent on `idempotency_key` scoped per subject
    /// (`context.subject.id`); an empty key always creates.
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
impl<P> pb::agent_server::Agent for AgentServer<P>
where
    P: AgentProvider,
{
    async fn create_session(
        &self,
        request: GrpcRequest<pb::CreateAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentSession>, Status> {
        let request = request.into_inner();
        let context = request.context.clone();
        let session = scope_request_context(
            context,
            self.provider
                .create_session(create_session_request_from_proto(request)),
        )
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
        let request = request.into_inner();
        let context = request.context.clone();
        let session = scope_request_context(
            context,
            self.provider.get_session(GetAgentProviderSessionRequest {
                provider_name: request.provider_name,
                session_id: request.session_id,
            }),
        )
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
        let request = request.into_inner();
        let context = request.context.clone();
        let response = scope_request_context(
            context,
            self.provider
                .list_sessions(ListAgentProviderSessionsRequest {
                    provider_name: request.provider_name,
                    session_ids: request.session_ids,
                    state: AgentSessionState::from_i32_lossy(request.state),
                    limit: request.limit,
                    summary_only: request.summary_only,
                }),
        )
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
        let request = request.into_inner();
        let context = request.context.clone();
        let session = scope_request_context(
            context,
            self.provider
                .update_session(UpdateAgentProviderSessionRequest {
                    provider_name: request.provider_name,
                    session_id: request.session_id,
                    client_ref: request.client_ref,
                    state: AgentSessionState::from_i32_lossy(request.state),
                    metadata: json_from_struct(request.metadata),
                }),
        )
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
        let request = request.into_inner();
        let context = request.context.clone();
        let turn = scope_request_context(
            context,
            self.provider.create_turn(
                create_turn_request_from_proto(request)
                    .map_err(|error| rpc_status("agent create turn", error))?,
            ),
        )
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
        let request = request.into_inner();
        let context = request.context.clone();
        let turn = scope_request_context(
            context,
            self.provider.get_turn(GetAgentProviderTurnRequest {
                provider_name: request.provider_name,
                turn_id: request.turn_id,
            }),
        )
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
        let request = request.into_inner();
        let context = request.context.clone();
        let response = scope_request_context(
            context,
            self.provider.list_turns(ListAgentProviderTurnsRequest {
                provider_name: request.provider_name,
                session_id: request.session_id,
                turn_ids: request.turn_ids,
                status: AgentExecutionStatus::from_i32_lossy(request.status),
                limit: request.limit,
                summary_only: request.summary_only,
            }),
        )
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
        let request = request.into_inner();
        let context = request.context.clone();
        let turn = scope_request_context(
            context,
            self.provider.cancel_turn(CancelAgentProviderTurnRequest {
                provider_name: request.provider_name,
                turn_id: request.turn_id,
                reason: request.reason,
            }),
        )
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
        let request = request.into_inner();
        let context = request.context.clone();
        let response = scope_request_context(
            context,
            self.provider
                .list_turn_events(ListAgentProviderTurnEventsRequest {
                    provider_name: request.provider_name,
                    turn_id: request.turn_id,
                    after_seq: request.after_seq,
                    limit: request.limit,
                }),
        )
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
        let request = request.into_inner();
        let context = request.context.clone();
        let interaction = scope_request_context(
            context,
            self.provider
                .get_interaction(GetAgentProviderInteractionRequest {
                    interaction_id: request.interaction_id,
                }),
        )
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
        let request = request.into_inner();
        let context = request.context.clone();
        let response = scope_request_context(
            context,
            self.provider
                .list_interactions(ListAgentProviderInteractionsRequest {
                    provider_name: request.provider_name,
                    turn_id: request.turn_id,
                }),
        )
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
        let request = request.into_inner();
        let context = request.context.clone();
        let interaction = scope_request_context(
            context,
            self.provider
                .resolve_interaction(ResolveAgentProviderInteractionRequest {
                    provider_name: request.provider_name,
                    interaction_id: request.interaction_id,
                    resolution: json_from_struct(request.resolution),
                }),
        )
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
