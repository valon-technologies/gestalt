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

use crate::agent::{AgentToolRef as NativeAgentToolRef, agent_tool_ref_to_proto};
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
#[doc(hidden)]
pub const ENV_WORKFLOW_HOST_SOCKET: &str = "GESTALT_WORKFLOW_HOST_SOCKET";
/// Environment variable containing the optional workflow-host relay token.
#[doc(hidden)]
pub const ENV_WORKFLOW_HOST_SOCKET_TOKEN: &str = "GESTALT_WORKFLOW_HOST_SOCKET_TOKEN";
const WORKFLOW_HOST_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

/// Native JSON value used by authored workflow providers.
pub type WorkflowJson = serde_json::Value;

pub use crate::generated::v1::{
    ApplyWorkflowDefinitionRequest, BoundWorkflowTarget, CancelWorkflowRunRequest,
    DeleteWorkflowDefinitionRequest, DeliverWorkflowEventRequest, DeliverWorkflowEventResponse,
    GetWorkflowDefinitionRequest, GetWorkflowExecutionReferenceRequest,
    GetWorkflowRunEventsRequest, GetWorkflowRunOutputRequest, GetWorkflowRunRequest,
    InvokeWorkflowActionRequest, ListWorkflowDefinitionsRequest, ListWorkflowDefinitionsResponse,
    ListWorkflowExecutionReferencesRequest, ListWorkflowExecutionReferencesResponse,
    ListWorkflowRunEventsResponse, ListWorkflowRunsRequest, ListWorkflowRunsResponse,
    ManagedWorkflowDefinition, ManagedWorkflowRun, ManagedWorkflowRunSignal,
    SetWorkflowActivationPausedRequest, SetWorkflowDefinitionPausedRequest,
    SignalOrStartWorkflowRunRequest, SignalWorkflowRunRequest, StartWorkflowRunRequest,
    WorkflowAccessPermission, WorkflowActionDescriptor, WorkflowActionKind, WorkflowActionResult,
    WorkflowActionTable, WorkflowActivation,
    WorkflowActivationMode, WorkflowActor, WorkflowAgentMessage, WorkflowAgentTurnPayload,
    WorkflowDefinition, WorkflowDefinitionBinding, WorkflowDefinitionSpec,
    WorkflowDefinitionStatus, WorkflowEvent, WorkflowEventActivation, WorkflowEventDeliveryResult,
    WorkflowEventMatch, WorkflowEventTrigger, WorkflowExecutionReference,
    WorkflowHostActionSelector, WorkflowManualActivation, WorkflowManualTrigger,
    WorkflowOutputSummary, WorkflowPathSource, WorkflowPluginActionPayload, WorkflowRun,
    WorkflowRunAsSubject, WorkflowRunError, WorkflowRunEvent, WorkflowRunEventType,
    WorkflowRunOutput, WorkflowRunSignal, WorkflowRunStatus, WorkflowRunTrigger,
    WorkflowScheduleActivation, WorkflowScheduleTrigger, WorkflowSignal, WorkflowStep,
    WorkflowStepAgentTurn, WorkflowStepDelivery, WorkflowStepOutputSource, WorkflowStepPluginCall,
    WorkflowStepState, WorkflowStepStatus, WorkflowStepWhen, WorkflowText, WorkflowValue,
    invoke_workflow_action_request, workflow_activation, workflow_run_trigger, workflow_step,
    workflow_value,
};

/// Workflow agent-tool reference used inside workflow agent steps.
pub type WorkflowAgentToolRef = pb::AgentToolRef;

/// Converts any JSON-object-like value into the workflow struct payload.
pub fn workflow_struct<T: Serialize>(value: T) -> ProviderResult<prost_types::Struct> {
    protocol::struct_from_json(protocol::json_from_serializable(value)?)
}

/// Converts a workflow struct payload into a JSON object.
pub fn workflow_json_from_struct(value: &prost_types::Struct) -> WorkflowJson {
    protocol::json_from_struct(value)
}

/// Converts a `SystemTime` into the workflow timestamp payload.
pub fn workflow_timestamp_from_system_time(value: SystemTime) -> prost_types::Timestamp {
    protocol::timestamp_from_system_time(value)
}

/// Converts a workflow timestamp payload into a `SystemTime`.
pub fn workflow_system_time_from_timestamp(
    value: &prost_types::Timestamp,
) -> ProviderResult<SystemTime> {
    protocol::system_time_from_timestamp(value)
}

