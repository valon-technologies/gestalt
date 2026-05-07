use std::sync::Arc;

use tonic::codegen::async_trait;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::api::RuntimeMetadata;
use crate::error::Result as ProviderResult;
use crate::generated::v1::{self as pb};
use crate::rpc_status::rpc_status;

#[async_trait]
/// Provider trait for serving hosted plugin-runtime sessions.
pub trait PluginRuntimeProvider: Send + Sync + 'static {
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

    async fn get_support(&self, _request: ()) -> ProviderResult<pb::PluginRuntimeSupport> {
        Err(crate::Error::unimplemented(
            "runtime get support is not implemented",
        ))
    }

    async fn start_session(
        &self,
        _request: pb::StartPluginRuntimeSessionRequest,
    ) -> ProviderResult<pb::PluginRuntimeSession> {
        Err(crate::Error::unimplemented(
            "runtime start session is not implemented",
        ))
    }

    async fn get_session(
        &self,
        _request: pb::GetPluginRuntimeSessionRequest,
    ) -> ProviderResult<pb::PluginRuntimeSession> {
        Err(crate::Error::unimplemented(
            "runtime get session is not implemented",
        ))
    }

    async fn list_sessions(
        &self,
        _request: pb::ListPluginRuntimeSessionsRequest,
    ) -> ProviderResult<pb::ListPluginRuntimeSessionsResponse> {
        Err(crate::Error::unimplemented(
            "runtime list sessions is not implemented",
        ))
    }

    async fn stop_session(
        &self,
        _request: pb::StopPluginRuntimeSessionRequest,
    ) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "runtime stop session is not implemented",
        ))
    }

    async fn prepare_workspace(
        &self,
        _request: pb::PreparePluginRuntimeWorkspaceRequest,
    ) -> ProviderResult<pb::PreparePluginRuntimeWorkspaceResponse> {
        Err(crate::Error::unimplemented(
            "runtime prepare workspace is not implemented",
        ))
    }

    async fn remove_workspace(
        &self,
        _request: pb::RemovePluginRuntimeWorkspaceRequest,
    ) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "runtime remove workspace is not implemented",
        ))
    }

    async fn start_plugin(
        &self,
        _request: pb::StartHostedPluginRequest,
    ) -> ProviderResult<pb::HostedPlugin> {
        Err(crate::Error::unimplemented(
            "runtime start plugin is not implemented",
        ))
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
        let support = self
            .provider
            .get_support(request.into_inner())
            .await
            .map_err(|error| rpc_status("runtime get support", error))?;
        Ok(GrpcResponse::new(support))
    }

    async fn start_session(
        &self,
        request: GrpcRequest<pb::StartPluginRuntimeSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::PluginRuntimeSession>, Status> {
        let session = self
            .provider
            .start_session(request.into_inner())
            .await
            .map_err(|error| rpc_status("runtime start session", error))?;
        Ok(GrpcResponse::new(session))
    }

    async fn get_session(
        &self,
        request: GrpcRequest<pb::GetPluginRuntimeSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::PluginRuntimeSession>, Status> {
        let session = self
            .provider
            .get_session(request.into_inner())
            .await
            .map_err(|error| rpc_status("runtime get session", error))?;
        Ok(GrpcResponse::new(session))
    }

    async fn list_sessions(
        &self,
        request: GrpcRequest<pb::ListPluginRuntimeSessionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListPluginRuntimeSessionsResponse>, Status> {
        let response = self
            .provider
            .list_sessions(request.into_inner())
            .await
            .map_err(|error| rpc_status("runtime list sessions", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn stop_session(
        &self,
        request: GrpcRequest<pb::StopPluginRuntimeSessionRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .stop_session(request.into_inner())
            .await
            .map_err(|error| rpc_status("runtime stop session", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn prepare_workspace(
        &self,
        request: GrpcRequest<pb::PreparePluginRuntimeWorkspaceRequest>,
    ) -> std::result::Result<GrpcResponse<pb::PreparePluginRuntimeWorkspaceResponse>, Status> {
        let response = self
            .provider
            .prepare_workspace(request.into_inner())
            .await
            .map_err(|error| rpc_status("runtime prepare workspace", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn remove_workspace(
        &self,
        request: GrpcRequest<pb::RemovePluginRuntimeWorkspaceRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .remove_workspace(request.into_inner())
            .await
            .map_err(|error| rpc_status("runtime remove workspace", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn start_plugin(
        &self,
        request: GrpcRequest<pb::StartHostedPluginRequest>,
    ) -> std::result::Result<GrpcResponse<pb::HostedPlugin>, Status> {
        let plugin = self
            .provider
            .start_plugin(request.into_inner())
            .await
            .map_err(|error| rpc_status("runtime start plugin", error))?;
        Ok(GrpcResponse::new(plugin))
    }
}
