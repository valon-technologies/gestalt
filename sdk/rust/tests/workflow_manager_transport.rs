#[allow(dead_code)]
mod helpers;

use std::path::Path;
use std::sync::{Arc, Mutex};

use gestalt::proto::v1::workflow_manager_host_server::{
    WorkflowManagerHost as ProtoWorkflowManagerHost, WorkflowManagerHostServer,
};
use gestalt::proto::v1::{
    BoundWorkflowTarget, ManagedWorkflowDeployment, ManagedWorkflowRun, ManagedWorkflowRunSignal,
    PlanWorkflowResponse, WorkflowDeployment, WorkflowDeploymentSpec, WorkflowDeploymentStatus,
    WorkflowEventDeliveryResult, WorkflowManagerApplyDeploymentRequest,
    WorkflowManagerCancelRunRequest, WorkflowManagerDeleteDeploymentRequest,
    WorkflowManagerDeliverEventRequest, WorkflowManagerDeliverEventResponse,
    WorkflowManagerGetDeploymentRequest, WorkflowManagerListDeploymentsRequest,
    WorkflowManagerListDeploymentsResponse, WorkflowManagerPlanDeploymentRequest,
    WorkflowManagerSetActivationPausedRequest, WorkflowManagerSetDeploymentPausedRequest,
    WorkflowManagerSignalOrStartRunRequest, WorkflowManagerSignalRunRequest,
    WorkflowManagerStartRunRequest, WorkflowRun, WorkflowRunStatus, WorkflowStep,
    WorkflowStepPluginCall, workflow_step,
};
use gestalt::{
    Request, WorkflowManager, WorkflowManagerApplyDeployment, WorkflowManagerCancelRun,
    WorkflowManagerDeleteDeployment, WorkflowManagerDeliverEvent, WorkflowManagerGetDeployment,
    WorkflowManagerListDeployments, WorkflowManagerPlanDeployment,
    WorkflowManagerSetActivationPaused, WorkflowManagerSetDeploymentPaused,
    WorkflowManagerSignalOrStartRun, WorkflowManagerSignalRun, WorkflowManagerStartRun,
    WorkflowSignal,
};
use tokio::net::{TcpListener, UnixListener};
use tokio_stream::wrappers::{TcpListenerStream, UnixListenerStream};
use tonic::codegen::async_trait;
use tonic::transport::Server;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

const ENV_WORKFLOW_MANAGER_SOCKET_TOKEN: &str = "GESTALT_WORKFLOW_MANAGER_SOCKET_TOKEN";

#[derive(Clone, Debug, Default, PartialEq)]
struct SeenRequest {
    method: String,
    invocation_token: String,
    idempotency_key: String,
}

#[derive(Clone, Default)]
struct TestWorkflowManagerServer {
    seen: Arc<Mutex<Vec<SeenRequest>>>,
    relay_tokens: Arc<Mutex<Vec<String>>>,
    signal_or_start_requests: Arc<Mutex<Vec<WorkflowManagerSignalOrStartRunRequest>>>,
}

fn target() -> BoundWorkflowTarget {
    BoundWorkflowTarget::from_steps([WorkflowStep {
        id: "sync".to_string(),
        action: Some(workflow_step::Action::Plugin(WorkflowStepPluginCall {
            name: "roadmap".to_string(),
            operation: "sync".to_string(),
            ..Default::default()
        })),
        ..Default::default()
    }])
}

fn deployment(
    provider_name: String,
    spec: Option<WorkflowDeploymentSpec>,
) -> ManagedWorkflowDeployment {
    ManagedWorkflowDeployment {
        provider_name,
        deployment: Some(WorkflowDeployment {
            spec,
            status: WorkflowDeploymentStatus::Active as i32,
            ..Default::default()
        }),
    }
}

