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

    pub async fn search_tools(
        &mut self,
        request: pb::SearchAgentToolsRequest,
    ) -> std::result::Result<pb::SearchAgentToolsResponse, AgentHostError> {
        Ok(self.client.search_tools(request).await?.into_inner())
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
    async fn create_session(
        &self,
        request: GrpcRequest<pb::CreateAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentSession>, Status> {
        self.provider.create_session(request).await
    }

    async fn get_session(
        &self,
        request: GrpcRequest<pb::GetAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentSession>, Status> {
        self.provider.get_session(request).await
    }

    async fn list_sessions(
        &self,
        request: GrpcRequest<pb::ListAgentProviderSessionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderSessionsResponse>, Status> {
        self.provider.list_sessions(request).await
    }

    async fn update_session(
        &self,
        request: GrpcRequest<pb::UpdateAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentSession>, Status> {
        self.provider.update_session(request).await
    }

    async fn create_turn(
        &self,
        request: GrpcRequest<pb::CreateAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        self.provider.create_turn(request).await
    }

    async fn get_turn(
        &self,
        request: GrpcRequest<pb::GetAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        self.provider.get_turn(request).await
    }

    async fn list_turns(
        &self,
        request: GrpcRequest<pb::ListAgentProviderTurnsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderTurnsResponse>, Status> {
        self.provider.list_turns(request).await
    }

    async fn cancel_turn(
        &self,
        request: GrpcRequest<pb::CancelAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        self.provider.cancel_turn(request).await
    }

    async fn list_turn_events(
        &self,
        request: GrpcRequest<pb::ListAgentProviderTurnEventsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderTurnEventsResponse>, Status> {
        self.provider.list_turn_events(request).await
    }

    async fn get_interaction(
        &self,
        request: GrpcRequest<pb::GetAgentProviderInteractionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentInteraction>, Status> {
        self.provider.get_interaction(request).await
    }

    async fn list_interactions(
        &self,
        request: GrpcRequest<pb::ListAgentProviderInteractionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderInteractionsResponse>, Status> {
        self.provider.list_interactions(request).await
    }

    async fn resolve_interaction(
        &self,
        request: GrpcRequest<pb::ResolveAgentProviderInteractionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentInteraction>, Status> {
        self.provider.resolve_interaction(request).await
    }

    async fn get_capabilities(
        &self,
        request: GrpcRequest<pb::GetAgentProviderCapabilitiesRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentProviderCapabilities>, Status> {
        self.provider.get_capabilities(request).await
    }
}
