use std::collections::BTreeMap;
use std::sync::Arc;
use std::time::SystemTime;

use hyper_util::rt::TokioIo;
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
use crate::error::Error;
use crate::error::Result as ProviderResult;
use crate::generated::v1::{
    self as pb, workflow_host_client::WorkflowHostClient as ProtoWorkflowHostClient,
};
use crate::protocol;
use crate::rpc_status::rpc_status;

type WorkflowHostTransport = InterceptedService<Channel, WorkflowHostRelayTokenInterceptor>;

/// Environment variable containing the workflow-host service target.
pub const ENV_WORKFLOW_HOST_SOCKET: &str = "GESTALT_WORKFLOW_HOST_SOCKET";
/// Environment variable containing the optional workflow-host relay token.
pub const ENV_WORKFLOW_HOST_SOCKET_TOKEN: &str = "GESTALT_WORKFLOW_HOST_SOCKET_TOKEN";
const WORKFLOW_HOST_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

/// Native input for a bound plugin workflow target.
#[derive(Clone, Debug, Default)]
pub struct BoundWorkflowPluginTargetInput {
    pub plugin_name: String,
    pub operation: String,
    pub input: Option<serde_json::Value>,
    pub connection: String,
    pub instance: String,
    pub credential_mode: String,
}

impl BoundWorkflowPluginTargetInput {
    /// Sets the target input from any JSON-object-like serializable value.
    pub fn with_input<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.input = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }
}

/// Native input for a workflow output value source.
#[derive(Clone, Debug, Default)]
pub enum WorkflowOutputValueSourceInput {
    #[default]
    Empty,
    AgentOutput(String),
    SignalPayload(String),
    SignalMetadata(String),
    Literal(serde_json::Value),
    AgentSession(String),
}

impl WorkflowOutputValueSourceInput {
    /// Creates a literal value source from any JSON-compatible serializable value.
    pub fn literal<T: Serialize>(value: T) -> ProviderResult<Self> {
        Ok(Self::Literal(protocol::json_from_serializable(value)?))
    }
}

/// Native input for one workflow output binding.
#[derive(Clone, Debug, Default)]
pub struct WorkflowOutputBindingInput {
    pub input_field: String,
    pub value: Option<WorkflowOutputValueSourceInput>,
}

/// Native input for a workflow output delivery.
#[derive(Clone, Debug, Default)]
pub struct WorkflowOutputDeliveryInput {
    pub target: Option<BoundWorkflowPluginTargetInput>,
    pub input_bindings: Vec<WorkflowOutputBindingInput>,
    pub credential_mode: String,
}

/// Native input for an agent tool-call message part.
#[derive(Clone, Debug, Default)]
pub struct AgentMessagePartToolCallInput {
    pub id: String,
    pub tool_id: String,
    pub arguments: Option<serde_json::Value>,
}

/// Native input for an agent tool-result message part.
#[derive(Clone, Debug, Default)]
pub struct AgentMessagePartToolResultInput {
    pub tool_call_id: String,
    pub status: i32,
    pub content: String,
    pub output: Option<serde_json::Value>,
}

/// Native input for an agent image-reference message part.
#[derive(Clone, Debug, Default)]
pub struct AgentMessagePartImageRefInput {
    pub uri: String,
    pub mime_type: String,
}

/// Native input for one agent message part.
#[derive(Clone, Debug)]
pub struct AgentMessagePartInput {
    pub part_type: pb::AgentMessagePartType,
    pub text: String,
    pub json: Option<serde_json::Value>,
    pub tool_call: Option<AgentMessagePartToolCallInput>,
    pub tool_result: Option<AgentMessagePartToolResultInput>,
    pub image_ref: Option<AgentMessagePartImageRefInput>,
}

impl Default for AgentMessagePartInput {
    fn default() -> Self {
        Self {
            part_type: pb::AgentMessagePartType::Unspecified,
            text: String::new(),
            json: None,
            tool_call: None,
            tool_result: None,
            image_ref: None,
        }
    }
}

/// Native input for one agent message.
#[derive(Clone, Debug, Default)]
pub struct AgentMessageInput {
    pub role: String,
    pub text: String,
    pub parts: Vec<AgentMessagePartInput>,
    pub metadata: Option<serde_json::Value>,
}

impl AgentMessageInput {
    /// Sets metadata from any JSON-object-like serializable value.
    pub fn with_metadata<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.metadata = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }
}

/// Native input for one agent tool reference.
#[derive(Clone, Debug, Default)]
pub struct AgentToolRefInput {
    pub plugin: String,
    pub operation: String,
    pub connection: String,
    pub instance: String,
    pub title: String,
    pub description: String,
    pub system: String,
}

/// Native input for a bound agent workflow target.
#[derive(Clone, Debug, Default)]
pub struct BoundWorkflowAgentTargetInput {
    pub provider_name: String,
    pub model: String,
    pub prompt: String,
    pub messages: Vec<AgentMessageInput>,
    pub tool_refs: Vec<AgentToolRefInput>,
    pub response_schema: Option<serde_json::Value>,
    pub metadata: Option<serde_json::Value>,
    pub timeout_seconds: i32,
    pub output_delivery: Option<WorkflowOutputDeliveryInput>,
    pub model_options: Option<serde_json::Value>,
    pub session_ready_delivery: Option<WorkflowOutputDeliveryInput>,
}

impl BoundWorkflowAgentTargetInput {
    /// Sets the response schema from any JSON-object-like serializable value.
    pub fn with_response_schema<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.response_schema = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }

    /// Sets metadata from any JSON-object-like serializable value.
    pub fn with_metadata<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.metadata = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }

    /// Sets model options from any JSON-object-like serializable value.
    pub fn with_model_options<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.model_options = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }
}

/// Native input for a bound workflow target.
#[derive(Clone, Debug, Default)]
#[allow(clippy::large_enum_variant)]
pub enum BoundWorkflowTargetInput {
    #[default]
    Empty,
    Plugin(BoundWorkflowPluginTargetInput),
    Agent(BoundWorkflowAgentTargetInput),
}

/// Native input for workflow actor metadata.
#[derive(Clone, Debug, Default)]
pub struct WorkflowActorInput {
    pub subject_id: String,
    pub subject_kind: String,
    pub display_name: String,
    pub auth_source: String,
}

/// Native input for workflow run-as metadata.
#[derive(Clone, Debug, Default)]
pub struct WorkflowRunAsSubjectInput {
    pub subject_id: String,
    pub subject_kind: String,
    pub display_name: String,
    pub auth_source: String,
}

/// Native input for an execution-reference permission.
#[derive(Clone, Debug, Default)]
pub struct WorkflowAccessPermissionInput {
    pub plugin: String,
    pub operations: Vec<String>,
}

/// Native input for a workflow event.
#[derive(Clone, Debug, Default)]
pub struct WorkflowEventInput {
    pub id: String,
    pub source: String,
    pub spec_version: String,
    pub event_type: String,
    pub subject: String,
    pub time: Option<SystemTime>,
    pub datacontenttype: String,
    pub data: Option<serde_json::Value>,
    pub extensions: BTreeMap<String, serde_json::Value>,
}

impl WorkflowEventInput {
    /// Sets event data from any JSON-object-like serializable value.
    pub fn with_data<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.data = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }

    /// Adds an extension value from any JSON-compatible serializable value.
    pub fn with_extension<T: Serialize>(
        mut self,
        key: impl Into<String>,
        value: T,
    ) -> ProviderResult<Self> {
        self.extensions
            .insert(key.into(), protocol::json_from_serializable(value)?);
        Ok(self)
    }
}

/// Native input for workflow event matching fields.
#[derive(Clone, Debug, Default)]
pub struct WorkflowEventMatchInput {
    pub event_type: String,
    pub source: String,
    pub subject: String,
}

