#[allow(dead_code)]
mod helpers;

use std::path::Path;
use std::sync::{Arc, Mutex};

use gestalt::proto::v1::workflow_manager_host_server::{
    WorkflowManagerHost as ProtoWorkflowManagerHost, WorkflowManagerHostServer,
};
use gestalt::proto::v1::{
    BoundWorkflowEventTrigger, BoundWorkflowPluginTarget, BoundWorkflowSchedule,
    BoundWorkflowTarget, ManagedWorkflowEventTrigger, ManagedWorkflowSchedule, WorkflowEvent,
    WorkflowEventMatch, WorkflowManagerCreateEventTriggerRequest,
    WorkflowManagerCreateScheduleRequest, WorkflowManagerDeleteEventTriggerRequest,
    WorkflowManagerDeleteScheduleRequest, WorkflowManagerGetEventTriggerRequest,
    WorkflowManagerGetScheduleRequest, WorkflowManagerPauseEventTriggerRequest,
    WorkflowManagerPauseScheduleRequest, WorkflowManagerPublishEventRequest,
    WorkflowManagerResumeEventTriggerRequest, WorkflowManagerResumeScheduleRequest,
    WorkflowManagerUpdateEventTriggerRequest, WorkflowManagerUpdateScheduleRequest,
};
use gestalt::{
    ENV_WORKFLOW_MANAGER_SOCKET, ENV_WORKFLOW_MANAGER_SOCKET_TOKEN, Request, WorkflowManager,
};
use tokio::net::{TcpListener, UnixListener};
use tokio_stream::wrappers::{TcpListenerStream, UnixListenerStream};
use tonic::codegen::async_trait;
use tonic::transport::Server;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

#[derive(Clone, Debug, Default, PartialEq)]
struct SeenRequest {
    method: String,
    invocation_token: String,
    schedule_id: String,
    trigger_id: String,
    event_type: String,
}

#[derive(Clone, Default)]
struct TestWorkflowManagerServer {
    seen: Arc<Mutex<Vec<SeenRequest>>>,
    relay_tokens: Arc<Mutex<Vec<String>>>,
}

fn plugin_target(plugin_name: &str, operation: &str) -> BoundWorkflowTarget {
    BoundWorkflowTarget {
        plugin: Some(BoundWorkflowPluginTarget {
            plugin_name: plugin_name.to_string(),
            operation: operation.to_string(),
            ..Default::default()
        }),
        agent: None,
        ..Default::default()
    }
}