/// Creates workflow text from a template string.
pub fn workflow_text(template: impl Into<String>) -> WorkflowText {
    WorkflowText {
        template: template.into(),
    }
}

/// Creates a literal workflow value from any JSON-compatible value.
pub fn workflow_value_literal<T: Serialize>(value: T) -> ProviderResult<WorkflowValue> {
    Ok(WorkflowValue {
        kind: Some(workflow_value::Kind::Literal(protocol::value_from_json(
            protocol::json_from_serializable(value)?,
        ))),
    })
}

/// Creates an object workflow value.
pub fn workflow_value_object(
    fields: impl IntoIterator<Item = (impl Into<String>, WorkflowValue)>,
) -> WorkflowValue {
    WorkflowValue {
        kind: Some(workflow_value::Kind::Object(pb::WorkflowObject {
            fields: fields
                .into_iter()
                .map(|(key, value)| (key.into(), value))
                .collect(),
        })),
    }
}

/// Creates an array workflow value.
pub fn workflow_value_array(values: impl IntoIterator<Item = WorkflowValue>) -> WorkflowValue {
    WorkflowValue {
        kind: Some(workflow_value::Kind::Array(pb::WorkflowArray {
            values: values.into_iter().collect(),
        })),
    }
}

/// Creates a template workflow value.
pub fn workflow_value_template(template: impl Into<String>) -> WorkflowValue {
    WorkflowValue {
        kind: Some(workflow_value::Kind::Template(workflow_text(template))),
    }
}

/// Reads a value from the workflow run input.
pub fn workflow_value_run_input(path: impl Into<String>) -> WorkflowValue {
    workflow_path_value(workflow_value::Kind::RunInput, path)
}

/// Reads a value from the current signal payload.
pub fn workflow_value_signal_payload(path: impl Into<String>) -> WorkflowValue {
    workflow_path_value(workflow_value::Kind::SignalPayload, path)
}

/// Reads a value from another step's output.
pub fn workflow_value_step_output(
    step_id: impl Into<String>,
    path: impl Into<String>,
) -> WorkflowValue {
    WorkflowValue {
        kind: Some(workflow_value::Kind::StepOutput(WorkflowStepOutputSource {
            step_id: step_id.into(),
            path: path.into(),
        })),
    }
}

fn workflow_path_value(
    ctor: fn(WorkflowPathSource) -> workflow_value::Kind,
    path: impl Into<String>,
) -> WorkflowValue {
    WorkflowValue {
        kind: Some(ctor(WorkflowPathSource { path: path.into() })),
    }
}

/// Converts a native agent tool reference into the generated workflow tool-ref shape.
pub fn workflow_agent_tool_ref(input: NativeAgentToolRef) -> WorkflowAgentToolRef {
    agent_tool_ref_to_proto(input)
}

impl BoundWorkflowTarget {
    /// Creates a target from ordered workflow steps.
    pub fn from_steps(steps: impl IntoIterator<Item = WorkflowStep>) -> Self {
        Self {
            steps: steps.into_iter().collect(),
        }
    }
}

impl WorkflowStep {
    /// Adds metadata from any JSON-object-like serializable value.
    pub fn with_metadata<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.metadata = Some(workflow_struct(value)?);
        Ok(self)
    }
}

impl WorkflowStepPluginCall {
    /// Sets the plugin input to a literal workflow value.
    pub fn with_input<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.input = Some(workflow_value_literal(value)?);
        Ok(self)
    }
}

impl WorkflowStepAgentTurn {
    /// Adds native SDK agent tool refs to this workflow step.
    pub fn with_tools(mut self, tools: impl IntoIterator<Item = NativeAgentToolRef>) -> Self {
        self.tools = tools.into_iter().map(workflow_agent_tool_ref).collect();
        self
    }

    /// Sets the response schema from any JSON-object-like serializable value.
    pub fn with_response_schema<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.response_schema = Some(workflow_struct(value)?);
        Ok(self)
    }

    /// Sets model options from any JSON-object-like serializable value.
    pub fn with_model_options<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.model_options = Some(workflow_struct(value)?);
        Ok(self)
    }
}

impl WorkflowAgentMessage {
    /// Creates a workflow agent message with template text.
    pub fn text(role: impl Into<String>, template: impl Into<String>) -> Self {
        Self {
            role: role.into(),
            text: Some(workflow_text(template)),
            metadata: None,
        }
    }

