#[path = "../src/generated.rs"]
mod generated;

mod support_protocol;

#[allow(dead_code)]
mod helpers;

use std::path::Path;
use std::sync::{Arc, Mutex};

use generated::v1::workflow_provider_server::{
    WorkflowProvider as ProtoWorkflowProvider, WorkflowProviderServer,
};
use generated::v1::{
    BoundWorkflowDefinition, BoundWorkflowEventTrigger, BoundWorkflowRun, BoundWorkflowSchedule,
    CancelWorkflowProviderRunRequest, CreateWorkflowProviderDefinitionRequest,
    DeleteWorkflowProviderDefinitionRequest, DeleteWorkflowProviderEventTriggerRequest,
    DeleteWorkflowProviderScheduleRequest, GetWorkflowProviderDefinitionRequest,
    GetWorkflowProviderEventTriggerRequest, GetWorkflowProviderRunRequest,
    GetWorkflowProviderScheduleRequest, ListWorkflowProviderEventTriggersRequest,
    ListWorkflowProviderEventTriggersResponse, ListWorkflowProviderRunsRequest,
    ListWorkflowProviderRunsResponse, ListWorkflowProviderSchedulesRequest,
    ListWorkflowProviderSchedulesResponse, PauseWorkflowProviderEventTriggerRequest,
    PauseWorkflowProviderScheduleRequest, PublishWorkflowProviderEventRequest,
    ResumeWorkflowProviderEventTriggerRequest, ResumeWorkflowProviderScheduleRequest,
    SignalOrStartWorkflowProviderRunRequest, SignalWorkflowProviderRunRequest,
    SignalWorkflowRunResponse, StartWorkflowProviderRunRequest,
    UpdateWorkflowProviderDefinitionRequest, UpsertWorkflowProviderEventTriggerRequest,
    UpsertWorkflowProviderScheduleRequest, WorkflowEvent as ProtoWorkflowEvent, workflow_step,
};
use gestalt::{
    AgentOutput, BoundWorkflowTarget, Request, Workflow, WorkflowAgentMessage,
    WorkflowCreateDefinition, WorkflowCreateEventTrigger, WorkflowCreateSchedule,
    WorkflowDeleteDefinition, WorkflowDeleteEventTrigger, WorkflowDeleteSchedule, WorkflowEvent,
    WorkflowEventMatch, WorkflowGetDefinition, WorkflowGetEventTrigger, WorkflowGetSchedule,
    WorkflowPauseEventTrigger, WorkflowPauseSchedule, WorkflowPublishEvent,
    WorkflowResumeEventTrigger, WorkflowResumeSchedule, WorkflowSignal, WorkflowSignalOrStartRun,
    WorkflowSignalRun, WorkflowStartRun, WorkflowStep, WorkflowStepAction, WorkflowStepAgentTurn,
    WorkflowStepAppCall, WorkflowText, WorkflowUpdateDefinition, WorkflowUpdateEventTrigger,
    WorkflowUpdateSchedule,
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
struct TestWorkflowServer {
    seen: Arc<Mutex<Vec<SeenRequest>>>,
    relay_tokens: Arc<Mutex<Vec<String>>>,
    idempotency_keys: Arc<Mutex<Vec<String>>>,
    signal_or_start_requests: Arc<Mutex<Vec<SignalOrStartWorkflowProviderRunRequest>>>,
}

fn app_target(app_name: &str, operation: &str) -> BoundWorkflowTarget {
    BoundWorkflowTarget {
        steps: vec![WorkflowStep {
            id: operation.to_string(),
            action: WorkflowStepAction::App(WorkflowStepAppCall {
                name: app_name.to_string(),
                operation: operation.to_string(),
                ..Default::default()
            }),
            ..Default::default()
        }],
    }
}

#[async_trait]
impl ProtoWorkflowProvider for TestWorkflowServer {
    async fn start_run(
        &self,
        request: GrpcRequest<StartWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowRun>, Status> {
        let request = request.into_inner();
        if !request.idempotency_key.is_empty() {
            self.idempotency_keys
                .lock()
                .expect("lock idempotency keys")
                .push(request.idempotency_key.clone());
        }
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "start-run".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: String::new(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(BoundWorkflowRun {
            provider_name: if request.provider_name.is_empty() {
                "basic".to_string()
            } else {
                request.provider_name
            },
            id: "run-1".to_string(),
            target: request.target,
            workflow_key: request.workflow_key,
            ..Default::default()
        }))
    }

    async fn get_run(
        &self,
        request: GrpcRequest<GetWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowRun>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(BoundWorkflowRun {
            id: request.run_id,
            provider_name: "basic".to_string(),
            ..Default::default()
        }))
    }

    async fn list_runs(
        &self,
        _request: GrpcRequest<ListWorkflowProviderRunsRequest>,
    ) -> std::result::Result<GrpcResponse<ListWorkflowProviderRunsResponse>, Status> {
        Ok(GrpcResponse::new(
            ListWorkflowProviderRunsResponse::default(),
        ))
    }

    async fn cancel_run(
        &self,
        request: GrpcRequest<CancelWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowRun>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(BoundWorkflowRun {
            id: request.run_id,
            provider_name: "basic".to_string(),
            ..Default::default()
        }))
    }

    async fn signal_run(
        &self,
        request: GrpcRequest<SignalWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<SignalWorkflowRunResponse>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "signal-run".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: String::new(),
            event_type: request
                .signal
                .as_ref()
                .map(|signal| signal.name.clone())
                .unwrap_or_default(),
        });
        Ok(GrpcResponse::new(SignalWorkflowRunResponse {
            run: Some(BoundWorkflowRun {
                id: request.run_id,
                provider_name: "basic".to_string(),
                ..Default::default()
            }),
            signal: request.signal,
            started_run: false,
            workflow_key: String::new(),
        }))
    }

    async fn signal_or_start_run(
        &self,
        request: GrpcRequest<SignalOrStartWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<SignalWorkflowRunResponse>, Status> {
        let request = request.into_inner();
        if !request.idempotency_key.is_empty() {
            self.idempotency_keys
                .lock()
                .expect("lock idempotency keys")
                .push(request.idempotency_key.clone());
        }
        self.signal_or_start_requests
            .lock()
            .expect("lock signal or start requests")
            .push(request.clone());
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "signal-or-start-run".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: String::new(),
            event_type: request
                .signal
                .as_ref()
                .map(|signal| signal.name.clone())
                .unwrap_or_default(),
        });
        let provider_name = if request.provider_name.is_empty() {
            "basic".to_string()
        } else {
            request.provider_name
        };
        Ok(GrpcResponse::new(SignalWorkflowRunResponse {
            run: Some(BoundWorkflowRun {
                id: "run-1".to_string(),
                provider_name,
                target: request.target,
                workflow_key: request.workflow_key.clone(),
                ..Default::default()
            }),
            signal: request.signal,
            started_run: true,
            workflow_key: request.workflow_key,
        }))
    }

    async fn create_definition(
        &self,
        request: GrpcRequest<CreateWorkflowProviderDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowDefinition>, Status> {
        let request = request.into_inner();
        if !request.idempotency_key.is_empty() {
            self.idempotency_keys
                .lock()
                .expect("lock idempotency keys")
                .push(request.idempotency_key.clone());
        }
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "create-definition".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: String::new(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(BoundWorkflowDefinition {
            provider_name: if request.provider_name.is_empty() {
                "basic".to_string()
            } else {
                request.provider_name
            },
            id: "definition-1".to_string(),
            target: request.target,
            ..Default::default()
        }))
    }

    async fn get_definition(
        &self,
        request: GrpcRequest<GetWorkflowProviderDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowDefinition>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "get-definition".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: request.definition_id.clone(),
            trigger_id: String::new(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(BoundWorkflowDefinition {
            provider_name: "basic".to_string(),
            id: request.definition_id,
            ..Default::default()
        }))
    }

    async fn update_definition(
        &self,
        request: GrpcRequest<UpdateWorkflowProviderDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowDefinition>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "update-definition".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: request.definition_id.clone(),
            trigger_id: String::new(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(BoundWorkflowDefinition {
            provider_name: if request.provider_name.is_empty() {
                "basic".to_string()
            } else {
                request.provider_name
            },
            id: request.definition_id,
            target: request.target,
            ..Default::default()
        }))
    }

    async fn delete_definition(
        &self,
        request: GrpcRequest<DeleteWorkflowProviderDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "delete-definition".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: request.definition_id,
            trigger_id: String::new(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(()))
    }

    async fn upsert_schedule(
        &self,
        request: GrpcRequest<UpsertWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowSchedule>, Status> {
        if let Some(token) = request.metadata().get("x-gestalt-host-service-relay-token") {
            self.relay_tokens
                .lock()
                .expect("lock relay tokens")
                .push(token.to_str().expect("relay token ascii").to_string());
        }
        let request = request.into_inner();
        if !request.idempotency_key.is_empty() {
            self.idempotency_keys
                .lock()
                .expect("lock idempotency keys")
                .push(request.idempotency_key.clone());
        }
        let is_create = request.schedule_id.is_empty();
        let schedule_id = if is_create {
            "sched-1".to_string()
        } else {
            request.schedule_id.clone()
        };
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: if is_create { "create" } else { "update" }.to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: if is_create {
                String::new()
            } else {
                request.schedule_id.clone()
            },
            trigger_id: String::new(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(BoundWorkflowSchedule {
            provider_name: if request.provider_name.is_empty() {
                "basic".to_string()
            } else {
                request.provider_name
            },
            id: schedule_id,
            cron: request.cron,
            timezone: request.timezone,
            target: request.target,
            paused: request.paused,
            ..Default::default()
        }))
    }

    async fn get_schedule(
        &self,
        request: GrpcRequest<GetWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowSchedule>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "get".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: request.schedule_id.clone(),
            trigger_id: String::new(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(BoundWorkflowSchedule {
            provider_name: "basic".to_string(),
            id: request.schedule_id,
            ..Default::default()
        }))
    }

    async fn list_schedules(
        &self,
        _request: GrpcRequest<ListWorkflowProviderSchedulesRequest>,
    ) -> std::result::Result<GrpcResponse<ListWorkflowProviderSchedulesResponse>, Status> {
        Ok(GrpcResponse::new(
            ListWorkflowProviderSchedulesResponse::default(),
        ))
    }

    async fn delete_schedule(
        &self,
        request: GrpcRequest<DeleteWorkflowProviderScheduleRequest>,
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
        request: GrpcRequest<PauseWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowSchedule>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "pause".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: request.schedule_id.clone(),
            trigger_id: String::new(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(BoundWorkflowSchedule {
            provider_name: "basic".to_string(),
            id: request.schedule_id,
            paused: true,
            ..Default::default()
        }))
    }

    async fn resume_schedule(
        &self,
        request: GrpcRequest<ResumeWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowSchedule>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "resume".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: request.schedule_id.clone(),
            trigger_id: String::new(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(BoundWorkflowSchedule {
            provider_name: "basic".to_string(),
            id: request.schedule_id,
            paused: false,
            ..Default::default()
        }))
    }

    async fn upsert_event_trigger(
        &self,
        request: GrpcRequest<UpsertWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowEventTrigger>, Status> {
        let request = request.into_inner();
        if !request.idempotency_key.is_empty() {
            self.idempotency_keys
                .lock()
                .expect("lock idempotency keys")
                .push(request.idempotency_key.clone());
        }
        let is_create = request.trigger_id.is_empty();
        let trigger_id = if is_create {
            "trg-1".to_string()
        } else {
            request.trigger_id.clone()
        };
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: if is_create {
                "create-trigger"
            } else {
                "update-trigger"
            }
            .to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: if is_create {
                String::new()
            } else {
                request.trigger_id.clone()
            },
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(BoundWorkflowEventTrigger {
            provider_name: if request.provider_name.is_empty() {
                "basic".to_string()
            } else {
                request.provider_name
            },
            id: trigger_id,
            r#match: request.r#match,
            target: request.target,
            paused: request.paused,
            ..Default::default()
        }))
    }

    async fn get_event_trigger(
        &self,
        request: GrpcRequest<GetWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowEventTrigger>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "get-trigger".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: request.trigger_id.clone(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(BoundWorkflowEventTrigger {
            provider_name: "basic".to_string(),
            id: request.trigger_id,
            ..Default::default()
        }))
    }

    async fn list_event_triggers(
        &self,
        _request: GrpcRequest<ListWorkflowProviderEventTriggersRequest>,
    ) -> std::result::Result<GrpcResponse<ListWorkflowProviderEventTriggersResponse>, Status> {
        Ok(GrpcResponse::new(
            ListWorkflowProviderEventTriggersResponse::default(),
        ))
    }

    async fn delete_event_trigger(
        &self,
        request: GrpcRequest<DeleteWorkflowProviderEventTriggerRequest>,
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
        request: GrpcRequest<PauseWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowEventTrigger>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "pause-trigger".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: request.trigger_id.clone(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(BoundWorkflowEventTrigger {
            provider_name: "basic".to_string(),
            id: request.trigger_id,
            paused: true,
            ..Default::default()
        }))
    }

    async fn resume_event_trigger(
        &self,
        request: GrpcRequest<ResumeWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowEventTrigger>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "resume-trigger".to_string(),
            invocation_token: request.invocation_token.clone(),
            schedule_id: String::new(),
            trigger_id: request.trigger_id.clone(),
            event_type: String::new(),
        });
        Ok(GrpcResponse::new(BoundWorkflowEventTrigger {
            provider_name: "basic".to_string(),
            id: request.trigger_id,
            paused: false,
            ..Default::default()
        }))
    }

    async fn publish_event(
        &self,
        request: GrpcRequest<PublishWorkflowProviderEventRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoWorkflowEvent>, Status> {
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
async fn workflow_connects_over_tcp_and_sends_relay_token() {
    let _env_lock = helpers::env_lock().lock().await;

    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind tcp listener");
    let address = listener.local_addr().expect("local addr");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, format!("tcp://{address}"));
    let _token_guard = helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_TOKEN, "relay-token-rust");

    let server = TestWorkflowServer::default();
    let serve_server = server.clone();
    let serve_task = tokio::spawn(async move {
        serve_workflow_tcp(serve_server, listener)
            .await
            .expect("serve workflow");
    });

    let mut manager =
        Workflow::connect_with_idempotency_key("token-123", "workflow-request-key-rust")
            .await
            .expect("connect workflow");
    let created = manager
        .create_schedule(WorkflowCreateSchedule {
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
async fn workflow_connects_over_unix_socket_and_sends_invocation_token() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("g-rust-wm.sock");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, socket.as_os_str());

    let server = TestWorkflowServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_workflow(serve_server, &serve_socket)
            .await
            .expect("serve workflow");
    });

    helpers::wait_for_socket(&socket).await;

    let mut manager =
        Workflow::connect_with_idempotency_key("token-123", "workflow-request-key-rust")
            .await
            .expect("connect workflow");
    let started_run = manager
        .start_run(WorkflowStartRun {
            provider_name: "basic".to_string(),
            workflow_key: "workflow-key-1".to_string(),
            target: Some(app_target("roadmap", "sync")),
            ..Default::default()
        })
        .await
        .expect("start run");
    let signaled_run = manager
        .signal_run(WorkflowSignalRun {
            run_id: "run-1".to_string(),
            signal: Some(WorkflowSignal {
                name: "slack.event".to_string(),
                ..Default::default()
            }),
        })
        .await
        .expect("signal run");
    let signaled_or_started_run = manager
        .signal_or_start_run(WorkflowSignalOrStartRun {
            provider_name: "basic".to_string(),
            workflow_key: "workflow-key-1".to_string(),
            target: Some(app_target("roadmap", "sync")),
            signal: Some(WorkflowSignal {
                name: "slack.event".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("signal or start run");
    let created_definition = manager
        .create_definition(WorkflowCreateDefinition {
            provider_name: "basic".to_string(),
            target: Some(app_target("roadmap", "sync")),
            ..Default::default()
        })
        .await
        .expect("create definition");
    let fetched_definition = manager
        .get_definition(WorkflowGetDefinition {
            definition_id: "definition-1".to_string(),
        })
        .await
        .expect("get definition");
    let updated_definition = manager
        .update_definition(WorkflowUpdateDefinition {
            definition_id: "definition-1".to_string(),
            provider_name: "secondary".to_string(),
            target: Some(app_target("roadmap", "status")),
        })
        .await
        .expect("update definition");
    manager
        .delete_definition(WorkflowDeleteDefinition {
            definition_id: "definition-1".to_string(),
        })
        .await
        .expect("delete definition");
    let created = manager
        .create_schedule(WorkflowCreateSchedule {
            provider_name: "basic".to_string(),
            cron: "*/5 * * * *".to_string(),
            timezone: "UTC".to_string(),
            target: Some(app_target("roadmap", "sync")),
            paused: false,
            ..Default::default()
        })
        .await
        .expect("create schedule");
    let fetched = manager
        .get_schedule(WorkflowGetSchedule {
            schedule_id: "sched-1".to_string(),
        })
        .await
        .expect("get schedule");
    let updated = manager
        .update_schedule(WorkflowUpdateSchedule {
            schedule_id: "sched-1".to_string(),
            provider_name: "secondary".to_string(),
            cron: "0 * * * *".to_string(),
            timezone: "America/New_York".to_string(),
            target: Some(app_target("roadmap", "status")),
            paused: true,
            ..Default::default()
        })
        .await
        .expect("update schedule");
    let paused = manager
        .pause_schedule(WorkflowPauseSchedule {
            schedule_id: "sched-1".to_string(),
        })
        .await
        .expect("pause schedule");
    let resumed = manager
        .resume_schedule(WorkflowResumeSchedule {
            schedule_id: "sched-1".to_string(),
        })
        .await
        .expect("resume schedule");
    manager
        .delete_schedule(WorkflowDeleteSchedule {
            schedule_id: "sched-1".to_string(),
        })
        .await
        .expect("delete schedule");
    let created_trigger = manager
        .create_trigger(WorkflowCreateEventTrigger {
            provider_name: "basic".to_string(),
            event_match: Some(WorkflowEventMatch {
                event_type: "roadmap.item.updated".to_string(),
                source: "roadmap".to_string(),
                ..Default::default()
            }),
            target: Some(app_target("slack", "chat.postMessage")),
            paused: false,
            ..Default::default()
        })
        .await
        .expect("create trigger");
    let fetched_trigger = manager
        .get_trigger(WorkflowGetEventTrigger {
            trigger_id: "trg-1".to_string(),
        })
        .await
        .expect("get trigger");
    let updated_trigger = manager
        .update_trigger(WorkflowUpdateEventTrigger {
            trigger_id: "trg-1".to_string(),
            provider_name: "secondary".to_string(),
            event_match: Some(WorkflowEventMatch {
                event_type: "roadmap.item.synced".to_string(),
                ..Default::default()
            }),
            target: Some(app_target("slack", "chat.postMessage")),
            paused: true,
            ..Default::default()
        })
        .await
        .expect("update trigger");
    let paused_trigger = manager
        .pause_trigger(WorkflowPauseEventTrigger {
            trigger_id: "trg-1".to_string(),
        })
        .await
        .expect("pause trigger");
    let resumed_trigger = manager
        .resume_trigger(WorkflowResumeEventTrigger {
            trigger_id: "trg-1".to_string(),
        })
        .await
        .expect("resume trigger");
    manager
        .delete_trigger(WorkflowDeleteEventTrigger {
            trigger_id: "trg-1".to_string(),
        })
        .await
        .expect("delete trigger");
    let published_event = manager
        .publish_event(WorkflowPublishEvent {
            event: Some(WorkflowEvent {
                event_type: "roadmap.item.updated".to_string(),
                source: "roadmap".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("publish event");

    let started = started_run.run.expect("started run");
    assert_eq!(started.id, "run-1");
    assert_eq!(started.workflow_key, "workflow-key-1");
    let signaled = signaled_run.signal.expect("signaled signal");
    assert_eq!(signaled.name, "slack.event");
    assert!(signaled_or_started_run.started_run);
    assert_eq!(signaled_or_started_run.workflow_key, "workflow-key-1");
    assert_eq!(
        created_definition
            .definition
            .expect("created definition")
            .id,
        "definition-1"
    );
    assert_eq!(
        fetched_definition
            .definition
            .expect("fetched definition")
            .id,
        "definition-1"
    );
    assert_eq!(updated_definition.provider_name, "secondary");
    assert_eq!(created.provider_name, "basic");
    assert_eq!(created.schedule.expect("created schedule").id, "sched-1");
    assert_eq!(fetched.schedule.expect("fetched schedule").id, "sched-1");
    assert_eq!(updated.provider_name, "secondary");
    assert!(updated.schedule.expect("updated schedule").paused);
    assert!(paused.schedule.expect("paused schedule").paused);
    assert!(!resumed.schedule.expect("resumed schedule").paused);
    assert_eq!(created_trigger.provider_name, "basic");
    let created_trigger = created_trigger.trigger.expect("created trigger");
    assert_eq!(created_trigger.id, "trg-1");
    assert_eq!(
        created_trigger
            .event_match
            .as_ref()
            .expect("created trigger match")
            .event_type,
        "roadmap.item.updated"
    );
    assert_eq!(
        fetched_trigger.trigger.expect("fetched trigger").id,
        "trg-1"
    );
    assert_eq!(updated_trigger.provider_name, "secondary");
    let updated_trigger = updated_trigger.trigger.expect("updated trigger");
    assert!(updated_trigger.paused);
    assert_eq!(
        updated_trigger
            .event_match
            .as_ref()
            .expect("updated trigger match")
            .event_type,
        "roadmap.item.synced"
    );
    assert!(paused_trigger.trigger.expect("paused trigger").paused);
    assert!(!resumed_trigger.trigger.expect("resumed trigger").paused);
    assert_eq!(published_event.event_type, "roadmap.item.updated");

    let seen = server.seen.lock().expect("lock seen").clone();
    let idempotency_keys = server
        .idempotency_keys
        .lock()
        .expect("lock idempotency keys")
        .clone();
    assert_eq!(
        idempotency_keys,
        vec![
            "workflow-request-key-rust".to_string(),
            "workflow-request-key-rust".to_string(),
            "workflow-request-key-rust".to_string(),
            "workflow-request-key-rust".to_string(),
            "workflow-request-key-rust".to_string(),
        ]
    );
    assert_eq!(
        seen,
        vec![
            SeenRequest {
                method: "start-run".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: String::new(),
                trigger_id: String::new(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "signal-run".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: String::new(),
                trigger_id: String::new(),
                event_type: "slack.event".to_string(),
            },
            SeenRequest {
                method: "signal-or-start-run".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: String::new(),
                trigger_id: String::new(),
                event_type: "slack.event".to_string(),
            },
            SeenRequest {
                method: "create-definition".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: String::new(),
                trigger_id: String::new(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "get-definition".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: "definition-1".to_string(),
                trigger_id: String::new(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "update-definition".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: "definition-1".to_string(),
                trigger_id: String::new(),
                event_type: String::new(),
            },
            SeenRequest {
                method: "delete-definition".to_string(),
                invocation_token: "token-123".to_string(),
                schedule_id: "definition-1".to_string(),
                trigger_id: String::new(),
                event_type: String::new(),
            },
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
async fn workflow_signal_or_start_accepts_native_values() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("g-rust-wm-native.sock");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, socket.as_os_str());

    let server = TestWorkflowServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_workflow(serve_server, &serve_socket)
            .await
            .expect("serve workflow");
    });

    helpers::wait_for_socket(&socket).await;

    let mut manager =
        Workflow::connect_with_idempotency_key("token-123", "workflow-request-key-rust")
            .await
            .expect("connect workflow");
    let signaled = manager
        .signal_or_start_run(WorkflowSignalOrStartRun {
            provider_name: "basic".to_string(),
            workflow_key: "workflow-key-1".to_string(),
            idempotency_key: "signal-request-key".to_string(),
            target: Some(BoundWorkflowTarget {
                steps: vec![WorkflowStep {
                    id: "reply".to_string(),
                    action: WorkflowStepAction::Agent(WorkflowStepAgentTurn {
                        provider: "openai".to_string(),
                        model: "gpt-5.1".to_string(),
                        messages: vec![WorkflowAgentMessage {
                            role: "user".to_string(),
                            text: Some(WorkflowText {
                                template: "Respond in thread.".to_string(),
                            }),
                            ..Default::default()
                        }],
                        output: AgentOutput::text(),
                        session_key: String::new(),
                        prompt: None,
                        tools: Vec::new(),
                        model_options: None,
                    }),
                    ..Default::default()
                }],
            }),
            signal: Some(WorkflowSignal {
                name: "slack.event".to_string(),
                payload: Some(serde_json::json!({ "channel": "C123" })),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("signal or start run");

    assert!(signaled.started_run);

    let requests = server
        .signal_or_start_requests
        .lock()
        .expect("lock signal or start requests")
        .clone();
    assert_eq!(requests.len(), 1);
    let request = &requests[0];
    assert_eq!(request.invocation_token, "token-123");
    assert_eq!(request.provider_name, "basic");
    assert_eq!(request.workflow_key, "workflow-key-1");
    assert_eq!(request.idempotency_key, "signal-request-key");
    let signal = request.signal.as_ref().expect("signal");
    assert_eq!(signal.name, "slack.event");
    assert_eq!(
        support_protocol::json_from_struct(signal.payload.as_ref().unwrap()),
        serde_json::json!({ "channel": "C123" })
    );

    let target = request.target.as_ref().expect("target");
    let step = target.steps.first().expect("workflow step");
    let agent = match step.action.as_ref().expect("step action") {
        workflow_step::Action::Agent(agent) => agent,
        _ => panic!("expected agent step"),
    };
    assert_eq!(agent.provider, "openai");
    assert_eq!(agent.model, "gpt-5.1");
    assert_eq!(agent.messages.len(), 1);
    assert_eq!(agent.messages[0].role, "user");
    assert_eq!(
        agent.messages[0]
            .text
            .as_ref()
            .expect("message text")
            .template,
        "Respond in thread."
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn request_workflow_uses_embedded_invocation_token() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("g-rust-req-wm.sock");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, socket.as_os_str());

    let server = TestWorkflowServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_workflow(serve_server, &serve_socket)
            .await
            .expect("serve workflow");
    });

    helpers::wait_for_socket(&socket).await;

    let request = Request {
        invocation_token: "token-embedded".to_string(),
        ..Request::default()
    };
    let mut manager = request.workflow().await.expect("request workflow");
    let response = manager
        .get_schedule(WorkflowGetSchedule {
            schedule_id: "sched-1".to_string(),
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

async fn serve_workflow(
    server: TestWorkflowServer,
    socket: &Path,
) -> std::result::Result<(), tonic::transport::Error> {
    let _ = std::fs::remove_file(socket);
    let listener = UnixListener::bind(socket).expect("bind unix listener");

    Server::builder()
        .add_service(WorkflowProviderServer::new(server))
        .serve_with_incoming(UnixListenerStream::new(listener))
        .await
}

async fn serve_workflow_tcp(
    server: TestWorkflowServer,
    listener: TcpListener,
) -> std::result::Result<(), tonic::transport::Error> {
    Server::builder()
        .add_service(WorkflowProviderServer::new(server))
        .serve_with_incoming(TcpListenerStream::new(listener))
        .await
}