#[async_trait]
impl ProtoWorkflowManagerHost for TestWorkflowManagerServer {
    async fn create_schedule(
        &self,
        request: GrpcRequest<WorkflowManagerCreateScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowSchedule>, Status> {
        if let Some(token) = request.metadata().get("x-gestalt-host-service-relay-token") {
            self.relay_tokens
                .lock()
                .expect("lock relay tokens")
                .push(token.to_str().expect("relay token ascii").to_string());
        }
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "create".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: String::new(),
            event_type: String::new(),
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
            invocation_token: request.invocation_token.clone(),
            schedule_id: request.schedule_id.clone(),
            trigger_id: String::new(),
            event_type: String::new(),
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
            invocation_token: request.invocation_token.clone(),
            schedule_id: request.schedule_id.clone(),
            trigger_id: String::new(),
            event_type: String::new(),
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
            invocation_token: request.invocation_token,
            schedule_id: request.schedule_id,
            trigger_id: String::new(),
            event_type: String::new(),
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
            invocation_token: request.invocation_token.clone(),
            schedule_id: request.schedule_id.clone(),
            trigger_id: String::new(),
            event_type: String::new(),
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
            invocation_token: request.invocation_token.clone(),
            schedule_id: request.schedule_id.clone(),
            trigger_id: String::new(),
            event_type: String::new(),
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

    async fn create_event_trigger(
        &self,
        request: GrpcRequest<WorkflowManagerCreateEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowEventTrigger>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "create-trigger".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: String::new(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(ManagedWorkflowEventTrigger {
            provider_name: if request.provider_name.is_empty() {
                "basic".to_string()
            } else {
                request.provider_name
            },
            trigger: Some(BoundWorkflowEventTrigger {
                id: "trg-1".to_string(),
                r#match: request.r#match,
                target: request.target,
                paused: request.paused,
                ..Default::default()
            }),
        }))
    }

    async fn get_event_trigger(
        &self,
        request: GrpcRequest<WorkflowManagerGetEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowEventTrigger>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "get-trigger".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: request.trigger_id.clone(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(ManagedWorkflowEventTrigger {
            provider_name: "basic".to_string(),
            trigger: Some(BoundWorkflowEventTrigger {
                id: request.trigger_id,
                ..Default::default()
            }),
        }))
    }

    async fn update_event_trigger(
        &self,
        request: GrpcRequest<WorkflowManagerUpdateEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowEventTrigger>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "update-trigger".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: request.trigger_id.clone(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(ManagedWorkflowEventTrigger {
            provider_name: if request.provider_name.is_empty() {
                "basic".to_string()
            } else {
                request.provider_name
            },
            trigger: Some(BoundWorkflowEventTrigger {
                id: request.trigger_id,
                r#match: request.r#match,
                target: request.target,
                paused: request.paused,
                ..Default::default()
            }),
        }))
    }

    async fn delete_event_trigger(
        &self,
        request: GrpcRequest<WorkflowManagerDeleteEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "delete-trigger".to_string(),
            invocation_token: request.invocation_token,
            schedule_id: String::new(),
            trigger_id: request.trigger_id,
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(()))
    }

    async fn pause_event_trigger(
        &self,
        request: GrpcRequest<WorkflowManagerPauseEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowEventTrigger>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "pause-trigger".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: request.trigger_id.clone(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(ManagedWorkflowEventTrigger {
            provider_name: "basic".to_string(),
            trigger: Some(BoundWorkflowEventTrigger {
                id: request.trigger_id,
                paused: true,
                ..Default::default()
            }),
        }))
    }

    async fn resume_event_trigger(
        &self,
        request: GrpcRequest<WorkflowManagerResumeEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedWorkflowEventTrigger>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "resume-trigger".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: request.trigger_id.clone(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(ManagedWorkflowEventTrigger {
            provider_name: "basic".to_string(),
            trigger: Some(BoundWorkflowEventTrigger {
                id: request.trigger_id,
                paused: false,
                ..Default::default()
            }),
        }))
    }

    async fn publish_event(
        &self,
        request: GrpcRequest<WorkflowManagerPublishEventRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowEvent>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "publish-event".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: String::new(),
            event_type: request
                .event
                .as_ref()
                .map(|event| event.r#type.clone())
                .unwrap_or_default(),
        });
        let mut event = request.event.unwrap_or_default();
        if event.id.is_empty() {
            event.id = "evt-1".to_string();
        }
        Ok(GrpcResponse::new(event))
    }
}