/// Native input for a workflow signal.
#[derive(Clone, Debug, Default)]
pub struct WorkflowSignalInput {
    pub id: String,
    pub name: String,
    pub payload: Option<serde_json::Value>,
    pub metadata: Option<serde_json::Value>,
    pub created_by: Option<WorkflowActorInput>,
    pub created_at: Option<SystemTime>,
    pub idempotency_key: String,
    pub sequence: i64,
}

impl WorkflowSignalInput {
    /// Sets the signal payload from any JSON-object-like serializable value.
    pub fn with_payload<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.payload = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }

    /// Sets signal metadata from any JSON-object-like serializable value.
    pub fn with_metadata<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.metadata = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }
}

/// Native input for a schedule-triggered workflow run.
#[derive(Clone, Debug, Default)]
pub struct WorkflowScheduleTriggerInput {
    pub schedule_id: String,
    pub scheduled_for: Option<SystemTime>,
}

/// Native input for an event-triggered workflow run.
#[derive(Clone, Debug, Default)]
pub struct WorkflowEventTriggerInvocationInput {
    pub trigger_id: String,
    pub event: Option<WorkflowEventInput>,
}

/// Native input for a workflow run trigger.
#[derive(Clone, Debug, Default)]
pub enum WorkflowRunTriggerInput {
    #[default]
    Empty,
    Manual,
    Schedule(WorkflowScheduleTriggerInput),
    Event(WorkflowEventTriggerInvocationInput),
}

/// Native input for a workflow-provider run.
#[derive(Clone, Debug)]
pub struct BoundWorkflowRunInput {
    pub id: String,
    pub status: pb::WorkflowRunStatus,
    pub target: Option<BoundWorkflowTargetInput>,
    pub trigger: Option<WorkflowRunTriggerInput>,
    pub created_at: Option<SystemTime>,
    pub started_at: Option<SystemTime>,
    pub completed_at: Option<SystemTime>,
    pub status_message: String,
    pub result_body: String,
    pub created_by: Option<WorkflowActorInput>,
    pub execution_ref: String,
    pub workflow_key: String,
}

impl Default for BoundWorkflowRunInput {
    fn default() -> Self {
        Self {
            id: String::new(),
            status: pb::WorkflowRunStatus::Unspecified,
            target: None,
            trigger: None,
            created_at: None,
            started_at: None,
            completed_at: None,
            status_message: String::new(),
            result_body: String::new(),
            created_by: None,
            execution_ref: String::new(),
            workflow_key: String::new(),
        }
    }
}

/// Native input for a workflow-provider schedule.
#[derive(Clone, Debug, Default)]
pub struct BoundWorkflowScheduleInput {
    pub id: String,
    pub cron: String,
    pub timezone: String,
    pub target: Option<BoundWorkflowTargetInput>,
    pub paused: bool,
    pub created_at: Option<SystemTime>,
    pub updated_at: Option<SystemTime>,
    pub next_run_at: Option<SystemTime>,
    pub created_by: Option<WorkflowActorInput>,
    pub execution_ref: String,
}

/// Native input for a workflow-provider event trigger.
#[derive(Clone, Debug, Default)]
pub struct BoundWorkflowEventTriggerInput {
    pub id: String,
    pub event_match: Option<WorkflowEventMatchInput>,
    pub target: Option<BoundWorkflowTargetInput>,
    pub paused: bool,
    pub created_at: Option<SystemTime>,
    pub updated_at: Option<SystemTime>,
    pub created_by: Option<WorkflowActorInput>,
    pub execution_ref: String,
}

/// Native input for a workflow execution reference.
#[derive(Clone, Debug, Default)]
pub struct WorkflowExecutionReferenceInput {
    pub id: String,
    pub provider_name: String,
    pub target: Option<BoundWorkflowTargetInput>,
    pub subject_id: String,
    pub credential_subject_id: String,
    pub permissions: Vec<WorkflowAccessPermissionInput>,
    pub created_at: Option<SystemTime>,
    pub revoked_at: Option<SystemTime>,
    pub subject_kind: String,
    pub display_name: String,
    pub auth_source: String,
    pub caller_plugin_name: String,
    pub run_as: Option<WorkflowRunAsSubjectInput>,
    pub source_definition_id: String,
}

/// Native input for invoking a workflow operation through the host service.
#[derive(Clone, Debug, Default)]
pub struct InvokeWorkflowOperationInput {
    pub target: Option<BoundWorkflowTargetInput>,
    pub run_id: String,
    pub trigger: Option<WorkflowRunTriggerInput>,
    pub input: Option<serde_json::Value>,
    pub metadata: Option<serde_json::Value>,
    pub created_by: Option<WorkflowActorInput>,
    pub execution_ref: String,
    pub signals: Vec<WorkflowSignalInput>,
}

impl InvokeWorkflowOperationInput {
    /// Sets operation input from any JSON-object-like serializable value.
    pub fn with_input<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.input = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }

    /// Sets workflow invocation metadata from any JSON-object-like serializable value.
    pub fn with_metadata<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.metadata = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }
}

/// Native response returned after invoking a workflow operation.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct InvokeWorkflowOperationResponse {
    pub status: i32,
    pub body: String,
}

/// Creates workflow actor metadata from native input.
pub fn new_workflow_actor(input: WorkflowActorInput) -> pb::WorkflowActor {
    pb::WorkflowActor {
        subject_id: input.subject_id,
        subject_kind: input.subject_kind,
        display_name: input.display_name,
        auth_source: input.auth_source,
    }
}

/// Returns native input copied from workflow actor metadata.
pub fn workflow_actor_input_from_actor(input: &pb::WorkflowActor) -> WorkflowActorInput {
    WorkflowActorInput {
        subject_id: input.subject_id.clone(),
        subject_kind: input.subject_kind.clone(),
        display_name: input.display_name.clone(),
        auth_source: input.auth_source.clone(),
    }
}

/// Creates workflow run-as metadata from native input.
pub fn new_workflow_run_as_subject(input: WorkflowRunAsSubjectInput) -> pb::WorkflowRunAsSubject {
    pb::WorkflowRunAsSubject {
        subject_id: input.subject_id,
        subject_kind: input.subject_kind,
        display_name: input.display_name,
        auth_source: input.auth_source,
    }
}

/// Returns native input copied from workflow run-as metadata.
pub fn workflow_run_as_subject_input_from_subject(
    input: &pb::WorkflowRunAsSubject,
) -> WorkflowRunAsSubjectInput {
    WorkflowRunAsSubjectInput {
        subject_id: input.subject_id.clone(),
        subject_kind: input.subject_kind.clone(),
        display_name: input.display_name.clone(),
        auth_source: input.auth_source.clone(),
    }
}

/// Creates an execution-reference permission from native input.
pub fn new_workflow_access_permission(
    input: WorkflowAccessPermissionInput,
) -> pb::WorkflowAccessPermission {
    pb::WorkflowAccessPermission {
        plugin: input.plugin,
        operations: input.operations,
    }
}

/// Returns native input copied from an execution-reference permission.
pub fn workflow_access_permission_input_from_permission(
    input: &pb::WorkflowAccessPermission,
) -> WorkflowAccessPermissionInput {
    WorkflowAccessPermissionInput {
        plugin: input.plugin.clone(),
        operations: input.operations.clone(),
    }
}

/// Creates workflow event-match fields from native input.
pub fn new_workflow_event_match(input: WorkflowEventMatchInput) -> pb::WorkflowEventMatch {
    pb::WorkflowEventMatch {
        r#type: input.event_type,
        source: input.source,
        subject: input.subject,
    }
}

/// Returns native input copied from workflow event-match fields.
pub fn workflow_event_match_input_from_match(
    input: &pb::WorkflowEventMatch,
) -> WorkflowEventMatchInput {
    WorkflowEventMatchInput {
        event_type: input.r#type.clone(),
        source: input.source.clone(),
        subject: input.subject.clone(),
    }
}

