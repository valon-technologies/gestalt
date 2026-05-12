use std::time::SystemTime;

use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::Request;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tower::service_fn;

use crate::generated::v1::{
    self as pb,
    workflow_manager_host_client::WorkflowManagerHostClient as ProtoWorkflowManagerHostClient,
};
use crate::protocol;
use crate::workflow::{
    BoundWorkflowTargetInput, WorkflowActorInput, WorkflowEventInput, WorkflowEventMatchInput,
    WorkflowRunTriggerInput, WorkflowSignalInput, bound_workflow_target_input_from_target,
    new_bound_workflow_target, new_workflow_event, new_workflow_event_match, new_workflow_signal,
    workflow_actor_input_from_actor, workflow_event_input_from_event,
    workflow_event_match_input_from_match, workflow_run_trigger_input_from_trigger,
    workflow_signal_input_from_signal,
};

type WorkflowManagerTransport = InterceptedService<Channel, RelayTokenInterceptor>;

/// Environment variable containing the workflow-manager host-service target.
pub const ENV_WORKFLOW_MANAGER_SOCKET: &str = "GESTALT_WORKFLOW_MANAGER_SOCKET";
/// Environment variable containing the optional workflow-manager relay token.
pub const ENV_WORKFLOW_MANAGER_SOCKET_TOKEN: &str = "GESTALT_WORKFLOW_MANAGER_SOCKET_TOKEN";
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
pub struct WorkflowManagerStartRunInput {
    pub provider_name: String,
    pub target: Option<BoundWorkflowTargetInput>,
    pub idempotency_key: String,
    pub workflow_key: String,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerSignalRunInput {
    pub run_id: String,
    pub signal: Option<WorkflowSignalInput>,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerSignalOrStartRunInput {
    pub provider_name: String,
    pub workflow_key: String,
    pub target: Option<BoundWorkflowTargetInput>,
    pub idempotency_key: String,
    pub signal: Option<WorkflowSignalInput>,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerCreateDefinitionInput {
    pub provider_name: String,
    pub target: Option<BoundWorkflowTargetInput>,
    pub idempotency_key: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerGetDefinitionInput {
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerUpdateDefinitionInput {
    pub definition_id: String,
    pub provider_name: String,
    pub target: Option<BoundWorkflowTargetInput>,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerDeleteDefinitionInput {
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerCreateScheduleInput {
    pub provider_name: String,
    pub cron: String,
    pub timezone: String,
    pub target: Option<BoundWorkflowTargetInput>,
    pub paused: bool,
    pub idempotency_key: String,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerGetScheduleInput {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerUpdateScheduleInput {
    pub schedule_id: String,
    pub provider_name: String,
    pub cron: String,
    pub timezone: String,
    pub target: Option<BoundWorkflowTargetInput>,
    pub paused: bool,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerDeleteScheduleInput {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerPauseScheduleInput {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerResumeScheduleInput {
    pub schedule_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerCreateEventTriggerInput {
    pub provider_name: String,
    pub event_match: Option<WorkflowEventMatchInput>,
    pub target: Option<BoundWorkflowTargetInput>,
    pub paused: bool,
    pub idempotency_key: String,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerGetEventTriggerInput {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerUpdateEventTriggerInput {
    pub trigger_id: String,
    pub provider_name: String,
    pub event_match: Option<WorkflowEventMatchInput>,
    pub target: Option<BoundWorkflowTargetInput>,
    pub paused: bool,
    pub definition_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerDeleteEventTriggerInput {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerPauseEventTriggerInput {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerResumeEventTriggerInput {
    pub trigger_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerPublishEventInput {
    pub provider_name: String,
    pub event: Option<WorkflowEventInput>,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerBoundRun {
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

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerBoundDefinition {
    pub id: String,
    pub target: Option<BoundWorkflowTargetInput>,
    pub created_by: Option<WorkflowActorInput>,
    pub created_at: Option<SystemTime>,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerBoundSchedule {
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

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerBoundEventTrigger {
    pub id: String,
    pub r#match: Option<WorkflowEventMatchInput>,
    pub target: Option<BoundWorkflowTargetInput>,
    pub paused: bool,
    pub created_at: Option<SystemTime>,
    pub updated_at: Option<SystemTime>,
    pub created_by: Option<WorkflowActorInput>,
    pub execution_ref: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerRun {
    pub provider_name: String,
    pub run: Option<WorkflowManagerBoundRun>,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerRunSignal {
    pub provider_name: String,
    pub run: Option<WorkflowManagerBoundRun>,
    pub signal: Option<WorkflowSignalInput>,
    pub started_run: bool,
    pub workflow_key: String,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerDefinition {
    pub provider_name: String,
    pub definition: Option<WorkflowManagerBoundDefinition>,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerSchedule {
    pub provider_name: String,
    pub schedule: Option<WorkflowManagerBoundSchedule>,
}

#[derive(Clone, Debug, Default)]
pub struct WorkflowManagerEventTrigger {
    pub provider_name: String,
    pub trigger: Option<WorkflowManagerBoundEventTrigger>,
}

pub type WorkflowManagerPublishedEvent = WorkflowEventInput;

pub fn new_workflow_manager_start_run_request(
    input: WorkflowManagerStartRunInput,
) -> crate::Result<pb::WorkflowManagerStartRunRequest> {
    Ok(pb::WorkflowManagerStartRunRequest {
        provider_name: input.provider_name,
        target: input.target.map(new_bound_workflow_target).transpose()?,
        idempotency_key: input.idempotency_key,
        workflow_key: input.workflow_key,
        invocation_token: String::new(),
        definition_id: input.definition_id,
    })
}

pub fn new_workflow_manager_signal_run_request(
    input: WorkflowManagerSignalRunInput,
) -> crate::Result<pb::WorkflowManagerSignalRunRequest> {
    Ok(pb::WorkflowManagerSignalRunRequest {
        run_id: input.run_id,
        signal: input.signal.map(new_workflow_signal).transpose()?,
        invocation_token: String::new(),
    })
}

pub fn new_workflow_manager_signal_or_start_run_request(
    input: WorkflowManagerSignalOrStartRunInput,
) -> crate::Result<pb::WorkflowManagerSignalOrStartRunRequest> {
    Ok(pb::WorkflowManagerSignalOrStartRunRequest {
        provider_name: input.provider_name,
        workflow_key: input.workflow_key,
        target: input.target.map(new_bound_workflow_target).transpose()?,
        idempotency_key: input.idempotency_key,
        signal: input.signal.map(new_workflow_signal).transpose()?,
        invocation_token: String::new(),
        definition_id: input.definition_id,
    })
}

pub fn new_workflow_manager_create_definition_request(
    input: WorkflowManagerCreateDefinitionInput,
) -> crate::Result<pb::WorkflowManagerCreateDefinitionRequest> {
    Ok(pb::WorkflowManagerCreateDefinitionRequest {
        provider_name: input.provider_name,
        target: input.target.map(new_bound_workflow_target).transpose()?,
        invocation_token: String::new(),
        idempotency_key: input.idempotency_key,
    })
}

pub fn new_workflow_manager_get_definition_request(
    input: WorkflowManagerGetDefinitionInput,
) -> pb::WorkflowManagerGetDefinitionRequest {
    pb::WorkflowManagerGetDefinitionRequest {
        definition_id: input.definition_id,
        invocation_token: String::new(),
    }
}

pub fn new_workflow_manager_update_definition_request(
    input: WorkflowManagerUpdateDefinitionInput,
) -> crate::Result<pb::WorkflowManagerUpdateDefinitionRequest> {
    Ok(pb::WorkflowManagerUpdateDefinitionRequest {
        definition_id: input.definition_id,
        provider_name: input.provider_name,
        target: input.target.map(new_bound_workflow_target).transpose()?,
        invocation_token: String::new(),
    })
}

pub fn new_workflow_manager_delete_definition_request(
    input: WorkflowManagerDeleteDefinitionInput,
) -> pb::WorkflowManagerDeleteDefinitionRequest {
    pb::WorkflowManagerDeleteDefinitionRequest {
        definition_id: input.definition_id,
        invocation_token: String::new(),
    }
}

pub fn new_workflow_manager_create_schedule_request(
    input: WorkflowManagerCreateScheduleInput,
) -> crate::Result<pb::WorkflowManagerCreateScheduleRequest> {
    Ok(pb::WorkflowManagerCreateScheduleRequest {
        provider_name: input.provider_name,
        cron: input.cron,
        timezone: input.timezone,
        target: input.target.map(new_bound_workflow_target).transpose()?,
        paused: input.paused,
        invocation_token: String::new(),
        idempotency_key: input.idempotency_key,
        definition_id: input.definition_id,
    })
}

pub fn new_workflow_manager_get_schedule_request(
    input: WorkflowManagerGetScheduleInput,
) -> pb::WorkflowManagerGetScheduleRequest {
    pb::WorkflowManagerGetScheduleRequest {
        schedule_id: input.schedule_id,
        invocation_token: String::new(),
    }
}

pub fn new_workflow_manager_update_schedule_request(
    input: WorkflowManagerUpdateScheduleInput,
) -> crate::Result<pb::WorkflowManagerUpdateScheduleRequest> {
    Ok(pb::WorkflowManagerUpdateScheduleRequest {
        schedule_id: input.schedule_id,
        provider_name: input.provider_name,
        cron: input.cron,
        timezone: input.timezone,
        target: input.target.map(new_bound_workflow_target).transpose()?,
        paused: input.paused,
        invocation_token: String::new(),
        definition_id: input.definition_id,
    })
}

pub fn new_workflow_manager_delete_schedule_request(
    input: WorkflowManagerDeleteScheduleInput,
) -> pb::WorkflowManagerDeleteScheduleRequest {
    pb::WorkflowManagerDeleteScheduleRequest {
        schedule_id: input.schedule_id,
        invocation_token: String::new(),
    }
}

pub fn new_workflow_manager_pause_schedule_request(
    input: WorkflowManagerPauseScheduleInput,
) -> pb::WorkflowManagerPauseScheduleRequest {
    pb::WorkflowManagerPauseScheduleRequest {
        schedule_id: input.schedule_id,
        invocation_token: String::new(),
    }
}

pub fn new_workflow_manager_resume_schedule_request(
    input: WorkflowManagerResumeScheduleInput,
) -> pb::WorkflowManagerResumeScheduleRequest {
    pb::WorkflowManagerResumeScheduleRequest {
        schedule_id: input.schedule_id,
        invocation_token: String::new(),
    }
}

pub fn new_workflow_manager_create_event_trigger_request(
    input: WorkflowManagerCreateEventTriggerInput,
) -> crate::Result<pb::WorkflowManagerCreateEventTriggerRequest> {
    Ok(pb::WorkflowManagerCreateEventTriggerRequest {
        provider_name: input.provider_name,
        r#match: input.event_match.map(new_workflow_event_match),
        target: input.target.map(new_bound_workflow_target).transpose()?,
        paused: input.paused,
        invocation_token: String::new(),
        idempotency_key: input.idempotency_key,
        definition_id: input.definition_id,
    })
}

pub fn new_workflow_manager_get_event_trigger_request(
    input: WorkflowManagerGetEventTriggerInput,
) -> pb::WorkflowManagerGetEventTriggerRequest {
    pb::WorkflowManagerGetEventTriggerRequest {
        trigger_id: input.trigger_id,
        invocation_token: String::new(),
    }
}

pub fn new_workflow_manager_update_event_trigger_request(
    input: WorkflowManagerUpdateEventTriggerInput,
) -> crate::Result<pb::WorkflowManagerUpdateEventTriggerRequest> {
    Ok(pb::WorkflowManagerUpdateEventTriggerRequest {
        trigger_id: input.trigger_id,
        provider_name: input.provider_name,
        r#match: input.event_match.map(new_workflow_event_match),
        target: input.target.map(new_bound_workflow_target).transpose()?,
        paused: input.paused,
        invocation_token: String::new(),
        definition_id: input.definition_id,
    })
}

pub fn new_workflow_manager_delete_event_trigger_request(
    input: WorkflowManagerDeleteEventTriggerInput,
) -> pb::WorkflowManagerDeleteEventTriggerRequest {
    pb::WorkflowManagerDeleteEventTriggerRequest {
        trigger_id: input.trigger_id,
        invocation_token: String::new(),
    }
}

pub fn new_workflow_manager_pause_event_trigger_request(
    input: WorkflowManagerPauseEventTriggerInput,
) -> pb::WorkflowManagerPauseEventTriggerRequest {
    pb::WorkflowManagerPauseEventTriggerRequest {
        trigger_id: input.trigger_id,
        invocation_token: String::new(),
    }
}

pub fn new_workflow_manager_resume_event_trigger_request(
    input: WorkflowManagerResumeEventTriggerInput,
) -> pb::WorkflowManagerResumeEventTriggerRequest {
    pb::WorkflowManagerResumeEventTriggerRequest {
        trigger_id: input.trigger_id,
        invocation_token: String::new(),
    }
}

pub fn new_workflow_manager_publish_event_request(
    input: WorkflowManagerPublishEventInput,
) -> crate::Result<pb::WorkflowManagerPublishEventRequest> {
    Ok(pb::WorkflowManagerPublishEventRequest {
        event: input.event.map(new_workflow_event).transpose()?,
        invocation_token: String::new(),
        provider_name: input.provider_name,
    })
}

fn workflow_manager_bound_run_from_proto(
    input: pb::BoundWorkflowRun,
) -> crate::Result<WorkflowManagerBoundRun> {
    Ok(WorkflowManagerBoundRun {
        id: input.id,
        status: pb::WorkflowRunStatus::try_from(input.status).map_err(|_| {
            crate::Error::bad_request(format!("unknown workflow run status {}", input.status))
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
        status_message: input.status_message,
        result_body: input.result_body,
        created_by: input
            .created_by
            .as_ref()
            .map(workflow_actor_input_from_actor),
        execution_ref: input.execution_ref,
        workflow_key: input.workflow_key,
    })
}

fn workflow_manager_bound_definition_from_proto(
    input: pb::BoundWorkflowDefinition,
) -> crate::Result<WorkflowManagerBoundDefinition> {
    Ok(WorkflowManagerBoundDefinition {
        id: input.id,
        target: input
            .target
            .as_ref()
            .map(bound_workflow_target_input_from_target)
            .transpose()?,
        created_by: input
            .created_by
            .as_ref()
            .map(workflow_actor_input_from_actor),
        created_at: input
            .created_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
    })
}

fn workflow_manager_bound_schedule_from_proto(
    input: pb::BoundWorkflowSchedule,
) -> crate::Result<WorkflowManagerBoundSchedule> {
    Ok(WorkflowManagerBoundSchedule {
        id: input.id,
        cron: input.cron,
        timezone: input.timezone,
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
        execution_ref: input.execution_ref,
    })
}

fn workflow_manager_bound_event_trigger_from_proto(
    input: pb::BoundWorkflowEventTrigger,
) -> crate::Result<WorkflowManagerBoundEventTrigger> {
    Ok(WorkflowManagerBoundEventTrigger {
        id: input.id,
        r#match: input
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
        execution_ref: input.execution_ref,
    })
}

fn workflow_manager_run_from_proto(
    input: pb::ManagedWorkflowRun,
) -> crate::Result<WorkflowManagerRun> {
    Ok(WorkflowManagerRun {
        provider_name: input.provider_name,
        run: input
            .run
            .map(workflow_manager_bound_run_from_proto)
            .transpose()?,
    })
}

fn workflow_manager_run_signal_from_proto(
    input: pb::ManagedWorkflowRunSignal,
) -> crate::Result<WorkflowManagerRunSignal> {
    Ok(WorkflowManagerRunSignal {
        provider_name: input.provider_name,
        run: input
            .run
            .map(workflow_manager_bound_run_from_proto)
            .transpose()?,
        signal: input
            .signal
            .as_ref()
            .map(workflow_signal_input_from_signal)
            .transpose()?,
        started_run: input.started_run,
        workflow_key: input.workflow_key,
    })
}

fn workflow_manager_definition_from_proto(
    input: pb::ManagedWorkflowDefinition,
) -> crate::Result<WorkflowManagerDefinition> {
    Ok(WorkflowManagerDefinition {
        provider_name: input.provider_name,
        definition: input
            .definition
            .map(workflow_manager_bound_definition_from_proto)
            .transpose()?,
    })
}

fn workflow_manager_schedule_from_proto(
    input: pb::ManagedWorkflowSchedule,
) -> crate::Result<WorkflowManagerSchedule> {
    Ok(WorkflowManagerSchedule {
        provider_name: input.provider_name,
        schedule: input
            .schedule
            .map(workflow_manager_bound_schedule_from_proto)
            .transpose()?,
    })
}

fn workflow_manager_event_trigger_from_proto(
    input: pb::ManagedWorkflowEventTrigger,
) -> crate::Result<WorkflowManagerEventTrigger> {
    Ok(WorkflowManagerEventTrigger {
        provider_name: input.provider_name,
        trigger: input
            .trigger
            .map(workflow_manager_bound_event_trigger_from_proto)
            .transpose()?,
    })
}

/// Client for creating workflow definitions, starting runs, and managing schedules or triggers.
pub struct WorkflowManager {
    client: ProtoWorkflowManagerHostClient<WorkflowManagerTransport>,
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

        let socket_path = std::env::var(ENV_WORKFLOW_MANAGER_SOCKET).map_err(|_| {
            WorkflowManagerError::Env(format!("{ENV_WORKFLOW_MANAGER_SOCKET} is not set"))
        })?;
        let relay_token = std::env::var(ENV_WORKFLOW_MANAGER_SOCKET_TOKEN).unwrap_or_default();
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
            client: ProtoWorkflowManagerHostClient::with_interceptor(
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
        input: WorkflowManagerCreateDefinitionInput,
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
        input: WorkflowManagerGetDefinitionInput,
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
        input: WorkflowManagerUpdateDefinitionInput,
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
        input: WorkflowManagerDeleteDefinitionInput,
    ) -> std::result::Result<(), WorkflowManagerError> {
        let mut request = new_workflow_manager_delete_definition_request(input);
        request.invocation_token = self.invocation_token.clone();
        self.client.delete_definition(request).await?;
        Ok(())
    }

    /// Creates a workflow schedule.
    pub async fn create_schedule(
        &mut self,
        input: WorkflowManagerCreateScheduleInput,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError> {
        let mut request = new_workflow_manager_create_schedule_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(workflow_manager_schedule_from_proto(
            self.client.create_schedule(request).await?.into_inner(),
        )?)
    }

    /// Starts a workflow run.
    pub async fn start_run(
        &mut self,
        input: WorkflowManagerStartRunInput,
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
        input: WorkflowManagerSignalRunInput,
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
        input: WorkflowManagerSignalOrStartRunInput,
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
        input: WorkflowManagerGetScheduleInput,
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
        input: WorkflowManagerUpdateScheduleInput,
    ) -> std::result::Result<WorkflowManagerSchedule, WorkflowManagerError> {
        let mut request = new_workflow_manager_update_schedule_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_manager_schedule_from_proto(
            self.client.update_schedule(request).await?.into_inner(),
        )?)
    }

    /// Deletes a workflow schedule.
    pub async fn delete_schedule(
        &mut self,
        input: WorkflowManagerDeleteScheduleInput,
    ) -> std::result::Result<(), WorkflowManagerError> {
        let mut request = new_workflow_manager_delete_schedule_request(input);
        request.invocation_token = self.invocation_token.clone();
        self.client.delete_schedule(request).await?;
        Ok(())
    }

    /// Pauses a workflow schedule.
    pub async fn pause_schedule(
        &mut self,
        input: WorkflowManagerPauseScheduleInput,
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
        input: WorkflowManagerResumeScheduleInput,
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
        input: WorkflowManagerCreateEventTriggerInput,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError> {
        let mut request = new_workflow_manager_create_event_trigger_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        if request.idempotency_key.trim().is_empty() {
            request.idempotency_key = self.idempotency_key.clone();
        }
        Ok(workflow_manager_event_trigger_from_proto(
            self.client
                .create_event_trigger(request)
                .await?
                .into_inner(),
        )?)
    }

    /// Fetches one event trigger.
    pub async fn get_trigger(
        &mut self,
        input: WorkflowManagerGetEventTriggerInput,
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
        input: WorkflowManagerUpdateEventTriggerInput,
    ) -> std::result::Result<WorkflowManagerEventTrigger, WorkflowManagerError> {
        let mut request = new_workflow_manager_update_event_trigger_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_manager_event_trigger_from_proto(
            self.client
                .update_event_trigger(request)
                .await?
                .into_inner(),
        )?)
    }

    /// Deletes an event trigger.
    pub async fn delete_trigger(
        &mut self,
        input: WorkflowManagerDeleteEventTriggerInput,
    ) -> std::result::Result<(), WorkflowManagerError> {
        let mut request = new_workflow_manager_delete_event_trigger_request(input);
        request.invocation_token = self.invocation_token.clone();
        self.client.delete_event_trigger(request).await?;
        Ok(())
    }

    /// Pauses an event trigger.
    pub async fn pause_trigger(
        &mut self,
        input: WorkflowManagerPauseEventTriggerInput,
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
        input: WorkflowManagerResumeEventTriggerInput,
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
        input: WorkflowManagerPublishEventInput,
    ) -> std::result::Result<WorkflowManagerPublishedEvent, WorkflowManagerError> {
        let mut request = new_workflow_manager_publish_event_request(input)?;
        request.invocation_token = self.invocation_token.clone();
        Ok(workflow_event_input_from_event(
            &self.client.publish_event(request).await?.into_inner(),
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
