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

use crate::agent::{
    AgentToolRef, agent_tool_ref_from_proto, agent_tool_ref_to_proto, new_agent_tool_ref,
};
use crate::api::RuntimeMetadata;
use crate::env::{ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN};
use crate::error::Error;
use crate::error::Result as ProviderResult;
use crate::generated::v1::{
    self as pb, workflow_host_client::WorkflowHostClient as ProtoWorkflowHostClient,
};
use crate::protocol;
use crate::rpc_status::rpc_status;

type WorkflowHostTransport = InterceptedService<Channel, WorkflowHostRelayTokenInterceptor>;

const WORKFLOW_HOST_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

/// Native JSON object used by authored workflow providers.
pub type WorkflowJson = serde_json::Value;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
#[repr(i32)]
pub enum WorkflowRunStatus {
    #[default]
    Unspecified = 0,
    Pending = 1,
    Running = 2,
    Succeeded = 3,
    Failed = 4,
    Canceled = 5,
}

impl WorkflowRunStatus {
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
            _ => Self::Unspecified,
        }
    }
}

impl TryFrom<i32> for WorkflowRunStatus {
    type Error = crate::Error;

    fn try_from(value: i32) -> ProviderResult<Self> {
        match value {
            0 => Ok(Self::Unspecified),
            1 => Ok(Self::Pending),
            2 => Ok(Self::Running),
            3 => Ok(Self::Succeeded),
            4 => Ok(Self::Failed),
            5 => Ok(Self::Canceled),
            _ => Err(crate::Error::bad_request(format!(
                "unknown workflow run status {value}"
            ))),
        }
    }
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct BoundWorkflowTarget {
    pub steps: Vec<WorkflowStep>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct WorkflowStep {
    pub id: String,
    pub inputs: BTreeMap<String, WorkflowValue>,
    pub action: WorkflowStepAction,
    pub when: Option<WorkflowStepWhen>,
    pub timeout_seconds: i32,
    pub metadata: Option<WorkflowJson>,
}

#[derive(Clone, Debug, Default, PartialEq)]
#[allow(clippy::large_enum_variant)]
pub enum WorkflowStepAction {
    #[default]
    Empty,
    App(WorkflowStepAppCall),
    Agent(WorkflowStepAgentTurn),
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct WorkflowStepAppCall {
    pub name: String,
    pub operation: String,
    pub input: Option<WorkflowValue>,
    pub connection: String,
    pub instance: String,
    pub credential_mode: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct WorkflowStepAgentTurn {
    pub provider: String,
    pub model: String,
    pub session_key: String,
    pub prompt: Option<WorkflowText>,
    pub messages: Vec<WorkflowAgentMessage>,
    pub tools: Vec<AgentToolRef>,
    pub response_schema: Option<WorkflowJson>,
    pub model_options: Option<WorkflowJson>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct WorkflowAgentMessage {
    pub role: String,
    pub text: Option<WorkflowText>,
    pub metadata: Option<WorkflowJson>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct WorkflowText {
    pub template: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct WorkflowStepWhen {
    pub value: Option<WorkflowValue>,
    pub equals: Option<WorkflowJson>,
}

#[derive(Clone, Debug, Default, PartialEq)]
#[allow(clippy::large_enum_variant)]
pub enum WorkflowValue {
    #[default]
    Empty,
    Literal(WorkflowJson),
    Object(BTreeMap<String, WorkflowValue>),
    Array(Vec<WorkflowValue>),
    Template(WorkflowText),
    RunInput(String),
    SignalPayload(String),
    StepOutput(WorkflowStepOutputSource),
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct WorkflowStepOutputSource {
    pub step_id: String,
    pub path: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct WorkflowActor {
    pub subject_id: String,
    pub subject_kind: String,
    pub display_name: String,
    pub auth_source: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct WorkflowRunAsSubject {
    pub subject_id: String,
    pub subject_kind: String,
    pub display_name: String,
    pub auth_source: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct WorkflowAccessPermission {
    pub app: String,
    pub operations: Vec<String>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct WorkflowEvent {
    pub id: String,
    pub source: String,
    pub spec_version: String,
    pub event_type: String,
    pub subject: String,
    pub time: Option<SystemTime>,
    pub datacontenttype: String,
    pub data: Option<WorkflowJson>,
    pub extensions: BTreeMap<String, WorkflowJson>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct WorkflowEventMatch {
    pub event_type: String,
    pub source: String,
    pub subject: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct WorkflowSignal {
    pub id: String,
    pub name: String,
    pub payload: Option<WorkflowJson>,
    pub metadata: Option<WorkflowJson>,
    pub created_by: Option<WorkflowActor>,
    pub created_at: Option<SystemTime>,
    pub idempotency_key: String,
    pub sequence: i64,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct WorkflowScheduleTrigger {
    pub schedule_id: String,
    pub scheduled_for: Option<SystemTime>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct WorkflowEventTriggerInvocation {
    pub trigger_id: String,
    pub event: Option<WorkflowEvent>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub enum WorkflowRunTrigger {
    #[default]
    Empty,
    Manual,
    Schedule(WorkflowScheduleTrigger),
    Event(WorkflowEventTriggerInvocation),
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct BoundWorkflowRun {
    pub id: String,
    pub status: WorkflowRunStatus,
    pub target: Option<BoundWorkflowTarget>,
    pub trigger: Option<WorkflowRunTrigger>,
    pub created_at: Option<SystemTime>,
    pub started_at: Option<SystemTime>,
    pub completed_at: Option<SystemTime>,
    pub status_message: String,
    pub result_body: String,
    pub created_by: Option<WorkflowActor>,
    pub execution_ref: String,
    pub workflow_key: String,
    pub provider_name: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct BoundWorkflowSchedule {
    pub id: String,
    pub cron: String,
    pub timezone: String,
    pub target: Option<BoundWorkflowTarget>,
    pub paused: bool,
    pub created_at: Option<SystemTime>,
    pub updated_at: Option<SystemTime>,
    pub next_run_at: Option<SystemTime>,
    pub created_by: Option<WorkflowActor>,
    pub execution_ref: String,
    pub provider_name: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct BoundWorkflowEventTrigger {
    pub id: String,
    pub event_match: Option<WorkflowEventMatch>,
    pub target: Option<BoundWorkflowTarget>,
    pub paused: bool,
    pub created_at: Option<SystemTime>,
    pub updated_at: Option<SystemTime>,
    pub created_by: Option<WorkflowActor>,
    pub execution_ref: String,
    pub provider_name: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct BoundWorkflowDefinition {
    pub id: String,
    pub target: Option<BoundWorkflowTarget>,
    pub created_by: Option<WorkflowActor>,
    pub created_at: Option<SystemTime>,
    pub provider_name: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct WorkflowExecutionReference {
    pub id: String,
    pub provider_name: String,
    pub target: Option<BoundWorkflowTarget>,
    pub subject_id: String,
    pub credential_subject_id: String,
    pub permissions: Vec<WorkflowAccessPermission>,
    pub created_at: Option<SystemTime>,
    pub revoked_at: Option<SystemTime>,
    pub subject_kind: String,
    pub display_name: String,
    pub auth_source: String,
    pub caller_app_name: String,
    pub run_as: Option<WorkflowRunAsSubject>,
    pub source_definition_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct SignalWorkflowRunResponse {
    pub run: Option<BoundWorkflowRun>,
    pub signal: Option<WorkflowSignal>,
    pub started_run: bool,
    pub workflow_key: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct CreateWorkflowProviderDefinitionRequest {
    pub provider_name: String,
    pub target: Option<BoundWorkflowTarget>,
    pub idempotency_key: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct GetWorkflowProviderDefinitionRequest {
    pub definition_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct UpdateWorkflowProviderDefinitionRequest {
    pub definition_id: String,
    pub provider_name: String,
    pub target: Option<BoundWorkflowTarget>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct DeleteWorkflowProviderDefinitionRequest {
    pub definition_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct ListWorkflowProviderRunsResponse {
    pub runs: Vec<BoundWorkflowRun>,
    pub next_page_token: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct ListWorkflowProviderSchedulesResponse {
    pub schedules: Vec<BoundWorkflowSchedule>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct ListWorkflowProviderEventTriggersResponse {
    pub triggers: Vec<BoundWorkflowEventTrigger>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct ListWorkflowExecutionReferencesResponse {
    pub references: Vec<WorkflowExecutionReference>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct WorkflowManagerSchedule {
    pub provider_name: String,
    pub schedule: Option<BoundWorkflowSchedule>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct WorkflowManagerEventTrigger {
    pub provider_name: String,
    pub trigger: Option<BoundWorkflowEventTrigger>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct WorkflowManagerDefinition {
    pub provider_name: String,
    pub definition: Option<BoundWorkflowDefinition>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct WorkflowManagerRun {
    pub provider_name: String,
    pub run: Option<BoundWorkflowRun>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct WorkflowManagerRunSignal {
    pub provider_name: String,
    pub run: Option<BoundWorkflowRun>,
    pub signal: Option<WorkflowSignal>,
    pub started_run: bool,
    pub workflow_key: String,
}

impl WorkflowStepAppCall {
    /// Sets the target input from any JSON-object-like serializable value.
    pub fn with_input<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.input = Some(WorkflowValue::Literal(protocol::json_from_serializable(
            value,
        )?));
        Ok(self)
    }
}

impl WorkflowValue {
    /// Creates a literal value source from any JSON-compatible serializable value.
    pub fn literal<T: Serialize>(value: T) -> ProviderResult<Self> {
        Ok(Self::Literal(protocol::json_from_serializable(value)?))
    }
}

impl WorkflowStepAgentTurn {
    /// Sets the response schema from any JSON-object-like serializable value.
    pub fn with_response_schema<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.response_schema = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }

    /// Sets model options from any JSON-object-like serializable value.
    pub fn with_model_options<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.model_options = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }
}

impl WorkflowStep {
    /// Sets step metadata from any JSON-object-like serializable value.
    pub fn with_metadata<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.metadata = Some(protocol::json_from_serializable(value)?);
        Ok(self)
    }
}

impl WorkflowEvent {
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

impl WorkflowSignal {
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

#[derive(Clone, Debug, Default, PartialEq)]
pub struct StartWorkflowProviderRunRequest {
    pub target: Option<BoundWorkflowTarget>,
    pub idempotency_key: String,
    pub created_by: Option<WorkflowActor>,
    pub execution_ref: String,
    pub workflow_key: String,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct GetWorkflowProviderRunRequest {
    pub run_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ListWorkflowProviderRunsRequest {
    pub page_size: i32,
    pub page_token: String,
    pub status: WorkflowRunStatus,
    pub target_app: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct CancelWorkflowProviderRunRequest {
    pub run_id: String,
    pub reason: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct SignalWorkflowProviderRunRequest {
    pub run_id: String,
    pub signal: Option<WorkflowSignal>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct SignalOrStartWorkflowProviderRunRequest {
    pub workflow_key: String,
    pub target: Option<BoundWorkflowTarget>,
    pub idempotency_key: String,
    pub created_by: Option<WorkflowActor>,
    pub execution_ref: String,
    pub signal: Option<WorkflowSignal>,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct UpsertWorkflowProviderScheduleRequest {
    pub schedule_id: String,
    pub cron: String,
    pub timezone: String,
    pub target: Option<BoundWorkflowTarget>,
    pub paused: bool,
    pub requested_by: Option<WorkflowActor>,
    pub execution_ref: String,
    pub idempotency_key: String,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct GetWorkflowProviderScheduleRequest {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ListWorkflowProviderSchedulesRequest {}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct DeleteWorkflowProviderScheduleRequest {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct PauseWorkflowProviderScheduleRequest {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ResumeWorkflowProviderScheduleRequest {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct UpsertWorkflowProviderEventTriggerRequest {
    pub trigger_id: String,
    pub event_match: Option<WorkflowEventMatch>,
    pub target: Option<BoundWorkflowTarget>,
    pub paused: bool,
    pub requested_by: Option<WorkflowActor>,
    pub execution_ref: String,
    pub idempotency_key: String,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct GetWorkflowProviderEventTriggerRequest {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ListWorkflowProviderEventTriggersRequest {}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct DeleteWorkflowProviderEventTriggerRequest {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct PauseWorkflowProviderEventTriggerRequest {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ResumeWorkflowProviderEventTriggerRequest {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct PublishWorkflowProviderEventRequest {
    pub app_name: String,
    pub event: Option<WorkflowEvent>,
    pub published_by: Option<WorkflowActor>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct PutWorkflowExecutionReferenceRequest {
    pub reference: Option<WorkflowExecutionReference>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct GetWorkflowExecutionReferenceRequest {
    pub id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ListWorkflowExecutionReferencesRequest {
    pub subject_id: String,
}

/// Input for invoking a workflow operation through the host service.
#[derive(Clone, Debug, Default)]
pub struct InvokeWorkflowOperationInput {
    pub target: Option<BoundWorkflowTarget>,
    pub run_id: String,
    pub trigger: Option<WorkflowRunTrigger>,
    pub input: Option<serde_json::Value>,
    pub metadata: Option<serde_json::Value>,
    pub created_by: Option<WorkflowActor>,
    pub execution_ref: String,
    pub signals: Vec<WorkflowSignal>,
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

/// Creates workflow actor metadata.
pub fn new_workflow_actor(input: WorkflowActor) -> WorkflowActor {
    WorkflowActor {
        subject_id: input.subject_id,
        subject_kind: input.subject_kind,
        display_name: input.display_name,
        auth_source: input.auth_source,
    }
}

/// Returns input copied from workflow actor metadata.
pub fn workflow_actor_input_from_actor(input: &WorkflowActor) -> WorkflowActor {
    WorkflowActor {
        subject_id: input.subject_id.clone(),
        subject_kind: input.subject_kind.clone(),
        display_name: input.display_name.clone(),
        auth_source: input.auth_source.clone(),
    }
}

fn workflow_actor_to_proto(input: WorkflowActor) -> pb::WorkflowActor {
    pb::WorkflowActor {
        subject_id: input.subject_id,
        subject_kind: input.subject_kind,
        display_name: input.display_name,
        auth_source: input.auth_source,
    }
}

fn workflow_actor_from_proto(input: pb::WorkflowActor) -> WorkflowActor {
    WorkflowActor {
        subject_id: input.subject_id,
        subject_kind: input.subject_kind,
        display_name: input.display_name,
        auth_source: input.auth_source,
    }
}

/// Creates workflow run-as metadata.
pub fn new_workflow_run_as_subject(input: WorkflowRunAsSubject) -> WorkflowRunAsSubject {
    WorkflowRunAsSubject {
        subject_id: input.subject_id,
        subject_kind: input.subject_kind,
        display_name: input.display_name,
        auth_source: input.auth_source,
    }
}

/// Returns input copied from workflow run-as metadata.
pub fn workflow_run_as_subject_input_from_subject(
    input: &WorkflowRunAsSubject,
) -> WorkflowRunAsSubject {
    WorkflowRunAsSubject {
        subject_id: input.subject_id.clone(),
        subject_kind: input.subject_kind.clone(),
        display_name: input.display_name.clone(),
        auth_source: input.auth_source.clone(),
    }
}

fn workflow_run_as_subject_to_proto(input: WorkflowRunAsSubject) -> pb::WorkflowRunAsSubject {
    pb::WorkflowRunAsSubject {
        subject_id: input.subject_id,
        subject_kind: input.subject_kind,
        display_name: input.display_name,
        auth_source: input.auth_source,
    }
}

fn workflow_run_as_subject_from_proto(input: pb::WorkflowRunAsSubject) -> WorkflowRunAsSubject {
    WorkflowRunAsSubject {
        subject_id: input.subject_id,
        subject_kind: input.subject_kind,
        display_name: input.display_name,
        auth_source: input.auth_source,
    }
}

/// Creates an execution-reference permission.
pub fn new_workflow_access_permission(input: WorkflowAccessPermission) -> WorkflowAccessPermission {
    WorkflowAccessPermission {
        app: input.app,
        operations: input.operations,
    }
}

/// Returns input copied from an execution-reference permission.
pub fn workflow_access_permission_input_from_permission(
    input: &WorkflowAccessPermission,
) -> WorkflowAccessPermission {
    WorkflowAccessPermission {
        app: input.app.clone(),
        operations: input.operations.clone(),
    }
}

fn workflow_access_permission_to_proto(
    input: WorkflowAccessPermission,
) -> pb::WorkflowAccessPermission {
    pb::WorkflowAccessPermission {
        app: input.app,
        operations: input.operations,
    }
}

fn workflow_access_permission_from_proto(
    input: pb::WorkflowAccessPermission,
) -> WorkflowAccessPermission {
    WorkflowAccessPermission {
        app: input.app,
        operations: input.operations,
    }
}

/// Creates workflow event-match fields.
pub fn new_workflow_event_match(input: WorkflowEventMatch) -> WorkflowEventMatch {
    WorkflowEventMatch {
        event_type: input.event_type,
        source: input.source,
        subject: input.subject,
    }
}

/// Returns input copied from workflow event-match fields.
pub fn workflow_event_match_input_from_match(input: &WorkflowEventMatch) -> WorkflowEventMatch {
    WorkflowEventMatch {
        event_type: input.event_type.clone(),
        source: input.source.clone(),
        subject: input.subject.clone(),
    }
}

pub(crate) fn workflow_event_match_to_proto(input: WorkflowEventMatch) -> pb::WorkflowEventMatch {
    pb::WorkflowEventMatch {
        r#type: input.event_type,
        source: input.source,
        subject: input.subject,
    }
}

fn workflow_event_match_from_proto(input: pb::WorkflowEventMatch) -> WorkflowEventMatch {
    WorkflowEventMatch {
        event_type: input.r#type,
        source: input.source,
        subject: input.subject,
    }
}

/// Creates a workflow value.
pub fn new_workflow_value(input: WorkflowValue) -> WorkflowValue {
    input
}

/// Returns input copied from a workflow value.
pub fn workflow_value_input_from_value(input: &WorkflowValue) -> WorkflowValue {
    input.clone()
}

fn workflow_value_to_proto(input: WorkflowValue) -> pb::WorkflowValue {
    use pb::workflow_value::Kind;
    let kind = match input {
        WorkflowValue::Empty => None,
        WorkflowValue::Literal(value) => Some(Kind::Literal(protocol::value_from_json(value))),
        WorkflowValue::Object(fields) => Some(Kind::Object(pb::WorkflowObject {
            fields: fields
                .into_iter()
                .map(|(key, value)| (key, workflow_value_to_proto(value)))
                .collect(),
        })),
        WorkflowValue::Array(values) => Some(Kind::Array(pb::WorkflowArray {
            values: values.into_iter().map(workflow_value_to_proto).collect(),
        })),
        WorkflowValue::Template(value) => Some(Kind::Template(workflow_text_to_proto(value))),
        WorkflowValue::RunInput(path) => Some(Kind::RunInput(pb::WorkflowPathSource { path })),
        WorkflowValue::SignalPayload(path) => {
            Some(Kind::SignalPayload(pb::WorkflowPathSource { path }))
        }
        WorkflowValue::StepOutput(value) => Some(Kind::StepOutput(
            workflow_step_output_source_to_proto(value),
        )),
    };
    pb::WorkflowValue { kind }
}

fn workflow_value_from_proto(input: pb::WorkflowValue) -> WorkflowValue {
    use pb::workflow_value::Kind;
    match input.kind {
        None => WorkflowValue::Empty,
        Some(Kind::Literal(value)) => WorkflowValue::Literal(protocol::json_from_value(&value)),
        Some(Kind::Object(value)) => WorkflowValue::Object(
            value
                .fields
                .into_iter()
                .map(|(key, value)| (key, workflow_value_from_proto(value)))
                .collect(),
        ),
        Some(Kind::Array(value)) => WorkflowValue::Array(
            value
                .values
                .into_iter()
                .map(workflow_value_from_proto)
                .collect(),
        ),
        Some(Kind::Template(value)) => WorkflowValue::Template(workflow_text_from_proto(value)),
        Some(Kind::RunInput(value)) => WorkflowValue::RunInput(value.path),
        Some(Kind::SignalPayload(value)) => WorkflowValue::SignalPayload(value.path),
        Some(Kind::StepOutput(value)) => {
            WorkflowValue::StepOutput(workflow_step_output_source_from_proto(value))
        }
    }
}

/// Creates workflow text.
pub fn new_workflow_text(input: WorkflowText) -> WorkflowText {
    input
}

fn workflow_text_to_proto(input: WorkflowText) -> pb::WorkflowText {
    pb::WorkflowText {
        template: input.template,
    }
}

fn workflow_text_from_proto(input: pb::WorkflowText) -> WorkflowText {
    WorkflowText {
        template: input.template,
    }
}

fn workflow_step_output_source_to_proto(
    input: WorkflowStepOutputSource,
) -> pb::WorkflowStepOutputSource {
    pb::WorkflowStepOutputSource {
        step_id: input.step_id,
        path: input.path,
    }
}

fn workflow_step_output_source_from_proto(
    input: pb::WorkflowStepOutputSource,
) -> WorkflowStepOutputSource {
    WorkflowStepOutputSource {
        step_id: input.step_id,
        path: input.path,
    }
}

/// Creates a workflow step app call.
pub fn new_workflow_step_app_call(
    input: WorkflowStepAppCall,
) -> ProviderResult<WorkflowStepAppCall> {
    Ok(WorkflowStepAppCall {
        name: input.name,
        operation: input.operation,
        input: input.input.map(new_workflow_value),
        connection: input.connection,
        instance: input.instance,
        credential_mode: input.credential_mode,
    })
}

/// Returns input copied from a workflow step app call.
pub fn workflow_step_app_call_input_from_call(
    input: &WorkflowStepAppCall,
) -> ProviderResult<WorkflowStepAppCall> {
    Ok(input.clone())
}

fn workflow_step_app_call_to_proto(
    input: WorkflowStepAppCall,
) -> ProviderResult<pb::WorkflowStepAppCall> {
    Ok(pb::WorkflowStepAppCall {
        name: input.name,
        operation: input.operation,
        input: input.input.map(workflow_value_to_proto),
        connection: input.connection,
        instance: input.instance,
        credential_mode: input.credential_mode,
    })
}

fn workflow_step_app_call_from_proto(
    input: pb::WorkflowStepAppCall,
) -> ProviderResult<WorkflowStepAppCall> {
    Ok(WorkflowStepAppCall {
        name: input.name,
        operation: input.operation,
        input: input.input.map(workflow_value_from_proto),
        connection: input.connection,
        instance: input.instance,
        credential_mode: input.credential_mode,
    })
}

/// Creates a workflow agent message.
pub fn new_workflow_agent_message(input: WorkflowAgentMessage) -> WorkflowAgentMessage {
    input
}

fn workflow_agent_message_to_proto(
    input: WorkflowAgentMessage,
) -> ProviderResult<pb::WorkflowAgentMessage> {
    Ok(pb::WorkflowAgentMessage {
        role: input.role,
        text: input.text.map(workflow_text_to_proto),
        metadata: input.metadata.map(protocol::struct_from_json).transpose()?,
    })
}

fn workflow_agent_message_from_proto(input: pb::WorkflowAgentMessage) -> WorkflowAgentMessage {
    WorkflowAgentMessage {
        role: input.role,
        text: input.text.map(workflow_text_from_proto),
        metadata: input.metadata.as_ref().map(protocol::json_from_struct),
    }
}

/// Creates a workflow step agent turn.
pub fn new_workflow_step_agent_turn(
    input: WorkflowStepAgentTurn,
) -> ProviderResult<WorkflowStepAgentTurn> {
    Ok(WorkflowStepAgentTurn {
        provider: input.provider,
        model: input.model,
        session_key: input.session_key,
        prompt: input.prompt,
        messages: input
            .messages
            .into_iter()
            .map(new_workflow_agent_message)
            .collect(),
        tools: input.tools.into_iter().map(new_agent_tool_ref).collect(),
        response_schema: input.response_schema,
        model_options: input.model_options,
    })
}

/// Returns input copied from a workflow step agent turn.
pub fn workflow_step_agent_turn_input_from_turn(
    input: &WorkflowStepAgentTurn,
) -> ProviderResult<WorkflowStepAgentTurn> {
    Ok(input.clone())
}

fn workflow_step_agent_turn_to_proto(
    input: WorkflowStepAgentTurn,
) -> ProviderResult<pb::WorkflowStepAgentTurn> {
    Ok(pb::WorkflowStepAgentTurn {
        provider: input.provider,
        model: input.model,
        session_key: input.session_key,
        prompt: input.prompt.map(workflow_text_to_proto),
        messages: input
            .messages
            .into_iter()
            .map(workflow_agent_message_to_proto)
            .collect::<ProviderResult<Vec<_>>>()?,
        tools: input
            .tools
            .into_iter()
            .map(agent_tool_ref_to_proto)
            .collect(),
        response_schema: input
            .response_schema
            .map(protocol::struct_from_json)
            .transpose()?,
        model_options: input
            .model_options
            .map(protocol::struct_from_json)
            .transpose()?,
    })
}

fn workflow_step_agent_turn_from_proto(input: pb::WorkflowStepAgentTurn) -> WorkflowStepAgentTurn {
    WorkflowStepAgentTurn {
        provider: input.provider,
        model: input.model,
        session_key: input.session_key,
        prompt: input.prompt.map(workflow_text_from_proto),
        messages: input
            .messages
            .into_iter()
            .map(workflow_agent_message_from_proto)
            .collect(),
        tools: input
            .tools
            .into_iter()
            .map(agent_tool_ref_from_proto)
            .collect(),
        response_schema: input
            .response_schema
            .as_ref()
            .map(protocol::json_from_struct),
        model_options: input.model_options.as_ref().map(protocol::json_from_struct),
    }
}

/// Creates a workflow step condition.
pub fn new_workflow_step_when(input: WorkflowStepWhen) -> WorkflowStepWhen {
    input
}

fn workflow_step_when_to_proto(input: WorkflowStepWhen) -> pb::WorkflowStepWhen {
    pb::WorkflowStepWhen {
        value: input.value.map(workflow_value_to_proto),
        equals: input.equals.map(protocol::value_from_json),
    }
}

fn workflow_step_when_from_proto(input: pb::WorkflowStepWhen) -> WorkflowStepWhen {
    WorkflowStepWhen {
        value: input.value.map(workflow_value_from_proto),
        equals: input.equals.as_ref().map(protocol::json_from_value),
    }
}

/// Creates a workflow step.
pub fn new_workflow_step(input: WorkflowStep) -> ProviderResult<WorkflowStep> {
    Ok(WorkflowStep {
        id: input.id,
        inputs: input
            .inputs
            .into_iter()
            .map(|(key, value)| (key, new_workflow_value(value)))
            .collect(),
        action: match input.action {
            WorkflowStepAction::Empty => WorkflowStepAction::Empty,
            WorkflowStepAction::App(value) => {
                WorkflowStepAction::App(new_workflow_step_app_call(value)?)
            }
            WorkflowStepAction::Agent(value) => {
                WorkflowStepAction::Agent(new_workflow_step_agent_turn(value)?)
            }
        },
        when: input.when.map(new_workflow_step_when),
        timeout_seconds: input.timeout_seconds,
        metadata: input.metadata,
    })
}

/// Returns input copied from a workflow step.
pub fn workflow_step_input_from_step(input: &WorkflowStep) -> ProviderResult<WorkflowStep> {
    Ok(input.clone())
}

fn workflow_step_to_proto(input: WorkflowStep) -> ProviderResult<pb::WorkflowStep> {
    use pb::workflow_step::Action;
    let action = match input.action {
        WorkflowStepAction::Empty => None,
        WorkflowStepAction::App(value) => {
            Some(Action::App(workflow_step_app_call_to_proto(value)?))
        }
        WorkflowStepAction::Agent(value) => {
            Some(Action::Agent(workflow_step_agent_turn_to_proto(value)?))
        }
    };
    Ok(pb::WorkflowStep {
        id: input.id,
        inputs: input
            .inputs
            .into_iter()
            .map(|(key, value)| (key, workflow_value_to_proto(value)))
            .collect(),
        action,
        when: input.when.map(workflow_step_when_to_proto),
        timeout_seconds: input.timeout_seconds,
        metadata: input.metadata.map(protocol::struct_from_json).transpose()?,
    })
}

fn workflow_step_from_proto(input: pb::WorkflowStep) -> ProviderResult<WorkflowStep> {
    use pb::workflow_step::Action;
    let action = match input.action {
        None => WorkflowStepAction::Empty,
        Some(Action::App(value)) => {
            WorkflowStepAction::App(workflow_step_app_call_from_proto(value)?)
        }
        Some(Action::Agent(value)) => {
            WorkflowStepAction::Agent(workflow_step_agent_turn_from_proto(value))
        }
    };
    Ok(WorkflowStep {
        id: input.id,
        inputs: input
            .inputs
            .into_iter()
            .map(|(key, value)| (key, workflow_value_from_proto(value)))
            .collect(),
        action,
        when: input.when.map(workflow_step_when_from_proto),
        timeout_seconds: input.timeout_seconds,
        metadata: input.metadata.as_ref().map(protocol::json_from_struct),
    })
}

/// Creates a bound workflow target.
pub fn new_bound_workflow_target(
    input: BoundWorkflowTarget,
) -> ProviderResult<BoundWorkflowTarget> {
    Ok(BoundWorkflowTarget {
        steps: input
            .steps
            .into_iter()
            .map(new_workflow_step)
            .collect::<ProviderResult<Vec<_>>>()?,
    })
}

/// Returns input copied from a bound workflow target.
pub fn bound_workflow_target_input_from_target(
    input: &BoundWorkflowTarget,
) -> ProviderResult<BoundWorkflowTarget> {
    Ok(input.clone())
}

pub(crate) fn bound_workflow_target_to_proto(
    input: BoundWorkflowTarget,
) -> ProviderResult<pb::BoundWorkflowTarget> {
    Ok(pb::BoundWorkflowTarget {
        steps: input
            .steps
            .into_iter()
            .map(workflow_step_to_proto)
            .collect::<ProviderResult<Vec<_>>>()?,
    })
}

fn bound_workflow_target_from_proto(
    input: pb::BoundWorkflowTarget,
) -> ProviderResult<BoundWorkflowTarget> {
    Ok(BoundWorkflowTarget {
        steps: input
            .steps
            .into_iter()
            .map(workflow_step_from_proto)
            .collect::<ProviderResult<Vec<_>>>()?,
    })
}

/// Returns a deep copy of a bound workflow target.
pub fn new_bound_workflow_target_from_target(
    input: &BoundWorkflowTarget,
) -> ProviderResult<BoundWorkflowTarget> {
    Ok(input.clone())
}

/// Creates a workflow event.
pub fn new_workflow_event(input: WorkflowEvent) -> ProviderResult<WorkflowEvent> {
    Ok(WorkflowEvent {
        id: input.id,
        source: input.source,
        spec_version: input.spec_version,
        event_type: input.event_type,
        subject: input.subject,
        time: input.time,
        datacontenttype: input.datacontenttype,
        data: input.data,
        extensions: input.extensions,
    })
}

/// Returns input copied from a workflow event.
pub fn workflow_event_input_from_event(input: &WorkflowEvent) -> ProviderResult<WorkflowEvent> {
    Ok(WorkflowEvent {
        id: input.id.clone(),
        source: input.source.clone(),
        spec_version: input.spec_version.clone(),
        event_type: input.event_type.clone(),
        subject: input.subject.clone(),
        time: input.time,
        datacontenttype: input.datacontenttype.clone(),
        data: input.data.clone(),
        extensions: input.extensions.clone(),
    })
}

pub(crate) fn workflow_event_to_proto(input: WorkflowEvent) -> ProviderResult<pb::WorkflowEvent> {
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

pub(crate) fn workflow_event_from_proto(input: pb::WorkflowEvent) -> ProviderResult<WorkflowEvent> {
    Ok(WorkflowEvent {
        id: input.id,
        source: input.source,
        spec_version: input.spec_version,
        event_type: input.r#type,
        subject: input.subject,
        time: input
            .time
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        datacontenttype: input.datacontenttype,
        data: input.data.as_ref().map(protocol::json_from_struct),
        extensions: input
            .extensions
            .iter()
            .map(|(key, value)| (key.clone(), protocol::json_from_value(value)))
            .collect(),
    })
}

/// Returns a deep copy of a workflow event.
pub fn new_workflow_event_from_event(input: &WorkflowEvent) -> ProviderResult<WorkflowEvent> {
    Ok(input.clone())
}

/// Creates a workflow signal.
pub fn new_workflow_signal(input: WorkflowSignal) -> ProviderResult<WorkflowSignal> {
    Ok(WorkflowSignal {
        id: input.id,
        name: input.name,
        payload: input.payload,
        metadata: input.metadata,
        created_by: input.created_by.map(new_workflow_actor),
        created_at: input.created_at,
        idempotency_key: input.idempotency_key,
        sequence: input.sequence,
    })
}

/// Returns input copied from a workflow signal.
pub fn workflow_signal_input_from_signal(input: &WorkflowSignal) -> ProviderResult<WorkflowSignal> {
    Ok(WorkflowSignal {
        id: input.id.clone(),
        name: input.name.clone(),
        payload: input.payload.clone(),
        metadata: input.metadata.clone(),
        created_by: input
            .created_by
            .as_ref()
            .map(workflow_actor_input_from_actor),
        created_at: input.created_at,
        idempotency_key: input.idempotency_key.clone(),
        sequence: input.sequence,
    })
}

pub(crate) fn workflow_signal_to_proto(
    input: WorkflowSignal,
) -> ProviderResult<pb::WorkflowSignal> {
    Ok(pb::WorkflowSignal {
        id: input.id,
        name: input.name,
        payload: input.payload.map(protocol::struct_from_json).transpose()?,
        metadata: input.metadata.map(protocol::struct_from_json).transpose()?,
        created_by: input.created_by.map(workflow_actor_to_proto),
        created_at: input.created_at.map(protocol::timestamp_from_system_time),
        idempotency_key: input.idempotency_key,
        sequence: input.sequence,
    })
}

pub(crate) fn workflow_signal_from_proto(
    input: pb::WorkflowSignal,
) -> ProviderResult<WorkflowSignal> {
    Ok(WorkflowSignal {
        id: input.id,
        name: input.name,
        payload: input.payload.as_ref().map(protocol::json_from_struct),
        metadata: input.metadata.as_ref().map(protocol::json_from_struct),
        created_by: input.created_by.map(workflow_actor_from_proto),
        created_at: input
            .created_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        idempotency_key: input.idempotency_key,
        sequence: input.sequence,
    })
}

/// Returns a deep copy of a workflow signal.
pub fn new_workflow_signal_from_signal(input: &WorkflowSignal) -> ProviderResult<WorkflowSignal> {
    Ok(input.clone())
}

/// Creates a workflow schedule trigger.
pub fn new_workflow_schedule_trigger(input: WorkflowScheduleTrigger) -> WorkflowScheduleTrigger {
    WorkflowScheduleTrigger {
        schedule_id: input.schedule_id,
        scheduled_for: input.scheduled_for,
    }
}

fn workflow_schedule_trigger_to_proto(
    input: WorkflowScheduleTrigger,
) -> pb::WorkflowScheduleTrigger {
    pb::WorkflowScheduleTrigger {
        schedule_id: input.schedule_id,
        scheduled_for: input
            .scheduled_for
            .map(protocol::timestamp_from_system_time),
    }
}

fn workflow_schedule_trigger_from_proto(
    input: pb::WorkflowScheduleTrigger,
) -> ProviderResult<WorkflowScheduleTrigger> {
    Ok(WorkflowScheduleTrigger {
        schedule_id: input.schedule_id,
        scheduled_for: input
            .scheduled_for
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
    })
}

/// Creates a workflow event-trigger invocation.
pub fn new_workflow_event_trigger_invocation(
    input: WorkflowEventTriggerInvocation,
) -> ProviderResult<WorkflowEventTriggerInvocation> {
    Ok(WorkflowEventTriggerInvocation {
        trigger_id: input.trigger_id,
        event: input.event.map(new_workflow_event).transpose()?,
    })
}

fn workflow_event_trigger_invocation_to_proto(
    input: WorkflowEventTriggerInvocation,
) -> ProviderResult<pb::WorkflowEventTriggerInvocation> {
    Ok(pb::WorkflowEventTriggerInvocation {
        trigger_id: input.trigger_id,
        event: input.event.map(workflow_event_to_proto).transpose()?,
    })
}

fn workflow_event_trigger_invocation_from_proto(
    input: pb::WorkflowEventTriggerInvocation,
) -> ProviderResult<WorkflowEventTriggerInvocation> {
    Ok(WorkflowEventTriggerInvocation {
        trigger_id: input.trigger_id,
        event: input.event.map(workflow_event_from_proto).transpose()?,
    })
}

/// Creates a workflow run trigger.
pub fn new_workflow_run_trigger(input: WorkflowRunTrigger) -> ProviderResult<WorkflowRunTrigger> {
    match input {
        WorkflowRunTrigger::Empty => Ok(WorkflowRunTrigger::Empty),
        WorkflowRunTrigger::Manual => Ok(WorkflowRunTrigger::Manual),
        WorkflowRunTrigger::Schedule(input) => Ok(WorkflowRunTrigger::Schedule(
            new_workflow_schedule_trigger(input),
        )),
        WorkflowRunTrigger::Event(input) => Ok(WorkflowRunTrigger::Event(
            new_workflow_event_trigger_invocation(input)?,
        )),
    }
}

/// Returns input copied from a workflow run trigger.
pub fn workflow_run_trigger_input_from_trigger(
    input: &WorkflowRunTrigger,
) -> ProviderResult<WorkflowRunTrigger> {
    match input {
        WorkflowRunTrigger::Empty => Ok(WorkflowRunTrigger::Empty),
        WorkflowRunTrigger::Manual => Ok(WorkflowRunTrigger::Manual),
        WorkflowRunTrigger::Schedule(value) => {
            Ok(WorkflowRunTrigger::Schedule(WorkflowScheduleTrigger {
                schedule_id: value.schedule_id.clone(),
                scheduled_for: value.scheduled_for,
            }))
        }
        WorkflowRunTrigger::Event(value) => {
            Ok(WorkflowRunTrigger::Event(WorkflowEventTriggerInvocation {
                trigger_id: value.trigger_id.clone(),
                event: value
                    .event
                    .as_ref()
                    .map(workflow_event_input_from_event)
                    .transpose()?,
            }))
        }
    }
}

fn workflow_run_trigger_to_proto(
    input: WorkflowRunTrigger,
) -> ProviderResult<pb::WorkflowRunTrigger> {
    use pb::workflow_run_trigger::Kind;
    let kind = match input {
        WorkflowRunTrigger::Empty => None,
        WorkflowRunTrigger::Manual => Some(Kind::Manual(pb::WorkflowManualTrigger {})),
        WorkflowRunTrigger::Schedule(input) => {
            Some(Kind::Schedule(workflow_schedule_trigger_to_proto(input)))
        }
        WorkflowRunTrigger::Event(input) => Some(Kind::Event(
            workflow_event_trigger_invocation_to_proto(input)?,
        )),
    };
    Ok(pb::WorkflowRunTrigger { kind })
}

fn workflow_run_trigger_from_proto(
    input: pb::WorkflowRunTrigger,
) -> ProviderResult<WorkflowRunTrigger> {
    use pb::workflow_run_trigger::Kind;
    match input.kind {
        None => Ok(WorkflowRunTrigger::Empty),
        Some(Kind::Manual(_)) => Ok(WorkflowRunTrigger::Manual),
        Some(Kind::Schedule(value)) => Ok(WorkflowRunTrigger::Schedule(
            workflow_schedule_trigger_from_proto(value)?,
        )),
        Some(Kind::Event(value)) => Ok(WorkflowRunTrigger::Event(
            workflow_event_trigger_invocation_from_proto(value)?,
        )),
    }
}

/// Returns a deep copy of a workflow run trigger.
pub fn new_workflow_run_trigger_from_trigger(
    input: &WorkflowRunTrigger,
) -> ProviderResult<WorkflowRunTrigger> {
    Ok(input.clone())
}

/// Creates a workflow-provider run.
pub fn new_bound_workflow_run(input: BoundWorkflowRun) -> ProviderResult<BoundWorkflowRun> {
    Ok(BoundWorkflowRun {
        id: input.id,
        status: input.status,
        target: input.target.map(new_bound_workflow_target).transpose()?,
        trigger: input.trigger.map(new_workflow_run_trigger).transpose()?,
        created_at: input.created_at,
        started_at: input.started_at,
        completed_at: input.completed_at,
        status_message: input.status_message,
        result_body: input.result_body,
        created_by: input.created_by.map(new_workflow_actor),
        execution_ref: input.execution_ref,
        workflow_key: input.workflow_key,
        provider_name: input.provider_name,
    })
}

/// Returns input copied from a workflow-provider run.
pub fn bound_workflow_run_input_from_run(
    input: &BoundWorkflowRun,
) -> ProviderResult<BoundWorkflowRun> {
    Ok(BoundWorkflowRun {
        id: input.id.clone(),
        status: input.status,
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
        created_at: input.created_at,
        started_at: input.started_at,
        completed_at: input.completed_at,
        status_message: input.status_message.clone(),
        result_body: input.result_body.clone(),
        created_by: input
            .created_by
            .as_ref()
            .map(workflow_actor_input_from_actor),
        execution_ref: input.execution_ref.clone(),
        workflow_key: input.workflow_key.clone(),
        provider_name: input.provider_name.clone(),
    })
}

pub(crate) fn bound_workflow_run_to_proto(
    input: BoundWorkflowRun,
) -> ProviderResult<pb::BoundWorkflowRun> {
    Ok(pb::BoundWorkflowRun {
        id: input.id,
        status: input.status.as_i32(),
        target: input
            .target
            .map(bound_workflow_target_to_proto)
            .transpose()?,
        trigger: input
            .trigger
            .map(workflow_run_trigger_to_proto)
            .transpose()?,
        created_at: input.created_at.map(protocol::timestamp_from_system_time),
        started_at: input.started_at.map(protocol::timestamp_from_system_time),
        completed_at: input.completed_at.map(protocol::timestamp_from_system_time),
        status_message: input.status_message,
        result_body: input.result_body,
        created_by: input.created_by.map(workflow_actor_to_proto),
        execution_ref: input.execution_ref,
        workflow_key: input.workflow_key,
        provider_name: input.provider_name,
    })
}

pub(crate) fn bound_workflow_run_from_proto(
    input: pb::BoundWorkflowRun,
) -> ProviderResult<BoundWorkflowRun> {
    Ok(BoundWorkflowRun {
        id: input.id,
        status: WorkflowRunStatus::try_from(input.status)?,
        target: input
            .target
            .map(bound_workflow_target_from_proto)
            .transpose()?,
        trigger: input
            .trigger
            .map(workflow_run_trigger_from_proto)
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
        status_message: input.status_message,
        result_body: input.result_body,
        created_by: input.created_by.map(workflow_actor_from_proto),
        execution_ref: input.execution_ref,
        workflow_key: input.workflow_key,
        provider_name: input.provider_name,
    })
}

/// Returns a deep copy of a workflow-provider run.
pub fn new_bound_workflow_run_from_run(
    input: &BoundWorkflowRun,
) -> ProviderResult<BoundWorkflowRun> {
    Ok(input.clone())
}

/// Returns input copied from a workflow-provider definition.
pub fn bound_workflow_definition_input_from_definition(
    input: &BoundWorkflowDefinition,
) -> ProviderResult<BoundWorkflowDefinition> {
    Ok(BoundWorkflowDefinition {
        id: input.id.clone(),
        target: input
            .target
            .as_ref()
            .map(bound_workflow_target_input_from_target)
            .transpose()?,
        created_by: input
            .created_by
            .as_ref()
            .map(workflow_actor_input_from_actor),
        created_at: input.created_at,
        provider_name: input.provider_name.clone(),
    })
}

/// Creates a workflow-provider schedule.
pub fn new_bound_workflow_schedule(
    input: BoundWorkflowSchedule,
) -> ProviderResult<BoundWorkflowSchedule> {
    Ok(BoundWorkflowSchedule {
        id: input.id,
        cron: input.cron,
        timezone: input.timezone,
        target: input.target.map(new_bound_workflow_target).transpose()?,
        paused: input.paused,
        created_at: input.created_at,
        updated_at: input.updated_at,
        next_run_at: input.next_run_at,
        created_by: input.created_by.map(new_workflow_actor),
        execution_ref: input.execution_ref,
        provider_name: input.provider_name,
    })
}

/// Returns input copied from a workflow-provider schedule.
pub fn bound_workflow_schedule_input_from_schedule(
    input: &BoundWorkflowSchedule,
) -> ProviderResult<BoundWorkflowSchedule> {
    Ok(BoundWorkflowSchedule {
        id: input.id.clone(),
        cron: input.cron.clone(),
        timezone: input.timezone.clone(),
        target: input
            .target
            .as_ref()
            .map(bound_workflow_target_input_from_target)
            .transpose()?,
        paused: input.paused,
        created_at: input.created_at,
        updated_at: input.updated_at,
        next_run_at: input.next_run_at,
        created_by: input
            .created_by
            .as_ref()
            .map(workflow_actor_input_from_actor),
        execution_ref: input.execution_ref.clone(),
        provider_name: input.provider_name.clone(),
    })
}

pub(crate) fn bound_workflow_schedule_to_proto(
    input: BoundWorkflowSchedule,
) -> ProviderResult<pb::BoundWorkflowSchedule> {
    Ok(pb::BoundWorkflowSchedule {
        id: input.id,
        cron: input.cron,
        timezone: input.timezone,
        target: input
            .target
            .map(bound_workflow_target_to_proto)
            .transpose()?,
        paused: input.paused,
        created_at: input.created_at.map(protocol::timestamp_from_system_time),
        updated_at: input.updated_at.map(protocol::timestamp_from_system_time),
        next_run_at: input.next_run_at.map(protocol::timestamp_from_system_time),
        created_by: input.created_by.map(workflow_actor_to_proto),
        execution_ref: input.execution_ref,
        provider_name: input.provider_name,
    })
}

pub(crate) fn bound_workflow_schedule_from_proto(
    input: pb::BoundWorkflowSchedule,
) -> ProviderResult<BoundWorkflowSchedule> {
    Ok(BoundWorkflowSchedule {
        id: input.id,
        cron: input.cron,
        timezone: input.timezone,
        target: input
            .target
            .map(bound_workflow_target_from_proto)
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
        created_by: input.created_by.map(workflow_actor_from_proto),
        execution_ref: input.execution_ref,
        provider_name: input.provider_name,
    })
}

/// Returns a deep copy of a workflow-provider schedule.
pub fn new_bound_workflow_schedule_from_schedule(
    input: &BoundWorkflowSchedule,
) -> ProviderResult<BoundWorkflowSchedule> {
    Ok(input.clone())
}

/// Creates a workflow-provider event trigger.
pub fn new_bound_workflow_event_trigger(
    input: BoundWorkflowEventTrigger,
) -> ProviderResult<BoundWorkflowEventTrigger> {
    Ok(BoundWorkflowEventTrigger {
        id: input.id,
        event_match: input.event_match.map(new_workflow_event_match),
        target: input.target.map(new_bound_workflow_target).transpose()?,
        paused: input.paused,
        created_at: input.created_at,
        updated_at: input.updated_at,
        created_by: input.created_by.map(new_workflow_actor),
        execution_ref: input.execution_ref,
        provider_name: input.provider_name,
    })
}

/// Returns input copied from a workflow-provider event trigger.
pub fn bound_workflow_event_trigger_input_from_trigger(
    input: &BoundWorkflowEventTrigger,
) -> ProviderResult<BoundWorkflowEventTrigger> {
    Ok(BoundWorkflowEventTrigger {
        id: input.id.clone(),
        event_match: input
            .event_match
            .as_ref()
            .map(workflow_event_match_input_from_match),
        target: input
            .target
            .as_ref()
            .map(bound_workflow_target_input_from_target)
            .transpose()?,
        paused: input.paused,
        created_at: input.created_at,
        updated_at: input.updated_at,
        created_by: input
            .created_by
            .as_ref()
            .map(workflow_actor_input_from_actor),
        execution_ref: input.execution_ref.clone(),
        provider_name: input.provider_name.clone(),
    })
}

pub(crate) fn bound_workflow_event_trigger_to_proto(
    input: BoundWorkflowEventTrigger,
) -> ProviderResult<pb::BoundWorkflowEventTrigger> {
    Ok(pb::BoundWorkflowEventTrigger {
        id: input.id,
        r#match: input.event_match.map(workflow_event_match_to_proto),
        target: input
            .target
            .map(bound_workflow_target_to_proto)
            .transpose()?,
        paused: input.paused,
        created_at: input.created_at.map(protocol::timestamp_from_system_time),
        updated_at: input.updated_at.map(protocol::timestamp_from_system_time),
        created_by: input.created_by.map(workflow_actor_to_proto),
        execution_ref: input.execution_ref,
        provider_name: input.provider_name,
    })
}

pub(crate) fn bound_workflow_event_trigger_from_proto(
    input: pb::BoundWorkflowEventTrigger,
) -> ProviderResult<BoundWorkflowEventTrigger> {
    Ok(BoundWorkflowEventTrigger {
        id: input.id,
        event_match: input.r#match.map(workflow_event_match_from_proto),
        target: input
            .target
            .map(bound_workflow_target_from_proto)
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
        created_by: input.created_by.map(workflow_actor_from_proto),
        execution_ref: input.execution_ref,
        provider_name: input.provider_name,
    })
}

/// Returns a deep copy of a workflow-provider event trigger.
pub fn new_bound_workflow_event_trigger_from_trigger(
    input: &BoundWorkflowEventTrigger,
) -> ProviderResult<BoundWorkflowEventTrigger> {
    Ok(input.clone())
}

pub(crate) fn bound_workflow_definition_from_proto(
    input: pb::BoundWorkflowDefinition,
) -> ProviderResult<BoundWorkflowDefinition> {
    Ok(BoundWorkflowDefinition {
        id: input.id,
        target: input
            .target
            .map(bound_workflow_target_from_proto)
            .transpose()?,
        created_by: input.created_by.map(workflow_actor_from_proto),
        created_at: input
            .created_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        provider_name: input.provider_name,
    })
}

pub(crate) fn bound_workflow_definition_to_proto(
    input: BoundWorkflowDefinition,
) -> ProviderResult<pb::BoundWorkflowDefinition> {
    Ok(pb::BoundWorkflowDefinition {
        id: input.id,
        target: input
            .target
            .map(bound_workflow_target_to_proto)
            .transpose()?,
        created_by: input.created_by.map(workflow_actor_to_proto),
        created_at: input.created_at.map(protocol::timestamp_from_system_time),
        provider_name: input.provider_name,
    })
}

/// Creates a workflow execution reference.
pub fn new_workflow_execution_reference(
    input: WorkflowExecutionReference,
) -> ProviderResult<WorkflowExecutionReference> {
    Ok(WorkflowExecutionReference {
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
        created_at: input.created_at,
        revoked_at: input.revoked_at,
        subject_kind: input.subject_kind,
        display_name: input.display_name,
        auth_source: input.auth_source,
        caller_app_name: input.caller_app_name,
        run_as: input.run_as.map(new_workflow_run_as_subject),
        source_definition_id: input.source_definition_id,
    })
}

/// Returns input copied from a workflow execution reference.
pub fn workflow_execution_reference_input_from_reference(
    input: &WorkflowExecutionReference,
) -> ProviderResult<WorkflowExecutionReference> {
    Ok(WorkflowExecutionReference {
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
        created_at: input.created_at,
        revoked_at: input.revoked_at,
        subject_kind: input.subject_kind.clone(),
        display_name: input.display_name.clone(),
        auth_source: input.auth_source.clone(),
        caller_app_name: input.caller_app_name.clone(),
        run_as: input
            .run_as
            .as_ref()
            .map(workflow_run_as_subject_input_from_subject),
        source_definition_id: input.source_definition_id.clone(),
    })
}

pub(crate) fn workflow_execution_reference_to_proto(
    input: WorkflowExecutionReference,
) -> ProviderResult<pb::WorkflowExecutionReference> {
    Ok(pb::WorkflowExecutionReference {
        id: input.id,
        provider_name: input.provider_name,
        target: input
            .target
            .map(bound_workflow_target_to_proto)
            .transpose()?,
        subject_id: input.subject_id,
        credential_subject_id: input.credential_subject_id,
        permissions: input
            .permissions
            .into_iter()
            .map(workflow_access_permission_to_proto)
            .collect(),
        created_at: input.created_at.map(protocol::timestamp_from_system_time),
        revoked_at: input.revoked_at.map(protocol::timestamp_from_system_time),
        subject_kind: input.subject_kind,
        display_name: input.display_name,
        auth_source: input.auth_source,
        caller_app_name: input.caller_app_name,
        run_as: input.run_as.map(workflow_run_as_subject_to_proto),
        source_definition_id: input.source_definition_id,
    })
}

pub(crate) fn workflow_execution_reference_from_proto(
    input: pb::WorkflowExecutionReference,
) -> ProviderResult<WorkflowExecutionReference> {
    Ok(WorkflowExecutionReference {
        id: input.id,
        provider_name: input.provider_name,
        target: input
            .target
            .map(bound_workflow_target_from_proto)
            .transpose()?,
        subject_id: input.subject_id,
        credential_subject_id: input.credential_subject_id,
        permissions: input
            .permissions
            .into_iter()
            .map(workflow_access_permission_from_proto)
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
        subject_kind: input.subject_kind,
        display_name: input.display_name,
        auth_source: input.auth_source,
        caller_app_name: input.caller_app_name,
        run_as: input.run_as.map(workflow_run_as_subject_from_proto),
        source_definition_id: input.source_definition_id,
    })
}

/// Returns a deep copy of a workflow execution reference.
pub fn new_workflow_execution_reference_from_reference(
    input: &WorkflowExecutionReference,
) -> ProviderResult<WorkflowExecutionReference> {
    Ok(input.clone())
}

fn invoke_workflow_operation_request_from_input(
    input: InvokeWorkflowOperationInput,
) -> ProviderResult<pb::InvokeWorkflowOperationRequest> {
    Ok(pb::InvokeWorkflowOperationRequest {
        target: input
            .target
            .map(new_bound_workflow_target)
            .transpose()?
            .map(bound_workflow_target_to_proto)
            .transpose()?,
        run_id: input.run_id,
        trigger: input
            .trigger
            .map(new_workflow_run_trigger)
            .transpose()?
            .map(workflow_run_trigger_to_proto)
            .transpose()?,
        input: input.input.map(protocol::struct_from_json).transpose()?,
        metadata: input.metadata.map(protocol::struct_from_json).transpose()?,
        created_by: input
            .created_by
            .map(new_workflow_actor)
            .map(workflow_actor_to_proto),
        execution_ref: input.execution_ref,
        signals: input
            .signals
            .into_iter()
            .map(new_workflow_signal)
            .map(|signal| signal.and_then(workflow_signal_to_proto))
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

fn start_workflow_provider_run_request_from_proto(
    request: pb::StartWorkflowProviderRunRequest,
) -> ProviderResult<StartWorkflowProviderRunRequest> {
    Ok(StartWorkflowProviderRunRequest {
        target: request
            .target
            .map(bound_workflow_target_from_proto)
            .transpose()?,
        idempotency_key: request.idempotency_key,
        created_by: request.created_by.map(workflow_actor_from_proto),
        execution_ref: request.execution_ref,
        workflow_key: request.workflow_key,
        definition_id: request.definition_id,
    })
}

fn signal_workflow_run_response_to_proto(
    response: SignalWorkflowRunResponse,
) -> ProviderResult<pb::SignalWorkflowRunResponse> {
    Ok(pb::SignalWorkflowRunResponse {
        run: response.run.map(bound_workflow_run_to_proto).transpose()?,
        signal: response.signal.map(workflow_signal_to_proto).transpose()?,
        started_run: response.started_run,
        workflow_key: response.workflow_key,
    })
}

fn create_workflow_provider_definition_request_from_proto(
    request: pb::CreateWorkflowProviderDefinitionRequest,
) -> ProviderResult<CreateWorkflowProviderDefinitionRequest> {
    Ok(CreateWorkflowProviderDefinitionRequest {
        provider_name: request.provider_name,
        target: request
            .target
            .map(bound_workflow_target_from_proto)
            .transpose()?,
        idempotency_key: request.idempotency_key,
    })
}

fn update_workflow_provider_definition_request_from_proto(
    request: pb::UpdateWorkflowProviderDefinitionRequest,
) -> ProviderResult<UpdateWorkflowProviderDefinitionRequest> {
    Ok(UpdateWorkflowProviderDefinitionRequest {
        definition_id: request.definition_id,
        provider_name: request.provider_name,
        target: request
            .target
            .map(bound_workflow_target_from_proto)
            .transpose()?,
    })
}

fn list_runs_response_to_proto(
    response: ListWorkflowProviderRunsResponse,
) -> ProviderResult<pb::ListWorkflowProviderRunsResponse> {
    Ok(pb::ListWorkflowProviderRunsResponse {
        runs: response
            .runs
            .into_iter()
            .map(bound_workflow_run_to_proto)
            .collect::<ProviderResult<Vec<_>>>()?,
        next_page_token: response.next_page_token,
    })
}

fn list_workflow_provider_runs_request_from_proto(
    request: pb::ListWorkflowProviderRunsRequest,
) -> ProviderResult<ListWorkflowProviderRunsRequest> {
    Ok(ListWorkflowProviderRunsRequest {
        page_size: request.page_size,
        page_token: request.page_token,
        status: WorkflowRunStatus::try_from(request.status)?,
        target_app: request.target_app,
    })
}

fn upsert_schedule_request_from_proto(
    request: pb::UpsertWorkflowProviderScheduleRequest,
) -> ProviderResult<UpsertWorkflowProviderScheduleRequest> {
    Ok(UpsertWorkflowProviderScheduleRequest {
        schedule_id: request.schedule_id,
        cron: request.cron,
        timezone: request.timezone,
        target: request
            .target
            .map(bound_workflow_target_from_proto)
            .transpose()?,
        paused: request.paused,
        requested_by: request.requested_by.map(workflow_actor_from_proto),
        execution_ref: request.execution_ref,
        idempotency_key: request.idempotency_key,
        definition_id: request.definition_id,
    })
}

fn list_schedules_response_to_proto(
    response: ListWorkflowProviderSchedulesResponse,
) -> ProviderResult<pb::ListWorkflowProviderSchedulesResponse> {
    Ok(pb::ListWorkflowProviderSchedulesResponse {
        schedules: response
            .schedules
            .into_iter()
            .map(bound_workflow_schedule_to_proto)
            .collect::<ProviderResult<Vec<_>>>()?,
    })
}

fn upsert_event_trigger_request_from_proto(
    request: pb::UpsertWorkflowProviderEventTriggerRequest,
) -> ProviderResult<UpsertWorkflowProviderEventTriggerRequest> {
    Ok(UpsertWorkflowProviderEventTriggerRequest {
        trigger_id: request.trigger_id,
        event_match: request.r#match.map(workflow_event_match_from_proto),
        target: request
            .target
            .map(bound_workflow_target_from_proto)
            .transpose()?,
        paused: request.paused,
        requested_by: request.requested_by.map(workflow_actor_from_proto),
        execution_ref: request.execution_ref,
        idempotency_key: request.idempotency_key,
        definition_id: request.definition_id,
    })
}

fn list_event_triggers_response_to_proto(
    response: ListWorkflowProviderEventTriggersResponse,
) -> ProviderResult<pb::ListWorkflowProviderEventTriggersResponse> {
    Ok(pb::ListWorkflowProviderEventTriggersResponse {
        triggers: response
            .triggers
            .into_iter()
            .map(bound_workflow_event_trigger_to_proto)
            .collect::<ProviderResult<Vec<_>>>()?,
    })
}

fn publish_event_request_from_proto(
    request: pb::PublishWorkflowProviderEventRequest,
) -> ProviderResult<PublishWorkflowProviderEventRequest> {
    Ok(PublishWorkflowProviderEventRequest {
        app_name: request.app_name,
        event: request.event.map(workflow_event_from_proto).transpose()?,
        published_by: request.published_by.map(workflow_actor_from_proto),
    })
}

fn list_execution_references_response_to_proto(
    response: ListWorkflowExecutionReferencesResponse,
) -> ProviderResult<pb::ListWorkflowExecutionReferencesResponse> {
    Ok(pb::ListWorkflowExecutionReferencesResponse {
        references: response
            .references
            .into_iter()
            .map(workflow_execution_reference_to_proto)
            .collect::<ProviderResult<Vec<_>>>()?,
    })
}

pub(crate) fn workflow_manager_schedule_from_proto(
    input: pb::BoundWorkflowSchedule,
) -> ProviderResult<WorkflowManagerSchedule> {
    Ok(WorkflowManagerSchedule {
        provider_name: input.provider_name.clone(),
        schedule: Some(bound_workflow_schedule_from_proto(input)?),
    })
}

pub(crate) fn workflow_manager_event_trigger_from_proto(
    input: pb::BoundWorkflowEventTrigger,
) -> ProviderResult<WorkflowManagerEventTrigger> {
    Ok(WorkflowManagerEventTrigger {
        provider_name: input.provider_name.clone(),
        trigger: Some(bound_workflow_event_trigger_from_proto(input)?),
    })
}

pub(crate) fn workflow_manager_definition_from_proto(
    input: pb::BoundWorkflowDefinition,
) -> ProviderResult<WorkflowManagerDefinition> {
    Ok(WorkflowManagerDefinition {
        provider_name: input.provider_name.clone(),
        definition: Some(bound_workflow_definition_from_proto(input)?),
    })
}

pub(crate) fn workflow_manager_run_from_proto(
    input: pb::BoundWorkflowRun,
) -> ProviderResult<WorkflowManagerRun> {
    Ok(WorkflowManagerRun {
        provider_name: input.provider_name.clone(),
        run: Some(bound_workflow_run_from_proto(input)?),
    })
}

pub(crate) fn workflow_manager_run_signal_from_proto(
    input: pb::SignalWorkflowRunResponse,
) -> ProviderResult<WorkflowManagerRunSignal> {
    let provider_name = input
        .run
        .as_ref()
        .map(|run| run.provider_name.clone())
        .unwrap_or_default();
    Ok(WorkflowManagerRunSignal {
        provider_name,
        run: input.run.map(bound_workflow_run_from_proto).transpose()?,
        signal: input.signal.map(workflow_signal_from_proto).transpose()?,
        started_run: input.started_run,
        workflow_key: input.workflow_key,
    })
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

#[async_trait]
/// Fakeable client contract for workflow host calls.
pub trait WorkflowHostApi: Send {
    async fn invoke_operation(
        &mut self,
        input: InvokeWorkflowOperationInput,
    ) -> std::result::Result<InvokeWorkflowOperationResponse, WorkflowHostError>;
}

/// Client for invoking operations from workflow provider code.
pub struct WorkflowHost {
    client: ProtoWorkflowHostClient<WorkflowHostTransport>,
}

impl WorkflowHost {
    /// Connects to the workflow host service described by the environment.
    pub async fn connect() -> std::result::Result<Self, WorkflowHostError> {
        let target = std::env::var(ENV_HOST_SERVICE_SOCKET)
            .map_err(|_| WorkflowHostError::Env(format!("{ENV_HOST_SERVICE_SOCKET} is not set")))?;
        let relay_token = std::env::var(ENV_HOST_SERVICE_TOKEN).unwrap_or_default();
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

#[async_trait]
impl WorkflowHostApi for WorkflowHost {
    async fn invoke_operation(
        &mut self,
        input: InvokeWorkflowOperationInput,
    ) -> std::result::Result<InvokeWorkflowOperationResponse, WorkflowHostError> {
        WorkflowHost::invoke_operation(self, input).await
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

    /// Creates or idempotently returns a workflow definition.
    async fn create_definition(
        &self,
        _request: CreateWorkflowProviderDefinitionRequest,
    ) -> ProviderResult<BoundWorkflowDefinition> {
        Err(crate::Error::unimplemented(
            "workflow create definition is not implemented",
        ))
    }

    /// Returns one workflow definition by ID.
    async fn get_definition(
        &self,
        _request: GetWorkflowProviderDefinitionRequest,
    ) -> ProviderResult<BoundWorkflowDefinition> {
        Err(crate::Error::unimplemented(
            "workflow get definition is not implemented",
        ))
    }

    /// Updates a workflow definition.
    async fn update_definition(
        &self,
        _request: UpdateWorkflowProviderDefinitionRequest,
    ) -> ProviderResult<BoundWorkflowDefinition> {
        Err(crate::Error::unimplemented(
            "workflow update definition is not implemented",
        ))
    }

    /// Deletes a workflow definition.
    async fn delete_definition(
        &self,
        _request: DeleteWorkflowProviderDefinitionRequest,
    ) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "workflow delete definition is not implemented",
        ))
    }

    /// Starts or idempotently returns a workflow run.
    async fn start_run(
        &self,
        _request: StartWorkflowProviderRunRequest,
    ) -> ProviderResult<BoundWorkflowRun> {
        Err(crate::Error::unimplemented(
            "workflow start run is not implemented",
        ))
    }

    /// Returns one workflow run by ID.
    async fn get_run(
        &self,
        _request: GetWorkflowProviderRunRequest,
    ) -> ProviderResult<BoundWorkflowRun> {
        Err(crate::Error::unimplemented(
            "workflow get run is not implemented",
        ))
    }

    /// Lists workflow runs visible to the request subject.
    async fn list_runs(
        &self,
        _request: ListWorkflowProviderRunsRequest,
    ) -> ProviderResult<ListWorkflowProviderRunsResponse> {
        Err(crate::Error::unimplemented(
            "workflow list runs is not implemented",
        ))
    }

    /// Requests cancellation of a pending or running workflow run.
    async fn cancel_run(
        &self,
        _request: CancelWorkflowProviderRunRequest,
    ) -> ProviderResult<BoundWorkflowRun> {
        Err(crate::Error::unimplemented(
            "workflow cancel run is not implemented",
        ))
    }

    /// Delivers a signal to an existing workflow run.
    async fn signal_run(
        &self,
        _request: SignalWorkflowProviderRunRequest,
    ) -> ProviderResult<SignalWorkflowRunResponse> {
        Err(crate::Error::unimplemented(
            "workflow signal run is not implemented",
        ))
    }

    /// Delivers a signal or starts a run when no target run exists.
    async fn signal_or_start_run(
        &self,
        _request: SignalOrStartWorkflowProviderRunRequest,
    ) -> ProviderResult<SignalWorkflowRunResponse> {
        Err(crate::Error::unimplemented(
            "workflow signal or start run is not implemented",
        ))
    }

    /// Creates or updates a workflow schedule.
    async fn upsert_schedule(
        &self,
        _request: UpsertWorkflowProviderScheduleRequest,
    ) -> ProviderResult<BoundWorkflowSchedule> {
        Err(crate::Error::unimplemented(
            "workflow upsert schedule is not implemented",
        ))
    }

    /// Returns one workflow schedule by ID.
    async fn get_schedule(
        &self,
        _request: GetWorkflowProviderScheduleRequest,
    ) -> ProviderResult<BoundWorkflowSchedule> {
        Err(crate::Error::unimplemented(
            "workflow get schedule is not implemented",
        ))
    }

    /// Lists workflow schedules visible to the request subject.
    async fn list_schedules(
        &self,
        _request: ListWorkflowProviderSchedulesRequest,
    ) -> ProviderResult<ListWorkflowProviderSchedulesResponse> {
        Err(crate::Error::unimplemented(
            "workflow list schedules is not implemented",
        ))
    }

    /// Deletes a workflow schedule.
    async fn delete_schedule(
        &self,
        _request: DeleteWorkflowProviderScheduleRequest,
    ) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "workflow delete schedule is not implemented",
        ))
    }

    /// Pauses a workflow schedule without deleting it.
    async fn pause_schedule(
        &self,
        _request: PauseWorkflowProviderScheduleRequest,
    ) -> ProviderResult<BoundWorkflowSchedule> {
        Err(crate::Error::unimplemented(
            "workflow pause schedule is not implemented",
        ))
    }

    /// Resumes a paused workflow schedule.
    async fn resume_schedule(
        &self,
        _request: ResumeWorkflowProviderScheduleRequest,
    ) -> ProviderResult<BoundWorkflowSchedule> {
        Err(crate::Error::unimplemented(
            "workflow resume schedule is not implemented",
        ))
    }

    /// Creates or updates a workflow event trigger.
    async fn upsert_event_trigger(
        &self,
        _request: UpsertWorkflowProviderEventTriggerRequest,
    ) -> ProviderResult<BoundWorkflowEventTrigger> {
        Err(crate::Error::unimplemented(
            "workflow upsert event trigger is not implemented",
        ))
    }

    /// Returns one workflow event trigger by ID.
    async fn get_event_trigger(
        &self,
        _request: GetWorkflowProviderEventTriggerRequest,
    ) -> ProviderResult<BoundWorkflowEventTrigger> {
        Err(crate::Error::unimplemented(
            "workflow get event trigger is not implemented",
        ))
    }

    /// Lists workflow event triggers visible to the request subject.
    async fn list_event_triggers(
        &self,
        _request: ListWorkflowProviderEventTriggersRequest,
    ) -> ProviderResult<ListWorkflowProviderEventTriggersResponse> {
        Err(crate::Error::unimplemented(
            "workflow list event triggers is not implemented",
        ))
    }

    /// Deletes a workflow event trigger.
    async fn delete_event_trigger(
        &self,
        _request: DeleteWorkflowProviderEventTriggerRequest,
    ) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "workflow delete event trigger is not implemented",
        ))
    }

    /// Pauses a workflow event trigger without deleting it.
    async fn pause_event_trigger(
        &self,
        _request: PauseWorkflowProviderEventTriggerRequest,
    ) -> ProviderResult<BoundWorkflowEventTrigger> {
        Err(crate::Error::unimplemented(
            "workflow pause event trigger is not implemented",
        ))
    }

    /// Resumes a paused workflow event trigger.
    async fn resume_event_trigger(
        &self,
        _request: ResumeWorkflowProviderEventTriggerRequest,
    ) -> ProviderResult<BoundWorkflowEventTrigger> {
        Err(crate::Error::unimplemented(
            "workflow resume event trigger is not implemented",
        ))
    }

    /// Publishes a workflow event for trigger matching.
    async fn publish_event(
        &self,
        _request: PublishWorkflowProviderEventRequest,
    ) -> ProviderResult<WorkflowEvent> {
        Err(crate::Error::unimplemented(
            "workflow publish event is not implemented",
        ))
    }

    /// Stores or updates a workflow execution reference.
    async fn put_execution_reference(
        &self,
        _request: PutWorkflowExecutionReferenceRequest,
    ) -> ProviderResult<WorkflowExecutionReference> {
        Err(crate::Error::unimplemented(
            "workflow put execution reference is not implemented",
        ))
    }

    /// Returns one workflow execution reference.
    async fn get_execution_reference(
        &self,
        _request: GetWorkflowExecutionReferenceRequest,
    ) -> ProviderResult<WorkflowExecutionReference> {
        Err(crate::Error::unimplemented(
            "workflow get execution reference is not implemented",
        ))
    }

    /// Lists workflow execution references for a scope.
    async fn list_execution_references(
        &self,
        _request: ListWorkflowExecutionReferencesRequest,
    ) -> ProviderResult<ListWorkflowExecutionReferencesResponse> {
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
    async fn create_definition(
        &self,
        request: GrpcRequest<pb::CreateWorkflowProviderDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowDefinition>, Status> {
        let definition = self
            .provider
            .create_definition(
                create_workflow_provider_definition_request_from_proto(request.into_inner())
                    .map_err(|error| rpc_status("workflow create definition", error))?,
            )
            .await
            .map_err(|error| rpc_status("workflow create definition", error))?;
        Ok(GrpcResponse::new(
            bound_workflow_definition_to_proto(definition)
                .map_err(|error| rpc_status("workflow create definition", error))?,
        ))
    }

    async fn get_definition(
        &self,
        request: GrpcRequest<pb::GetWorkflowProviderDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowDefinition>, Status> {
        let definition = self
            .provider
            .get_definition({
                let request = request.into_inner();
                GetWorkflowProviderDefinitionRequest {
                    definition_id: request.definition_id,
                }
            })
            .await
            .map_err(|error| rpc_status("workflow get definition", error))?;
        Ok(GrpcResponse::new(
            bound_workflow_definition_to_proto(definition)
                .map_err(|error| rpc_status("workflow get definition", error))?,
        ))
    }

    async fn update_definition(
        &self,
        request: GrpcRequest<pb::UpdateWorkflowProviderDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowDefinition>, Status> {
        let definition = self
            .provider
            .update_definition(
                update_workflow_provider_definition_request_from_proto(request.into_inner())
                    .map_err(|error| rpc_status("workflow update definition", error))?,
            )
            .await
            .map_err(|error| rpc_status("workflow update definition", error))?;
        Ok(GrpcResponse::new(
            bound_workflow_definition_to_proto(definition)
                .map_err(|error| rpc_status("workflow update definition", error))?,
        ))
    }

    async fn delete_definition(
        &self,
        request: GrpcRequest<pb::DeleteWorkflowProviderDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .delete_definition({
                let request = request.into_inner();
                DeleteWorkflowProviderDefinitionRequest {
                    definition_id: request.definition_id,
                }
            })
            .await
            .map_err(|error| rpc_status("workflow delete definition", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn start_run(
        &self,
        request: GrpcRequest<pb::StartWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowRun>, Status> {
        let run = self
            .provider
            .start_run(
                start_workflow_provider_run_request_from_proto(request.into_inner())
                    .map_err(|error| rpc_status("workflow start run", error))?,
            )
            .await
            .map_err(|error| rpc_status("workflow start run", error))?;
        Ok(GrpcResponse::new(
            bound_workflow_run_to_proto(run)
                .map_err(|error| rpc_status("workflow start run", error))?,
        ))
    }

    async fn get_run(
        &self,
        request: GrpcRequest<pb::GetWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowRun>, Status> {
        let run = self
            .provider
            .get_run({
                let request = request.into_inner();
                GetWorkflowProviderRunRequest {
                    run_id: request.run_id,
                }
            })
            .await
            .map_err(|error| rpc_status("workflow get run", error))?;
        Ok(GrpcResponse::new(
            bound_workflow_run_to_proto(run)
                .map_err(|error| rpc_status("workflow get run", error))?,
        ))
    }

    async fn list_runs(
        &self,
        request: GrpcRequest<pb::ListWorkflowProviderRunsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListWorkflowProviderRunsResponse>, Status> {
        let response = self
            .provider
            .list_runs(
                list_workflow_provider_runs_request_from_proto(request.into_inner())
                    .map_err(|error| rpc_status("workflow list runs", error))?,
            )
            .await
            .map_err(|error| rpc_status("workflow list runs", error))?;
        Ok(GrpcResponse::new(
            list_runs_response_to_proto(response)
                .map_err(|error| rpc_status("workflow list runs", error))?,
        ))
    }

    async fn cancel_run(
        &self,
        request: GrpcRequest<pb::CancelWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowRun>, Status> {
        let run = self
            .provider
            .cancel_run({
                let request = request.into_inner();
                CancelWorkflowProviderRunRequest {
                    run_id: request.run_id,
                    reason: request.reason,
                }
            })
            .await
            .map_err(|error| rpc_status("workflow cancel run", error))?;
        Ok(GrpcResponse::new(
            bound_workflow_run_to_proto(run)
                .map_err(|error| rpc_status("workflow cancel run", error))?,
        ))
    }

    async fn signal_run(
        &self,
        request: GrpcRequest<pb::SignalWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::SignalWorkflowRunResponse>, Status> {
        let response = self
            .provider
            .signal_run({
                let request = request.into_inner();
                SignalWorkflowProviderRunRequest {
                    run_id: request.run_id,
                    signal: request
                        .signal
                        .map(workflow_signal_from_proto)
                        .transpose()
                        .map_err(|error| rpc_status("workflow signal run", error))?,
                }
            })
            .await
            .map_err(|error| rpc_status("workflow signal run", error))?;
        Ok(GrpcResponse::new(
            signal_workflow_run_response_to_proto(response)
                .map_err(|error| rpc_status("workflow signal run", error))?,
        ))
    }

    async fn signal_or_start_run(
        &self,
        request: GrpcRequest<pb::SignalOrStartWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::SignalWorkflowRunResponse>, Status> {
        let response = self
            .provider
            .signal_or_start_run({
                let request = request.into_inner();
                SignalOrStartWorkflowProviderRunRequest {
                    workflow_key: request.workflow_key,
                    target: request
                        .target
                        .map(bound_workflow_target_from_proto)
                        .transpose()
                        .map_err(|error| rpc_status("workflow signal or start run", error))?,
                    idempotency_key: request.idempotency_key,
                    created_by: request.created_by.map(workflow_actor_from_proto),
                    execution_ref: request.execution_ref,
                    signal: request
                        .signal
                        .map(workflow_signal_from_proto)
                        .transpose()
                        .map_err(|error| rpc_status("workflow signal or start run", error))?,
                    definition_id: request.definition_id,
                }
            })
            .await
            .map_err(|error| rpc_status("workflow signal or start run", error))?;
        Ok(GrpcResponse::new(
            signal_workflow_run_response_to_proto(response)
                .map_err(|error| rpc_status("workflow signal or start run", error))?,
        ))
    }

    async fn upsert_schedule(
        &self,
        request: GrpcRequest<pb::UpsertWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowSchedule>, Status> {
        let schedule = self
            .provider
            .upsert_schedule(
                upsert_schedule_request_from_proto(request.into_inner())
                    .map_err(|error| rpc_status("workflow upsert schedule", error))?,
            )
            .await
            .map_err(|error| rpc_status("workflow upsert schedule", error))?;
        Ok(GrpcResponse::new(
            bound_workflow_schedule_to_proto(schedule)
                .map_err(|error| rpc_status("workflow upsert schedule", error))?,
        ))
    }

    async fn get_schedule(
        &self,
        request: GrpcRequest<pb::GetWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowSchedule>, Status> {
        let schedule = self
            .provider
            .get_schedule({
                let request = request.into_inner();
                GetWorkflowProviderScheduleRequest {
                    schedule_id: request.schedule_id,
                }
            })
            .await
            .map_err(|error| rpc_status("workflow get schedule", error))?;
        Ok(GrpcResponse::new(
            bound_workflow_schedule_to_proto(schedule)
                .map_err(|error| rpc_status("workflow get schedule", error))?,
        ))
    }

    async fn list_schedules(
        &self,
        request: GrpcRequest<pb::ListWorkflowProviderSchedulesRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListWorkflowProviderSchedulesResponse>, Status> {
        let response = self
            .provider
            .list_schedules({
                let _request = request.into_inner();
                ListWorkflowProviderSchedulesRequest {}
            })
            .await
            .map_err(|error| rpc_status("workflow list schedules", error))?;
        Ok(GrpcResponse::new(
            list_schedules_response_to_proto(response)
                .map_err(|error| rpc_status("workflow list schedules", error))?,
        ))
    }

    async fn delete_schedule(
        &self,
        request: GrpcRequest<pb::DeleteWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .delete_schedule({
                let request = request.into_inner();
                DeleteWorkflowProviderScheduleRequest {
                    schedule_id: request.schedule_id,
                }
            })
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
            .pause_schedule({
                let request = request.into_inner();
                PauseWorkflowProviderScheduleRequest {
                    schedule_id: request.schedule_id,
                }
            })
            .await
            .map_err(|error| rpc_status("workflow pause schedule", error))?;
        Ok(GrpcResponse::new(
            bound_workflow_schedule_to_proto(schedule)
                .map_err(|error| rpc_status("workflow pause schedule", error))?,
        ))
    }

    async fn resume_schedule(
        &self,
        request: GrpcRequest<pb::ResumeWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowSchedule>, Status> {
        let schedule = self
            .provider
            .resume_schedule({
                let request = request.into_inner();
                ResumeWorkflowProviderScheduleRequest {
                    schedule_id: request.schedule_id,
                }
            })
            .await
            .map_err(|error| rpc_status("workflow resume schedule", error))?;
        Ok(GrpcResponse::new(
            bound_workflow_schedule_to_proto(schedule)
                .map_err(|error| rpc_status("workflow resume schedule", error))?,
        ))
    }

    async fn upsert_event_trigger(
        &self,
        request: GrpcRequest<pb::UpsertWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowEventTrigger>, Status> {
        let trigger = self
            .provider
            .upsert_event_trigger(
                upsert_event_trigger_request_from_proto(request.into_inner())
                    .map_err(|error| rpc_status("workflow upsert event trigger", error))?,
            )
            .await
            .map_err(|error| rpc_status("workflow upsert event trigger", error))?;
        Ok(GrpcResponse::new(
            bound_workflow_event_trigger_to_proto(trigger)
                .map_err(|error| rpc_status("workflow upsert event trigger", error))?,
        ))
    }

    async fn get_event_trigger(
        &self,
        request: GrpcRequest<pb::GetWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowEventTrigger>, Status> {
        let trigger = self
            .provider
            .get_event_trigger({
                let request = request.into_inner();
                GetWorkflowProviderEventTriggerRequest {
                    trigger_id: request.trigger_id,
                }
            })
            .await
            .map_err(|error| rpc_status("workflow get event trigger", error))?;
        Ok(GrpcResponse::new(
            bound_workflow_event_trigger_to_proto(trigger)
                .map_err(|error| rpc_status("workflow get event trigger", error))?,
        ))
    }

    async fn list_event_triggers(
        &self,
        request: GrpcRequest<pb::ListWorkflowProviderEventTriggersRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListWorkflowProviderEventTriggersResponse>, Status>
    {
        let response = self
            .provider
            .list_event_triggers({
                let _request = request.into_inner();
                ListWorkflowProviderEventTriggersRequest {}
            })
            .await
            .map_err(|error| rpc_status("workflow list event triggers", error))?;
        Ok(GrpcResponse::new(
            list_event_triggers_response_to_proto(response)
                .map_err(|error| rpc_status("workflow list event triggers", error))?,
        ))
    }

    async fn delete_event_trigger(
        &self,
        request: GrpcRequest<pb::DeleteWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .delete_event_trigger({
                let request = request.into_inner();
                DeleteWorkflowProviderEventTriggerRequest {
                    trigger_id: request.trigger_id,
                }
            })
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
            .pause_event_trigger({
                let request = request.into_inner();
                PauseWorkflowProviderEventTriggerRequest {
                    trigger_id: request.trigger_id,
                }
            })
            .await
            .map_err(|error| rpc_status("workflow pause event trigger", error))?;
        Ok(GrpcResponse::new(
            bound_workflow_event_trigger_to_proto(trigger)
                .map_err(|error| rpc_status("workflow pause event trigger", error))?,
        ))
    }

    async fn resume_event_trigger(
        &self,
        request: GrpcRequest<pb::ResumeWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowEventTrigger>, Status> {
        let trigger = self
            .provider
            .resume_event_trigger({
                let request = request.into_inner();
                ResumeWorkflowProviderEventTriggerRequest {
                    trigger_id: request.trigger_id,
                }
            })
            .await
            .map_err(|error| rpc_status("workflow resume event trigger", error))?;
        Ok(GrpcResponse::new(
            bound_workflow_event_trigger_to_proto(trigger)
                .map_err(|error| rpc_status("workflow resume event trigger", error))?,
        ))
    }

    async fn publish_event(
        &self,
        request: GrpcRequest<pb::PublishWorkflowProviderEventRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowEvent>, Status> {
        let event = self
            .provider
            .publish_event(
                publish_event_request_from_proto(request.into_inner())
                    .map_err(|error| rpc_status("workflow publish event", error))?,
            )
            .await
            .map_err(|error| rpc_status("workflow publish event", error))?;
        Ok(GrpcResponse::new(workflow_event_to_proto(event).map_err(
            |error| rpc_status("workflow publish event", error),
        )?))
    }

    async fn put_execution_reference(
        &self,
        request: GrpcRequest<pb::PutWorkflowExecutionReferenceRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowExecutionReference>, Status> {
        let reference = self
            .provider
            .put_execution_reference({
                let request = request.into_inner();
                PutWorkflowExecutionReferenceRequest {
                    reference: request
                        .reference
                        .map(workflow_execution_reference_from_proto)
                        .transpose()
                        .map_err(|error| rpc_status("workflow put execution reference", error))?,
                }
            })
            .await
            .map_err(|error| rpc_status("workflow put execution reference", error))?;
        Ok(GrpcResponse::new(
            workflow_execution_reference_to_proto(reference)
                .map_err(|error| rpc_status("workflow put execution reference", error))?,
        ))
    }

    async fn get_execution_reference(
        &self,
        request: GrpcRequest<pb::GetWorkflowExecutionReferenceRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowExecutionReference>, Status> {
        let reference = self
            .provider
            .get_execution_reference({
                let request = request.into_inner();
                GetWorkflowExecutionReferenceRequest { id: request.id }
            })
            .await
            .map_err(|error| rpc_status("workflow get execution reference", error))?;
        Ok(GrpcResponse::new(
            workflow_execution_reference_to_proto(reference)
                .map_err(|error| rpc_status("workflow get execution reference", error))?,
        ))
    }

    async fn list_execution_references(
        &self,
        request: GrpcRequest<pb::ListWorkflowExecutionReferencesRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListWorkflowExecutionReferencesResponse>, Status>
    {
        let response = self
            .provider
            .list_execution_references({
                let request = request.into_inner();
                ListWorkflowExecutionReferencesRequest {
                    subject_id: request.subject_id,
                }
            })
            .await
            .map_err(|error| rpc_status("workflow list execution references", error))?;
        Ok(GrpcResponse::new(
            list_execution_references_response_to_proto(response)
                .map_err(|error| rpc_status("workflow list execution references", error))?,
        ))
    }
}
