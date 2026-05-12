use std::sync::Arc;
use std::time::SystemTime;

use tonic::codegen::async_trait;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::agent::{AgentPreparedWorkspace, AgentWorkspaceInput};
use crate::api::RuntimeMetadata;
use crate::error::Result as ProviderResult;
use crate::generated::v1::{self as pb};
use crate::protocol;
use crate::rpc_status::rpc_status;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
#[repr(i32)]
pub enum PluginRuntimeEgressMode {
    #[default]
    Unspecified = 0,
    None = 1,
    Cidr = 2,
    Hostname = 3,
}

impl PluginRuntimeEgressMode {
    pub const fn as_i32(self) -> i32 {
        self as i32
    }

    pub const fn from_i32_lossy(value: i32) -> Self {
        match value {
            1 => Self::None,
            2 => Self::Cidr,
            3 => Self::Hostname,
            _ => Self::Unspecified,
        }
    }
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct PluginRuntimeSupport {
    pub can_host_plugins: bool,
    pub egress_mode: PluginRuntimeEgressMode,
    pub supports_prepare_workspace: bool,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct PluginRuntimeSession {
    pub id: String,
    pub state: String,
    pub metadata: std::collections::BTreeMap<String, String>,
    pub lifecycle: Option<PluginRuntimeSessionLifecycle>,
    pub state_reason: String,
    pub state_message: String,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct PluginRuntimeSessionLifecycle {
    pub started_at: Option<SystemTime>,
    pub recommended_drain_at: Option<SystemTime>,
    pub expires_at: Option<SystemTime>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct PluginRuntimeImagePullAuth {
    pub docker_config_json: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct StartPluginRuntimeSessionRequest {
    pub plugin_name: String,
    pub template: String,
    pub image: String,
    pub metadata: std::collections::BTreeMap<String, String>,
    pub image_pull_auth: Option<PluginRuntimeImagePullAuth>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct GetPluginRuntimeSessionRequest {
    pub session_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ListPluginRuntimeSessionsRequest {}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct ListPluginRuntimeSessionsResponse {
    pub sessions: Vec<PluginRuntimeSession>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct StopPluginRuntimeSessionRequest {
    pub session_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct PreparePluginRuntimeWorkspaceRequest {
    pub session_id: String,
    pub agent_session_id: String,
    pub workspace: Option<AgentWorkspaceInput>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct PreparePluginRuntimeWorkspaceResponse {
    pub workspace: Option<AgentPreparedWorkspace>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct RemovePluginRuntimeWorkspaceRequest {
    pub session_id: String,
    pub agent_session_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct StartHostedPluginRequest {
    pub session_id: String,
    pub plugin_name: String,
    pub command: String,
    pub args: Vec<String>,
    pub env: std::collections::BTreeMap<String, String>,
    pub allowed_hosts: Vec<String>,
    pub default_action: String,
    pub host_binary: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct HostedPlugin {
    pub id: String,
    pub session_id: String,
    pub plugin_name: String,
    pub dial_target: String,
}

fn support_to_proto(value: PluginRuntimeSupport) -> pb::PluginRuntimeSupport {
    pb::PluginRuntimeSupport {
        can_host_plugins: value.can_host_plugins,
        egress_mode: value.egress_mode.as_i32(),
        supports_prepare_workspace: value.supports_prepare_workspace,
    }
}

fn session_to_proto(value: PluginRuntimeSession) -> pb::PluginRuntimeSession {
    pb::PluginRuntimeSession {
        id: value.id,
        state: value.state,
        metadata: value.metadata,
        lifecycle: value
            .lifecycle
            .map(|lifecycle| pb::PluginRuntimeSessionLifecycle {
                started_at: lifecycle
                    .started_at
                    .map(protocol::timestamp_from_system_time),
                recommended_drain_at: lifecycle
                    .recommended_drain_at
                    .map(protocol::timestamp_from_system_time),
                expires_at: lifecycle
                    .expires_at
                    .map(protocol::timestamp_from_system_time),
            }),
        state_reason: value.state_reason,
        state_message: value.state_message,
    }
}

fn start_session_request_from_proto(
    value: pb::StartPluginRuntimeSessionRequest,
) -> StartPluginRuntimeSessionRequest {
    StartPluginRuntimeSessionRequest {
        plugin_name: value.plugin_name,
        template: value.template,
        image: value.image,
        metadata: value.metadata,
        image_pull_auth: value
            .image_pull_auth
            .map(|auth| PluginRuntimeImagePullAuth {
                docker_config_json: auth.docker_config_json,
            }),
    }
}

fn list_sessions_response_to_proto(
    value: ListPluginRuntimeSessionsResponse,
) -> pb::ListPluginRuntimeSessionsResponse {
    pb::ListPluginRuntimeSessionsResponse {
        sessions: value.sessions.into_iter().map(session_to_proto).collect(),
    }
}

fn prepare_workspace_request_from_proto(
    value: pb::PreparePluginRuntimeWorkspaceRequest,
) -> PreparePluginRuntimeWorkspaceRequest {
    PreparePluginRuntimeWorkspaceRequest {
        session_id: value.session_id,
        agent_session_id: value.agent_session_id,
        workspace: value.workspace.map(|workspace| AgentWorkspaceInput {
            checkouts: workspace
                .checkouts
                .into_iter()
                .map(|checkout| crate::agent::AgentWorkspaceGitCheckoutInput {
                    url: checkout.url,
                    reference: checkout.r#ref,
                    path: checkout.path,
                })
                .collect(),
            cwd: workspace.cwd,
        }),
    }
}

fn prepare_workspace_response_to_proto(
    value: PreparePluginRuntimeWorkspaceResponse,
) -> pb::PreparePluginRuntimeWorkspaceResponse {
    pb::PreparePluginRuntimeWorkspaceResponse {
        workspace: value.workspace.map(|workspace| pb::PreparedAgentWorkspace {
            root: workspace.root,
            cwd: workspace.cwd,
        }),
    }
}

fn start_plugin_request_from_proto(
    value: pb::StartHostedPluginRequest,
) -> StartHostedPluginRequest {
    StartHostedPluginRequest {
        session_id: value.session_id,
        plugin_name: value.plugin_name,
        command: value.command,
        args: value.args,
        env: value.env,
        allowed_hosts: value.allowed_hosts,
        default_action: value.default_action,
        host_binary: value.host_binary,
    }
}

fn hosted_plugin_to_proto(value: HostedPlugin) -> pb::HostedPlugin {
    pb::HostedPlugin {
        id: value.id,
        session_id: value.session_id,
        plugin_name: value.plugin_name,
        dial_target: value.dial_target,
    }
}

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

    /// Returns the runtime capabilities supported by this provider.
    async fn get_support(&self, _request: ()) -> ProviderResult<PluginRuntimeSupport> {
        Err(crate::Error::unimplemented(
            "runtime get support is not implemented",
        ))
    }

    /// Starts a hosted plugin-runtime session.
    async fn start_session(
        &self,
        _request: StartPluginRuntimeSessionRequest,
    ) -> ProviderResult<PluginRuntimeSession> {
        Err(crate::Error::unimplemented(
            "runtime start session is not implemented",
        ))
    }

    /// Returns one hosted plugin-runtime session by ID.
    async fn get_session(
        &self,
        _request: GetPluginRuntimeSessionRequest,
    ) -> ProviderResult<PluginRuntimeSession> {
        Err(crate::Error::unimplemented(
            "runtime get session is not implemented",
        ))
    }

    /// Lists hosted plugin-runtime sessions.
    async fn list_sessions(
        &self,
        _request: ListPluginRuntimeSessionsRequest,
    ) -> ProviderResult<ListPluginRuntimeSessionsResponse> {
        Err(crate::Error::unimplemented(
            "runtime list sessions is not implemented",
        ))
    }

    /// Stops a hosted plugin-runtime session.
    async fn stop_session(&self, _request: StopPluginRuntimeSessionRequest) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "runtime stop session is not implemented",
        ))
    }

    /// Prepares an agent workspace for use by a hosted plugin.
    async fn prepare_workspace(
        &self,
        _request: PreparePluginRuntimeWorkspaceRequest,
    ) -> ProviderResult<PreparePluginRuntimeWorkspaceResponse> {
        Err(crate::Error::unimplemented(
            "runtime prepare workspace is not implemented",
        ))
    }

    /// Removes a previously prepared agent workspace.
    async fn remove_workspace(
        &self,
        _request: RemovePluginRuntimeWorkspaceRequest,
    ) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "runtime remove workspace is not implemented",
        ))
    }

    /// Starts one hosted plugin process inside a runtime session.
    async fn start_plugin(
        &self,
        _request: StartHostedPluginRequest,
    ) -> ProviderResult<HostedPlugin> {
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
        Ok(GrpcResponse::new(support_to_proto(support)))
    }

    async fn start_session(
        &self,
        request: GrpcRequest<pb::StartPluginRuntimeSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::PluginRuntimeSession>, Status> {
        let session = self
            .provider
            .start_session(start_session_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("runtime start session", error))?;
        Ok(GrpcResponse::new(session_to_proto(session)))
    }

    async fn get_session(
        &self,
        request: GrpcRequest<pb::GetPluginRuntimeSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::PluginRuntimeSession>, Status> {
        let session = self
            .provider
            .get_session({
                let request = request.into_inner();
                GetPluginRuntimeSessionRequest {
                    session_id: request.session_id,
                }
            })
            .await
            .map_err(|error| rpc_status("runtime get session", error))?;
        Ok(GrpcResponse::new(session_to_proto(session)))
    }

    async fn list_sessions(
        &self,
        request: GrpcRequest<pb::ListPluginRuntimeSessionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListPluginRuntimeSessionsResponse>, Status> {
        let response = self
            .provider
            .list_sessions({
                let _request = request.into_inner();
                ListPluginRuntimeSessionsRequest {}
            })
            .await
            .map_err(|error| rpc_status("runtime list sessions", error))?;
        Ok(GrpcResponse::new(list_sessions_response_to_proto(response)))
    }

    async fn stop_session(
        &self,
        request: GrpcRequest<pb::StopPluginRuntimeSessionRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .stop_session({
                let request = request.into_inner();
                StopPluginRuntimeSessionRequest {
                    session_id: request.session_id,
                }
            })
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
            .prepare_workspace(prepare_workspace_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("runtime prepare workspace", error))?;
        Ok(GrpcResponse::new(prepare_workspace_response_to_proto(
            response,
        )))
    }

    async fn remove_workspace(
        &self,
        request: GrpcRequest<pb::RemovePluginRuntimeWorkspaceRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .remove_workspace({
                let request = request.into_inner();
                RemovePluginRuntimeWorkspaceRequest {
                    session_id: request.session_id,
                    agent_session_id: request.agent_session_id,
                }
            })
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
            .start_plugin(start_plugin_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("runtime start plugin", error))?;
        Ok(GrpcResponse::new(hosted_plugin_to_proto(plugin)))
    }
}