#[async_trait]
impl ProtoWorkflowManagerHost for TestWorkflowManagerServer {
    async fn plan_deployment(
        &self,
        request: GrpcRequest<WorkflowManagerPlanDeploymentRequest>,
    ) -> std::result::Result<GrpcResponse<PlanWorkflowResponse>, Status> {
        if let Some(token) = request.metadata().get("x-gestalt-host-service-relay-token") {
            self.relay_tokens
                .lock()
                .expect("lock relay tokens")
                .push(token.to_str().expect("relay token ascii").to_string());
        }
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "plan".to_string(),
            invocation_token: request.invocation_token,
            idempotency_key: request.idempotency_key,
        });
        Ok(GrpcResponse::new(PlanWorkflowResponse {
            accepted_spec_digest: "sha256:spec".to_string(),
            provider_plan_id: "plan-1".to_string(),
            provider_plan_digest: "sha256:plan".to_string(),
            ..Default::default()
        }))
    }

    async fn apply_deployment(
        &self,
        request: GrpcRequest<WorkflowManagerApplyDeploymentRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowDeployment>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "apply".to_string(),
            invocation_token: request.invocation_token,
            idempotency_key: request.idempotency_key,
        });
        Ok(GrpcResponse::new(deployment(
            request.provider_name,
            request.spec,
        )))
    }

    async fn get_deployment(
        &self,
        request: GrpcRequest<WorkflowManagerGetDeploymentRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowDeployment>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "get".to_string(),
            invocation_token: request.invocation_token,
            idempotency_key: String::new(),
        });
        Ok(GrpcResponse::new(deployment(
            "basic".to_string(),
            Some(WorkflowDeploymentSpec {
                id: request.deployment_id,
                ..Default::default()
            }),
        )))
    }

    async fn list_deployments(
        &self,
        request: GrpcRequest<WorkflowManagerListDeploymentsRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowManagerListDeploymentsResponse>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "list".to_string(),
            invocation_token: request.invocation_token,
            idempotency_key: String::new(),
        });
        Ok(GrpcResponse::new(WorkflowManagerListDeploymentsResponse {
            deployments: vec![deployment(request.provider_name, None)],
        }))
    }

    async fn delete_deployment(
        &self,
        request: GrpcRequest<WorkflowManagerDeleteDeploymentRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "delete".to_string(),
            invocation_token: request.invocation_token,
            idempotency_key: String::new(),
        });
        Ok(GrpcResponse::new(()))
    }

    async fn set_deployment_paused(
        &self,
        request: GrpcRequest<WorkflowManagerSetDeploymentPausedRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowDeployment>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "set-deployment-paused".to_string(),
            invocation_token: request.invocation_token,
            idempotency_key: String::new(),
        });
        Ok(GrpcResponse::new(deployment("basic".to_string(), None)))
    }

    async fn set_activation_paused(
        &self,
        request: GrpcRequest<WorkflowManagerSetActivationPausedRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowDeployment>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "set-activation-paused".to_string(),
            invocation_token: request.invocation_token,
            idempotency_key: String::new(),
        });
        Ok(GrpcResponse::new(deployment("basic".to_string(), None)))
    }

    async fn start_run(
        &self,
        request: GrpcRequest<WorkflowManagerStartRunRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowRun>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "start-run".to_string(),
            invocation_token: request.invocation_token,
            idempotency_key: request.idempotency_key,
        });
        Ok(GrpcResponse::new(ManagedWorkflowRun {
            provider_name: request.provider_name,
            run: Some(WorkflowRun {
                id: "run-1".to_string(),
                deployment_id: request.deployment_id,
                workflow_key: request.workflow_key,
                status: WorkflowRunStatus::Pending as i32,
                ..Default::default()
            }),
        }))
    }

    async fn signal_run(
        &self,
        request: GrpcRequest<WorkflowManagerSignalRunRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowRunSignal>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "signal-run".to_string(),
            invocation_token: request.invocation_token,
            idempotency_key: String::new(),
        });
        Ok(GrpcResponse::new(ManagedWorkflowRunSignal {
            provider_name: "basic".to_string(),
            run: Some(WorkflowRun {
                id: request.run_id,
                ..Default::default()
            }),
            signal: request.signal,
            ..Default::default()
        }))
    }

    async fn signal_or_start_run(
        &self,
        request: GrpcRequest<WorkflowManagerSignalOrStartRunRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowRunSignal>, Status> {
        let request = request.into_inner();
        self.signal_or_start_requests
            .lock()
            .expect("lock signal-or-start requests")
            .push(request.clone());
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "signal-or-start-run".to_string(),
            invocation_token: request.invocation_token,
            idempotency_key: request.idempotency_key,
        });
        Ok(GrpcResponse::new(ManagedWorkflowRunSignal {
            provider_name: request.provider_name,
            run: Some(WorkflowRun {
                id: "run-1".to_string(),
                workflow_key: request.workflow_key.clone(),
                ..Default::default()
            }),
            signal: request.signal,
            started_run: true,
            workflow_key: request.workflow_key,
        }))
    }

    async fn cancel_run(
        &self,
        request: GrpcRequest<WorkflowManagerCancelRunRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowRun>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "cancel-run".to_string(),
            invocation_token: request.invocation_token,
            idempotency_key: String::new(),
        });
        Ok(GrpcResponse::new(ManagedWorkflowRun {
            provider_name: "basic".to_string(),
            run: Some(WorkflowRun {
                id: request.run_id,
                status: WorkflowRunStatus::Canceled as i32,
                ..Default::default()
            }),
        }))
    }

    async fn deliver_event(
        &self,
        request: GrpcRequest<WorkflowManagerDeliverEventRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowManagerDeliverEventResponse>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "deliver-event".to_string(),
            invocation_token: request.invocation_token,
            idempotency_key: request.idempotency_key,
        });
        Ok(GrpcResponse::new(WorkflowManagerDeliverEventResponse {
            results: vec![WorkflowEventDeliveryResult {
                deployment_id: "deployment-1".to_string(),
                activation_id: "evt".to_string(),
                started_run: true,
                ..Default::default()
            }],
        }))
    }
}

