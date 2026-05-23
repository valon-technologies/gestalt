#[path = "../src/generated.rs"]
mod generated;

#[allow(dead_code)]
mod helpers;

use std::collections::BTreeMap;
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::time::{Duration, UNIX_EPOCH};

use generated::v1::app_runtime_provider_client::AppRuntimeProviderClient;
use generated::v1::provider_lifecycle_client::ProviderLifecycleClient;
use generated::v1::{
    AgentWorkspace, AgentWorkspaceGitCheckout, AppRuntimeEgressMode as ProtoAppRuntimeEgressMode,
    AppRuntimeImagePullAuth as ProtoAppRuntimeImagePullAuth, ConfigureProviderRequest,
    GetAppRuntimeSessionRequest as ProtoGetAppRuntimeSessionRequest,
    ListAppRuntimeSessionsRequest as ProtoListAppRuntimeSessionsRequest, ProviderKind,
    RemoveAppRuntimeWorkspaceRequest as ProtoRemoveAppRuntimeWorkspaceRequest,
    StartAppRuntimeSessionRequest as ProtoStartAppRuntimeSessionRequest,
    StartHostedAppRequest as ProtoStartHostedAppRequest,
    StopAppRuntimeSessionRequest as ProtoStopAppRuntimeSessionRequest,
};
use gestalt::{
    AgentPreparedWorkspace, AppRuntimeEgressMode, AppRuntimeProvider, AppRuntimeSession,
    AppRuntimeSessionLifecycle, AppRuntimeSupport, HostedApp, ListAppRuntimeSessionsRequest,
    ListAppRuntimeSessionsResponse, PrepareAppRuntimeWorkspaceRequest,
    PrepareAppRuntimeWorkspaceResponse, RuntimeMetadata, StartAppRuntimeSessionRequest,
    StartHostedAppRequest, StopAppRuntimeSessionRequest,
};
use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::codegen::async_trait;
use tonic::transport::Endpoint;
use tower::service_fn;

#[derive(Clone, Debug, Default, PartialEq, Eq)]
struct SeenRequest {
    method: String,
    session_id: String,
    app_name: String,
    agent_session_id: String,
    command: String,
    cwd: String,
    workdir: String,
}

#[derive(Default)]
struct TestAppRuntimeProvider {
    configured_name: Mutex<String>,
    seen: Mutex<Vec<SeenRequest>>,
}

#[async_trait]
impl AppRuntimeProvider for TestAppRuntimeProvider {
    async fn configure(
        &self,
        name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> gestalt::Result<()> {
        *self.configured_name.lock().expect("lock configured_name") = name.to_string();
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        Some(RuntimeMetadata {
            name: "runtime-example".to_string(),
            display_name: "Runtime Example".to_string(),
            description: "Test app runtime provider".to_string(),
            version: "0.1.0".to_string(),
        })
    }

    fn warnings(&self) -> Vec<String> {
        vec!["set RUNTIME_POOL".to_string()]
    }

    async fn get_support(&self, _request: ()) -> gestalt::Result<AppRuntimeSupport> {
        Ok(AppRuntimeSupport {
            can_host_apps: true,
            egress_mode: AppRuntimeEgressMode::Hostname,
            supports_prepare_workspace: true,
        })
    }

    async fn start_session(
        &self,
        request: StartAppRuntimeSessionRequest,
    ) -> gestalt::Result<AppRuntimeSession> {
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "start-session".to_string(),
            app_name: request.app_name.clone(),
            ..Default::default()
        });
        assert_eq!(
            request
                .image_pull_auth
                .as_ref()
                .expect("image pull auth")
                .docker_config_json,
            "{}"
        );
        Ok(runtime_session("session-1", request.metadata))
    }

    async fn get_session(
        &self,
        request: gestalt::GetAppRuntimeSessionRequest,
    ) -> gestalt::Result<AppRuntimeSession> {
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "get-session".to_string(),
            session_id: request.session_id.clone(),
            ..Default::default()
        });
        Ok(runtime_session(&request.session_id, BTreeMap::new()))
    }

    async fn list_sessions(
        &self,
        _request: ListAppRuntimeSessionsRequest,
    ) -> gestalt::Result<ListAppRuntimeSessionsResponse> {
        Ok(ListAppRuntimeSessionsResponse {
            sessions: vec![runtime_session("session-1", BTreeMap::new())],
        })
    }

    async fn stop_session(&self, request: StopAppRuntimeSessionRequest) -> gestalt::Result<()> {
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "stop-session".to_string(),
            session_id: request.session_id,
            ..Default::default()
        });
        Ok(())
    }

    async fn prepare_workspace(
        &self,
        request: PrepareAppRuntimeWorkspaceRequest,
    ) -> gestalt::Result<PrepareAppRuntimeWorkspaceResponse> {
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "prepare-workspace".to_string(),
            session_id: request.session_id,
            agent_session_id: request.agent_session_id,
            cwd: request
                .workspace
                .as_ref()
                .map(|w| w.cwd.clone())
                .unwrap_or_default(),
            ..Default::default()
        });
        Ok(PrepareAppRuntimeWorkspaceResponse {
            workspace: Some(AgentPreparedWorkspace {
                root: "/workspaces/session-1".to_string(),
                cwd: "/workspaces/session-1/app".to_string(),
            }),
        })
    }

    async fn remove_workspace(
        &self,
        request: gestalt::RemoveAppRuntimeWorkspaceRequest,
    ) -> gestalt::Result<()> {
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "remove-workspace".to_string(),
            session_id: request.session_id,
            agent_session_id: request.agent_session_id,
            ..Default::default()
        });
        Ok(())
    }

    async fn start_plugin(&self, request: StartHostedAppRequest) -> gestalt::Result<HostedApp> {
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "start-plugin".to_string(),
            session_id: request.session_id.clone(),
            app_name: request.app_name.clone(),
            command: request.command,
            workdir: request.workdir,
            ..Default::default()
        });
        Ok(HostedApp {
            id: "hosted-plugin-1".to_string(),
            session_id: request.session_id,
            app_name: request.app_name,
            dial_target: "unix:///tmp/app.sock".to_string(),
        })
    }
}