    /// Adds metadata from any JSON-object-like serializable value.
    pub fn with_metadata<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.metadata = Some(workflow_struct(value)?);
        Ok(self)
    }
}

impl WorkflowEvent {
    /// Sets event data from any JSON-object-like serializable value.
    pub fn with_data<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.data = Some(workflow_struct(value)?);
        Ok(self)
    }

    /// Adds an extension value from any JSON-compatible serializable value.
    pub fn with_extension<T: Serialize>(
        mut self,
        key: impl Into<String>,
        value: T,
    ) -> ProviderResult<Self> {
        self.extensions.insert(
            key.into(),
            protocol::value_from_json(protocol::json_from_serializable(value)?),
        );
        Ok(self)
    }
}

impl WorkflowSignal {
    /// Sets the signal payload from any JSON-object-like serializable value.
    pub fn with_payload<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.payload = Some(workflow_struct(value)?);
        Ok(self)
    }

    /// Sets signal metadata from any JSON-object-like serializable value.
    pub fn with_metadata<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.metadata = Some(workflow_struct(value)?);
        Ok(self)
    }
}

impl StartWorkflowRunRequest {
    /// Sets run input from any JSON-object-like serializable value.
    pub fn with_input<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.input = Some(workflow_struct(value)?);
        Ok(self)
    }
}

impl SignalOrStartWorkflowRunRequest {
    /// Sets run input from any JSON-object-like serializable value.
    pub fn with_input<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.input = Some(workflow_struct(value)?);
        Ok(self)
    }
}

impl WorkflowPluginActionPayload {
    /// Sets plugin action input from any JSON-object-like serializable value.
    pub fn with_input<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.input = Some(workflow_struct(value)?);
        Ok(self)
    }
}

impl InvokeWorkflowActionRequest {
    /// Sets invocation metadata from any JSON-object-like serializable value.
    pub fn with_metadata<T: Serialize>(mut self, value: T) -> ProviderResult<Self> {
        self.metadata = Some(workflow_struct(value)?);
        Ok(self)
    }
}

/// Returns a workflow target after validating builder-owned nested values.
pub fn new_bound_workflow_target(
    input: BoundWorkflowTarget,
) -> ProviderResult<BoundWorkflowTarget> {
    Ok(input)
}

/// Returns a deep copy of a workflow target.
pub fn new_bound_workflow_target_from_target(input: &BoundWorkflowTarget) -> BoundWorkflowTarget {
    input.clone()
}

/// Returns a workflow value after validating builder-owned nested values.
pub fn new_workflow_value(input: WorkflowValue) -> ProviderResult<WorkflowValue> {
    Ok(input)
}

/// Returns a workflow event after validating builder-owned nested values.
pub fn new_workflow_event(input: WorkflowEvent) -> ProviderResult<WorkflowEvent> {
    Ok(input)
}

/// Returns a deep copy of a workflow event.
pub fn new_workflow_event_from_event(input: &WorkflowEvent) -> WorkflowEvent {
    input.clone()
}

/// Returns a workflow signal after validating builder-owned nested values.
pub fn new_workflow_signal(input: WorkflowSignal) -> ProviderResult<WorkflowSignal> {
    Ok(input)
}

/// Returns a deep copy of a workflow signal.
pub fn new_workflow_signal_from_signal(input: &WorkflowSignal) -> WorkflowSignal {
    input.clone()
}

/// Returns a workflow definition spec after validating builder-owned nested values.
pub fn new_workflow_definition_spec(
    input: WorkflowDefinitionSpec,
) -> ProviderResult<WorkflowDefinitionSpec> {
    Ok(input)
}

/// Returns a workflow activation after validating builder-owned nested values.
pub fn new_workflow_activation(input: WorkflowActivation) -> ProviderResult<WorkflowActivation> {
    Ok(input)
}

