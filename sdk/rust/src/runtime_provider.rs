use std::sync::Arc;
use std::time::SystemTime;

use tonic::codegen::async_trait;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::agent::{AgentPreparedWorkspace, AgentWorkspace};
use crate::api::RuntimeMetadata;
use crate::error::Result as ProviderResult;
use crate::generated::v1::{self as pb};
use crate::protocol;
use crate::rpc_status::rpc_status;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
#[repr(i32)]
pub enum RuntimeEgressMode {
    #[default]
    Unspecified = 0,
    None = 1,
    Cidr = 2,
    Hostname = 3,
}

impl RuntimeEgressMode {
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
pub struct RuntimeSupport {
    pub can_host_apps: bool,
    pub egress_mode: RuntimeEgressMode,
    pub supports_prepare_workspace: bool,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct RuntimeSession {
    pub id: String,
    pub state: String,
    pub metadata: std::collections::BTreeMap<String, String>,
    pub lifecycle: Option<RuntimeSessionLifecycle>,
    pub state_reason: String,
    pub state_message: String,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct RuntimeSessionLifecycle {
    pub started_at: Option<SystemTime>,
    pub recommended_drain_at: Option<SystemTime>,
    pub expires_at: Option<SystemTime>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct RuntimeImagePullAuth {
    pub docker_config_json: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct StartRuntimeSessionRequest {
    pub app_name: String,
    pub template: String,
    pub image: String,
    pub metadata: std::collections::BTreeMap<String, String>,
    pub image_pull_auth: Option<RuntimeImagePullAuth>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct GetRuntimeSessionRequest {
    pub session_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ListRuntimeSessionsRequest {
    pub page_size: i32,
    pub page_token: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct ListRuntimeSessionsResponse {
    pub sessions: Vec<RuntimeSession>,
    pub next_page_token: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct StopRuntimeSessionRequest {
    pub session_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct PrepareRuntimeWorkspaceRequest {
    pub session_id: String,
    pub agent_session_id: String,
    pub workspace: Option<AgentWorkspace>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct PrepareRuntimeWorkspaceResponse {
    pub workspace: Option<AgentPreparedWorkspace>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct RemoveRuntimeWorkspaceRequest {
    pub session_id: String,
    pub agent_session_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct StartHostedAppRequest {
    pub session_id: String,
    pub app_name: String,
    pub command: String,
    pub args: Vec<String>,
    pub env: std::collections::BTreeMap<String, String>,
    pub allowed_hosts: Vec<String>,
    pub default_action: String,
    pub host_binary: String,
    pub workdir: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct HostedApp {
    pub id: String,
    pub session_id: String,
    pub app_name: String,
    pub dial_target: String,
}

fn support_to_proto(value: RuntimeSupport) -> pb::RuntimeSupport {
    pb::RuntimeSupport {
        can_host_apps: value.can_host_apps,
        egress_mode: value.egress_mode.as_i32(),
        supports_prepare_workspace: value.supports_prepare_workspace,
    }
}

fn session_to_proto(value: RuntimeSession) -> pb::RuntimeSession {
    pb::RuntimeSession {
        id: value.id,
        state: value.state,
        metadata: value.metadata,
        lifecycle: value
            .lifecycle
            .map(|lifecycle| pb::RuntimeSessionLifecycle {
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
    value: pb::StartRuntimeSessionRequest,
) -> StartRuntimeSessionRequest {
    StartRuntimeSessionRequest {
        app_name: value.app_name,
        template: value.template,
        image: value.image,
        metadata: value.metadata,
        image_pull_auth: value.image_pull_auth.map(|auth| RuntimeImagePullAuth {
            docker_config_json: auth.docker_config_json,
        }),
    }
}

fn list_sessions_response_to_proto(
    value: ListRuntimeSessionsResponse,
) -> pb::ListRuntimeSessionsResponse {
    pb::ListRuntimeSessionsResponse {
        sessions: value.sessions.into_iter().map(session_to_proto).collect(),
        next_page_token: value.next_page_token,
    }
}

fn list_sessions_request_from_proto(
    value: pb::ListRuntimeSessionsRequest,
) -> std::result::Result<ListRuntimeSessionsRequest, Status> {
    let mut page_size = value.page_size;
    if page_size < 0 {
        return Err(Status::invalid_argument("page_size must be non-negative"));
    }
    if page_size == 0 {
        page_size = 100;
    }
    if page_size > 200 {
        page_size = 200;
    }
    Ok(ListRuntimeSessionsRequest {
        page_size,
        page_token: value.page_token,
    })
}

fn prepare_workspace_request_from_proto(
    value: pb::PrepareRuntimeWorkspaceRequest,
) -> PrepareRuntimeWorkspaceRequest {
    PrepareRuntimeWorkspaceRequest {
        session_id: value.session_id,
        agent_session_id: value.agent_session_id,
        workspace: value.workspace.map(|workspace| AgentWorkspace {
            checkouts: workspace
                .checkouts
                .into_iter()
                .map(|checkout| crate::agent::AgentWorkspaceGitCheckout {
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
    value: PrepareRuntimeWorkspaceResponse,
) -> pb::PrepareRuntimeWorkspaceResponse {
    pb::PrepareRuntimeWorkspaceResponse {
        workspace: value.workspace.map(|workspace| pb::PreparedAgentWorkspace {
            root: workspace.root,
            cwd: workspace.cwd,
        }),
    }
}

fn start_app_request_from_proto(value: pb::StartHostedAppRequest) -> StartHostedAppRequest {
    StartHostedAppRequest {
        session_id: value.session_id,
        app_name: value.app_name,
        command: value.command,
        args: value.args,
        env: value.env,
        allowed_hosts: value.allowed_hosts,
        default_action: value.default_action,
        host_binary: value.host_binary,
        workdir: value.workdir,
    }
}

fn hosted_app_to_proto(value: HostedApp) -> pb::HostedApp {
    pb::HostedApp {
        id: value.id,
        session_id: value.session_id,
        app_name: value.app_name,
        dial_target: value.dial_target,
    }
}

#[async_trait]
/// Provider trait for serving hosted runtime sessions.
pub trait RuntimeProvider: Send + Sync + 'static {
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
    async fn get_support(&self, _request: ()) -> ProviderResult<RuntimeSupport> {
        Err(crate::Error::unimplemented(
            "runtime get support is not implemented",
        ))
    }

    /// Starts a hosted runtime session.
    async fn start_session(
        &self,
        _request: StartRuntimeSessionRequest,
    ) -> ProviderResult<RuntimeSession> {
        Err(crate::Error::unimplemented(
            "runtime start session is not implemented",
        ))
    }

    /// Returns one hosted runtime session by ID.
    async fn get_session(
        &self,
        _request: GetRuntimeSessionRequest,
    ) -> ProviderResult<RuntimeSession> {
        Err(crate::Error::unimplemented(
            "runtime get session is not implemented",
        ))
    }

    /// Lists hosted runtime sessions.
    async fn list_sessions(
        &self,
        _request: ListRuntimeSessionsRequest,
    ) -> ProviderResult<ListRuntimeSessionsResponse> {
        Err(crate::Error::unimplemented(
            "runtime list sessions is not implemented",
        ))
    }

    /// Stops a hosted runtime session.
    async fn stop_session(&self, _request: StopRuntimeSessionRequest) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "runtime stop session is not implemented",
        ))
    }

    /// Prepares an agent workspace for use by a hosted app.
    async fn prepare_workspace(
        &self,
        _request: PrepareRuntimeWorkspaceRequest,
    ) -> ProviderResult<PrepareRuntimeWorkspaceResponse> {
        Err(crate::Error::unimplemented(
            "runtime prepare workspace is not implemented",
        ))
    }

    /// Removes a previously prepared agent workspace.
    async fn remove_workspace(
        &self,
        _request: RemoveRuntimeWorkspaceRequest,
    ) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "runtime remove workspace is not implemented",
        ))
    }

    /// Starts one hosted app process inside a runtime session.
    async fn start_app(&self, _request: StartHostedAppRequest) -> ProviderResult<HostedApp> {
        Err(crate::Error::unimplemented(
            "runtime start app is not implemented",
        ))
    }
}

#[derive(Clone)]
pub(crate) struct RuntimeServer<P> {
    provider: Arc<P>,
}

impl<P> RuntimeServer<P> {
    pub(crate) fn new(provider: Arc<P>) -> Self {
        Self { provider }
    }
}

#[async_trait]
impl<P> pb::runtime_provider_server::RuntimeProvider for RuntimeServer<P>
where
    P: RuntimeProvider,
{
    async fn get_support(
        &self,
        request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<pb::RuntimeSupport>, Status> {
        let support = self
            .provider
            .get_support(request.into_inner())
            .await
            .map_err(|error| rpc_status("runtime get support", error))?;
        Ok(GrpcResponse::new(support_to_proto(support)))
    }

    async fn start_session(
        &self,
        request: GrpcRequest<pb::StartRuntimeSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::RuntimeSession>, Status> {
        let session = self
            .provider
            .start_session(start_session_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("runtime start session", error))?;
        Ok(GrpcResponse::new(session_to_proto(session)))
    }

    async fn get_session(
        &self,
        request: GrpcRequest<pb::GetRuntimeSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::RuntimeSession>, Status> {
        let session = self
            .provider
            .get_session({
                let request = request.into_inner();
                GetRuntimeSessionRequest {
                    session_id: request.session_id,
                }
            })
            .await
            .map_err(|error| rpc_status("runtime get session", error))?;
        Ok(GrpcResponse::new(session_to_proto(session)))
    }

    async fn list_sessions(
        &self,
        request: GrpcRequest<pb::ListRuntimeSessionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListRuntimeSessionsResponse>, Status> {
        let response = self
            .provider
            .list_sessions(list_sessions_request_from_proto(request.into_inner())?)
            .await
            .map_err(|error| rpc_status("runtime list sessions", error))?;
        Ok(GrpcResponse::new(list_sessions_response_to_proto(response)))
    }

    async fn stop_session(
        &self,
        request: GrpcRequest<pb::StopRuntimeSessionRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .stop_session({
                let request = request.into_inner();
                StopRuntimeSessionRequest {
                    session_id: request.session_id,
                }
            })
            .await
            .map_err(|error| rpc_status("runtime stop session", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn prepare_workspace(
        &self,
        request: GrpcRequest<pb::PrepareRuntimeWorkspaceRequest>,
    ) -> std::result::Result<GrpcResponse<pb::PrepareRuntimeWorkspaceResponse>, Status> {
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
        request: GrpcRequest<pb::RemoveRuntimeWorkspaceRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .remove_workspace({
                let request = request.into_inner();
                RemoveRuntimeWorkspaceRequest {
                    session_id: request.session_id,
                    agent_session_id: request.agent_session_id,
                }
            })
            .await
            .map_err(|error| rpc_status("runtime remove workspace", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn start_app(
        &self,
        request: GrpcRequest<pb::StartHostedAppRequest>,
    ) -> std::result::Result<GrpcResponse<pb::HostedApp>, Status> {
        let app = self
            .provider
            .start_app(start_app_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("runtime start app", error))?;
        Ok(GrpcResponse::new(hosted_app_to_proto(app)))
    }
}
