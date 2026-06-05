use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::Request as GrpcRequest;
use tonic::codegen::async_trait;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tower::service_fn;

use crate::env::{ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN};
use crate::generated::v1::{
    self as pb, workflow_provider_client::WorkflowProviderClient as ProtoWorkflowProviderClient,
};
use crate::workflow::{
    WorkflowDefinition, WorkflowDefinitionSpec, WorkflowEvent, WorkflowJson, WorkflowRun,
    WorkflowRunStatus, WorkflowSignal, workflow_event_from_proto, workflow_run_from_proto,
    workflow_run_signal_from_proto, workflow_subject_to_proto,
};
use crate::{Request, Subject};

type WorkflowTransport = InterceptedService<Channel, RelayTokenInterceptor>;

const WORKFLOW_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

#[derive(Debug, thiserror::Error)]
/// Errors returned by [`Workflow`].
pub enum WorkflowError {
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

#[derive(Clone, Debug, Default)]
pub struct WorkflowApplyDefinition {
    pub provider_name: String,
    pub spec: Option<WorkflowDefinitionSpec>,
    pub idempotency_key: String,
    pub requested_by_subject_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowGetDefinition {
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowSetDefinitionPaused {
    pub definition_id: String,
    pub paused: bool,
    pub requested_by_subject_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowSetActivationPaused {
    pub definition_id: String,
    pub activation_id: String,
    pub paused: bool,
    pub requested_by_subject_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowDeleteDefinition {
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowStartRun {
    pub provider_name: String,
    pub workflow_key: String,
    pub definition_id: String,
    pub input: Option<WorkflowJson>,
    pub expected_definition_generation: i64,
    pub idempotency_key: String,
    pub created_by_subject_id: String,
    pub run_as: Option<Subject>,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowListRuns {
    pub page_size: i32,
    pub page_token: String,
    pub status: WorkflowRunStatus,
    pub target_app: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowGetRun {
    pub run_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowGetRunEvents {
    pub run_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowGetRunOutput {
    pub run_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowCancelRun {
    pub run_id: String,
    pub reason: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowSignalRun {
    pub run_id: String,
    pub signal: Option<WorkflowSignal>,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowSignalOrStartRun {
    pub provider_name: String,
    pub workflow_key: String,
    pub definition_id: String,
    pub input: Option<WorkflowJson>,
    pub expected_definition_generation: i64,
    pub idempotency_key: String,
    pub created_by_subject_id: String,
    pub signal: Option<WorkflowSignal>,
    pub run_as: Option<Subject>,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowDeliverEvent {
    pub provider_name: String,
    pub app_name: String,
    pub event: Option<WorkflowEvent>,
    pub delivered_by_subject_id: String,
}

#[async_trait]
/// Fakeable client contract for workflow calls.
pub trait WorkflowContract: Send {
    async fn apply_definition(
        &mut self,
        input: WorkflowApplyDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError>;
    async fn get_definition(
        &mut self,
        input: WorkflowGetDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError>;
    async fn list_definitions(
        &mut self,
    ) -> std::result::Result<pb::ListWorkflowProviderDefinitionsResponse, WorkflowError>;
    async fn set_definition_paused(
        &mut self,
        input: WorkflowSetDefinitionPaused,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError>;
    async fn set_activation_paused(
        &mut self,
        input: WorkflowSetActivationPaused,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError>;
    async fn delete_definition(
        &mut self,
        input: WorkflowDeleteDefinition,
    ) -> std::result::Result<(), WorkflowError>;
    async fn start_run(
        &mut self,
        input: WorkflowStartRun,
    ) -> std::result::Result<WorkflowRun, WorkflowError>;
    async fn list_runs(
        &mut self,
        input: WorkflowListRuns,
    ) -> std::result::Result<pb::ListWorkflowProviderRunsResponse, WorkflowError>;
    async fn get_run(
        &mut self,
        input: WorkflowGetRun,
    ) -> std::result::Result<WorkflowRun, WorkflowError>;
    async fn get_run_events(
        &mut self,
        input: WorkflowGetRunEvents,
    ) -> std::result::Result<pb::GetWorkflowProviderRunEventsResponse, WorkflowError>;
    async fn get_run_output(
        &mut self,
        input: WorkflowGetRunOutput,
    ) -> std::result::Result<pb::GetWorkflowProviderRunOutputResponse, WorkflowError>;
    async fn cancel_run(
        &mut self,
        input: WorkflowCancelRun,
    ) -> std::result::Result<WorkflowRun, WorkflowError>;
    async fn signal_run(
        &mut self,
        input: WorkflowSignalRun,
    ) -> std::result::Result<pb::SignalWorkflowRunResponse, WorkflowError>;
    async fn signal_or_start_run(
        &mut self,
        input: WorkflowSignalOrStartRun,
    ) -> std::result::Result<pb::SignalWorkflowRunResponse, WorkflowError>;
    async fn deliver_event(
        &mut self,
        input: WorkflowDeliverEvent,
    ) -> std::result::Result<WorkflowEvent, WorkflowError>;
}

pub(crate) fn new_workflow_apply_definition_request(
    input: WorkflowApplyDefinition,
) -> pb::ApplyWorkflowProviderDefinitionRequest {
    pb::ApplyWorkflowProviderDefinitionRequest {
        provider_name: input.provider_name,
        spec: input.spec,
        context: None,
        idempotency_key: input.idempotency_key,
        requested_by_subject_id: input.requested_by_subject_id,
    }
}

pub(crate) fn new_workflow_get_definition_request(
    input: WorkflowGetDefinition,
) -> pb::GetWorkflowProviderDefinitionRequest {
    pb::GetWorkflowProviderDefinitionRequest {
        definition_id: input.definition_id,
        context: None,
    }
}

pub(crate) fn new_workflow_set_definition_paused_request(
    input: WorkflowSetDefinitionPaused,
) -> pb::SetWorkflowProviderDefinitionPausedRequest {
    pb::SetWorkflowProviderDefinitionPausedRequest {
        definition_id: input.definition_id,
        paused: input.paused,
        context: None,
        requested_by_subject_id: input.requested_by_subject_id,
    }
}

pub(crate) fn new_workflow_set_activation_paused_request(
    input: WorkflowSetActivationPaused,
) -> pb::SetWorkflowProviderActivationPausedRequest {
    pb::SetWorkflowProviderActivationPausedRequest {
        definition_id: input.definition_id,
        activation_id: input.activation_id,
        paused: input.paused,
        context: None,
        requested_by_subject_id: input.requested_by_subject_id,
    }
}

pub(crate) fn new_workflow_delete_definition_request(
    input: WorkflowDeleteDefinition,
) -> pb::DeleteWorkflowProviderDefinitionRequest {
    pb::DeleteWorkflowProviderDefinitionRequest {
        definition_id: input.definition_id,
        context: None,
    }
}

pub(crate) fn new_workflow_start_run_request(
    input: WorkflowStartRun,
) -> crate::Result<pb::StartWorkflowProviderRunRequest> {
    Ok(pb::StartWorkflowProviderRunRequest {
        provider_name: input.provider_name,
        idempotency_key: input.idempotency_key,
        created_by_subject_id: input.created_by_subject_id,
        workflow_key: input.workflow_key,
        context: None,
        definition_id: input.definition_id,
        run_as: input.run_as.map(workflow_subject_to_proto),
        input: input
            .input
            .map(crate::protocol::struct_from_json)
            .transpose()?,
        expected_definition_generation: input.expected_definition_generation,
    })
}

pub(crate) fn new_workflow_list_runs_request(
    input: WorkflowListRuns,
) -> pb::ListWorkflowProviderRunsRequest {
    pb::ListWorkflowProviderRunsRequest {
        page_size: input.page_size,
        page_token: input.page_token,
        status: input.status as i32,
        context: None,
        target_app: input.target_app,
    }
}

pub(crate) fn new_workflow_get_run_request(
    input: WorkflowGetRun,
) -> pb::GetWorkflowProviderRunRequest {
    pb::GetWorkflowProviderRunRequest {
        run_id: input.run_id,
        context: None,
    }
}

pub(crate) fn new_workflow_get_run_events_request(
    input: WorkflowGetRunEvents,
) -> pb::GetWorkflowProviderRunEventsRequest {
    pb::GetWorkflowProviderRunEventsRequest {
        run_id: input.run_id,
        context: None,
    }
}

pub(crate) fn new_workflow_get_run_output_request(
    input: WorkflowGetRunOutput,
) -> pb::GetWorkflowProviderRunOutputRequest {
    pb::GetWorkflowProviderRunOutputRequest {
        run_id: input.run_id,
        context: None,
    }
}

pub(crate) fn new_workflow_cancel_run_request(
    input: WorkflowCancelRun,
) -> pb::CancelWorkflowProviderRunRequest {
    pb::CancelWorkflowProviderRunRequest {
        run_id: input.run_id,
        reason: input.reason,
        context: None,
    }
}

pub(crate) fn new_workflow_signal_run_request(
    input: WorkflowSignalRun,
) -> pb::SignalWorkflowProviderRunRequest {
    pb::SignalWorkflowProviderRunRequest {
        run_id: input.run_id,
        signal: input.signal,
        context: None,
    }
}

pub(crate) fn new_workflow_signal_or_start_run_request(
    input: WorkflowSignalOrStartRun,
) -> crate::Result<pb::SignalOrStartWorkflowProviderRunRequest> {
    Ok(pb::SignalOrStartWorkflowProviderRunRequest {
        provider_name: input.provider_name,
        workflow_key: input.workflow_key,
        idempotency_key: input.idempotency_key,
        created_by_subject_id: input.created_by_subject_id,
        signal: input.signal,
        context: None,
        definition_id: input.definition_id,
        run_as: input.run_as.map(workflow_subject_to_proto),
        input: input
            .input
            .map(crate::protocol::struct_from_json)
            .transpose()?,
        expected_definition_generation: input.expected_definition_generation,
    })
}

pub(crate) fn new_workflow_deliver_event_request(
    input: WorkflowDeliverEvent,
) -> pb::DeliverWorkflowProviderEventRequest {
    pb::DeliverWorkflowProviderEventRequest {
        app_name: input.app_name,
        event: input.event,
        delivered_by_subject_id: input.delivered_by_subject_id,
        context: None,
        provider_name: input.provider_name,
    }
}

/// Client for applying workflow definitions, starting runs, signaling, and delivering events.
pub struct Workflow {
    client: ProtoWorkflowProviderClient<WorkflowTransport>,
    context: Option<pb::RequestContext>,
    idempotency_key: String,
}

impl Workflow {
    /// Connects to the workflow service with host request context from Gestalt.
    pub async fn connect(request: &Request) -> std::result::Result<Self, WorkflowError> {
        let socket_path = std::env::var(ENV_HOST_SERVICE_SOCKET)
            .map_err(|_| WorkflowError::Env(format!("{ENV_HOST_SERVICE_SOCKET} is not set")))?;
        let relay_token = std::env::var(ENV_HOST_SERVICE_TOKEN).unwrap_or_default();
        let channel = match parse_workflow_target(&socket_path)? {
            WorkflowTarget::Unix(path) => {
                Endpoint::try_from("http://[::]:50051")?
                    .connect_with_connector(service_fn(move |_: Uri| {
                        let path = path.clone();
                        async move { UnixStream::connect(path).await.map(TokioIo::new) }
                    }))
                    .await?
            }
            WorkflowTarget::Tcp(address) => {
                Endpoint::from_shared(format!("http://{address}"))?
                    .connect()
                    .await?
            }
            WorkflowTarget::Tls(address) => {
                Endpoint::from_shared(format!("https://{address}"))?
                    .tls_config(ClientTlsConfig::new().with_native_roots())?
                    .connect()
                    .await?
            }
        };

        Ok(Self {
            client: ProtoWorkflowProviderClient::with_interceptor(
                channel,
                relay_token_interceptor(relay_token.trim())?,
            ),
            context: request.context.clone(),
            idempotency_key: request.idempotency_key.trim().to_owned(),
        })
    }

    pub async fn apply_definition(
        &mut self,
        input: WorkflowApplyDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError> {
        let mut request = new_workflow_apply_definition_request(input);
        self.attach_context(&mut request);
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(self.client.apply_definition(request).await?.into_inner())
    }

    pub async fn get_definition(
        &mut self,
        input: WorkflowGetDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError> {
        let mut request = new_workflow_get_definition_request(input);
        self.attach_context(&mut request);
        Ok(self.client.get_definition(request).await?.into_inner())
    }

    pub async fn list_definitions(
        &mut self,
    ) -> std::result::Result<pb::ListWorkflowProviderDefinitionsResponse, WorkflowError> {
        Ok(self
            .client
            .list_definitions(pb::ListWorkflowProviderDefinitionsRequest {
                context: self.context.clone(),
            })
            .await?
            .into_inner())
    }

    pub async fn set_definition_paused(
        &mut self,
        input: WorkflowSetDefinitionPaused,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError> {
        let mut request = new_workflow_set_definition_paused_request(input);
        self.attach_context(&mut request);
        Ok(self
            .client
            .set_definition_paused(request)
            .await?
            .into_inner())
    }

    pub async fn set_activation_paused(
        &mut self,
        input: WorkflowSetActivationPaused,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError> {
        let mut request = new_workflow_set_activation_paused_request(input);
        self.attach_context(&mut request);
        Ok(self
            .client
            .set_activation_paused(request)
            .await?
            .into_inner())
    }

    pub async fn delete_definition(
        &mut self,
        input: WorkflowDeleteDefinition,
    ) -> std::result::Result<(), WorkflowError> {
        let mut request = new_workflow_delete_definition_request(input);
        self.attach_context(&mut request);
        self.client.delete_definition(request).await?;
        Ok(())
    }

    pub async fn start_run(
        &mut self,
        input: WorkflowStartRun,
    ) -> std::result::Result<WorkflowRun, WorkflowError> {
        let mut request = new_workflow_start_run_request(input)?;
        self.attach_context(&mut request);
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(workflow_run_from_proto(
            self.client.start_run(request).await?.into_inner(),
        )?)
    }

    pub async fn list_runs(
        &mut self,
        input: WorkflowListRuns,
    ) -> std::result::Result<pb::ListWorkflowProviderRunsResponse, WorkflowError> {
        let mut request = new_workflow_list_runs_request(input);
        self.attach_context(&mut request);
        Ok(self.client.list_runs(request).await?.into_inner())
    }

    pub async fn get_run(
        &mut self,
        input: WorkflowGetRun,
    ) -> std::result::Result<WorkflowRun, WorkflowError> {
        let mut request = new_workflow_get_run_request(input);
        self.attach_context(&mut request);
        Ok(workflow_run_from_proto(
            self.client.get_run(request).await?.into_inner(),
        )?)
    }

    pub async fn get_run_events(
        &mut self,
        input: WorkflowGetRunEvents,
    ) -> std::result::Result<pb::GetWorkflowProviderRunEventsResponse, WorkflowError> {
        let mut request = new_workflow_get_run_events_request(input);
        self.attach_context(&mut request);
        Ok(self.client.get_run_events(request).await?.into_inner())
    }

    pub async fn get_run_output(
        &mut self,
        input: WorkflowGetRunOutput,
    ) -> std::result::Result<pb::GetWorkflowProviderRunOutputResponse, WorkflowError> {
        let mut request = new_workflow_get_run_output_request(input);
        self.attach_context(&mut request);
        Ok(self.client.get_run_output(request).await?.into_inner())
    }

    pub async fn cancel_run(
        &mut self,
        input: WorkflowCancelRun,
    ) -> std::result::Result<WorkflowRun, WorkflowError> {
        let mut request = new_workflow_cancel_run_request(input);
        self.attach_context(&mut request);
        Ok(workflow_run_from_proto(
            self.client.cancel_run(request).await?.into_inner(),
        )?)
    }

    pub async fn signal_run(
        &mut self,
        input: WorkflowSignalRun,
    ) -> std::result::Result<pb::SignalWorkflowRunResponse, WorkflowError> {
        let mut request = new_workflow_signal_run_request(input);
        self.attach_context(&mut request);
        Ok(workflow_run_signal_from_proto(
            self.client.signal_run(request).await?.into_inner(),
        )?)
    }

    pub async fn signal_or_start_run(
        &mut self,
        input: WorkflowSignalOrStartRun,
    ) -> std::result::Result<pb::SignalWorkflowRunResponse, WorkflowError> {
        let mut request = new_workflow_signal_or_start_run_request(input)?;
        self.attach_context(&mut request);
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(workflow_run_signal_from_proto(
            self.client.signal_or_start_run(request).await?.into_inner(),
        )?)
    }

    pub async fn deliver_event(
        &mut self,
        input: WorkflowDeliverEvent,
    ) -> std::result::Result<WorkflowEvent, WorkflowError> {
        let mut request = new_workflow_deliver_event_request(input);
        self.attach_context(&mut request);
        Ok(workflow_event_from_proto(
            self.client.deliver_event(request).await?.into_inner(),
        )?)
    }

    fn attach_context<T: HasWorkflowRequestContext>(&self, request: &mut T) {
        request.set_context(self.context.clone());
    }
}

trait HasWorkflowRequestContext {
    fn set_context(&mut self, context: Option<pb::RequestContext>);
}

macro_rules! impl_workflow_request_context {
    ($($ty:ty),+ $(,)?) => {
        $(
            impl HasWorkflowRequestContext for $ty {
                fn set_context(&mut self, context: Option<pb::RequestContext>) {
                    self.context = context;
                }
            }
        )+
    };
}

impl_workflow_request_context!(
    pb::ApplyWorkflowProviderDefinitionRequest,
    pb::GetWorkflowProviderDefinitionRequest,
    pb::SetWorkflowProviderDefinitionPausedRequest,
    pb::SetWorkflowProviderActivationPausedRequest,
    pb::DeleteWorkflowProviderDefinitionRequest,
    pb::StartWorkflowProviderRunRequest,
    pb::ListWorkflowProviderRunsRequest,
    pb::GetWorkflowProviderRunRequest,
    pb::GetWorkflowProviderRunEventsRequest,
    pb::GetWorkflowProviderRunOutputRequest,
    pb::CancelWorkflowProviderRunRequest,
    pb::SignalWorkflowProviderRunRequest,
    pb::SignalOrStartWorkflowProviderRunRequest,
    pb::DeliverWorkflowProviderEventRequest,
);

#[async_trait]
impl WorkflowContract for Workflow {
    async fn apply_definition(
        &mut self,
        input: WorkflowApplyDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError> {
        Workflow::apply_definition(self, input).await
    }

    async fn get_definition(
        &mut self,
        input: WorkflowGetDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError> {
        Workflow::get_definition(self, input).await
    }

    async fn list_definitions(
        &mut self,
    ) -> std::result::Result<pb::ListWorkflowProviderDefinitionsResponse, WorkflowError> {
        Workflow::list_definitions(self).await
    }

    async fn set_definition_paused(
        &mut self,
        input: WorkflowSetDefinitionPaused,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError> {
        Workflow::set_definition_paused(self, input).await
    }

    async fn set_activation_paused(
        &mut self,
        input: WorkflowSetActivationPaused,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError> {
        Workflow::set_activation_paused(self, input).await
    }

    async fn delete_definition(
        &mut self,
        input: WorkflowDeleteDefinition,
    ) -> std::result::Result<(), WorkflowError> {
        Workflow::delete_definition(self, input).await
    }

    async fn start_run(
        &mut self,
        input: WorkflowStartRun,
    ) -> std::result::Result<WorkflowRun, WorkflowError> {
        Workflow::start_run(self, input).await
    }

    async fn list_runs(
        &mut self,
        input: WorkflowListRuns,
    ) -> std::result::Result<pb::ListWorkflowProviderRunsResponse, WorkflowError> {
        Workflow::list_runs(self, input).await
    }

    async fn get_run(
        &mut self,
        input: WorkflowGetRun,
    ) -> std::result::Result<WorkflowRun, WorkflowError> {
        Workflow::get_run(self, input).await
    }

    async fn get_run_events(
        &mut self,
        input: WorkflowGetRunEvents,
    ) -> std::result::Result<pb::GetWorkflowProviderRunEventsResponse, WorkflowError> {
        Workflow::get_run_events(self, input).await
    }

    async fn get_run_output(
        &mut self,
        input: WorkflowGetRunOutput,
    ) -> std::result::Result<pb::GetWorkflowProviderRunOutputResponse, WorkflowError> {
        Workflow::get_run_output(self, input).await
    }

    async fn cancel_run(
        &mut self,
        input: WorkflowCancelRun,
    ) -> std::result::Result<WorkflowRun, WorkflowError> {
        Workflow::cancel_run(self, input).await
    }

    async fn signal_run(
        &mut self,
        input: WorkflowSignalRun,
    ) -> std::result::Result<pb::SignalWorkflowRunResponse, WorkflowError> {
        Workflow::signal_run(self, input).await
    }

    async fn signal_or_start_run(
        &mut self,
        input: WorkflowSignalOrStartRun,
    ) -> std::result::Result<pb::SignalWorkflowRunResponse, WorkflowError> {
        Workflow::signal_or_start_run(self, input).await
    }

    async fn deliver_event(
        &mut self,
        input: WorkflowDeliverEvent,
    ) -> std::result::Result<WorkflowEvent, WorkflowError> {
        Workflow::deliver_event(self, input).await
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
                .insert(WORKFLOW_RELAY_TOKEN_HEADER, token);
        }
        Ok(request)
    }
}

fn relay_token_interceptor(
    token: &str,
) -> std::result::Result<RelayTokenInterceptor, WorkflowError> {
    let trimmed = token.trim();
    let token = if trimmed.is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(trimmed).map_err(|err| {
            WorkflowError::Env(format!("workflow: invalid relay token metadata: {err}"))
        })?)
    };
    Ok(RelayTokenInterceptor { token })
}

enum WorkflowTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_workflow_target(raw: &str) -> std::result::Result<WorkflowTarget, WorkflowError> {
    let target = raw.trim();
    if target.is_empty() {
        return Err(WorkflowError::Env(
            "workflow: transport target is required".to_string(),
        ));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(WorkflowError::Env(format!(
                "workflow: tcp target {raw:?} is missing host:port"
            )));
        }
        return Ok(WorkflowTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(WorkflowError::Env(format!(
                "workflow: tls target {raw:?} is missing host:port"
            )));
        }
        return Ok(WorkflowTarget::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(WorkflowError::Env(format!(
                "workflow: unix target {raw:?} is missing a socket path"
            )));
        }
        return Ok(WorkflowTarget::Unix(path.to_string()));
    }
    if target.contains("://") {
        return Err(WorkflowError::Env(format!(
            "workflow: unsupported target scheme in {raw:?}"
        )));
    }
    Ok(WorkflowTarget::Unix(target.to_string()))
}