/// Returns a workflow run after validating builder-owned nested values.
pub fn new_workflow_run(input: WorkflowRun) -> ProviderResult<WorkflowRun> {
    Ok(input)
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

/// Client for invoking workflow actions from workflow provider code.
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

    /// Invokes one workflow action through the workflow host service.
    pub async fn invoke_action(
        &mut self,
        input: InvokeWorkflowActionRequest,
    ) -> std::result::Result<WorkflowActionResult, WorkflowHostError> {
        Ok(self
            .client
            .invoke_workflow_action(input)
            .await?
            .into_inner())
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

    /// Applies a workflow definition.
    async fn apply_workflow_definition(
        &self,
        _request: ApplyWorkflowDefinitionRequest,
    ) -> ProviderResult<WorkflowDefinition> {
        Err(crate::Error::unimplemented(
            "workflow definition apply is not implemented",
        ))
    }

    /// Returns one workflow definition.
    async fn get_workflow_definition(
        &self,
        _request: GetWorkflowDefinitionRequest,
    ) -> ProviderResult<WorkflowDefinition> {
        Err(crate::Error::unimplemented(
            "workflow definition get is not implemented",
        ))
    }

    /// Lists workflow definitions.
    async fn list_workflow_definitions(
        &self,
        _request: ListWorkflowDefinitionsRequest,
    ) -> ProviderResult<ListWorkflowDefinitionsResponse> {
        Err(crate::Error::unimplemented(
            "workflow definition list is not implemented",
        ))
    }

    /// Deletes a workflow definition.
    async fn delete_workflow_definition(
        &self,
        _request: DeleteWorkflowDefinitionRequest,
    ) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "workflow definition delete is not implemented",
        ))
    }

    /// Pauses or resumes a workflow definition.
    async fn set_workflow_definition_paused(
        &self,
        _request: SetWorkflowDefinitionPausedRequest,
    ) -> ProviderResult<WorkflowDefinition> {
        Err(crate::Error::unimplemented(
            "workflow definition pause is not implemented",
        ))
    }

    /// Pauses or resumes one workflow activation.
    async fn set_workflow_activation_paused(
        &self,
        _request: SetWorkflowActivationPausedRequest,
    ) -> ProviderResult<WorkflowDefinition> {
        Err(crate::Error::unimplemented(
            "workflow activation pause is not implemented",
        ))
    }

    /// Starts or idempotently returns a workflow run.
    async fn start_workflow_run(
        &self,
        _request: StartWorkflowRunRequest,
    ) -> ProviderResult<WorkflowRun> {
        Err(crate::Error::unimplemented(
            "workflow start run is not implemented",
        ))
    }

    /// Delivers a signal to an existing workflow run.
    async fn signal_workflow_run(
        &self,
        _request: SignalWorkflowRunRequest,
    ) -> ProviderResult<WorkflowRunSignal> {
        Err(crate::Error::unimplemented(
            "workflow signal run is not implemented",
        ))
    }

    /// Delivers a signal or starts a run when no target run exists.
    async fn signal_or_start_workflow_run(
        &self,
        _request: SignalOrStartWorkflowRunRequest,
    ) -> ProviderResult<WorkflowRunSignal> {
        Err(crate::Error::unimplemented(
            "workflow signal or start run is not implemented",
        ))
    }

    /// Requests cancellation of a pending or running workflow run.
    async fn cancel_workflow_run(
        &self,
        _request: CancelWorkflowRunRequest,
    ) -> ProviderResult<WorkflowRun> {
        Err(crate::Error::unimplemented(
            "workflow cancel run is not implemented",
        ))
    }

    /// Delivers an event for workflow activation matching.
    async fn deliver_workflow_event(
        &self,
        _request: DeliverWorkflowEventRequest,
    ) -> ProviderResult<DeliverWorkflowEventResponse> {
        Err(crate::Error::unimplemented(
            "workflow deliver event is not implemented",
        ))
    }

    /// Returns one workflow run by ID.
    async fn get_workflow_run(
        &self,
        _request: GetWorkflowRunRequest,
    ) -> ProviderResult<WorkflowRun> {
        Err(crate::Error::unimplemented(
            "workflow get run is not implemented",
        ))
    }

    /// Lists workflow runs.
    async fn list_workflow_runs(
        &self,
        _request: ListWorkflowRunsRequest,
    ) -> ProviderResult<ListWorkflowRunsResponse> {
        Err(crate::Error::unimplemented(
            "workflow list runs is not implemented",
        ))
    }

    /// Lists workflow run events.
    async fn get_workflow_run_events(
        &self,
        _request: GetWorkflowRunEventsRequest,
    ) -> ProviderResult<ListWorkflowRunEventsResponse> {
        Err(crate::Error::unimplemented(
            "workflow get run events is not implemented",
        ))
    }

    /// Returns a workflow run output body.
    async fn get_workflow_run_output(
        &self,
        _request: GetWorkflowRunOutputRequest,
    ) -> ProviderResult<WorkflowRunOutput> {
        Err(crate::Error::unimplemented(
            "workflow get run output is not implemented",
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

    /// Lists workflow execution references for one subject.
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
    provider: std::sync::Arc<P>,
}

impl<P> WorkflowServer<P> {
    pub(crate) fn new(provider: std::sync::Arc<P>) -> Self {
        Self { provider }
    }
}

#[async_trait]
impl<P> pb::workflow_provider_server::WorkflowProvider for WorkflowServer<P>
where
    P: WorkflowProvider,
{
    async fn apply_workflow_definition(
        &self,
        request: GrpcRequest<ApplyWorkflowDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowDefinition>, Status> {
        let response = self
            .provider
            .apply_workflow_definition(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow definition apply", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn get_workflow_definition(
        &self,
        request: GrpcRequest<GetWorkflowDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowDefinition>, Status> {
        let response = self
            .provider
            .get_workflow_definition(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow definition get", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn list_workflow_definitions(
        &self,
        request: GrpcRequest<ListWorkflowDefinitionsRequest>,
    ) -> std::result::Result<GrpcResponse<ListWorkflowDefinitionsResponse>, Status> {
        let response = self
            .provider
            .list_workflow_definitions(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow definition list", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn delete_workflow_definition(
        &self,
        request: GrpcRequest<DeleteWorkflowDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .delete_workflow_definition(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow definition delete", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn set_workflow_definition_paused(
        &self,
        request: GrpcRequest<SetWorkflowDefinitionPausedRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowDefinition>, Status> {
        let response = self
            .provider
            .set_workflow_definition_paused(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow definition pause", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn set_workflow_activation_paused(
        &self,
        request: GrpcRequest<SetWorkflowActivationPausedRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowDefinition>, Status> {
        let response = self
            .provider
            .set_workflow_activation_paused(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow activation pause", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn start_workflow_run(
        &self,
        request: GrpcRequest<StartWorkflowRunRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowRun>, Status> {
        let response = self
            .provider
            .start_workflow_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow start run", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn signal_workflow_run(
        &self,
        request: GrpcRequest<SignalWorkflowRunRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowRunSignal>, Status> {
        let response = self
            .provider
            .signal_workflow_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow signal run", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn signal_or_start_workflow_run(
        &self,
        request: GrpcRequest<SignalOrStartWorkflowRunRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowRunSignal>, Status> {
        let response = self
            .provider
            .signal_or_start_workflow_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow signal or start run", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn cancel_workflow_run(
        &self,
        request: GrpcRequest<CancelWorkflowRunRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowRun>, Status> {
        let response = self
            .provider
            .cancel_workflow_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow cancel run", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn deliver_workflow_event(
        &self,
        request: GrpcRequest<DeliverWorkflowEventRequest>,
    ) -> std::result::Result<GrpcResponse<DeliverWorkflowEventResponse>, Status> {
        let response = self
            .provider
            .deliver_workflow_event(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow deliver event", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn get_workflow_run(
        &self,
        request: GrpcRequest<GetWorkflowRunRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowRun>, Status> {
        let response = self
            .provider
            .get_workflow_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow get run", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn list_workflow_runs(
        &self,
        request: GrpcRequest<ListWorkflowRunsRequest>,
    ) -> std::result::Result<GrpcResponse<ListWorkflowRunsResponse>, Status> {
        let response = self
            .provider
            .list_workflow_runs(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow list runs", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn get_workflow_run_events(
        &self,
        request: GrpcRequest<GetWorkflowRunEventsRequest>,
    ) -> std::result::Result<GrpcResponse<ListWorkflowRunEventsResponse>, Status> {
        let response = self
            .provider
            .get_workflow_run_events(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow get run events", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn get_workflow_run_output(
        &self,
        request: GrpcRequest<GetWorkflowRunOutputRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowRunOutput>, Status> {
        let response = self
            .provider
            .get_workflow_run_output(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow get run output", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn get_execution_reference(
        &self,
        request: GrpcRequest<GetWorkflowExecutionReferenceRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowExecutionReference>, Status> {
        let response = self
            .provider
            .get_execution_reference(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow get execution reference", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn list_execution_references(
        &self,
        request: GrpcRequest<ListWorkflowExecutionReferencesRequest>,
    ) -> std::result::Result<GrpcResponse<ListWorkflowExecutionReferencesResponse>, Status> {
        let response = self
            .provider
            .list_execution_references(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow list execution references", error))?;
        Ok(GrpcResponse::new(response))
    }
}
