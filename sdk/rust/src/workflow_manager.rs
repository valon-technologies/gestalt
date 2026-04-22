use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::Request;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, Endpoint, Uri};
use tower::service_fn;

use crate::generated::v1::{
    self as pb,
    workflow_manager_host_client::WorkflowManagerHostClient as ProtoWorkflowManagerHostClient,
};

type WorkflowManagerTransport = InterceptedService<Channel, RelayTokenInterceptor>;

pub const ENV_WORKFLOW_MANAGER_SOCKET: &str = "GESTALT_WORKFLOW_MANAGER_SOCKET";
pub const ENV_WORKFLOW_MANAGER_SOCKET_TOKEN: &str = "GESTALT_WORKFLOW_MANAGER_SOCKET_TOKEN";
const WORKFLOW_MANAGER_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

#[derive(Debug, thiserror::Error)]
pub enum WorkflowManagerError {
    #[error("workflow manager: invocation token is not available")]
    MissingInvocationToken,
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    #[error("{0}")]
    Status(#[from] tonic::Status),
    #[error("{0}")]
    Env(String),
}

pub struct WorkflowManager {
    client: ProtoWorkflowManagerHostClient<WorkflowManagerTransport>,
    invocation_token: String,
}

impl WorkflowManager {
    pub async fn connect(
        invocation_token: impl AsRef<str>,
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
        })
    }

    pub async fn create_schedule(
        &mut self,
        mut request: pb::WorkflowManagerCreateScheduleRequest,
    ) -> std::result::Result<pb::ManagedWorkflowSchedule, WorkflowManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self.client.create_schedule(request).await?.into_inner())
    }

    pub async fn get_schedule(
        &mut self,
        mut request: pb::WorkflowManagerGetScheduleRequest,
    ) -> std::result::Result<pb::ManagedWorkflowSchedule, WorkflowManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self.client.get_schedule(request).await?.into_inner())
    }

    pub async fn update_schedule(
        &mut self,
        mut request: pb::WorkflowManagerUpdateScheduleRequest,
    ) -> std::result::Result<pb::ManagedWorkflowSchedule, WorkflowManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self.client.update_schedule(request).await?.into_inner())
    }

    pub async fn delete_schedule(
        &mut self,
        mut request: pb::WorkflowManagerDeleteScheduleRequest,
    ) -> std::result::Result<(), WorkflowManagerError> {
        request.invocation_token = self.invocation_token.clone();
        self.client.delete_schedule(request).await?;
        Ok(())
    }

    pub async fn pause_schedule(
        &mut self,
        mut request: pb::WorkflowManagerPauseScheduleRequest,
    ) -> std::result::Result<pb::ManagedWorkflowSchedule, WorkflowManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self.client.pause_schedule(request).await?.into_inner())
    }

    pub async fn resume_schedule(
        &mut self,
        mut request: pb::WorkflowManagerResumeScheduleRequest,
    ) -> std::result::Result<pb::ManagedWorkflowSchedule, WorkflowManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self.client.resume_schedule(request).await?.into_inner())
    }

    pub async fn create_trigger(
        &mut self,
        mut request: pb::WorkflowManagerCreateEventTriggerRequest,
    ) -> std::result::Result<pb::ManagedWorkflowEventTrigger, WorkflowManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self
            .client
            .create_event_trigger(request)
            .await?
            .into_inner())
    }

    pub async fn get_trigger(
        &mut self,
        mut request: pb::WorkflowManagerGetEventTriggerRequest,
    ) -> std::result::Result<pb::ManagedWorkflowEventTrigger, WorkflowManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self.client.get_event_trigger(request).await?.into_inner())
    }

    pub async fn update_trigger(
        &mut self,
        mut request: pb::WorkflowManagerUpdateEventTriggerRequest,
    ) -> std::result::Result<pb::ManagedWorkflowEventTrigger, WorkflowManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self
            .client
            .update_event_trigger(request)
            .await?
            .into_inner())
    }

    pub async fn delete_trigger(
        &mut self,
        mut request: pb::WorkflowManagerDeleteEventTriggerRequest,
    ) -> std::result::Result<(), WorkflowManagerError> {
        request.invocation_token = self.invocation_token.clone();
        self.client.delete_event_trigger(request).await?;
        Ok(())
    }

    pub async fn pause_trigger(
        &mut self,
        mut request: pb::WorkflowManagerPauseEventTriggerRequest,
    ) -> std::result::Result<pb::ManagedWorkflowEventTrigger, WorkflowManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self.client.pause_event_trigger(request).await?.into_inner())
    }

    pub async fn resume_trigger(
        &mut self,
        mut request: pb::WorkflowManagerResumeEventTriggerRequest,
    ) -> std::result::Result<pb::ManagedWorkflowEventTrigger, WorkflowManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self
            .client
            .resume_event_trigger(request)
            .await?
            .into_inner())
    }

    pub async fn publish_event(
        &mut self,
        mut request: pb::WorkflowManagerPublishEventRequest,
    ) -> std::result::Result<pb::WorkflowEvent, WorkflowManagerError> {
        request.invocation_token = self.invocation_token.clone();
        Ok(self.client.publish_event(request).await?.into_inner())
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