#[tokio::test]
async fn workflow_manager_connects_over_tcp_and_sends_relay_token() {
    let _env_lock = helpers::env_lock().lock().await;

    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind tcp listener");
    let address = listener.local_addr().expect("local addr");
    let _socket_guard = helpers::EnvGuard::set(
        gestalt::ENV_WORKFLOW_MANAGER_SOCKET,
        format!("tcp://{address}"),
    );
    let _token_guard =
        helpers::EnvGuard::set(ENV_WORKFLOW_MANAGER_SOCKET_TOKEN, "relay-token-rust");

    let server = TestWorkflowManagerServer::default();
    let serve_server = server.clone();
    let serve_task = tokio::spawn(async move {
        serve_workflow_manager_tcp(serve_server, listener)
            .await
            .expect("serve workflow manager");
    });

    let mut manager =
        WorkflowManager::connect_with_idempotency_key("token-123", "workflow-request-key-rust")
            .await
            .expect("connect workflow manager");
    let planned = manager
        .plan_deployment(WorkflowManagerPlanDeployment {
            provider_name: "managed".to_string(),
            spec: Some(WorkflowDeploymentSpec {
                id: "deployment-1".to_string(),
                target: Some(target()),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("plan deployment");

    assert_eq!(planned.provider_plan_id, "plan-1");
    assert_eq!(
        server
            .relay_tokens
            .lock()
            .expect("lock relay tokens")
            .clone(),
        vec!["relay-token-rust".to_string()]
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn workflow_manager_connects_over_unix_socket_and_sends_invocation_token() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("g-rust-wm.sock");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt::ENV_WORKFLOW_MANAGER_SOCKET, socket.as_os_str());

    let server = TestWorkflowManagerServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_workflow_manager(serve_server, &serve_socket)
            .await
            .expect("serve workflow manager");
    });

    helpers::wait_for_socket(&socket).await;

    let mut manager =
        WorkflowManager::connect_with_idempotency_key("token-123", "workflow-request-key-rust")
            .await
            .expect("connect workflow manager");
    let _planned = manager
        .plan_deployment(WorkflowManagerPlanDeployment {
            provider_name: "basic".to_string(),
            spec: Some(WorkflowDeploymentSpec {
                id: "deployment-1".to_string(),
                target: Some(target()),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("plan deployment");
    let applied = manager
        .apply_deployment(WorkflowManagerApplyDeployment {
            provider_name: "basic".to_string(),
            spec: Some(WorkflowDeploymentSpec {
                id: "deployment-1".to_string(),
                target: Some(target()),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("apply deployment");
    let fetched = manager
        .get_deployment(WorkflowManagerGetDeployment {
            deployment_id: "deployment-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("get deployment");
    let listed = manager
        .list_deployments(WorkflowManagerListDeployments {
            provider_name: "basic".to_string(),
            ..Default::default()
        })
        .await
        .expect("list deployments");
    manager
        .set_deployment_paused(WorkflowManagerSetDeploymentPaused {
            deployment_id: "deployment-1".to_string(),
            paused: true,
            ..Default::default()
        })
        .await
        .expect("set deployment paused");
    manager
        .set_activation_paused(WorkflowManagerSetActivationPaused {
            deployment_id: "deployment-1".to_string(),
            activation_id: "manual".to_string(),
            paused: true,
            ..Default::default()
        })
        .await
        .expect("set activation paused");
    let started_run = manager
        .start_run(
            WorkflowManagerStartRun {
                provider_name: "basic".to_string(),
                deployment_id: "deployment-1".to_string(),
                deployment_generation: 7,
                activation_id: "manual".to_string(),
                workflow_key: "workflow-key-1".to_string(),
                ..Default::default()
            }
            .with_input(serde_json::json!({ "customer_id": "cust_123" }))
            .expect("start input"),
        )
        .await
        .expect("start run");
    let signaled_run = manager
        .signal_run(WorkflowManagerSignalRun {
            run_id: "run-1".to_string(),
            signal: Some(WorkflowSignal {
                name: "slack.event".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("signal run");
    let signaled_or_started_run = manager
        .signal_or_start_run(WorkflowManagerSignalOrStartRun {
            provider_name: "basic".to_string(),
            deployment_id: "deployment-1".to_string(),
            deployment_generation: 7,
            activation_id: "evt".to_string(),
            workflow_key: "workflow-key-1".to_string(),
            signal: Some(WorkflowSignal {
                name: "slack.event".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("signal or start run");
    let canceled = manager
        .cancel_run(WorkflowManagerCancelRun {
            run_id: "run-1".to_string(),
            reason: "test".to_string(),
            ..Default::default()
        })
        .await
        .expect("cancel run");
    let delivered = manager
        .deliver_event(WorkflowManagerDeliverEvent {
            provider_name: "basic".to_string(),
            event: Some(gestalt::WorkflowEvent {
                r#type: "roadmap.item.updated".to_string(),
                source: "roadmap".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("deliver event");
    manager
        .delete_deployment(WorkflowManagerDeleteDeployment {
            deployment_id: "deployment-1".to_string(),
            generation: 7,
            ..Default::default()
        })
        .await
        .expect("delete deployment");

    assert_eq!(applied.provider_name, "basic");
    assert_eq!(
        fetched
            .deployment
            .expect("fetched deployment")
            .spec
            .expect("spec")
            .id,
        "deployment-1"
    );
    assert_eq!(listed.deployments.len(), 1);
    assert_eq!(started_run.run.expect("started run").id, "run-1");
    assert_eq!(signaled_run.signal.expect("signal").name, "slack.event");
    assert!(signaled_or_started_run.started_run);
    assert_eq!(
        WorkflowRunStatus::try_from(canceled.run.expect("canceled run").status)
            .expect("run status")
            .as_str_name(),
        "WORKFLOW_RUN_STATUS_CANCELED"
    );
    assert_eq!(delivered.results[0].deployment_id, "deployment-1");

    let seen = server.seen.lock().expect("lock seen").clone();
    assert_eq!(
        seen.iter()
            .map(|entry| entry.method.as_str())
            .collect::<Vec<_>>(),
        vec![
            "plan",
            "apply",
            "get",
            "list",
            "set-deployment-paused",
            "set-activation-paused",
            "start-run",
            "signal-run",
            "signal-or-start-run",
            "cancel-run",
            "deliver-event",
            "delete",
        ]
    );
    assert!(
        seen.iter()
            .all(|entry| entry.invocation_token == "token-123")
    );
    assert_eq!(
        seen.iter()
            .filter(|entry| !entry.idempotency_key.is_empty())
            .map(|entry| entry.idempotency_key.as_str())
            .collect::<Vec<_>>(),
        vec![
            "workflow-request-key-rust",
            "workflow-request-key-rust",
            "workflow-request-key-rust",
            "workflow-request-key-rust",
            "workflow-request-key-rust",
        ]
    );

    let requests = server
        .signal_or_start_requests
        .lock()
        .expect("lock signal-or-start requests")
        .clone();
    assert_eq!(requests[0].workflow_key, "workflow-key-1");

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn request_workflow_manager_uses_embedded_invocation_token() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("g-rust-req-wm.sock");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt::ENV_WORKFLOW_MANAGER_SOCKET, socket.as_os_str());

    let server = TestWorkflowManagerServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_workflow_manager(serve_server, &serve_socket)
            .await
            .expect("serve workflow manager");
    });

    helpers::wait_for_socket(&socket).await;

    let request = Request {
        invocation_token: "token-embedded".to_string(),
        ..Request::default()
    };
    let mut manager = request
        .workflow_manager()
        .await
        .expect("request workflow manager");
    let response = manager
        .get_deployment(WorkflowManagerGetDeployment {
            deployment_id: "deployment-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("get deployment");

    assert_eq!(
        response
            .deployment
            .expect("deployment")
            .spec
            .expect("spec")
            .id,
        "deployment-1"
    );

    let seen = server.seen.lock().expect("lock seen").clone();
    assert_eq!(seen.len(), 1);
    assert_eq!(seen[0].invocation_token, "token-embedded");
    assert_eq!(seen[0].method, "get");

    serve_task.abort();
    let _ = serve_task.await;
}

async fn serve_workflow_manager(
    server: TestWorkflowManagerServer,
    socket: &Path,
) -> std::result::Result<(), tonic::transport::Error> {
    let _ = std::fs::remove_file(socket);
    let listener = UnixListener::bind(socket).expect("bind unix listener");

    Server::builder()
        .add_service(WorkflowManagerHostServer::new(server))
        .serve_with_incoming(UnixListenerStream::new(listener))
        .await
}

async fn serve_workflow_manager_tcp(
    server: TestWorkflowManagerServer,
    listener: TcpListener,
) -> std::result::Result<(), tonic::transport::Error> {
    Server::builder()
        .add_service(WorkflowManagerHostServer::new(server))
        .serve_with_incoming(TcpListenerStream::new(listener))
        .await
}
