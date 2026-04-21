#[allow(dead_code)]
mod helpers;

use std::path::Path;
use std::sync::{Arc, Mutex};

use gestalt::proto::v1::workflow_manager_host_server::{
    WorkflowManagerHost as ProtoWorkflowManagerHost, WorkflowManagerHostServer,
};
use gestalt::proto::v1::{
    BoundWorkflowSchedule, BoundWorkflowTarget, ManagedWorkflowSchedule,
    WorkflowManagerCreateScheduleRequest, WorkflowManagerDeleteScheduleRequest,
    WorkflowManagerGetScheduleRequest, WorkflowManagerPauseScheduleRequest,
    WorkflowManagerResumeScheduleRequest, WorkflowManagerUpdateScheduleRequest,
};
use gestalt::{ENV_WORKFLOW_MANAGER_SOCKET, Request, WorkflowManager};
use tokio::net::UnixListener;
use tokio_stream::wrappers::UnixListenerStream;
use tonic::codegen::async_trait;
use tonic::transport::Server;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

#[derive(Clone, Debug, Default, PartialEq)]
struct SeenRequest {
    method: String,
    request_handle: String,
    schedule_id: String,
}

#[derive(Clone, Default)]
struct TestWorkflowManagerServer {
    seen: Arc<Mutex<Vec<SeenRequest>>>,
}

#[async_trait]
impl ProtoWorkflowManagerHost for TestWorkflowManagerServer {
    async fn create_schedule(
        &self,
        request: GrpcRequest<WorkflowManagerCreateScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowSchedule>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "create".to_string(),
            request_handle: request.request_handle.clone(),
            schedule_id: String::new(),
        });
        Ok(GrpcResponse::new(ManagedWorkflowSchedule {
            provider_name: if request.provider_name.is_empty() {
                "basic".to_string()
            } else {
                request.provider_name
            },
            schedule: Some(BoundWorkflowSchedule {
                id: "sched-1".to_string(),
                cron: request.cron,
                timezone: request.timezone,
                target: request.target,
                paused: request.paused,
                ..Default::default()
            }),
        }))
    }

    async fn get_schedule(
        &self,
        request: GrpcRequest<WorkflowManagerGetScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowSchedule>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "get".to_string(),
            request_handle: request.request_handle.clone(),
            schedule_id: request.schedule_id.clone(),
        });
        Ok(GrpcResponse::new(ManagedWorkflowSchedule {
            provider_name: "basic".to_string(),
            schedule: Some(BoundWorkflowSchedule {
                id: request.schedule_id,
                ..Default::default()
            }),
        }))
    }

    async fn update_schedule(
        &self,
        request: GrpcRequest<WorkflowManagerUpdateScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowSchedule>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "update".to_string(),
            request_handle: request.request_handle.clone(),
            schedule_id: request.schedule_id.clone(),
        });
        Ok(GrpcResponse::new(ManagedWorkflowSchedule {
            provider_name: if request.provider_name.is_empty() {
                "basic".to_string()
            } else {
                request.provider_name
            },
            schedule: Some(BoundWorkflowSchedule {
                id: request.schedule_id,
                cron: request.cron,
                timezone: request.timezone,
                target: request.target,
                paused: request.paused,
                ..Default::default()
            }),
        }))
    }

    async fn delete_schedule(
        &self,
        request: GrpcRequest<WorkflowManagerDeleteScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "delete".to_string(),
            request_handle: request.request_handle,
            schedule_id: request.schedule_id,
        });
        Ok(GrpcResponse::new(()))
    }

    async fn pause_schedule(
        &self,
        request: GrpcRequest<WorkflowManagerPauseScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowSchedule>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "pause".to_string(),
            request_handle: request.request_handle.clone(),
            schedule_id: request.schedule_id.clone(),
        });
        Ok(GrpcResponse::new(ManagedWorkflowSchedule {
            provider_name: "basic".to_string(),
            schedule: Some(BoundWorkflowSchedule {
                id: request.schedule_id,
                paused: true,
                ..Default::default()
            }),
        }))
    }

    async fn resume_schedule(
        &self,
        request: GrpcRequest<WorkflowManagerResumeScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowSchedule>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "resume".to_string(),
            request_handle: request.request_handle.clone(),
            schedule_id: request.schedule_id.clone(),
        });
        Ok(GrpcResponse::new(ManagedWorkflowSchedule {
            provider_name: "basic".to_string(),
            schedule: Some(BoundWorkflowSchedule {
                id: request.schedule_id,
                paused: false,
                ..Default::default()
            }),
        }))
    }
}

