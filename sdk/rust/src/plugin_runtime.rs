use std::sync::Arc;

use tonic::codegen::async_trait;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::api::RuntimeMetadata;
use crate::error::Result as ProviderResult;
use crate::generated::v1::{self as pb};

#[async_trait]
/// Provider trait for serving hosted plugin-runtime sessions.
pub trait PluginRuntimeProvider:
    pb::plugin_runtime_provider_server::PluginRuntimeProvider + Send + Sync + 'static
{
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
}

#[derive(Clone)]
pub(crate) struct PluginRuntimeServer<P> {
    provider: Arc<P>,
}

impl<P> PluginRuntimeServer<P> {
    pub(crate) fn new(provider: Arc<P>) -> Self {
        Self { provider }
    }
}

#[async_trait]
impl<P> pb::plugin_runtime_provider_server::PluginRuntimeProvider for PluginRuntimeServer<P>
where
    P: PluginRuntimeProvider,
{
    async fn get_support(
        &self,
        request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<pb::PluginRuntimeSupport>, Status> {
        self.provider.get_support(request).await
    }

    async fn start_session(
        &self,
        request: GrpcRequest<pb::StartPluginRuntimeSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::PluginRuntimeSession>, Status> {
        self.provider.start_session(request).await
    }

    async fn get_session(
        &self,
        request: GrpcRequest<pb::GetPluginRuntimeSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::PluginRuntimeSession>, Status> {
        self.provider.get_session(request).await
    }

    async fn list_sessions(
        &self,
        request: GrpcRequest<pb::ListPluginRuntimeSessionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListPluginRuntimeSessionsResponse>, Status> {
        self.provider.list_sessions(request).await
    }

    async fn stop_session(
        &self,
        request: GrpcRequest<pb::StopPluginRuntimeSessionRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider.stop_session(request).await
    }

    async fn prepare_workspace(
        &self,
        request: GrpcRequest<pb::PreparePluginRuntimeWorkspaceRequest>,
    ) -> std::result::Result<GrpcResponse<pb::PreparePluginRuntimeWorkspaceResponse>, Status> {
        self.provider.prepare_workspace(request).await
    }

    async fn remove_workspace(
        &self,
        request: GrpcRequest<pb::RemovePluginRuntimeWorkspaceRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider.remove_workspace(request).await
    }

    async fn start_plugin(
        &self,
        request: GrpcRequest<pb::StartHostedPluginRequest>,
    ) -> std::result::Result<GrpcResponse<pb::HostedPlugin>, Status> {
        self.provider.start_plugin(request).await
    }
}