#[tokio::test]
async fn workflow_manager_connects_over_tcp_and_sends_relay_token() {
    let _env_lock = helpers::env_lock().lock().await;

    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind tcp listener");
    let address = listener.local_addr().expect("local addr");
    let _socket_guard =
        helpers::EnvGuard::set(ENV_WORKFLOW_MANAGER_SOCKET, format!("tcp://{address}"));
    let _token_guard =
        helpers::EnvGuard::set(ENV_WORKFLOW_MANAGER_SOCKET_TOKEN, "relay-token-rust");

    let server = TestWorkflowManagerServer::default();
    let serve_server = server.clone();
    let serve_task = tokio::spawn(async move {
        serve_workflow_manager_tcp(serve_server, listener)
            .await
            .expect("serve workflow manager");
    });

    let mut manager = WorkflowManager::connect("token-123")
        .await
        .expect("connect workflow manager");
    let created = manager
        .create_schedule(WorkflowManagerCreateScheduleRequest {
            provider_name: "managed".to_string(),
            cron: "*/5 * * * *".to_string(),
            ..Default::default()
        })
        .await
        .expect("create schedule");

    assert_eq!(created.provider_name, "managed");
    assert_eq!(created.schedule.expect("created schedule").id, "sched-1");

    let relay_tokens = server
        .relay_tokens
        .lock()
        .expect("lock relay tokens")
        .clone();
    assert_eq!(relay_tokens, vec!["relay-token-rust".to_string()]);

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn workflow_manager_connects_over_unix_socket_and_sends_invocation_token() {
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

    let mut manager = WorkflowManager::connect("token-123")
        .await
        .expect("connect workflow manager");
    let created = manager
        .create_schedule(WorkflowManagerCreateScheduleRequest {
            provider_name: "basic".to_string(),
            cron: "*/5 * * * *".to_string(),
            timezone: "UTC".to_string(),
            target: Some(plugin_target("roadmap", "sync")),
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
            target: Some(plugin_target("roadmap", "status")),
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
    let created_trigger = manager
        .create_trigger(WorkflowManagerCreateEventTriggerRequest {
            provider_name: "basic".to_string(),
            r#match: Some(WorkflowEventMatch {
                r#type: "roadmap.item.updated".to_string(),
                source: "roadmap".to_string(),
                ..Default::default()
            }),
            target: Some(plugin_target("slack", "chat.postMessage")),
            paused: false,
            ..Default::default()
        })
        .await
        .expect("create trigger");
    let fetched_trigger = manager
        .get_trigger(WorkflowManagerGetEventTriggerRequest {
            trigger_id: "trg-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("get trigger");
    let updated_trigger = manager
        .update_trigger(WorkflowManagerUpdateEventTriggerRequest {
            trigger_id: "trg-1".to_string(),
            provider_name: "secondary".to_string(),
            r#match: Some(WorkflowEventMatch {
                r#type: "roadmap.item.synced".to_string(),
                ..Default::default()
            }),
            target: Some(plugin_target("slack", "chat.postMessage")),
            paused: true,
            ..Default::default()
        })
        .await
        .expect("update trigger");
    let paused_trigger = manager
        .pause_trigger(WorkflowManagerPauseEventTriggerRequest {
            trigger_id: "trg-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("pause trigger");
    let resumed_trigger = manager
        .resume_trigger(WorkflowManagerResumeEventTriggerRequest {
            trigger_id: "trg-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("resume trigger");
    manager
        .delete_trigger(WorkflowManagerDeleteEventTriggerRequest {
            trigger_id: "trg-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("delete trigger");
    let published_event = manager
        .publish_event(WorkflowManagerPublishEventRequest {
            event: Some(WorkflowEvent {
                r#type: "roadmap.item.updated".to_string(),
                source: "roadmap".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("publish event");

    assert_eq!(created.provider_name, "basic");
    assert_eq!(created.schedule.expect("created schedule").id, "sched-1");
    assert_eq!(fetched.schedule.expect("fetched schedule").id, "sched-1");
    assert_eq!(updated.provider_name, "secondary");
    assert!(updated.schedule.expect("updated schedule").paused);
    assert!(paused.schedule.expect("paused schedule").paused);
    assert!(!resumed.schedule.expect("resumed schedule").paused);
    assert_eq!(created_trigger.provider_name, "basic");
    assert_eq!(
        created_trigger.trigger.expect("created trigger").id,
        "trg-1"
    );
    assert_eq!(
        fetched_trigger.trigger.expect("fetched trigger").id,
        "trg-1"
    );
    assert_eq!(updated_trigger.provider_name, "secondary");
    assert!(updated_trigger.trigger.expect("updated trigger").paused);
    assert!(paused_trigger.trigger.expect("paused trigger").paused);
    assert!(!resumed_trigger.trigger.expect("resumed trigger").paused);
    assert_eq!(published_event.r#type, "roadmap.item.updated");

    let seen = server.seen.lock().expect("lock seen").clone();
    assert_eq!(
        seen,
        vec![
            SeenRequest {
                method: "create".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: String::new(),
                trigger_id: String::new(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "get".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: "sched-1".to_string(),
                trigger_id: String::new(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "update".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: "sched-1".to_string(),
                trigger_id: String::new(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "pause".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: "sched-1".to_string(),
                trigger_id: String::new(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "resume".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: "sched-1".to_string(),
                trigger_id: String::new(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "delete".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: "sched-1".to_string(),
                trigger_id: String::new(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "create-trigger".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: String::new(),
                trigger_id: String::new(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "get-trigger".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: String::new(),
                trigger_id: "trg-1".to_string(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "update-trigger".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: String::new(),
                trigger_id: "trg-1".to_string(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "pause-trigger".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: String::new(),
                trigger_id: "trg-1".to_string(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "resume-trigger".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: String::new(),
                trigger_id: "trg-1".to_string(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "delete-trigger".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: String::new(),
                trigger_id: "trg-1".to_string(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "publish-event".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: String::new(),
                trigger_id: String::new(),
                event_type: "roadmap.item.updated".to_string(),
            },
        ]
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn request_workflow_manager_uses_embedded_invocation_token() {
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
        invocation_token: "token-embedded".to_string(),
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