#[tokio::test]
async fn workflow_manager_connects_over_unix_socket_and_sends_request_handle() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("g-rust-wm.sock");
    let _socket_guard = helpers::EnvGuard::set(ENV_WORKFLOW_MANAGER_SOCKET, socket.as_os_str());

    let server = TestWorkflowManagerServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_workflow_manager(serve_server, &serve_socket)
            .await
            .expect("serve workflow manager");
    });

    helpers::wait_for_socket(&socket).await;

    let mut manager = WorkflowManager::connect("handle-123")
        .await
        .expect("connect workflow manager");
    let created = manager
        .create_schedule(WorkflowManagerCreateScheduleRequest {
            provider_name: "basic".to_string(),
            cron: "*/5 * * * *".to_string(),
            timezone: "UTC".to_string(),
            target: Some(BoundWorkflowTarget {
                plugin_name: "roadmap".to_string(),
                operation: "sync".to_string(),
                ..Default::default()
            }),
            paused: false,
            ..Default::default()
        })
        .await
        .expect("create schedule");
    let fetched = manager
        .get_schedule(WorkflowManagerGetScheduleRequest {
            schedule_id: "sched-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("get schedule");
    let updated = manager
        .update_schedule(WorkflowManagerUpdateScheduleRequest {
            schedule_id: "sched-1".to_string(),
            provider_name: "secondary".to_string(),
            cron: "0 * * * *".to_string(),
            timezone: "America/New_York".to_string(),
            target: Some(BoundWorkflowTarget {
                plugin_name: "roadmap".to_string(),
                operation: "status".to_string(),
                ..Default::default()
            }),
            paused: true,
            ..Default::default()
        })
        .await
        .expect("update schedule");
    let paused = manager
        .pause_schedule(WorkflowManagerPauseScheduleRequest {
            schedule_id: "sched-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("pause schedule");
    let resumed = manager
        .resume_schedule(WorkflowManagerResumeScheduleRequest {
            schedule_id: "sched-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("resume schedule");
    manager
        .delete_schedule(WorkflowManagerDeleteScheduleRequest {
            schedule_id: "sched-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("delete schedule");

    assert_eq!(created.provider_name, "basic");
    assert_eq!(created.schedule.expect("created schedule").id, "sched-1");
    assert_eq!(fetched.schedule.expect("fetched schedule").id, "sched-1");
    assert_eq!(updated.provider_name, "secondary");
    assert!(updated.schedule.expect("updated schedule").paused);
    assert!(paused.schedule.expect("paused schedule").paused);
    assert!(!resumed.schedule.expect("resumed schedule").paused);

    let seen = server.seen.lock().expect("lock seen").clone();
    assert_eq!(
        seen,
        vec![
            SeenRequest {
                method: "create".to_string(),
                request_handle: "handle-123".to_string(),
                schedule_id: String::new(),
            },
            SeenRequest {
                method: "get".to_string(),
                request_handle: "handle-123".to_string(),
                schedule_id: "sched-1".to_string(),
            },
            SeenRequest {
                method: "update".to_string(),
                request_handle: "handle-123".to_string(),
                schedule_id: "sched-1".to_string(),
            },
            SeenRequest {
                method: "pause".to_string(),
                request_handle: "handle-123".to_string(),
                schedule_id: "sched-1".to_string(),
            },
            SeenRequest {
                method: "resume".to_string(),
                request_handle: "handle-123".to_string(),
                schedule_id: "sched-1".to_string(),
            },
            SeenRequest {
                method: "delete".to_string(),
                request_handle: "handle-123".to_string(),
                schedule_id: "sched-1".to_string(),
            },
        ]
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn request_workflow_manager_uses_embedded_request_handle() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("g-rust-req-wm.sock");
    let _socket_guard = helpers::EnvGuard::set(ENV_WORKFLOW_MANAGER_SOCKET, socket.as_os_str());

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
        request_handle: "handle-embedded".to_string(),
        ..Request::default()
    };
    let mut manager = request
        .workflow_manager()
        .await
        .expect("request workflow manager");
    let response = manager
        .get_schedule(WorkflowManagerGetScheduleRequest {
            schedule_id: "sched-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("get schedule");

    assert_eq!(response.schedule.expect("schedule").id, "sched-1");

    let seen = server.seen.lock().expect("lock seen").clone();
    assert_eq!(seen.len(), 1);
    assert_eq!(seen[0].request_handle, "handle-embedded");
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
