use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::transport::{Channel, Endpoint, Uri};
use tower::service_fn;

use crate::generated::v1::{
    self as pb,
    workflow_manager_host_client::WorkflowManagerHostClient as ProtoWorkflowManagerHostClient,
};

pub const ENV_WORKFLOW_MANAGER_SOCKET: &str = "GESTALT_WORKFLOW_MANAGER_SOCKET";

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
    client: ProtoWorkflowManagerHostClient<Channel>,
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
        let channel = Endpoint::try_from("http://[::]:50051")?
            .connect_with_connector(service_fn(move |_: Uri| {
                let path = socket_path.clone();
                async move { UnixStream::connect(path).await.map(TokioIo::new) }
            }))
            .await?;

        Ok(Self {
            client: ProtoWorkflowManagerHostClient::new(channel),
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
}