/// Creates a workflow output value source from native input.
pub fn new_workflow_output_value_source(
    input: WorkflowOutputValueSourceInput,
) -> pb::WorkflowOutputValueSource {
    use pb::workflow_output_value_source::Kind;
    let kind = match input {
        WorkflowOutputValueSourceInput::Empty => None,
        WorkflowOutputValueSourceInput::AgentOutput(value) => Some(Kind::AgentOutput(value)),
        WorkflowOutputValueSourceInput::SignalPayload(value) => Some(Kind::SignalPayload(value)),
        WorkflowOutputValueSourceInput::SignalMetadata(value) => Some(Kind::SignalMetadata(value)),
        WorkflowOutputValueSourceInput::Literal(value) => {
            Some(Kind::Literal(protocol::value_from_json(value)))
        }
        WorkflowOutputValueSourceInput::AgentSession(value) => Some(Kind::AgentSession(value)),
    };
    pb::WorkflowOutputValueSource { kind }
}

/// Returns native input copied from a workflow output value source.
pub fn workflow_output_value_source_input_from_source(
    input: &pb::WorkflowOutputValueSource,
) -> WorkflowOutputValueSourceInput {
    use pb::workflow_output_value_source::Kind;
    match &input.kind {
        None => WorkflowOutputValueSourceInput::Empty,
        Some(Kind::AgentOutput(value)) => {
            WorkflowOutputValueSourceInput::AgentOutput(value.clone())
        }
        Some(Kind::SignalPayload(value)) => {
            WorkflowOutputValueSourceInput::SignalPayload(value.clone())
        }
        Some(Kind::SignalMetadata(value)) => {
            WorkflowOutputValueSourceInput::SignalMetadata(value.clone())
        }
        Some(Kind::Literal(value)) => {
            WorkflowOutputValueSourceInput::Literal(protocol::json_from_value(value))
        }
        Some(Kind::AgentSession(value)) => {
            WorkflowOutputValueSourceInput::AgentSession(value.clone())
        }
    }
}

/// Creates a workflow output binding from native input.
pub fn new_workflow_output_binding(input: WorkflowOutputBindingInput) -> pb::WorkflowOutputBinding {
    pb::WorkflowOutputBinding {
        input_field: input.input_field,
        value: input.value.map(new_workflow_output_value_source),
    }
}

/// Returns native input copied from a workflow output binding.
pub fn workflow_output_binding_input_from_binding(
    input: &pb::WorkflowOutputBinding,
) -> WorkflowOutputBindingInput {
    WorkflowOutputBindingInput {
        input_field: input.input_field.clone(),
        value: input
            .value
            .as_ref()
            .map(workflow_output_value_source_input_from_source),
    }
}

/// Creates a workflow output delivery from native input.
pub fn new_workflow_output_delivery(
    input: WorkflowOutputDeliveryInput,
) -> ProviderResult<pb::WorkflowOutputDelivery> {
    Ok(pb::WorkflowOutputDelivery {
        target: input
            .target
            .map(new_bound_workflow_plugin_target)
            .transpose()?,
        input_bindings: input
            .input_bindings
            .into_iter()
            .map(new_workflow_output_binding)
            .collect(),
        credential_mode: input.credential_mode,
    })
}

/// Returns native input copied from a workflow output delivery.
pub fn workflow_output_delivery_input_from_delivery(
    input: &pb::WorkflowOutputDelivery,
) -> ProviderResult<WorkflowOutputDeliveryInput> {
    Ok(WorkflowOutputDeliveryInput {
        target: input
            .target
            .as_ref()
            .map(bound_workflow_plugin_target_input_from_target)
            .transpose()?,
        input_bindings: input
            .input_bindings
            .iter()
            .map(workflow_output_binding_input_from_binding)
            .collect(),
        credential_mode: input.credential_mode.clone(),
    })
}

/// Creates a bound plugin workflow target from native input.
pub fn new_bound_workflow_plugin_target(
    input: BoundWorkflowPluginTargetInput,
) -> ProviderResult<pb::BoundWorkflowPluginTarget> {
    Ok(pb::BoundWorkflowPluginTarget {
        plugin_name: input.plugin_name,
        operation: input.operation,
        input: input.input.map(protocol::struct_from_json).transpose()?,
        connection: input.connection,
        instance: input.instance,
        credential_mode: input.credential_mode,
    })
}

/// Returns native input copied from a bound plugin workflow target.
pub fn bound_workflow_plugin_target_input_from_target(
    input: &pb::BoundWorkflowPluginTarget,
) -> ProviderResult<BoundWorkflowPluginTargetInput> {
    Ok(BoundWorkflowPluginTargetInput {
        plugin_name: input.plugin_name.clone(),
        operation: input.operation.clone(),
        input: input.input.as_ref().map(protocol::json_from_struct),
        connection: input.connection.clone(),
        instance: input.instance.clone(),
        credential_mode: input.credential_mode.clone(),
    })
}

/// Creates an agent message from native input.
pub fn new_agent_message(input: AgentMessageInput) -> ProviderResult<pb::AgentMessage> {
    Ok(pb::AgentMessage {
        role: input.role,
        text: input.text,
        parts: input
            .parts
            .into_iter()
            .map(new_agent_message_part)
            .collect::<ProviderResult<Vec<_>>>()?,
        metadata: input.metadata.map(protocol::struct_from_json).transpose()?,
    })
}

/// Returns native input copied from an agent message.
pub fn agent_message_input_from_message(
    input: &pb::AgentMessage,
) -> ProviderResult<AgentMessageInput> {
    Ok(AgentMessageInput {
        role: input.role.clone(),
        text: input.text.clone(),
        parts: input
            .parts
            .iter()
            .map(agent_message_part_input_from_part)
            .collect::<ProviderResult<Vec<_>>>()?,
        metadata: input.metadata.as_ref().map(protocol::json_from_struct),
    })
}

/// Creates an agent message part from native input.
pub fn new_agent_message_part(
    input: AgentMessagePartInput,
) -> ProviderResult<pb::AgentMessagePart> {
    Ok(pb::AgentMessagePart {
        r#type: input.part_type as i32,
        text: input.text,
        json: input.json.map(protocol::struct_from_json).transpose()?,
        tool_call: input
            .tool_call
            .map(new_agent_message_part_tool_call)
            .transpose()?,
        tool_result: input
            .tool_result
            .map(new_agent_message_part_tool_result)
            .transpose()?,
        image_ref: input.image_ref.map(new_agent_message_part_image_ref),
    })
}