#[tokio::test]
async fn app_runtime_provider_transport_uses_native_trait_types() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-plugin-runtime.sock");
    let _socket_guard = helpers::EnvGuard::set(gestalt::ENV_PROVIDER_SOCKET, socket.as_os_str());

    let provider = Arc::new(TestAppRuntimeProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_app_runtime_provider(serve_provider)
            .await
            .expect("serve app runtime provider");
    });

    helpers::wait_for_socket(&socket).await;

    let channel = connect_unix(&socket).await;
    let mut runtime = ProviderLifecycleClient::new(channel.clone());
    let mut client = AppRuntimeProviderClient::new(channel);

    let metadata = runtime
        .get_provider_identity(())
        .await
        .expect("get provider identity")
        .into_inner();
    assert_eq!(
        ProviderKind::try_from(metadata.kind)
            .expect("valid provider kind")
            .as_str_name(),
        "PROVIDER_KIND_RUNTIME"
    );
    assert_eq!(metadata.name, "runtime-example");
    assert_eq!(metadata.warnings, vec!["set RUNTIME_POOL"]);

    runtime
        .configure_provider(ConfigureProviderRequest {
            name: "runtime-provider".to_string(),
            config: Some(helpers::struct_from_json(serde_json::json!({}))),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION,
        })
        .await
        .expect("configure provider");

    let support = client
        .get_support(())
        .await
        .expect("get support")
        .into_inner();
    assert!(support.can_host_apps);
    assert_eq!(
        support.egress_mode,
        ProtoAppRuntimeEgressMode::Hostname as i32
    );
    assert!(support.supports_prepare_workspace);

    let session = client
        .start_session(ProtoStartAppRuntimeSessionRequest {
            app_name: "github".to_string(),
            template: "default".to_string(),
            image: "registry.example/plugin:latest".to_string(),
            metadata: BTreeMap::from([("env".to_string(), "test".to_string())]),
            image_pull_auth: Some(ProtoAppRuntimeImagePullAuth {
                docker_config_json: "{}".to_string(),
            }),
        })
        .await
        .expect("start session")
        .into_inner();
    assert_eq!(session.id, "session-1");
    assert_eq!(session.state, "running");
    assert_eq!(
        session.metadata.get("env").map(String::as_str),
        Some("test")
    );
    assert!(session.lifecycle.and_then(|l| l.started_at).is_some());

    let fetched = client
        .get_session(ProtoGetAppRuntimeSessionRequest {
            session_id: "session-1".to_string(),
        })
        .await
        .expect("get session")
        .into_inner();
    assert_eq!(fetched.id, "session-1");

    let listed = client
        .list_sessions(ProtoListAppRuntimeSessionsRequest {})
        .await
        .expect("list sessions")
        .into_inner();
    assert_eq!(listed.sessions.len(), 1);

    let prepared = client
        .prepare_workspace(generated::v1::PrepareAppRuntimeWorkspaceRequest {
            session_id: "session-1".to_string(),
            agent_session_id: "agent-session-1".to_string(),
            workspace: Some(AgentWorkspace {
                checkouts: vec![AgentWorkspaceGitCheckout {
                    url: "https://example.invalid/repo.git".to_string(),
                    r#ref: "main".to_string(),
                    path: "app".to_string(),
                }],
                cwd: "app".to_string(),
            }),
        })
        .await
        .expect("prepare workspace")
        .into_inner();
    assert_eq!(
        prepared.workspace.expect("workspace").cwd,
        "/workspaces/session-1/app"
    );

    client
        .remove_workspace(ProtoRemoveAppRuntimeWorkspaceRequest {
            session_id: "session-1".to_string(),
            agent_session_id: "agent-session-1".to_string(),
        })
        .await
        .expect("remove workspace");

    let hosted = client
        .start_app(ProtoStartHostedAppRequest {
            session_id: "session-1".to_string(),
            app_name: "github".to_string(),
            command: "/bin/plugin".to_string(),
            args: vec!["serve".to_string()],
            env: BTreeMap::from([("RUST_LOG".to_string(), "debug".to_string())]),
            allowed_hosts: vec!["api.github.com".to_string()],
            default_action: "deny".to_string(),
            host_binary: "/usr/bin/gestalt-app-host".to_string(),
            workdir: "/runtime/providers/github".to_string(),
        })
        .await
        .expect("start app")
        .into_inner();
    assert_eq!(hosted.id, "hosted-plugin-1");
    assert_eq!(hosted.dial_target, "unix:///tmp/app.sock");

    client
        .stop_session(ProtoStopAppRuntimeSessionRequest {
            session_id: "session-1".to_string(),
        })
        .await
        .expect("stop session");

    assert_eq!(
        *provider
            .configured_name
            .lock()
            .expect("lock configured_name"),
        "runtime-provider"
    );
    assert_eq!(
        provider.seen.lock().expect("lock seen").clone(),
        vec![
            SeenRequest {
                method: "start-session".to_string(),
                app_name: "github".to_string(),
                ..Default::default()
            },
            SeenRequest {
                method: "get-session".to_string(),
                session_id: "session-1".to_string(),
                ..Default::default()
            },
            SeenRequest {
                method: "prepare-workspace".to_string(),
                session_id: "session-1".to_string(),
                agent_session_id: "agent-session-1".to_string(),
                cwd: "app".to_string(),
                ..Default::default()
            },
            SeenRequest {
                method: "remove-workspace".to_string(),
                session_id: "session-1".to_string(),
                agent_session_id: "agent-session-1".to_string(),
                ..Default::default()
            },
            SeenRequest {
                method: "start-plugin".to_string(),
                session_id: "session-1".to_string(),
                app_name: "github".to_string(),
                command: "/bin/plugin".to_string(),
                workdir: "/runtime/providers/github".to_string(),
                ..Default::default()
            },
            SeenRequest {
                method: "stop-session".to_string(),
                session_id: "session-1".to_string(),
                ..Default::default()
            },
        ]
    );

    serve_task.abort();
    let _ = serve_task.await;
}

fn runtime_session(id: &str, metadata: BTreeMap<String, String>) -> AppRuntimeSession {
    AppRuntimeSession {
        id: id.to_string(),
        state: "running".to_string(),
        metadata,
        lifecycle: Some(AppRuntimeSessionLifecycle {
            started_at: Some(UNIX_EPOCH + Duration::from_secs(1_778_241_600)),
            recommended_drain_at: None,
            expires_at: None,
        }),
        state_reason: String::new(),
        state_message: "ready".to_string(),
    }
}

async fn connect_unix(path: &Path) -> tonic::transport::Channel {
    Endpoint::try_from("http://[::]:50051")
        .expect("endpoint")
        .connect_with_connector(service_fn({
            let path = path.to_path_buf();
            move |_| {
                let path = path.clone();
                async move { UnixStream::connect(path).await.map(TokioIo::new) }
            }
        }))
        .await
        .expect("connect channel")
}
