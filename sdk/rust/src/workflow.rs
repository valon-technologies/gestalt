use std::sync::Arc;

use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::codegen::async_trait;
use tonic::transport::{Channel, Endpoint, Uri};
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};
use tower::service_fn;

use crate::api::RuntimeMetadata;
use crate::error::Result as ProviderResult;
use crate::generated::v1::{
    self as pb, workflow_host_client::WorkflowHostClient as ProtoWorkflowHostClient,
};

pub const ENV_WORKFLOW_HOST_SOCKET: &str = "GESTALT_WORKFLOW_HOST_SOCKET";

#[derive(Debug, thiserror::Error)]
pub enum WorkflowHostError {
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    #[error("{0}")]
    Status(#[from] tonic::Status),
    #[error("{0}")]
    Env(String),
}

pub struct WorkflowHost {
    client: ProtoWorkflowHostClient<Channel>,
}

impl WorkflowHost {
    pub async fn connect() -> std::result::Result<Self, WorkflowHostError> {
        let socket_path = std::env::var(ENV_WORKFLOW_HOST_SOCKET).map_err(|_| {
            WorkflowHostError::Env(format!("{ENV_WORKFLOW_HOST_SOCKET} is not set"))
        })?;
        let channel = connect_unix(socket_path).await?;
        Ok(Self {
            client: ProtoWorkflowHostClient::new(channel),
        })
    }

    pub async fn invoke_operation(
        &mut self,
        request: pb::InvokeWorkflowOperationRequest,
    ) -> std::result::Result<pb::InvokeWorkflowOperationResponse, WorkflowHostError> {
        Ok(self.client.invoke_operation(request).await?.into_inner())
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

#[async_trait]
pub trait WorkflowProvider:
    pb::workflow_provider_server::WorkflowProvider + Send + Sync + 'static
{
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> ProviderResult<()> {
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        None
    }

    fn warnings(&self) -> Vec<String> {
        Vec::new()
    }

    async fn health_check(&self) -> ProviderResult<()> {
        Ok(())
    }

    async fn close(&self) -> ProviderResult<()> {
        Ok(())
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
        self.provider.start_run(request).await
    }

    async fn get_run(
        &self,
        request: GrpcRequest<pb::GetWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowRun>, Status> {
        self.provider.get_run(request).await
    }

    async fn list_runs(
        &self,
        request: GrpcRequest<pb::ListWorkflowProviderRunsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListWorkflowProviderRunsResponse>, Status> {
        self.provider.list_runs(request).await
    }

    async fn cancel_run(
        &self,
        request: GrpcRequest<pb::CancelWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowRun>, Status> {
        self.provider.cancel_run(request).await
    }

    async fn signal_run(
        &self,
        request: GrpcRequest<pb::SignalWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::SignalWorkflowRunResponse>, Status> {
        self.provider.signal_run(request).await
    }

    async fn signal_or_start_run(
        &self,
        request: GrpcRequest<pb::SignalOrStartWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::SignalWorkflowRunResponse>, Status> {
        self.provider.signal_or_start_run(request).await
    }

    async fn upsert_schedule(
        &self,
        request: GrpcRequest<pb::UpsertWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowSchedule>, Status> {
        self.provider.upsert_schedule(request).await
    }

    async fn get_schedule(
        &self,
        request: GrpcRequest<pb::GetWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowSchedule>, Status> {
        self.provider.get_schedule(request).await
    }

    async fn list_schedules(
        &self,
        request: GrpcRequest<pb::ListWorkflowProviderSchedulesRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListWorkflowProviderSchedulesResponse>, Status> {
        self.provider.list_schedules(request).await
    }

    async fn delete_schedule(
        &self,
        request: GrpcRequest<pb::DeleteWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider.delete_schedule(request).await
    }

    async fn pause_schedule(
        &self,
        request: GrpcRequest<pb::PauseWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowSchedule>, Status> {
        self.provider.pause_schedule(request).await
    }

    async fn resume_schedule(
        &self,
        request: GrpcRequest<pb::ResumeWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowSchedule>, Status> {
        self.provider.resume_schedule(request).await
    }

    async fn upsert_event_trigger(
        &self,
        request: GrpcRequest<pb::UpsertWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowEventTrigger>, Status> {
        self.provider.upsert_event_trigger(request).await
    }

    async fn get_event_trigger(
        &self,
        request: GrpcRequest<pb::GetWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowEventTrigger>, Status> {
        self.provider.get_event_trigger(request).await
    }

    async fn list_event_triggers(
        &self,
        request: GrpcRequest<pb::ListWorkflowProviderEventTriggersRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListWorkflowProviderEventTriggersResponse>, Status>
    {
        self.provider.list_event_triggers(request).await
    }

    async fn delete_event_trigger(
        &self,
        request: GrpcRequest<pb::DeleteWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider.delete_event_trigger(request).await
    }

    async fn pause_event_trigger(
        &self,
        request: GrpcRequest<pb::PauseWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowEventTrigger>, Status> {
        self.provider.pause_event_trigger(request).await
    }

    async fn resume_event_trigger(
        &self,
        request: GrpcRequest<pb::ResumeWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundWorkflowEventTrigger>, Status> {
        self.provider.resume_event_trigger(request).await
    }

    async fn publish_event(
        &self,
        request: GrpcRequest<pb::PublishWorkflowProviderEventRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider.publish_event(request).await
    }

    async fn put_execution_reference(
        &self,
        request: GrpcRequest<pb::PutWorkflowExecutionReferenceRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowExecutionReference>, Status> {
        self.provider.put_execution_reference(request).await
    }

    async fn get_execution_reference(
        &self,
        request: GrpcRequest<pb::GetWorkflowExecutionReferenceRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowExecutionReference>, Status> {
        self.provider.get_execution_reference(request).await
    }

    async fn list_execution_references(
        &self,
        request: GrpcRequest<pb::ListWorkflowExecutionReferencesRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListWorkflowExecutionReferencesResponse>, Status>
    {
        self.provider.list_execution_references(request).await
    }
}