/// Returns native input copied from an agent message part.
pub fn agent_message_part_input_from_part(
    input: &pb::AgentMessagePart,
) -> ProviderResult<AgentMessagePartInput> {
    Ok(AgentMessagePartInput {
        part_type: pb::AgentMessagePartType::try_from(input.r#type)
            .unwrap_or(pb::AgentMessagePartType::Unspecified),
        text: input.text.clone(),
        json: input.json.as_ref().map(protocol::json_from_struct),
        tool_call: input
            .tool_call
            .as_ref()
            .map(agent_message_part_tool_call_input_from_call),
        tool_result: input
            .tool_result
            .as_ref()
            .map(agent_message_part_tool_result_input_from_result),
        image_ref: input
            .image_ref
            .as_ref()
            .map(agent_message_part_image_ref_input_from_ref),
    })
}

fn new_agent_message_part_tool_call(
    input: AgentMessagePartToolCallInput,
) -> ProviderResult<pb::AgentMessagePartToolCall> {
    Ok(pb::AgentMessagePartToolCall {
        id: input.id,
        tool_id: input.tool_id,
        arguments: input
            .arguments
            .map(protocol::struct_from_json)
            .transpose()?,
    })
}

fn agent_message_part_tool_call_input_from_call(
    input: &pb::AgentMessagePartToolCall,
) -> AgentMessagePartToolCallInput {
    AgentMessagePartToolCallInput {
        id: input.id.clone(),
        tool_id: input.tool_id.clone(),
        arguments: input.arguments.as_ref().map(protocol::json_from_struct),
    }
}

fn new_agent_message_part_tool_result(
    input: AgentMessagePartToolResultInput,
) -> ProviderResult<pb::AgentMessagePartToolResult> {
    Ok(pb::AgentMessagePartToolResult {
        tool_call_id: input.tool_call_id,
        status: input.status,
        content: input.content,
        output: input.output.map(protocol::struct_from_json).transpose()?,
    })
}

fn agent_message_part_tool_result_input_from_result(
    input: &pb::AgentMessagePartToolResult,
) -> AgentMessagePartToolResultInput {
    AgentMessagePartToolResultInput {
        tool_call_id: input.tool_call_id.clone(),
        status: input.status,
        content: input.content.clone(),
        output: input.output.as_ref().map(protocol::json_from_struct),
    }
}

fn new_agent_message_part_image_ref(
    input: AgentMessagePartImageRefInput,
) -> pb::AgentMessagePartImageRef {
    pb::AgentMessagePartImageRef {
        uri: input.uri,
        mime_type: input.mime_type,
    }
}

fn agent_message_part_image_ref_input_from_ref(
    input: &pb::AgentMessagePartImageRef,
) -> AgentMessagePartImageRefInput {
    AgentMessagePartImageRefInput {
        uri: input.uri.clone(),
        mime_type: input.mime_type.clone(),
    }
}

/// Creates an agent tool reference from native input.
pub fn new_agent_tool_ref(input: AgentToolRefInput) -> pb::AgentToolRef {
    pb::AgentToolRef {
        plugin: input.plugin,
        operation: input.operation,
        connection: input.connection,
        instance: input.instance,
        title: input.title,
        description: input.description,
        system: input.system,
    }
}

/// Returns native input copied from an agent tool reference.
pub fn agent_tool_ref_input_from_ref(input: &pb::AgentToolRef) -> AgentToolRefInput {
    AgentToolRefInput {
        plugin: input.plugin.clone(),
        operation: input.operation.clone(),
        connection: input.connection.clone(),
        instance: input.instance.clone(),
        title: input.title.clone(),
        description: input.description.clone(),
        system: input.system.clone(),
    }
}

/// Creates a bound agent workflow target from native input.
pub fn new_bound_workflow_agent_target(
    input: BoundWorkflowAgentTargetInput,
) -> ProviderResult<pb::BoundWorkflowAgentTarget> {
    Ok(pb::BoundWorkflowAgentTarget {
        provider_name: input.provider_name,
        model: input.model,
        prompt: input.prompt,
        messages: input
            .messages
            .into_iter()
            .map(new_agent_message)
            .collect::<ProviderResult<Vec<_>>>()?,
        tool_refs: input
            .tool_refs
            .into_iter()
            .map(new_agent_tool_ref)
            .collect(),
        response_schema: input
            .response_schema
            .map(protocol::struct_from_json)
            .transpose()?,
        metadata: input.metadata.map(protocol::struct_from_json).transpose()?,
        timeout_seconds: input.timeout_seconds,
        output_delivery: input
            .output_delivery
            .map(new_workflow_output_delivery)
            .transpose()?,
        model_options: input
            .model_options
            .map(protocol::struct_from_json)
            .transpose()?,
        session_ready_delivery: input
            .session_ready_delivery
            .map(new_workflow_output_delivery)
            .transpose()?,
    })
}

/// Returns native input copied from a bound agent workflow target.
pub fn bound_workflow_agent_target_input_from_target(
    input: &pb::BoundWorkflowAgentTarget,
) -> ProviderResult<BoundWorkflowAgentTargetInput> {
    Ok(BoundWorkflowAgentTargetInput {
        provider_name: input.provider_name.clone(),
        model: input.model.clone(),
        prompt: input.prompt.clone(),
        messages: input
            .messages
            .iter()
            .map(agent_message_input_from_message)
            .collect::<ProviderResult<Vec<_>>>()?,
        tool_refs: input
            .tool_refs
            .iter()
            .map(agent_tool_ref_input_from_ref)
            .collect(),
        response_schema: input
            .response_schema
            .as_ref()
            .map(protocol::json_from_struct),
        metadata: input.metadata.as_ref().map(protocol::json_from_struct),
        timeout_seconds: input.timeout_seconds,
        output_delivery: input
            .output_delivery
            .as_ref()
            .map(workflow_output_delivery_input_from_delivery)
            .transpose()?,
        model_options: input.model_options.as_ref().map(protocol::json_from_struct),
        session_ready_delivery: input
            .session_ready_delivery
            .as_ref()
            .map(workflow_output_delivery_input_from_delivery)
            .transpose()?,
    })
}

/// Creates a bound workflow target from native input.
pub fn new_bound_workflow_target(
    input: BoundWorkflowTargetInput,
) -> ProviderResult<pb::BoundWorkflowTarget> {
    use pb::bound_workflow_target::Kind;
    let kind = match input {
        BoundWorkflowTargetInput::Empty => None,
        BoundWorkflowTargetInput::Plugin(input) => {
            Some(Kind::Plugin(new_bound_workflow_plugin_target(input)?))
        }
        BoundWorkflowTargetInput::Agent(input) => {
            Some(Kind::Agent(new_bound_workflow_agent_target(input)?))
        }
    };
    Ok(pb::BoundWorkflowTarget { kind })
}

/// Returns native input copied from a bound workflow target.
pub fn bound_workflow_target_input_from_target(
    input: &pb::BoundWorkflowTarget,
) -> ProviderResult<BoundWorkflowTargetInput> {
    use pb::bound_workflow_target::Kind;
    match &input.kind {
        None => Ok(BoundWorkflowTargetInput::Empty),
        Some(Kind::Plugin(value)) => Ok(BoundWorkflowTargetInput::Plugin(
            bound_workflow_plugin_target_input_from_target(value)?,
        )),
        Some(Kind::Agent(value)) => Ok(BoundWorkflowTargetInput::Agent(
            bound_workflow_agent_target_input_from_target(value)?,
        )),
    }
}

/// Returns a deep copy of a bound workflow target.
pub fn new_bound_workflow_target_from_target(
    input: &pb::BoundWorkflowTarget,
) -> ProviderResult<pb::BoundWorkflowTarget> {
    new_bound_workflow_target(bound_workflow_target_input_from_target(input)?)
}

/// Creates a workflow event from native input.
pub fn new_workflow_event(input: WorkflowEventInput) -> ProviderResult<pb::WorkflowEvent> {
    Ok(pb::WorkflowEvent {
        id: input.id,
        source: input.source,
        spec_version: input.spec_version,
        r#type: input.event_type,
        subject: input.subject,
        time: input.time.map(protocol::timestamp_from_system_time),
        datacontenttype: input.datacontenttype,
        data: input.data.map(protocol::struct_from_json).transpose()?,
        extensions: input
            .extensions
            .into_iter()
            .map(|(key, value)| (key, protocol::value_from_json(value)))
            .collect(),
    })
}

/// Returns native input copied from a workflow event.
pub fn workflow_event_input_from_event(
    input: &pb::WorkflowEvent,
) -> ProviderResult<WorkflowEventInput> {
    Ok(WorkflowEventInput {
        id: input.id.clone(),
        source: input.source.clone(),
        spec_version: input.spec_version.clone(),
        event_type: input.r#type.clone(),
        subject: input.subject.clone(),
        time: input
            .time
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        datacontenttype: input.datacontenttype.clone(),
        data: input.data.as_ref().map(protocol::json_from_struct),
        extensions: input
            .extensions
            .iter()
            .map(|(key, value)| (key.clone(), protocol::json_from_value(value)))
            .collect(),
    })
}

/// Returns a deep copy of a workflow event.
pub fn new_workflow_event_from_event(
    input: &pb::WorkflowEvent,
) -> ProviderResult<pb::WorkflowEvent> {
    new_workflow_event(workflow_event_input_from_event(input)?)
}

/// Creates a workflow signal from native input.
pub fn new_workflow_signal(input: WorkflowSignalInput) -> ProviderResult<pb::WorkflowSignal> {
    Ok(pb::WorkflowSignal {
        id: input.id,
        name: input.name,
        payload: input.payload.map(protocol::struct_from_json).transpose()?,
        metadata: input.metadata.map(protocol::struct_from_json).transpose()?,
        created_by: input.created_by.map(new_workflow_actor),
        created_at: input.created_at.map(protocol::timestamp_from_system_time),
        idempotency_key: input.idempotency_key,
        sequence: input.sequence,
    })
}

/// Returns native input copied from a workflow signal.
pub fn workflow_signal_input_from_signal(
    input: &pb::WorkflowSignal,
) -> ProviderResult<WorkflowSignalInput> {
    Ok(WorkflowSignalInput {
        id: input.id.clone(),
        name: input.name.clone(),
        payload: input.payload.as_ref().map(protocol::json_from_struct),
        metadata: input.metadata.as_ref().map(protocol::json_from_struct),
        created_by: input
            .created_by
            .as_ref()
            .map(workflow_actor_input_from_actor),
        created_at: input
            .created_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        idempotency_key: input.idempotency_key.clone(),
        sequence: input.sequence,
    })
}

/// Returns a deep copy of a workflow signal.
pub fn new_workflow_signal_from_signal(
    input: &pb::WorkflowSignal,
) -> ProviderResult<pb::WorkflowSignal> {
    new_workflow_signal(workflow_signal_input_from_signal(input)?)
}

/// Creates a workflow schedule trigger from native input.
pub fn new_workflow_schedule_trigger(
    input: WorkflowScheduleTriggerInput,
) -> pb::WorkflowScheduleTrigger {
    pb::WorkflowScheduleTrigger {
        schedule_id: input.schedule_id,
        scheduled_for: input
            .scheduled_for
            .map(protocol::timestamp_from_system_time),
    }
}

/// Creates a workflow event-trigger invocation from native input.
pub fn new_workflow_event_trigger_invocation(
    input: WorkflowEventTriggerInvocationInput,
) -> ProviderResult<pb::WorkflowEventTriggerInvocation> {
    Ok(pb::WorkflowEventTriggerInvocation {
        trigger_id: input.trigger_id,
        event: input.event.map(new_workflow_event).transpose()?,
    })
}

/// Creates a workflow run trigger from native input.
pub fn new_workflow_run_trigger(
    input: WorkflowRunTriggerInput,
) -> ProviderResult<pb::WorkflowRunTrigger> {
    use pb::workflow_run_trigger::Kind;
    let kind = match input {
        WorkflowRunTriggerInput::Empty => None,
        WorkflowRunTriggerInput::Manual => Some(Kind::Manual(pb::WorkflowManualTrigger {})),
        WorkflowRunTriggerInput::Schedule(input) => {
            Some(Kind::Schedule(new_workflow_schedule_trigger(input)))
        }
        WorkflowRunTriggerInput::Event(input) => {
            Some(Kind::Event(new_workflow_event_trigger_invocation(input)?))
        }
    };
    Ok(pb::WorkflowRunTrigger { kind })
}

/// Returns native input copied from a workflow run trigger.
pub fn workflow_run_trigger_input_from_trigger(
    input: &pb::WorkflowRunTrigger,
) -> ProviderResult<WorkflowRunTriggerInput> {
    use pb::workflow_run_trigger::Kind;
    match &input.kind {
        None => Ok(WorkflowRunTriggerInput::Empty),
        Some(Kind::Manual(_)) => Ok(WorkflowRunTriggerInput::Manual),
        Some(Kind::Schedule(value)) => Ok(WorkflowRunTriggerInput::Schedule(
            WorkflowScheduleTriggerInput {
                schedule_id: value.schedule_id.clone(),
                scheduled_for: value
                    .scheduled_for
                    .as_ref()
                    .map(protocol::system_time_from_timestamp)
                    .transpose()?,
            },
        )),
        Some(Kind::Event(value)) => Ok(WorkflowRunTriggerInput::Event(
            WorkflowEventTriggerInvocationInput {
                trigger_id: value.trigger_id.clone(),
                event: value
                    .event
                    .as_ref()
                    .map(workflow_event_input_from_event)
                    .transpose()?,
            },
        )),
    }
}

/// Returns a deep copy of a workflow run trigger.
pub fn new_workflow_run_trigger_from_trigger(
    input: &pb::WorkflowRunTrigger,
) -> ProviderResult<pb::WorkflowRunTrigger> {
    new_workflow_run_trigger(workflow_run_trigger_input_from_trigger(input)?)
}

/// Creates a workflow-provider run from native input.
pub fn new_bound_workflow_run(
    input: BoundWorkflowRunInput,
) -> ProviderResult<pb::BoundWorkflowRun> {
    Ok(pb::BoundWorkflowRun {
        id: input.id,
        status: input.status as i32,
        target: input.target.map(new_bound_workflow_target).transpose()?,
        trigger: input.trigger.map(new_workflow_run_trigger).transpose()?,
        created_at: input.created_at.map(protocol::timestamp_from_system_time),
        started_at: input.started_at.map(protocol::timestamp_from_system_time),
        completed_at: input.completed_at.map(protocol::timestamp_from_system_time),
        status_message: input.status_message,
        result_body: input.result_body,
        created_by: input.created_by.map(new_workflow_actor),
        execution_ref: input.execution_ref,
        workflow_key: input.workflow_key,
    })
}

/// Returns native input copied from a workflow-provider run.
pub fn bound_workflow_run_input_from_run(
    input: &pb::BoundWorkflowRun,
) -> ProviderResult<BoundWorkflowRunInput> {
    Ok(BoundWorkflowRunInput {
        id: input.id.clone(),
        status: pb::WorkflowRunStatus::try_from(input.status).map_err(|_| {
            Error::bad_request(format!("unknown workflow run status {}", input.status))
        })?,
        target: input
            .target
            .as_ref()
            .map(bound_workflow_target_input_from_target)
            .transpose()?,
        trigger: input
            .trigger
            .as_ref()
            .map(workflow_run_trigger_input_from_trigger)
            .transpose()?,
        created_at: input
            .created_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        started_at: input
            .started_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        completed_at: input
            .completed_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        status_message: input.status_message.clone(),
        result_body: input.result_body.clone(),
        created_by: input
            .created_by
            .as_ref()
            .map(workflow_actor_input_from_actor),
        execution_ref: input.execution_ref.clone(),
        workflow_key: input.workflow_key.clone(),
    })
}

/// Returns a deep copy of a workflow-provider run.
pub fn new_bound_workflow_run_from_run(
    input: &pb::BoundWorkflowRun,
) -> ProviderResult<pb::BoundWorkflowRun> {
    new_bound_workflow_run(bound_workflow_run_input_from_run(input)?)
}

/// Creates a workflow-provider schedule from native input.
pub fn new_bound_workflow_schedule(
    input: BoundWorkflowScheduleInput,
) -> ProviderResult<pb::BoundWorkflowSchedule> {
    Ok(pb::BoundWorkflowSchedule {
        id: input.id,
        cron: input.cron,
        timezone: input.timezone,
        target: input.target.map(new_bound_workflow_target).transpose()?,
        paused: input.paused,
        created_at: input.created_at.map(protocol::timestamp_from_system_time),
        updated_at: input.updated_at.map(protocol::timestamp_from_system_time),
        next_run_at: input.next_run_at.map(protocol::timestamp_from_system_time),
        created_by: input.created_by.map(new_workflow_actor),
        execution_ref: input.execution_ref,
    })
}

/// Returns native input copied from a workflow-provider schedule.
pub fn bound_workflow_schedule_input_from_schedule(
    input: &pb::BoundWorkflowSchedule,
) -> ProviderResult<BoundWorkflowScheduleInput> {
    Ok(BoundWorkflowScheduleInput {
        id: input.id.clone(),
        cron: input.cron.clone(),
        timezone: input.timezone.clone(),
        target: input
            .target
            .as_ref()
            .map(bound_workflow_target_input_from_target)
            .transpose()?,
        paused: input.paused,
        created_at: input
            .created_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        updated_at: input
            .updated_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        next_run_at: input
            .next_run_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        created_by: input
            .created_by
            .as_ref()
            .map(workflow_actor_input_from_actor),
        execution_ref: input.execution_ref.clone(),
    })
}

/// Returns a deep copy of a workflow-provider schedule.
pub fn new_bound_workflow_schedule_from_schedule(
    input: &pb::BoundWorkflowSchedule,
) -> ProviderResult<pb::BoundWorkflowSchedule> {
    new_bound_workflow_schedule(bound_workflow_schedule_input_from_schedule(input)?)
}

/// Creates a workflow-provider event trigger from native input.
pub fn new_bound_workflow_event_trigger(
    input: BoundWorkflowEventTriggerInput,
) -> ProviderResult<pb::BoundWorkflowEventTrigger> {
    Ok(pb::BoundWorkflowEventTrigger {
        id: input.id,
        r#match: input.event_match.map(new_workflow_event_match),
        target: input.target.map(new_bound_workflow_target).transpose()?,
        paused: input.paused,
        created_at: input.created_at.map(protocol::timestamp_from_system_time),
        updated_at: input.updated_at.map(protocol::timestamp_from_system_time),
        created_by: input.created_by.map(new_workflow_actor),
        execution_ref: input.execution_ref,
    })
}

/// Returns native input copied from a workflow-provider event trigger.
pub fn bound_workflow_event_trigger_input_from_trigger(
    input: &pb::BoundWorkflowEventTrigger,
) -> ProviderResult<BoundWorkflowEventTriggerInput> {
    Ok(BoundWorkflowEventTriggerInput {
        id: input.id.clone(),
        event_match: input
            .r#match
            .as_ref()
            .map(workflow_event_match_input_from_match),
        target: input
            .target
            .as_ref()
            .map(bound_workflow_target_input_from_target)
            .transpose()?,
        paused: input.paused,
        created_at: input
            .created_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        updated_at: input
            .updated_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        created_by: input
            .created_by
            .as_ref()
            .map(workflow_actor_input_from_actor),
        execution_ref: input.execution_ref.clone(),
    })
}

/// Returns a deep copy of a workflow-provider event trigger.
pub fn new_bound_workflow_event_trigger_from_trigger(
    input: &pb::BoundWorkflowEventTrigger,
) -> ProviderResult<pb::BoundWorkflowEventTrigger> {
    new_bound_workflow_event_trigger(bound_workflow_event_trigger_input_from_trigger(input)?)
}

/// Creates a workflow execution reference from native input.
pub fn new_workflow_execution_reference(
    input: WorkflowExecutionReferenceInput,
) -> ProviderResult<pb::WorkflowExecutionReference> {
    Ok(pb::WorkflowExecutionReference {
        id: input.id,
        provider_name: input.provider_name,
        target: input.target.map(new_bound_workflow_target).transpose()?,
        subject_id: input.subject_id,
        credential_subject_id: input.credential_subject_id,
        permissions: input
            .permissions
            .into_iter()
            .map(new_workflow_access_permission)
            .collect(),
        created_at: input.created_at.map(protocol::timestamp_from_system_time),
        revoked_at: input.revoked_at.map(protocol::timestamp_from_system_time),
        subject_kind: input.subject_kind,
        display_name: input.display_name,
        auth_source: input.auth_source,
        caller_plugin_name: input.caller_plugin_name,
        run_as: input.run_as.map(new_workflow_run_as_subject),
        source_definition_id: input.source_definition_id,
    })
}

/// Returns native input copied from a workflow execution reference.
pub fn workflow_execution_reference_input_from_reference(
    input: &pb::WorkflowExecutionReference,
) -> ProviderResult<WorkflowExecutionReferenceInput> {
    Ok(WorkflowExecutionReferenceInput {
        id: input.id.clone(),
        provider_name: input.provider_name.clone(),
        target: input
            .target
            .as_ref()
            .map(bound_workflow_target_input_from_target)
            .transpose()?,
        subject_id: input.subject_id.clone(),
        credential_subject_id: input.credential_subject_id.clone(),
        permissions: input
            .permissions
            .iter()
            .map(workflow_access_permission_input_from_permission)
            .collect(),
        created_at: input
            .created_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        revoked_at: input
            .revoked_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        subject_kind: input.subject_kind.clone(),
        display_name: input.display_name.clone(),
        auth_source: input.auth_source.clone(),
        caller_plugin_name: input.caller_plugin_name.clone(),
        run_as: input
            .run_as
            .as_ref()
            .map(workflow_run_as_subject_input_from_subject),
        source_definition_id: input.source_definition_id.clone(),
    })
}

/// Returns a deep copy of a workflow execution reference.
pub fn new_workflow_execution_reference_from_reference(
    input: &pb::WorkflowExecutionReference,
) -> ProviderResult<pb::WorkflowExecutionReference> {
    new_workflow_execution_reference(workflow_execution_reference_input_from_reference(input)?)
}

fn invoke_workflow_operation_request_from_input(
    input: InvokeWorkflowOperationInput,
) -> ProviderResult<pb::InvokeWorkflowOperationRequest> {
    Ok(pb::InvokeWorkflowOperationRequest {
        target: input.target.map(new_bound_workflow_target).transpose()?,
        run_id: input.run_id,
        trigger: input.trigger.map(new_workflow_run_trigger).transpose()?,
        input: input.input.map(protocol::struct_from_json).transpose()?,
        metadata: input.metadata.map(protocol::struct_from_json).transpose()?,
        created_by: input.created_by.map(new_workflow_actor),
        execution_ref: input.execution_ref,
        signals: input
            .signals
            .into_iter()
            .map(new_workflow_signal)
            .collect::<ProviderResult<Vec<_>>>()?,
    })
}

fn invoke_workflow_operation_response_from_proto(
    response: pb::InvokeWorkflowOperationResponse,
) -> InvokeWorkflowOperationResponse {
    InvokeWorkflowOperationResponse {
        status: response.status,
        body: response.body,
    }
}

#[derive(Debug, thiserror::Error)]
/// Errors returned by [`WorkflowHost`].
pub enum WorkflowHostError {
    /// The host-service transport could not be created.
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    /// The host-service RPC returned a gRPC status.
    #[error("{0}")]
    Status(#[from] tonic::Status),
    /// The operation input could not be converted into the wire protocol.
    #[error("{0}")]
    Conversion(#[from] Error),
    /// Required environment or target configuration was invalid.
    #[error("{0}")]
    Env(String),
}

/// Client for invoking operations from workflow provider code.
pub struct WorkflowHost {
    client: ProtoWorkflowHostClient<WorkflowHostTransport>,
}

impl WorkflowHost {
    /// Connects to the workflow host service described by the environment.
    pub async fn connect() -> std::result::Result<Self, WorkflowHostError> {
        let target = std::env::var(ENV_WORKFLOW_HOST_SOCKET).map_err(|_| {
            WorkflowHostError::Env(format!("{ENV_WORKFLOW_HOST_SOCKET} is not set"))
        })?;
        let relay_token = std::env::var(ENV_WORKFLOW_HOST_SOCKET_TOKEN).unwrap_or_default();
        let channel = match parse_workflow_host_target(&target)? {
            WorkflowHostTarget::Unix(path) => connect_unix(path).await?,
            WorkflowHostTarget::Tcp(address) => {
                Endpoint::from_shared(format!("http://{address}"))?
                    .connect()
                    .await?
            }
            WorkflowHostTarget::Tls(address) => {
                Endpoint::from_shared(format!("https://{address}"))?
                    .tls_config(ClientTlsConfig::new().with_native_roots())?
                    .connect()
                    .await?
            }
        };
        Ok(Self {
            client: ProtoWorkflowHostClient::with_interceptor(
                channel,
                workflow_host_relay_token_interceptor(relay_token.trim())?,
            ),
        })
    }

    /// Invokes an operation through the workflow host service.
    pub async fn invoke_operation(
        &mut self,
        input: InvokeWorkflowOperationInput,
    ) -> std::result::Result<InvokeWorkflowOperationResponse, WorkflowHostError> {
        let request = invoke_workflow_operation_request_from_input(input)?;
        let response = self.client.invoke_operation(request).await?.into_inner();
        Ok(invoke_workflow_operation_response_from_proto(response))
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
struct WorkflowHostRelayTokenInterceptor {
    token: Option<MetadataValue<tonic::metadata::Ascii>>,
}

impl Interceptor for WorkflowHostRelayTokenInterceptor {
    fn call(
        &mut self,
        mut request: tonic::Request<()>,
    ) -> std::result::Result<tonic::Request<()>, tonic::Status> {
        if let Some(token) = self.token.clone() {
            request
                .metadata_mut()
                .insert(WORKFLOW_HOST_RELAY_TOKEN_HEADER, token);
        }
        Ok(request)
    }
}

fn workflow_host_relay_token_interceptor(
    token: &str,
) -> std::result::Result<WorkflowHostRelayTokenInterceptor, WorkflowHostError> {
    let trimmed = token.trim();
    let token = if trimmed.is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(trimmed).map_err(|err| {
            WorkflowHostError::Env(format!(
                "workflow host: invalid relay token metadata: {err}"
            ))
        })?)
    };
    Ok(WorkflowHostRelayTokenInterceptor { token })
}

enum WorkflowHostTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_workflow_host_target(
    raw: &str,
) -> std::result::Result<WorkflowHostTarget, WorkflowHostError> {
    let target = raw.trim();
    if target.is_empty() {
        return Err(WorkflowHostError::Env(
            "workflow host: transport target is required".to_string(),
        ));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(WorkflowHostError::Env(format!(
                "workflow host: tcp target {raw:?} is missing host:port"
            )));
        }
        return Ok(WorkflowHostTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(WorkflowHostError::Env(format!(
                "workflow host: tls target {raw:?} is missing host:port"
            )));
        }
        return Ok(WorkflowHostTarget::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(WorkflowHostError::Env(format!(
                "workflow host: unix target {raw:?} is missing a socket path"
            )));
        }
        return Ok(WorkflowHostTarget::Unix(path.to_string()));
    }
    if target.contains("://") {
        return Err(WorkflowHostError::Env(format!(
            "workflow host: unsupported target scheme in {raw:?}"
        )));
    }
    Ok(WorkflowHostTarget::Unix(target.to_string()))
}

#[async_trait]
/// Provider trait for serving the Gestalt workflow-provider protocol.
pub trait WorkflowProvider: Send + Sync + 'static {
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

    /// Starts or idempotently returns a workflow run.
    async fn start_run(
        &self,
        _request: pb::StartWorkflowProviderRunRequest,
    ) -> ProviderResult<pb::BoundWorkflowRun> {
        Err(crate::Error::unimplemented(
            "workflow start run is not implemented",
        ))
    }

    /// Returns one workflow run by ID.
    async fn get_run(
        &self,
        _request: pb::GetWorkflowProviderRunRequest,
    ) -> ProviderResult<pb::BoundWorkflowRun> {
        Err(crate::Error::unimplemented(
            "workflow get run is not implemented",
        ))
    }

    /// Lists workflow runs visible to the request subject.
    async fn list_runs(
        &self,
        _request: pb::ListWorkflowProviderRunsRequest,
    ) -> ProviderResult<pb::ListWorkflowProviderRunsResponse> {
        Err(crate::Error::unimplemented(
            "workflow list runs is not implemented",
        ))
    }

    /// Requests cancellation of a pending or running workflow run.
    async fn cancel_run(
        &self,
        _request: pb::CancelWorkflowProviderRunRequest,
    ) -> ProviderResult<pb::BoundWorkflowRun> {
        Err(crate::Error::unimplemented(
            "workflow cancel run is not implemented",
        ))
    }

    /// Delivers a signal to an existing workflow run.
    async fn signal_run(
        &self,
        _request: pb::SignalWorkflowProviderRunRequest,
    ) -> ProviderResult<pb::SignalWorkflowRunResponse> {
        Err(crate::Error::unimplemented(
            "workflow signal run is not implemented",
        ))
    }

    /// Delivers a signal or starts a run when no target run exists.
    async fn signal_or_start_run(
        &self,
        _request: pb::SignalOrStartWorkflowProviderRunRequest,
    ) -> ProviderResult<pb::SignalWorkflowRunResponse> {
        Err(crate::Error::unimplemented(
            "workflow signal or start run is not implemented",
        ))
    }

    /// Creates or updates a workflow schedule.
    async fn upsert_schedule(
        &self,
        _request: pb::UpsertWorkflowProviderScheduleRequest,
    ) -> ProviderResult<pb::BoundWorkflowSchedule> {
        Err(crate::Error::unimplemented(
            "workflow upsert schedule is not implemented",
        ))
    }

    /// Returns one workflow schedule by ID.
    async fn get_schedule(
        &self,
        _request: pb::GetWorkflowProviderScheduleRequest,
    ) -> ProviderResult<pb::BoundWorkflowSchedule> {
        Err(crate::Error::unimplemented(
            "workflow get schedule is not implemented",
        ))
    }

    /// Lists workflow schedules visible to the request subject.
    async fn list_schedules(
        &self,
        _request: pb::ListWorkflowProviderSchedulesRequest,
    ) -> ProviderResult<pb::ListWorkflowProviderSchedulesResponse> {
        Err(crate::Error::unimplemented(
            "workflow list schedules is not implemented",
        ))
    }

    /// Deletes a workflow schedule.
    async fn delete_schedule(
        &self,
        _request: pb::DeleteWorkflowProviderScheduleRequest,
    ) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "workflow delete schedule is not implemented",
        ))
    }

    /// Pauses a workflow schedule without deleting it.
    async fn pause_schedule(
        &self,
        _request: pb::PauseWorkflowProviderScheduleRequest,
    ) -> ProviderResult<pb::BoundWorkflowSchedule> {
        Err(crate::Error::unimplemented(
            "workflow pause schedule is not implemented",
        ))
    }

    /// Resumes a paused workflow schedule.
    async fn resume_schedule(
        &self,
        _request: pb::ResumeWorkflowProviderScheduleRequest,
    ) -> ProviderResult<pb::BoundWorkflowSchedule> {
        Err(crate::Error::unimplemented(
            "workflow resume schedule is not implemented",
        ))
    }

    /// Creates or updates a workflow event trigger.
    async fn upsert_event_trigger(
        &self,
        _request: pb::UpsertWorkflowProviderEventTriggerRequest,
    ) -> ProviderResult<pb::BoundWorkflowEventTrigger> {
        Err(crate::Error::unimplemented(
            "workflow upsert event trigger is not implemented",
        ))
    }

    /// Returns one workflow event trigger by ID.
    async fn get_event_trigger(
        &self,
        _request: pb::GetWorkflowProviderEventTriggerRequest,
    ) -> ProviderResult<pb::BoundWorkflowEventTrigger> {
        Err(crate::Error::unimplemented(
            "workflow get event trigger is not implemented",
        ))
    }

    /// Lists workflow event triggers visible to the request subject.
    async fn list_event_triggers(
        &self,
        _request: pb::ListWorkflowProviderEventTriggersRequest,
    ) -> ProviderResult<pb::ListWorkflowProviderEventTriggersResponse> {
        Err(crate::Error::unimplemented(
            "workflow list event triggers is not implemented",
        ))
    }

    /// Deletes a workflow event trigger.
    async fn delete_event_trigger(
        &self,
        _request: pb::DeleteWorkflowProviderEventTriggerRequest,
    ) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "workflow delete event trigger is not implemented",
        ))
    }

    /// Pauses a workflow event trigger without deleting it.
    async fn pause_event_trigger(
        &self,
        _request: pb::PauseWorkflowProviderEventTriggerRequest,
    ) -> ProviderResult<pb::BoundWorkflowEventTrigger> {
        Err(crate::Error::unimplemented(
            "workflow pause event trigger is not implemented",
        ))
    }

    /// Resumes a paused workflow event trigger.
    async fn resume_event_trigger(
        &self,
        _request: pb::ResumeWorkflowProviderEventTriggerRequest,
    ) -> ProviderResult<pb::BoundWorkflowEventTrigger> {
        Err(crate::Error::unimplemented(
            "workflow resume event trigger is not implemented",
        ))
    }

    /// Publishes a workflow event for trigger matching.
    async fn publish_event(
        &self,
        _request: pb::PublishWorkflowProviderEventRequest,
    ) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "workflow publish event is not implemented",
        ))
    }

    /// Stores or updates a workflow execution reference.
    async fn put_execution_reference(
        &self,
        _request: pb::PutWorkflowExecutionReferenceRequest,
    ) -> ProviderResult<pb::WorkflowExecutionReference> {
        Err(crate::Error::unimplemented(
            "workflow put execution reference is not implemented",
        ))
    }

    /// Returns one workflow execution reference.
    async fn get_execution_reference(
        &self,
        _request: pb::GetWorkflowExecutionReferenceRequest,
    ) -> ProviderResult<pb::WorkflowExecutionReference> {
        Err(crate::Error::unimplemented(
            "workflow get execution reference is not implemented",
        ))
    }

    /// Lists workflow execution references for a scope.
    async fn list_execution_references(
        &self,
        _request: pb::ListWorkflowExecutionReferencesRequest,
    ) -> ProviderResult<pb::ListWorkflowExecutionReferencesResponse> {
        Err(crate::Error::unimplemented(
            "workflow list execution references is not implemented",
        ))
    }
}

