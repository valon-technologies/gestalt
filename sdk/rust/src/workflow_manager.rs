use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::Request;
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
    BoundWorkflowTarget, WorkflowEvent, WorkflowEventMatch, WorkflowManagerDefinition,
    WorkflowManagerEventTrigger, WorkflowManagerRun, WorkflowManagerRunSignal,
    WorkflowManagerSchedule, WorkflowSignal, bound_workflow_target_to_proto,
    new_bound_workflow_target, new_workflow_event, new_workflow_event_match, new_workflow_signal,
    workflow_event_match_to_proto, workflow_event_to_proto, workflow_manager_definition_from_proto,
    workflow_manager_event_trigger_from_proto, workflow_manager_run_from_proto,
    workflow_manager_run_signal_from_proto, workflow_manager_schedule_from_proto,
    workflow_signal_to_proto,
};

type WorkflowManagerTransport = InterceptedService<Channel, RelayTokenInterceptor>;

const WORKFLOW_MANAGER_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

#[derive(Debug, thiserror::Error)]
/// Errors returned by [`WorkflowManager`].
pub enum WorkflowManagerError {
    /// The invocation token was empty.
    #[error("workflow manager: invocation token is not available")]
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

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerStartRun {
    pub provider_name: String,
    pub target: Option<BoundWorkflowTarget>,
    pub idempotency_key: String,
    pub workflow_key: String,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerSignalRun {
    pub run_id: String,
    pub signal: Option<WorkflowSignal>,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerSignalOrStartRun {
    pub provider_name: String,
    pub workflow_key: String,
    pub target: Option<BoundWorkflowTarget>,
    pub idempotency_key: String,
    pub signal: Option<WorkflowSignal>,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerCreateDefinition {
    pub provider_name: String,
    pub target: Option<BoundWorkflowTarget>,
    pub idempotency_key: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerGetDefinition {
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerUpdateDefinition {
    pub definition_id: String,
    pub provider_name: String,
    pub target: Option<BoundWorkflowTarget>,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerDeleteDefinition {
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerCreateSchedule {
    pub provider_name: String,
    pub cron: String,
    pub timezone: String,
    pub target: Option<BoundWorkflowTarget>,
    pub paused: bool,
    pub idempotency_key: String,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerGetSchedule {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerUpdateSchedule {
    pub schedule_id: String,
    pub provider_name: String,
    pub cron: String,
    pub timezone: String,
    pub target: Option<BoundWorkflowTarget>,
    pub paused: bool,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerDeleteSchedule {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerPauseSchedule {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerResumeSchedule {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerCreateEventTrigger {
    pub provider_name: String,
    pub event_match: Option<WorkflowEventMatch>,
    pub target: Option<BoundWorkflowTarget>,
    pub paused: bool,
    pub idempotency_key: String,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerGetEventTrigger {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerUpdateEventTrigger {
    pub trigger_id: String,
    pub provider_name: String,
    pub event_match: Option<WorkflowEventMatch>,
    pub target: Option<BoundWorkflowTarget>,
    pub paused: bool,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerDeleteEventTrigger {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerPauseEventTrigger {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerResumeEventTrigger {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerPublishEvent {
    pub provider_name: String,
    pub event: Option<WorkflowEvent>,
}

#[async_trait]
/// Fakeable client contract for workflow manager calls.
pub trait WorkflowManagerClient: Send {
    async fn start_run(
        &mut self,
        input: WorkflowManagerStartRun,
    ) -> std::result::Result<WorkflowManagerRun, WorkflowManagerError>;
    async fn signal_run(
        &mut self,
        input: WorkflowManagerSignalRun,
    ) -> std::result::Result<WorkflowManagerRunSignal, WorkflowManagerError>;
    async fn signal_or_start_run(
        &mut self,
        input: WorkflowManagerSignalOrStartRun,
    ) -> std::result::Result<WorkflowManagerRunSignal, WorkflowManagerError>;
    async fn create_definition(
        &mut self,
        input: WorkflowManagerCreateDefinition,
    ) -> std::result::Result<WorkflowManagerDefinition, WorkflowManagerError>;
    async fn get_definition(
        &mut self,
        input: WorkflowManagerGetDefinition,
    ) -> std::result::Result<WorkflowManagerDefinition, WorkflowManagerError>;
    async fn update_definition(
        &mut self,
        input: WorkflowManagerUpdateDefinition,
    ) -> std::result::Result<WorkflowManagerDefinition, WorkflowManagerError>;
    async fn delete_definition(
        &mut self,
        input: WorkflowManagerDeleteDefinition,
    ) -> std::result::Result<(), WorkflowManagerError>;
    async fn create_schedule(
        &mut self,
        input: WorkflowManagerCreateSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError>;
    async fn get_schedule(
        &mut self,
        input: WorkflowManagerGetSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError>;
    async fn update_schedule(
        &mut self,
        input: WorkflowManagerUpdateSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError>;
    async fn delete_schedule(
        &mut self,
        input: WorkflowManagerDeleteSchedule,
    ) -> std::result::Result<(), WorkflowManagerError>;
    async fn pause_schedule(
        &mut self,
        input: WorkflowManagerPauseSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError>;
    async fn resume_schedule(
        &mut self,
        input: WorkflowManagerResumeSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError>;
    async fn create_trigger(
        &mut self,
        input: WorkflowManagerCreateEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError>;
    async fn get_trigger(
        &mut self,
        input: WorkflowManagerGetEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError>;
    async fn update_trigger(
        &mut self,
        input: WorkflowManagerUpdateEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError>;
    async fn delete_trigger(
        &mut self,
        input: WorkflowManagerDeleteEventTrigger,
    ) -> std::result::Result<(), WorkflowManagerError>;
    async fn pause_trigger(
        &mut self,
        input: WorkflowManagerPauseEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError>;
    async fn resume_trigger(
        &mut self,
        input: WorkflowManagerResumeEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError>;
    async fn publish_event(
        &mut self,
        input: WorkflowManagerPublishEvent,
    ) -> std::result::Result<WorkflowEvent, WorkflowManagerError>;
}

pub(crate) fn new_workflow_manager_start_run_request(
    input: WorkflowManagerStartRun,
) -> crate::Result<pb::StartWorkflowProviderRunRequest> {
    Ok(pb::StartWorkflowProviderRunRequest {
        provider_name: input.provider_name,
        target: input
            .target
            .map(new_bound_workflow_target)
            .transpose()?
            .map(bound_workflow_target_to_proto)
            .transpose()?,
        idempotency_key: input.idempotency_key,
        workflow_key: input.workflow_key,
        invocation_token: String::new(),
        definition_id: input.definition_id,
        ..Default::default()
    })
}

pub(crate) fn new_workflow_manager_signal_run_request(
    input: WorkflowManagerSignalRun,
) -> crate::Result<pb::SignalWorkflowProviderRunRequest> {
    Ok(pb::SignalWorkflowProviderRunRequest {
        run_id: input.run_id,
        signal: input
            .signal
            .map(new_workflow_signal)
            .transpose()?
            .map(workflow_signal_to_proto)
            .transpose()?,
        invocation_token: String::new(),
    })
}

pub(crate) fn new_workflow_manager_signal_or_start_run_request(
    input: WorkflowManagerSignalOrStartRun,
) -> crate::Result<pb::SignalOrStartWorkflowProviderRunRequest> {
    Ok(pb::SignalOrStartWorkflowProviderRunRequest {
        provider_name: input.provider_name,
        workflow_key: input.workflow_key,
        target: input
            .target
            .map(new_bound_workflow_target)
            .transpose()?
            .map(bound_workflow_target_to_proto)
            .transpose()?,
        idempotency_key: input.idempotency_key,
        signal: input
            .signal
            .map(new_workflow_signal)
            .transpose()?
            .map(workflow_signal_to_proto)
            .transpose()?,
        invocation_token: String::new(),
        definition_id: input.definition_id,
        ..Default::default()
    })
}

pub(crate) fn new_workflow_manager_create_definition_request(
    input: WorkflowManagerCreateDefinition,
) -> crate::Result<pb::CreateWorkflowProviderDefinitionRequest> {
    Ok(pb::CreateWorkflowProviderDefinitionRequest {
        provider_name: input.provider_name,
        target: input
            .target
            .map(new_bound_workflow_target)
            .transpose()?
            .map(bound_workflow_target_to_proto)
            .transpose()?,
        invocation_token: String::new(),
        idempotency_key: input.idempotency_key,
    })
}

pub(crate) fn new_workflow_manager_get_definition_request(
    input: WorkflowManagerGetDefinition,
) -> pb::GetWorkflowProviderDefinitionRequest {
    pb::GetWorkflowProviderDefinitionRequest {
        definition_id: input.definition_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_manager_update_definition_request(
    input: WorkflowManagerUpdateDefinition,
) -> crate::Result<pb::UpdateWorkflowProviderDefinitionRequest> {
    Ok(pb::UpdateWorkflowProviderDefinitionRequest {
        definition_id: input.definition_id,
        provider_name: input.provider_name,
        target: input
            .target
            .map(new_bound_workflow_target)
            .transpose()?
            .map(bound_workflow_target_to_proto)
            .transpose()?,
        invocation_token: String::new(),
    })
}

pub(crate) fn new_workflow_manager_delete_definition_request(
    input: WorkflowManagerDeleteDefinition,
) -> pb::DeleteWorkflowProviderDefinitionRequest {
    pb::DeleteWorkflowProviderDefinitionRequest {
        definition_id: input.definition_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_manager_create_schedule_request(
    input: WorkflowManagerCreateSchedule,
) -> crate::Result<pb::UpsertWorkflowProviderScheduleRequest> {
    Ok(pb::UpsertWorkflowProviderScheduleRequest {
        provider_name: input.provider_name,
        cron: input.cron,
        timezone: input.timezone,
        target: input
            .target
            .map(new_bound_workflow_target)
            .transpose()?
            .map(bound_workflow_target_to_proto)
            .transpose()?,
        paused: input.paused,
        invocation_token: String::new(),
        idempotency_key: input.idempotency_key,
        definition_id: input.definition_id,
        ..Default::default()
    })
}

pub(crate) fn new_workflow_manager_get_schedule_request(
    input: WorkflowManagerGetSchedule,
) -> pb::GetWorkflowProviderScheduleRequest {
    pb::GetWorkflowProviderScheduleRequest {
        schedule_id: input.schedule_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_manager_update_schedule_request(
    input: WorkflowManagerUpdateSchedule,
) -> crate::Result<pb::UpsertWorkflowProviderScheduleRequest> {
    Ok(pb::UpsertWorkflowProviderScheduleRequest {
        schedule_id: input.schedule_id,
        provider_name: input.provider_name,
        cron: input.cron,
        timezone: input.timezone,
        target: input
            .target
            .map(new_bound_workflow_target)
            .transpose()?
            .map(bound_workflow_target_to_proto)
            .transpose()?,
        paused: input.paused,
        invocation_token: String::new(),
        definition_id: input.definition_id,
        ..Default::default()
    })
}

pub(crate) fn new_workflow_manager_delete_schedule_request(
    input: WorkflowManagerDeleteSchedule,
) -> pb::DeleteWorkflowProviderScheduleRequest {
    pb::DeleteWorkflowProviderScheduleRequest {
        schedule_id: input.schedule_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_manager_pause_schedule_request(
    input: WorkflowManagerPauseSchedule,
) -> pb::PauseWorkflowProviderScheduleRequest {
    pb::PauseWorkflowProviderScheduleRequest {
        schedule_id: input.schedule_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_manager_resume_schedule_request(
    input: WorkflowManagerResumeSchedule,
) -> pb::ResumeWorkflowProviderScheduleRequest {
    pb::ResumeWorkflowProviderScheduleRequest {
        schedule_id: input.schedule_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_manager_create_event_trigger_request(
    input: WorkflowManagerCreateEventTrigger,
) -> crate::Result<pb::UpsertWorkflowProviderEventTriggerRequest> {
    Ok(pb::UpsertWorkflowProviderEventTriggerRequest {
        provider_name: input.provider_name,
        r#match: input
            .event_match
            .map(new_workflow_event_match)
            .map(workflow_event_match_to_proto),
        target: input
            .target
            .map(new_bound_workflow_target)
            .transpose()?
            .map(bound_workflow_target_to_proto)
            .transpose()?,
        paused: input.paused,
        invocation_token: String::new(),
        idempotency_key: input.idempotency_key,
        definition_id: input.definition_id,
        ..Default::default()
    })
}

pub(crate) fn new_workflow_manager_get_event_trigger_request(
    input: WorkflowManagerGetEventTrigger,
) -> pb::GetWorkflowProviderEventTriggerRequest {
    pb::GetWorkflowProviderEventTriggerRequest {
        trigger_id: input.trigger_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_manager_update_event_trigger_request(
    input: WorkflowManagerUpdateEventTrigger,
) -> crate::Result<pb::UpsertWorkflowProviderEventTriggerRequest> {
    Ok(pb::UpsertWorkflowProviderEventTriggerRequest {
        trigger_id: input.trigger_id,
        provider_name: input.provider_name,
        r#match: input
            .event_match
            .map(new_workflow_event_match)
            .map(workflow_event_match_to_proto),
        target: input
            .target
            .map(new_bound_workflow_target)
            .transpose()?
            .map(bound_workflow_target_to_proto)
            .transpose()?,
        paused: input.paused,
        invocation_token: String::new(),
        definition_id: input.definition_id,
        ..Default::default()
    })
}

pub(crate) fn new_workflow_manager_delete_event_trigger_request(
    input: WorkflowManagerDeleteEventTrigger,
) -> pb::DeleteWorkflowProviderEventTriggerRequest {
    pb::DeleteWorkflowProviderEventTriggerRequest {
        trigger_id: input.trigger_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_manager_pause_event_trigger_request(
    input: WorkflowManagerPauseEventTrigger,
) -> pb::PauseWorkflowProviderEventTriggerRequest {
    pb::PauseWorkflowProviderEventTriggerRequest {
        trigger_id: input.trigger_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_manager_resume_event_trigger_request(
    input: WorkflowManagerResumeEventTrigger,
) -> pb::ResumeWorkflowProviderEventTriggerRequest {
    pb::ResumeWorkflowProviderEventTriggerRequest {
        trigger_id: input.trigger_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_manager_publish_event_request(
    input: WorkflowManagerPublishEvent,
) -> crate::Result<pb::PublishWorkflowProviderEventRequest> {
    Ok(pb::PublishWorkflowProviderEventRequest {
        app_name: String::new(),
        event: input
            .event
            .map(new_workflow_event)
            .transpose()?
            .map(workflow_event_to_proto)
            .transpose()?,
        invocation_token: String::new(),
        provider_name: input.provider_name,
        ..Default::default()
    })
}

/// Client for creating workflow definitions, starting runs, and managing schedules or triggers.
pub struct WorkflowManager {
    client: ProtoWorkflowProviderClient<WorkflowManagerTransport>,
    invocation_token: String,
    idempotency_key: String,
}

impl WorkflowManager {
    /// Connects to the workflow manager with an invocation token from the host.
    pub async fn connect(
        invocation_token: impl AsRef<str>,
    ) -> std::result::Result<Self, WorkflowManagerError> {
        Self::connect_with_idempotency_key(invocation_token, "").await
    }

    /// Connects with a default idempotency key for create requests.
    pub async fn connect_with_idempotency_key(
        invocation_token: impl AsRef<str>,
        idempotency_key: impl AsRef<str>,
    ) -> std::result::Result<Self, WorkflowManagerError> {
        let invocation_token = invocation_token.as_ref().trim().to_owned();
        if invocation_token.is_empty() {
            return Err(WorkflowManagerError::MissingInvocationToken);
        }

        let socket_path = std::env::var(ENV_HOST_SERVICE_SOCKET).map_err(|_| {
            WorkflowManagerError::Env(format!("{ENV_HOST_SERVICE_SOCKET} is not set"))
        })?;
        let relay_token = std::env::var(ENV_HOST_SERVICE_TOKEN).unwrap_or_default();
        let channel = match parse_workflow_manager_target(&socket_path)? {
            WorkflowManagerTarget::Unix(path) => {
                Endpoint::try_from("http://[::]:50051")?
                    .connect_with_connector(service_fn(move |_: Uri| {
                        let path = path.clone();
                        async move { UnixStream::connect(path).await.map(TokioIo::new) }
                    }))
                    .await?
            }
            WorkflowManagerTarget::Tcp(address) => {
                Endpoint::from_shared(format!("http://{address}"))?
                    .connect()
                    .await?
            }
            WorkflowManagerTarget::Tls(address) => {
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
            invocation_token,
            idempotency_key: idempotency_key.as_ref().trim().to_owned(),
        })
    }

    /// Creates a reusable workflow definition.
    pub async fn create_definition(
        &mut self,
        input: WorkflowManagerCreateDefinition,
    ) -> std::result::Result<WorkflowManagerDefinition, WorkflowManagerError> {
        let mut request = new_workflow_manager_create_definition_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(workflow_manager_definition_from_proto(
            self.client.create_definition(request).await?.into_inner(),
        )?)
    }

    /// Fetches one workflow definition.
    pub async fn get_definition(
        &mut self,
        input: WorkflowManagerGetDefinition,
    ) -> std::result::Result<WorkflowManagerDefinition, WorkflowManagerError> {
        let mut request = new_workflow_manager_get_definition_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_manager_definition_from_proto(
            self.client.get_definition(request).await?.into_inner(),
        )?)
    }

    /// Updates a workflow definition.
    pub async fn update_definition(
        &mut self,
        input: WorkflowManagerUpdateDefinition,
    ) -> std::result::Result<WorkflowManagerDefinition, WorkflowManagerError> {
        let mut request = new_workflow_manager_update_definition_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_manager_definition_from_proto(
            self.client.update_definition(request).await?.into_inner(),
        )?)
    }

    /// Deletes a workflow definition.
    pub async fn delete_definition(
        &mut self,
        input: WorkflowManagerDeleteDefinition,
    ) -> std::result::Result<(), WorkflowManagerError> {
        let mut request = new_workflow_manager_delete_definition_request(input);
        request.invocation_token = self.invocation_token.clone();
        self.client.delete_definition(request).await?;
        Ok(())
    }

    /// Creates a workflow schedule.
    pub async fn create_schedule(
        &mut self,
        input: WorkflowManagerCreateSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError> {
        let mut request = new_workflow_manager_create_schedule_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(workflow_manager_schedule_from_proto(
            self.client.upsert_schedule(request).await?.into_inner(),
        )?)
    }

    /// Starts a workflow run.
    pub async fn start_run(
        &mut self,
        input: WorkflowManagerStartRun,
    ) -> std::result::Result<WorkflowManagerRun, WorkflowManagerError> {
        let mut request = new_workflow_manager_start_run_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(workflow_manager_run_from_proto(
            self.client.start_run(request).await?.into_inner(),
        )?)
    }

    /// Signals an existing workflow run.
    pub async fn signal_run(
        &mut self,
        input: WorkflowManagerSignalRun,
    ) -> std::result::Result<WorkflowManagerRunSignal, WorkflowManagerError> {
        let mut request = new_workflow_manager_signal_run_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_manager_run_signal_from_proto(
            self.client.signal_run(request).await?.into_inner(),
        )?)
    }

    /// Signals a run or starts it when no matching run exists.
    pub async fn signal_or_start_run(
        &mut self,
        input: WorkflowManagerSignalOrStartRun,
    ) -> std::result::Result<WorkflowManagerRunSignal, WorkflowManagerError> {
        let mut request = new_workflow_manager_signal_or_start_run_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(workflow_manager_run_signal_from_proto(
            self.client.signal_or_start_run(request).await?.into_inner(),
        )?)
    }

    /// Fetches one workflow schedule.
    pub async fn get_schedule(
        &mut self,
        input: WorkflowManagerGetSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError> {
        let mut request = new_workflow_manager_get_schedule_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_manager_schedule_from_proto(
            self.client.get_schedule(request).await?.into_inner(),
        )?)
    }

    /// Updates a workflow schedule.
    pub async fn update_schedule(
        &mut self,
        input: WorkflowManagerUpdateSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError> {
        let mut request = new_workflow_manager_update_schedule_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_manager_schedule_from_proto(
            self.client.upsert_schedule(request).await?.into_inner(),
        )?)
    }

    /// Deletes a workflow schedule.
    pub async fn delete_schedule(
        &mut self,
        input: WorkflowManagerDeleteSchedule,
    ) -> std::result::Result<(), WorkflowManagerError> {
        let mut request = new_workflow_manager_delete_schedule_request(input);
        request.invocation_token = self.invocation_token.clone();
        self.client.delete_schedule(request).await?;
        Ok(())
    }

    /// Pauses a workflow schedule.
    pub async fn pause_schedule(
        &mut self,
        input: WorkflowManagerPauseSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError> {
        let mut request = new_workflow_manager_pause_schedule_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_manager_schedule_from_proto(
            self.client.pause_schedule(request).await?.into_inner(),
        )?)
    }

    /// Resumes a workflow schedule.
    pub async fn resume_schedule(
        &mut self,
        input: WorkflowManagerResumeSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError> {
        let mut request = new_workflow_manager_resume_schedule_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_manager_schedule_from_proto(
            self.client.resume_schedule(request).await?.into_inner(),
        )?)
    }

    /// Creates an event trigger.
    pub async fn create_trigger(
        &mut self,
        input: WorkflowManagerCreateEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError> {
        let mut request = new_workflow_manager_create_event_trigger_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(workflow_manager_event_trigger_from_proto(
            self.client
                .upsert_event_trigger(request)
                .await?
                .into_inner(),
        )?)
    }

    /// Fetches one event trigger.
    pub async fn get_trigger(
        &mut self,
        input: WorkflowManagerGetEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError> {
        let mut request = new_workflow_manager_get_event_trigger_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_manager_event_trigger_from_proto(
            self.client.get_event_trigger(request).await?.into_inner(),
        )?)
    }

    /// Updates an event trigger.
    pub async fn update_trigger(
        &mut self,
        input: WorkflowManagerUpdateEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError> {
        let mut request = new_workflow_manager_update_event_trigger_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_manager_event_trigger_from_proto(
            self.client
                .upsert_event_trigger(request)
                .await?
                .into_inner(),
        )?)
    }

    /// Deletes an event trigger.
    pub async fn delete_trigger(
        &mut self,
        input: WorkflowManagerDeleteEventTrigger,
    ) -> std::result::Result<(), WorkflowManagerError> {
        let mut request = new_workflow_manager_delete_event_trigger_request(input);
        request.invocation_token = self.invocation_token.clone();
        self.client.delete_event_trigger(request).await?;
        Ok(())
    }

    /// Pauses an event trigger.
    pub async fn pause_trigger(
        &mut self,
        input: WorkflowManagerPauseEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError> {
        let mut request = new_workflow_manager_pause_event_trigger_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_manager_event_trigger_from_proto(
            self.client.pause_event_trigger(request).await?.into_inner(),
        )?)
    }

    /// Resumes an event trigger.
    pub async fn resume_trigger(
        &mut self,
        input: WorkflowManagerResumeEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError> {
        let mut request = new_workflow_manager_resume_event_trigger_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_manager_event_trigger_from_proto(
            self.client
                .resume_event_trigger(request)
                .await?
                .into_inner(),
        )?)
    }

    /// Publishes an event into the workflow manager.
    pub async fn publish_event(
        &mut self,
        input: WorkflowManagerPublishEvent,
    ) -> std::result::Result<WorkflowEvent, WorkflowManagerError> {
        let mut request = new_workflow_manager_publish_event_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(crate::workflow::workflow_event_from_proto(
            self.client.publish_event(request).await?.into_inner(),
        )?)
    }
}

#[async_trait]
impl WorkflowManagerClient for WorkflowManager {
    async fn start_run(
        &mut self,
        input: WorkflowManagerStartRun,
    ) -> std::result::Result<WorkflowManagerRun, WorkflowManagerError> {
        WorkflowManager::start_run(self, input).await
    }

    async fn signal_run(
        &mut self,
        input: WorkflowManagerSignalRun,
    ) -> std::result::Result<WorkflowManagerRunSignal, WorkflowManagerError> {
        WorkflowManager::signal_run(self, input).await
    }

    async fn signal_or_start_run(
        &mut self,
        input: WorkflowManagerSignalOrStartRun,
    ) -> std::result::Result<WorkflowManagerRunSignal, WorkflowManagerError> {
        WorkflowManager::signal_or_start_run(self, input).await
    }

    async fn create_definition(
        &mut self,
        input: WorkflowManagerCreateDefinition,
    ) -> std::result::Result<WorkflowManagerDefinition, WorkflowManagerError> {
        WorkflowManager::create_definition(self, input).await
    }

    async fn get_definition(
        &mut self,
        input: WorkflowManagerGetDefinition,
    ) -> std::result::Result<WorkflowManagerDefinition, WorkflowManagerError> {
        WorkflowManager::get_definition(self, input).await
    }

    async fn update_definition(
        &mut self,
        input: WorkflowManagerUpdateDefinition,
    ) -> std::result::Result<WorkflowManagerDefinition, WorkflowManagerError> {
        WorkflowManager::update_definition(self, input).await
    }

    async fn delete_definition(
        &mut self,
        input: WorkflowManagerDeleteDefinition,
    ) -> std::result::Result<(), WorkflowManagerError> {
        WorkflowManager::delete_definition(self, input).await
    }

    async fn create_schedule(
        &mut self,
        input: WorkflowManagerCreateSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError> {
        WorkflowManager::create_schedule(self, input).await
    }

    async fn get_schedule(
        &mut self,
        input: WorkflowManagerGetSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError> {
        WorkflowManager::get_schedule(self, input).await
    }

    async fn update_schedule(
        &mut self,
        input: WorkflowManagerUpdateSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError> {
        WorkflowManager::update_schedule(self, input).await
    }

    async fn delete_schedule(
        &mut self,
        input: WorkflowManagerDeleteSchedule,
    ) -> std::result::Result<(), WorkflowManagerError> {
        WorkflowManager::delete_schedule(self, input).await
    }

    async fn pause_schedule(
        &mut self,
        input: WorkflowManagerPauseSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError> {
        WorkflowManager::pause_schedule(self, input).await
    }

    async fn resume_schedule(
        &mut self,
        input: WorkflowManagerResumeSchedule,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError> {
        WorkflowManager::resume_schedule(self, input).await
    }

    async fn create_trigger(
        &mut self,
        input: WorkflowManagerCreateEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError> {
        WorkflowManager::create_trigger(self, input).await
    }

    async fn get_trigger(
        &mut self,
        input: WorkflowManagerGetEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError> {
        WorkflowManager::get_trigger(self, input).await
    }

    async fn update_trigger(
        &mut self,
        input: WorkflowManagerUpdateEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError> {
        WorkflowManager::update_trigger(self, input).await
    }

    async fn delete_trigger(
        &mut self,
        input: WorkflowManagerDeleteEventTrigger,
    ) -> std::result::Result<(), WorkflowManagerError> {
        WorkflowManager::delete_trigger(self, input).await
    }

    async fn pause_trigger(
        &mut self,
        input: WorkflowManagerPauseEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError> {
        WorkflowManager::pause_trigger(self, input).await
    }

    async fn resume_trigger(
        &mut self,
        input: WorkflowManagerResumeEventTrigger,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError> {
        WorkflowManager::resume_trigger(self, input).await
    }

    async fn publish_event(
        &mut self,
        input: WorkflowManagerPublishEvent,
    ) -> std::result::Result<WorkflowEvent, WorkflowManagerError> {
        WorkflowManager::publish_event(self, input).await
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
                .insert(WORKFLOW_MANAGER_RELAY_TOKEN_HEADER, token);
        }
        Ok(request)
    }
}

fn relay_token_interceptor(
    token: &str,
) -> std::result::Result<RelayTokenInterceptor, WorkflowManagerError> {
    let trimmed = token.trim();
    let token = if trimmed.is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(trimmed).map_err(|err| {
            WorkflowManagerError::Env(format!(
                "workflow manager: invalid relay token metadata: {err}"
            ))
        })?)
    };
    Ok(RelayTokenInterceptor { token })
}

enum WorkflowManagerTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_workflow_manager_target(
    raw: &str,
) -> std::result::Result<WorkflowManagerTarget, WorkflowManagerError> {
    let target = raw.trim();
    if target.is_empty() {
        return Err(WorkflowManagerError::Env(
            "workflow manager: transport target is required".to_string(),
        ));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(WorkflowManagerError::Env(format!(
                "workflow manager: tcp target {raw:?} is missing host:port"
            )));
        }
        return Ok(WorkflowManagerTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(WorkflowManagerError::Env(format!(
                "workflow manager: tls target {raw:?} is missing host:port"
            )));
        }
        return Ok(WorkflowManagerTarget::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(WorkflowManagerError::Env(format!(
                "workflow manager: unix target {raw:?} is missing a socket path"
            )));
        }
        return Ok(WorkflowManagerTarget::Unix(path.to_string()));
    }
    if target.contains("://") {
        return Err(WorkflowManagerError::Env(format!(
            "workflow manager: unsupported target scheme in {raw:?}"
        )));
    }
    Ok(WorkflowManagerTarget::Unix(target.to_string()))
}
