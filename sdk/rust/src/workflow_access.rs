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
    BoundWorkflowTarget, WorkflowDefinition, WorkflowEvent, WorkflowEventMatch,
    WorkflowEventTrigger, WorkflowRun, WorkflowRunSignal, WorkflowSchedule, WorkflowSignal,
    bound_workflow_target_to_proto, new_bound_workflow_target, new_workflow_event,
    new_workflow_event_match, new_workflow_signal, workflow_definition_from_proto,
    workflow_event_match_to_proto, workflow_event_to_proto, workflow_event_trigger_from_proto,
    workflow_run_from_proto, workflow_run_signal_from_proto, workflow_schedule_from_proto,
    workflow_signal_to_proto,
};

type WorkflowTransport = InterceptedService<Channel, RelayTokenInterceptor>;

const WORKFLOW_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

#[derive(Debug, thiserror::Error)]
/// Errors returned by [`Workflow`].
pub enum WorkflowError {
    /// The invocation token was empty.
    #[error("workflow: invocation token is not available")]
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
pub struct WorkflowStartRun {
    pub provider_name: String,
    pub target: Option<BoundWorkflowTarget>,
    pub idempotency_key: String,
    pub workflow_key: String,
    pub definition_id: String,
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
    pub target: Option<BoundWorkflowTarget>,
    pub idempotency_key: String,
    pub signal: Option<WorkflowSignal>,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowCreateDefinition {
    pub provider_name: String,
    pub target: Option<BoundWorkflowTarget>,
    pub idempotency_key: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowGetDefinition {
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowUpdateDefinition {
    pub definition_id: String,
    pub provider_name: String,
    pub target: Option<BoundWorkflowTarget>,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowDeleteDefinition {
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowCreateSchedule {
    pub provider_name: String,
    pub cron: String,
    pub timezone: String,
    pub target: Option<BoundWorkflowTarget>,
    pub paused: bool,
    pub idempotency_key: String,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowGetSchedule {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowUpdateSchedule {
    pub schedule_id: String,
    pub provider_name: String,
    pub cron: String,
    pub timezone: String,
    pub target: Option<BoundWorkflowTarget>,
    pub paused: bool,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowDeleteSchedule {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowPauseSchedule {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowResumeSchedule {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowCreateEventTrigger {
    pub provider_name: String,
    pub event_match: Option<WorkflowEventMatch>,
    pub target: Option<BoundWorkflowTarget>,
    pub paused: bool,
    pub idempotency_key: String,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowGetEventTrigger {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowUpdateEventTrigger {
    pub trigger_id: String,
    pub provider_name: String,
    pub event_match: Option<WorkflowEventMatch>,
    pub target: Option<BoundWorkflowTarget>,
    pub paused: bool,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowDeleteEventTrigger {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowPauseEventTrigger {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowResumeEventTrigger {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowPublishEvent {
    pub provider_name: String,
    pub event: Option<WorkflowEvent>,
}

#[async_trait]
/// Fakeable client contract for workflow calls.
pub trait WorkflowContract: Send {
    async fn start_run(
        &mut self,
        input: WorkflowStartRun,
    ) -> std::result::Result<WorkflowRun, WorkflowError>;
    async fn signal_run(
        &mut self,
        input: WorkflowSignalRun,
    ) -> std::result::Result<WorkflowRunSignal, WorkflowError>;
    async fn signal_or_start_run(
        &mut self,
        input: WorkflowSignalOrStartRun,
    ) -> std::result::Result<WorkflowRunSignal, WorkflowError>;
    async fn create_definition(
        &mut self,
        input: WorkflowCreateDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError>;
    async fn get_definition(
        &mut self,
        input: WorkflowGetDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError>;
    async fn update_definition(
        &mut self,
        input: WorkflowUpdateDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError>;
    async fn delete_definition(
        &mut self,
        input: WorkflowDeleteDefinition,
    ) -> std::result::Result<(), WorkflowError>;
    async fn create_schedule(
        &mut self,
        input: WorkflowCreateSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError>;
    async fn get_schedule(
        &mut self,
        input: WorkflowGetSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError>;
    async fn update_schedule(
        &mut self,
        input: WorkflowUpdateSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError>;
    async fn delete_schedule(
        &mut self,
        input: WorkflowDeleteSchedule,
    ) -> std::result::Result<(), WorkflowError>;
    async fn pause_schedule(
        &mut self,
        input: WorkflowPauseSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError>;
    async fn resume_schedule(
        &mut self,
        input: WorkflowResumeSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError>;
    async fn create_trigger(
        &mut self,
        input: WorkflowCreateEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError>;
    async fn get_trigger(
        &mut self,
        input: WorkflowGetEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError>;
    async fn update_trigger(
        &mut self,
        input: WorkflowUpdateEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError>;
    async fn delete_trigger(
        &mut self,
        input: WorkflowDeleteEventTrigger,
    ) -> std::result::Result<(), WorkflowError>;
    async fn pause_trigger(
        &mut self,
        input: WorkflowPauseEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError>;
    async fn resume_trigger(
        &mut self,
        input: WorkflowResumeEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError>;
    async fn publish_event(
        &mut self,
        input: WorkflowPublishEvent,
    ) -> std::result::Result<WorkflowEvent, WorkflowError>;
}

pub(crate) fn new_workflow_start_run_request(
    input: WorkflowStartRun,
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

pub(crate) fn new_workflow_signal_run_request(
    input: WorkflowSignalRun,
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

pub(crate) fn new_workflow_signal_or_start_run_request(
    input: WorkflowSignalOrStartRun,
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

pub(crate) fn new_workflow_create_definition_request(
    input: WorkflowCreateDefinition,
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
        created_by_subject_id: String::new(),
    })
}

pub(crate) fn new_workflow_get_definition_request(
    input: WorkflowGetDefinition,
) -> pb::GetWorkflowProviderDefinitionRequest {
    pb::GetWorkflowProviderDefinitionRequest {
        definition_id: input.definition_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_update_definition_request(
    input: WorkflowUpdateDefinition,
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
        requested_by_subject_id: String::new(),
    })
}

pub(crate) fn new_workflow_delete_definition_request(
    input: WorkflowDeleteDefinition,
) -> pb::DeleteWorkflowProviderDefinitionRequest {
    pb::DeleteWorkflowProviderDefinitionRequest {
        definition_id: input.definition_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_create_schedule_request(
    input: WorkflowCreateSchedule,
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

pub(crate) fn new_workflow_get_schedule_request(
    input: WorkflowGetSchedule,
) -> pb::GetWorkflowProviderScheduleRequest {
    pb::GetWorkflowProviderScheduleRequest {
        schedule_id: input.schedule_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_update_schedule_request(
    input: WorkflowUpdateSchedule,
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

pub(crate) fn new_workflow_delete_schedule_request(
    input: WorkflowDeleteSchedule,
) -> pb::DeleteWorkflowProviderScheduleRequest {
    pb::DeleteWorkflowProviderScheduleRequest {
        schedule_id: input.schedule_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_pause_schedule_request(
    input: WorkflowPauseSchedule,
) -> pb::PauseWorkflowProviderScheduleRequest {
    pb::PauseWorkflowProviderScheduleRequest {
        schedule_id: input.schedule_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_resume_schedule_request(
    input: WorkflowResumeSchedule,
) -> pb::ResumeWorkflowProviderScheduleRequest {
    pb::ResumeWorkflowProviderScheduleRequest {
        schedule_id: input.schedule_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_create_event_trigger_request(
    input: WorkflowCreateEventTrigger,
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

pub(crate) fn new_workflow_get_event_trigger_request(
    input: WorkflowGetEventTrigger,
) -> pb::GetWorkflowProviderEventTriggerRequest {
    pb::GetWorkflowProviderEventTriggerRequest {
        trigger_id: input.trigger_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_update_event_trigger_request(
    input: WorkflowUpdateEventTrigger,
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

pub(crate) fn new_workflow_delete_event_trigger_request(
    input: WorkflowDeleteEventTrigger,
) -> pb::DeleteWorkflowProviderEventTriggerRequest {
    pb::DeleteWorkflowProviderEventTriggerRequest {
        trigger_id: input.trigger_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_pause_event_trigger_request(
    input: WorkflowPauseEventTrigger,
) -> pb::PauseWorkflowProviderEventTriggerRequest {
    pb::PauseWorkflowProviderEventTriggerRequest {
        trigger_id: input.trigger_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_resume_event_trigger_request(
    input: WorkflowResumeEventTrigger,
) -> pb::ResumeWorkflowProviderEventTriggerRequest {
    pb::ResumeWorkflowProviderEventTriggerRequest {
        trigger_id: input.trigger_id,
        invocation_token: String::new(),
    }
}

pub(crate) fn new_workflow_publish_event_request(
    input: WorkflowPublishEvent,
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
pub struct Workflow {
    client: ProtoWorkflowProviderClient<WorkflowTransport>,
    invocation_token: String,
    idempotency_key: String,
}

impl Workflow {
    /// Connects to the workflow with an invocation token from the host.
    pub async fn connect(
        invocation_token: impl AsRef<str>,
    ) -> std::result::Result<Self, WorkflowError> {
        Self::connect_with_idempotency_key(invocation_token, "").await
    }

    /// Connects with a default idempotency key for create requests.
    pub async fn connect_with_idempotency_key(
        invocation_token: impl AsRef<str>,
        idempotency_key: impl AsRef<str>,
    ) -> std::result::Result<Self, WorkflowError> {
        let invocation_token = invocation_token.as_ref().trim().to_owned();
        if invocation_token.is_empty() {
            return Err(WorkflowError::MissingInvocationToken);
        }

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
            invocation_token,
            idempotency_key: idempotency_key.as_ref().trim().to_owned(),
        })
    }

    /// Creates a reusable workflow definition.
    pub async fn create_definition(
        &mut self,
        input: WorkflowCreateDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError> {
        let mut request = new_workflow_create_definition_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(workflow_definition_from_proto(
            self.client.create_definition(request).await?.into_inner(),
        )?)
    }

    /// Fetches one workflow definition.
    pub async fn get_definition(
        &mut self,
        input: WorkflowGetDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError> {
        let mut request = new_workflow_get_definition_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_definition_from_proto(
            self.client.get_definition(request).await?.into_inner(),
        )?)
    }

    /// Updates a workflow definition.
    pub async fn update_definition(
        &mut self,
        input: WorkflowUpdateDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError> {
        let mut request = new_workflow_update_definition_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_definition_from_proto(
            self.client.update_definition(request).await?.into_inner(),
        )?)
    }

    /// Deletes a workflow definition.
    pub async fn delete_definition(
        &mut self,
        input: WorkflowDeleteDefinition,
    ) -> std::result::Result<(), WorkflowError> {
        let mut request = new_workflow_delete_definition_request(input);
        request.invocation_token = self.invocation_token.clone();
        self.client.delete_definition(request).await?;
        Ok(())
    }

    /// Creates a workflow schedule.
    pub async fn create_schedule(
        &mut self,
        input: WorkflowCreateSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError> {
        let mut request = new_workflow_create_schedule_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(workflow_schedule_from_proto(
            self.client.upsert_schedule(request).await?.into_inner(),
        )?)
    }

    /// Starts a workflow run.
    pub async fn start_run(
        &mut self,
        input: WorkflowStartRun,
    ) -> std::result::Result<WorkflowRun, WorkflowError> {
        let mut request = new_workflow_start_run_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(workflow_run_from_proto(
            self.client.start_run(request).await?.into_inner(),
        )?)
    }

    /// Signals an existing workflow run.
    pub async fn signal_run(
        &mut self,
        input: WorkflowSignalRun,
    ) -> std::result::Result<WorkflowRunSignal, WorkflowError> {
        let mut request = new_workflow_signal_run_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_run_signal_from_proto(
            self.client.signal_run(request).await?.into_inner(),
        )?)
    }

    /// Signals a run or starts it when no matching run exists.
    pub async fn signal_or_start_run(
        &mut self,
        input: WorkflowSignalOrStartRun,
    ) -> std::result::Result<WorkflowRunSignal, WorkflowError> {
        let mut request = new_workflow_signal_or_start_run_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(workflow_run_signal_from_proto(
            self.client.signal_or_start_run(request).await?.into_inner(),
        )?)
    }

    /// Fetches one workflow schedule.
    pub async fn get_schedule(
        &mut self,
        input: WorkflowGetSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError> {
        let mut request = new_workflow_get_schedule_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_schedule_from_proto(
            self.client.get_schedule(request).await?.into_inner(),
        )?)
    }

    /// Updates a workflow schedule.
    pub async fn update_schedule(
        &mut self,
        input: WorkflowUpdateSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError> {
        let mut request = new_workflow_update_schedule_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_schedule_from_proto(
            self.client.upsert_schedule(request).await?.into_inner(),
        )?)
    }

    /// Deletes a workflow schedule.
    pub async fn delete_schedule(
        &mut self,
        input: WorkflowDeleteSchedule,
    ) -> std::result::Result<(), WorkflowError> {
        let mut request = new_workflow_delete_schedule_request(input);
        request.invocation_token = self.invocation_token.clone();
        self.client.delete_schedule(request).await?;
        Ok(())
    }

    /// Pauses a workflow schedule.
    pub async fn pause_schedule(
        &mut self,
        input: WorkflowPauseSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError> {
        let mut request = new_workflow_pause_schedule_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_schedule_from_proto(
            self.client.pause_schedule(request).await?.into_inner(),
        )?)
    }

    /// Resumes a workflow schedule.
    pub async fn resume_schedule(
        &mut self,
        input: WorkflowResumeSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError> {
        let mut request = new_workflow_resume_schedule_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_schedule_from_proto(
            self.client.resume_schedule(request).await?.into_inner(),
        )?)
    }

    /// Creates an event trigger.
    pub async fn create_trigger(
        &mut self,
        input: WorkflowCreateEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError> {
        let mut request = new_workflow_create_event_trigger_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(workflow_event_trigger_from_proto(
            self.client
                .upsert_event_trigger(request)
                .await?
                .into_inner(),
        )?)
    }

    /// Fetches one event trigger.
    pub async fn get_trigger(
        &mut self,
        input: WorkflowGetEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError> {
        let mut request = new_workflow_get_event_trigger_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_event_trigger_from_proto(
            self.client.get_event_trigger(request).await?.into_inner(),
        )?)
    }

    /// Updates an event trigger.
    pub async fn update_trigger(
        &mut self,
        input: WorkflowUpdateEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError> {
        let mut request = new_workflow_update_event_trigger_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_event_trigger_from_proto(
            self.client
                .upsert_event_trigger(request)
                .await?
                .into_inner(),
        )?)
    }

    /// Deletes an event trigger.
    pub async fn delete_trigger(
        &mut self,
        input: WorkflowDeleteEventTrigger,
    ) -> std::result::Result<(), WorkflowError> {
        let mut request = new_workflow_delete_event_trigger_request(input);
        request.invocation_token = self.invocation_token.clone();
        self.client.delete_event_trigger(request).await?;
        Ok(())
    }

    /// Pauses an event trigger.
    pub async fn pause_trigger(
        &mut self,
        input: WorkflowPauseEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError> {
        let mut request = new_workflow_pause_event_trigger_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_event_trigger_from_proto(
            self.client.pause_event_trigger(request).await?.into_inner(),
        )?)
    }

    /// Resumes an event trigger.
    pub async fn resume_trigger(
        &mut self,
        input: WorkflowResumeEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError> {
        let mut request = new_workflow_resume_event_trigger_request(input);
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_event_trigger_from_proto(
            self.client
                .resume_event_trigger(request)
                .await?
                .into_inner(),
        )?)
    }

    /// Publishes an event into the workflow.
    pub async fn publish_event(
        &mut self,
        input: WorkflowPublishEvent,
    ) -> std::result::Result<WorkflowEvent, WorkflowError> {
        let mut request = new_workflow_publish_event_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(crate::workflow::workflow_event_from_proto(
            self.client.publish_event(request).await?.into_inner(),
        )?)
    }
}

#[async_trait]
impl WorkflowContract for Workflow {
    async fn start_run(
        &mut self,
        input: WorkflowStartRun,
    ) -> std::result::Result<WorkflowRun, WorkflowError> {
        Workflow::start_run(self, input).await
    }

    async fn signal_run(
        &mut self,
        input: WorkflowSignalRun,
    ) -> std::result::Result<WorkflowRunSignal, WorkflowError> {
        Workflow::signal_run(self, input).await
    }

    async fn signal_or_start_run(
        &mut self,
        input: WorkflowSignalOrStartRun,
    ) -> std::result::Result<WorkflowRunSignal, WorkflowError> {
        Workflow::signal_or_start_run(self, input).await
    }

    async fn create_definition(
        &mut self,
        input: WorkflowCreateDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError> {
        Workflow::create_definition(self, input).await
    }

    async fn get_definition(
        &mut self,
        input: WorkflowGetDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError> {
        Workflow::get_definition(self, input).await
    }

    async fn update_definition(
        &mut self,
        input: WorkflowUpdateDefinition,
    ) -> std::result::Result<WorkflowDefinition, WorkflowError> {
        Workflow::update_definition(self, input).await
    }

    async fn delete_definition(
        &mut self,
        input: WorkflowDeleteDefinition,
    ) -> std::result::Result<(), WorkflowError> {
        Workflow::delete_definition(self, input).await
    }

    async fn create_schedule(
        &mut self,
        input: WorkflowCreateSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError> {
        Workflow::create_schedule(self, input).await
    }

    async fn get_schedule(
        &mut self,
        input: WorkflowGetSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError> {
        Workflow::get_schedule(self, input).await
    }

    async fn update_schedule(
        &mut self,
        input: WorkflowUpdateSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError> {
        Workflow::update_schedule(self, input).await
    }

    async fn delete_schedule(
        &mut self,
        input: WorkflowDeleteSchedule,
    ) -> std::result::Result<(), WorkflowError> {
        Workflow::delete_schedule(self, input).await
    }

    async fn pause_schedule(
        &mut self,
        input: WorkflowPauseSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError> {
        Workflow::pause_schedule(self, input).await
    }

    async fn resume_schedule(
        &mut self,
        input: WorkflowResumeSchedule,
    ) -> std::result::Result<WorkflowSchedule, WorkflowError> {
        Workflow::resume_schedule(self, input).await
    }

    async fn create_trigger(
        &mut self,
        input: WorkflowCreateEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError> {
        Workflow::create_trigger(self, input).await
    }

    async fn get_trigger(
        &mut self,
        input: WorkflowGetEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError> {
        Workflow::get_trigger(self, input).await
    }

    async fn update_trigger(
        &mut self,
        input: WorkflowUpdateEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError> {
        Workflow::update_trigger(self, input).await
    }

    async fn delete_trigger(
        &mut self,
        input: WorkflowDeleteEventTrigger,
    ) -> std::result::Result<(), WorkflowError> {
        Workflow::delete_trigger(self, input).await
    }

    async fn pause_trigger(
        &mut self,
        input: WorkflowPauseEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError> {
        Workflow::pause_trigger(self, input).await
    }

    async fn resume_trigger(
        &mut self,
        input: WorkflowResumeEventTrigger,
    ) -> std::result::Result<WorkflowEventTrigger, WorkflowError> {
        Workflow::resume_trigger(self, input).await
    }

    async fn publish_event(
        &mut self,
        input: WorkflowPublishEvent,
    ) -> std::result::Result<WorkflowEvent, WorkflowError> {
        Workflow::publish_event(self, input).await
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