#[derive(Clone)]
pub(crate) struct WorkflowServer<P> {
    provider: Arc<P>,
}

impl<P> WorkflowServer<P> {
    pub(crate) fn new(provider: Arc<P>) -> Self {
        Self { provider }
    }
}

#[async_trait]
impl<P> pb::workflow_provider_server::WorkflowProvider for WorkflowServer<P>
where
    P: WorkflowProvider,
{
    async fn start_run(
        &self,
        request: GrpcRequest<pb::StartWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowRun>, Status> {
        let run = self
            .provider
            .start_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow start run", error))?;
        Ok(GrpcResponse::new(run))
    }

    async fn get_run(
        &self,
        request: GrpcRequest<pb::GetWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowRun>, Status> {
        let run = self
            .provider
            .get_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow get run", error))?;
        Ok(GrpcResponse::new(run))
    }

    async fn list_runs(
        &self,
        request: GrpcRequest<pb::ListWorkflowProviderRunsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListWorkflowProviderRunsResponse>, Status> {
        let response = self
            .provider
            .list_runs(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow list runs", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn cancel_run(
        &self,
        request: GrpcRequest<pb::CancelWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowRun>, Status> {
        let run = self
            .provider
            .cancel_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow cancel run", error))?;
        Ok(GrpcResponse::new(run))
    }

    async fn signal_run(
        &self,
        request: GrpcRequest<pb::SignalWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::SignalWorkflowRunResponse>, Status> {
        let response = self
            .provider
            .signal_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow signal run", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn signal_or_start_run(
        &self,
        request: GrpcRequest<pb::SignalOrStartWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::SignalWorkflowRunResponse>, Status> {
        let response = self
            .provider
            .signal_or_start_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow signal or start run", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn upsert_schedule(
        &self,
        request: GrpcRequest<pb::UpsertWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowSchedule>, Status> {
        let schedule = self
            .provider
            .upsert_schedule(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow upsert schedule", error))?;
        Ok(GrpcResponse::new(schedule))
    }

    async fn get_schedule(
        &self,
        request: GrpcRequest<pb::GetWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowSchedule>, Status> {
        let schedule = self
            .provider
            .get_schedule(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow get schedule", error))?;
        Ok(GrpcResponse::new(schedule))
    }

    async fn list_schedules(
        &self,
        request: GrpcRequest<pb::ListWorkflowProviderSchedulesRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListWorkflowProviderSchedulesResponse>, Status> {
        let response = self
            .provider
            .list_schedules(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow list schedules", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn delete_schedule(
        &self,
        request: GrpcRequest<pb::DeleteWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .delete_schedule(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow delete schedule", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn pause_schedule(
        &self,
        request: GrpcRequest<pb::PauseWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowSchedule>, Status> {
        let schedule = self
            .provider
            .pause_schedule(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow pause schedule", error))?;
        Ok(GrpcResponse::new(schedule))
    }

    async fn resume_schedule(
        &self,
        request: GrpcRequest<pb::ResumeWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowSchedule>, Status> {
        let schedule = self
            .provider
            .resume_schedule(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow resume schedule", error))?;
        Ok(GrpcResponse::new(schedule))
    }

    async fn upsert_event_trigger(
        &self,
        request: GrpcRequest<pb::UpsertWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowEventTrigger>, Status> {
        let trigger = self
            .provider
            .upsert_event_trigger(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow upsert event trigger", error))?;
        Ok(GrpcResponse::new(trigger))
    }

    async fn get_event_trigger(
        &self,
        request: GrpcRequest<pb::GetWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowEventTrigger>, Status> {
        let trigger = self
            .provider
            .get_event_trigger(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow get event trigger", error))?;
        Ok(GrpcResponse::new(trigger))
    }

    async fn list_event_triggers(
        &self,
        request: GrpcRequest<pb::ListWorkflowProviderEventTriggersRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListWorkflowProviderEventTriggersResponse>, Status>
    {
        let response = self
            .provider
            .list_event_triggers(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow list event triggers", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn delete_event_trigger(
        &self,
        request: GrpcRequest<pb::DeleteWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .delete_event_trigger(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow delete event trigger", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn pause_event_trigger(
        &self,
        request: GrpcRequest<pb::PauseWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowEventTrigger>, Status> {
        let trigger = self
            .provider
            .pause_event_trigger(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow pause event trigger", error))?;
        Ok(GrpcResponse::new(trigger))
    }

    async fn resume_event_trigger(
        &self,
        request: GrpcRequest<pb::ResumeWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowEventTrigger>, Status> {
        let trigger = self
            .provider
            .resume_event_trigger(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow resume event trigger", error))?;
        Ok(GrpcResponse::new(trigger))
    }

    async fn publish_event(
        &self,
        request: GrpcRequest<pb::PublishWorkflowProviderEventRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .publish_event(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow publish event", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn put_execution_reference(
        &self,
        request: GrpcRequest<pb::PutWorkflowExecutionReferenceRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowExecutionReference>, Status> {
        let reference = self
            .provider
            .put_execution_reference(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow put execution reference", error))?;
        Ok(GrpcResponse::new(reference))
    }

    async fn get_execution_reference(
        &self,
        request: GrpcRequest<pb::GetWorkflowExecutionReferenceRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowExecutionReference>, Status> {
        let reference = self
            .provider
            .get_execution_reference(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow get execution reference", error))?;
        Ok(GrpcResponse::new(reference))
    }

    async fn list_execution_references(
        &self,
        request: GrpcRequest<pb::ListWorkflowExecutionReferencesRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListWorkflowExecutionReferencesResponse>, Status>
    {
        let response = self
            .provider
            .list_execution_references(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow list execution references", error))?;
        Ok(GrpcResponse::new(response))
    }
}
