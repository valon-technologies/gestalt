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
    self as pb, agent_host_client::AgentHostClient as ProtoAgentHostClient,
};

pub const ENV_AGENT_HOST_SOCKET: &str = "GESTALT_AGENT_HOST_SOCKET";

#[derive(Debug, thiserror::Error)]
pub enum AgentHostError {
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    #[error("{0}")]
    Status(#[from] tonic::Status),
    #[error("{0}")]
    Env(String),
}

pub struct AgentHost {
    client: ProtoAgentHostClient<Channel>,
}

impl AgentHost {
    pub async fn connect() -> std::result::Result<Self, AgentHostError> {
        let socket_path = std::env::var(ENV_AGENT_HOST_SOCKET)
            .map_err(|_| AgentHostError::Env(format!("{ENV_AGENT_HOST_SOCKET} is not set")))?;
        let channel = connect_unix(socket_path).await?;
        Ok(Self {
            client: ProtoAgentHostClient::new(channel),
        })
    }

    pub async fn execute_tool(
        &mut self,
        request: pb::ExecuteAgentToolRequest,
    ) -> std::result::Result<pb::ExecuteAgentToolResponse, AgentHostError> {
        Ok(self.client.execute_tool(request).await?.into_inner())
    }

    pub async fn emit_event(
        &mut self,
        request: pb::EmitAgentEventRequest,
    ) -> std::result::Result<(), AgentHostError> {
        self.client.emit_event(request).await?;
        Ok(())
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
pub trait AgentProvider: pb::agent_provider_server::AgentProvider + Send + Sync + 'static {
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
pub(crate) struct AgentServer<P> {
    provider: Arc<P>,
}

impl<P> AgentServer<P> {
    pub(crate) fn new(provider: Arc<P>) -> Self {
        Self { provider }
    }
}

#[async_trait]
impl<P> pb::agent_provider_server::AgentProvider for AgentServer<P>
where
    P: AgentProvider,
{
    async fn start_run(
        &self,
        request: GrpcRequest<pb::StartAgentProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundAgentRun>, Status> {
        self.provider.start_run(request).await
    }

    async fn get_run(
        &self,
        request: GrpcRequest<pb::GetAgentProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundAgentRun>, Status> {
        self.provider.get_run(request).await
    }

    async fn list_runs(
        &self,
        request: GrpcRequest<pb::ListAgentProviderRunsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderRunsResponse>, Status> {
        self.provider.list_runs(request).await
    }

    async fn cancel_run(
        &self,
        request: GrpcRequest<pb::CancelAgentProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::BoundAgentRun>, Status> {
        self.provider.cancel_run(request).await
    }
}
