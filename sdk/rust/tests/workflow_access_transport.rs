#[allow(dead_code)]
mod helpers;

mod support_protocol;

use std::path::Path;
use std::sync::{Arc, Mutex};

use gestalt::proto::v1::workflow_provider_server::{
    WorkflowProvider as ProtoWorkflowProvider, WorkflowProviderServer,
};
use gestalt::proto::v1::{
    ApplyWorkflowProviderDefinitionRequest, BoundWorkflowTarget, CancelWorkflowProviderRunRequest,
    DeleteWorkflowProviderDefinitionRequest, DeliverWorkflowProviderEventRequest,
    GetWorkflowProviderDefinitionRequest, GetWorkflowProviderRunEventsRequest,
    GetWorkflowProviderRunOutputRequest, GetWorkflowProviderRunRequest,
    ListWorkflowProviderDefinitionsRequest, ListWorkflowProviderDefinitionsResponse,
    ListWorkflowProviderRunsRequest, ListWorkflowProviderRunsResponse, RequestContext,
    SetWorkflowProviderActivationPausedRequest, SetWorkflowProviderDefinitionPausedRequest,
    SignalOrStartWorkflowProviderRunRequest, SignalWorkflowProviderRunRequest,
    SignalWorkflowRunResponse, StartWorkflowProviderRunRequest, SubjectContext, WorkflowActivation,
    WorkflowDefinition, WorkflowEvent, WorkflowRun, WorkflowRunEvent, WorkflowRunStatus,
    WorkflowScheduleActivation, WorkflowSignal, WorkflowStep, WorkflowStepAppCall,
    workflow_activation, workflow_step,
};
use gestalt::{
    Request, Workflow, WorkflowApplyDefinition, WorkflowCancelRun, WorkflowDeleteDefinition,
    WorkflowDeliverEvent, WorkflowGetDefinition, WorkflowGetRun, WorkflowGetRunEvents,
    WorkflowGetRunOutput, WorkflowListRuns, WorkflowSetActivationPaused,
    WorkflowSetDefinitionPaused, WorkflowSignalOrStartRun, WorkflowSignalRun, WorkflowStartRun,
};
use tokio::net::UnixListener;
use tokio_stream::wrappers::UnixListenerStream;
use tonic::codegen::async_trait;
use tonic::transport::Server;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

const RELAY_HEADER: &str = "x-gestalt-host-service-relay-token";

#[derive(Clone, Debug, Default, PartialEq)]
struct SeenRequest {
    method: String,
    context_subject_id: String,
    relay_token: String,
    provider_name: String,
    definition_id: String,
    activation_id: String,
    run_id: String,
    event_type: String,
}

#[derive(Clone, Default)]
struct TestWorkflowServer {
    seen: Arc<Mutex<Vec<SeenRequest>>>,
    idempotency_keys: Arc<Mutex<Vec<String>>>,
    start_inputs: Arc<Mutex<Vec<serde_json::Value>>>,
    signal_or_start_requests: Arc<Mutex<Vec<SignalOrStartWorkflowProviderRunRequest>>>,
}

#[async_trait]
impl ProtoWorkflowProvider for TestWorkflowServer {
    async fn apply_definition(
        &self,
        request: GrpcRequest<ApplyWorkflowProviderDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowDefinition>, Status> {
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        if !request.idempotency_key.is_empty() {
            self.idempotency_keys
                .lock()
                .expect("idempotency_keys lock")
                .push(request.idempotency_key.clone());
        }
        let spec = request
            .spec
            .ok_or_else(|| Status::invalid_argument("missing spec"))?;
        self.record(SeenRequest {
            method: "apply-definition".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            provider_name: request.provider_name.clone(),
            definition_id: spec.id.clone(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(WorkflowDefinition {
            id: spec.id,
            generation: 7,
            target: spec.target,
            activations: spec.activations,
            paused: spec.paused,
            provider_name: request.provider_name,
            ..Default::default()
        }))
    }

    async fn get_definition(
        &self,
        request: GrpcRequest<GetWorkflowProviderDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowDefinition>, Status> {
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        self.record(SeenRequest {
            method: "get-definition".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            definition_id: request.definition_id.clone(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(WorkflowDefinition {
            id: request.definition_id,
            provider_name: "temporal".to_string(),
            ..Default::default()
        }))
    }

    async fn list_definitions(
        &self,
        request: GrpcRequest<ListWorkflowProviderDefinitionsRequest>,
    ) -> std::result::Result<GrpcResponse<ListWorkflowProviderDefinitionsResponse>, Status> {
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        self.record(SeenRequest {
            method: "list-definitions".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            ..Default::default()
        });
        Ok(GrpcResponse::new(ListWorkflowProviderDefinitionsResponse {
            definitions: vec![WorkflowDefinition {
                id: "definition-42".to_string(),
                provider_name: "temporal".to_string(),
                ..Default::default()
            }],
        }))
    }

    async fn set_definition_paused(
        &self,
        request: GrpcRequest<SetWorkflowProviderDefinitionPausedRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowDefinition>, Status> {
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        self.record(SeenRequest {
            method: "set-definition-paused".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            definition_id: request.definition_id.clone(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(WorkflowDefinition {
            id: request.definition_id,
            paused: request.paused,
            provider_name: "temporal".to_string(),
            ..Default::default()
        }))
    }

    async fn set_activation_paused(
        &self,
        request: GrpcRequest<SetWorkflowProviderActivationPausedRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowDefinition>, Status> {
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        self.record(SeenRequest {
            method: "set-activation-paused".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            definition_id: request.definition_id.clone(),
            activation_id: request.activation_id.clone(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(WorkflowDefinition {
            id: request.definition_id,
            activations: vec![WorkflowActivation {
                id: request.activation_id,
                paused: request.paused,
                ..Default::default()
            }],
            provider_name: "temporal".to_string(),
            ..Default::default()
        }))
    }

    async fn delete_definition(
        &self,
        request: GrpcRequest<DeleteWorkflowProviderDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        self.record(SeenRequest {
            method: "delete-definition".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            definition_id: request.definition_id,
            ..Default::default()
        });
        Ok(GrpcResponse::new(()))
    }

    async fn start_run(
        &self,
        request: GrpcRequest<StartWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowRun>, Status> {
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        if !request.idempotency_key.is_empty() {
            self.idempotency_keys
                .lock()
                .expect("idempotency_keys lock")
                .push(request.idempotency_key.clone());
        }
        if let Some(input) = request.input.as_ref() {
            self.start_inputs
                .lock()
                .expect("start_inputs lock")
                .push(support_protocol::json_from_struct(input));
        }
        self.record(SeenRequest {
            method: "start-run".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            provider_name: request.provider_name.clone(),
            definition_id: request.definition_id.clone(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(WorkflowRun {
            id: "run-1".to_string(),
            status: WorkflowRunStatus::Pending as i32,
            provider_name: request.provider_name,
            definition_id: request.definition_id,
            workflow_key: request.workflow_key,
            input: request.input,
            definition_generation: request.expected_definition_generation,
            target: Some(app_target()),
            ..Default::default()
        }))
    }

    async fn list_runs(
        &self,
        request: GrpcRequest<ListWorkflowProviderRunsRequest>,
    ) -> std::result::Result<GrpcResponse<ListWorkflowProviderRunsResponse>, Status> {
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        self.record(SeenRequest {
            method: "list-runs".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            event_type: request.target_app,
            ..Default::default()
        });
        Ok(GrpcResponse::new(ListWorkflowProviderRunsResponse {
            runs: vec![WorkflowRun {
                id: "run-1".to_string(),
                provider_name: "temporal".to_string(),
                ..Default::default()
            }],
            next_page_token: "next".to_string(),
        }))
    }

    async fn get_run(
        &self,
        request: GrpcRequest<GetWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowRun>, Status> {
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        self.record(SeenRequest {
            method: "get-run".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            run_id: request.run_id.clone(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(WorkflowRun {
            id: request.run_id,
            provider_name: "temporal".to_string(),
            ..Default::default()
        }))
    }

    async fn get_run_events(
        &self,
        request: GrpcRequest<GetWorkflowProviderRunEventsRequest>,
    ) -> std::result::Result<GrpcResponse<gestalt::GetWorkflowProviderRunEventsResponse>, Status>
    {
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        self.record(SeenRequest {
            method: "get-run-events".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            run_id: request.run_id.clone(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(
            gestalt::GetWorkflowProviderRunEventsResponse {
                events: vec![WorkflowRunEvent {
                    id: "event-1".to_string(),
                    run_id: request.run_id,
                    step_id: "review".to_string(),
                    r#type: "step.succeeded".to_string(),
                    ..Default::default()
                }],
            },
        ))
    }

    async fn get_run_output(
        &self,
        request: GrpcRequest<GetWorkflowProviderRunOutputRequest>,
    ) -> std::result::Result<GrpcResponse<gestalt::GetWorkflowProviderRunOutputResponse>, Status>
    {
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        self.record(SeenRequest {
            method: "get-run-output".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            run_id: request.run_id,
            ..Default::default()
        });
        Ok(GrpcResponse::new(
            gestalt::GetWorkflowProviderRunOutputResponse {
                output: Some(helpers::json_to_prost(&serde_json::json!({"ok": true}))),
            },
        ))
    }

    async fn cancel_run(
        &self,
        request: GrpcRequest<CancelWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowRun>, Status> {
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        self.record(SeenRequest {
            method: "cancel-run".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            run_id: request.run_id.clone(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(WorkflowRun {
            id: request.run_id,
            status_message: request.reason,
            provider_name: "temporal".to_string(),
            ..Default::default()
        }))
    }

    async fn signal_run(
        &self,
        request: GrpcRequest<SignalWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<SignalWorkflowRunResponse>, Status> {
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        self.record(SeenRequest {
            method: "signal-run".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            run_id: request.run_id.clone(),
            event_type: request
                .signal
                .as_ref()
                .map(|signal| signal.name.clone())
                .unwrap_or_default(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(SignalWorkflowRunResponse {
            run: Some(WorkflowRun {
                id: request.run_id,
                provider_name: "temporal".to_string(),
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
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        if !request.idempotency_key.is_empty() {
            self.idempotency_keys
                .lock()
                .expect("idempotency_keys lock")
                .push(request.idempotency_key.clone());
        }
        self.signal_or_start_requests
            .lock()
            .expect("signal_or_start_requests lock")
            .push(request.clone());
        self.record(SeenRequest {
            method: "signal-or-start-run".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            provider_name: request.provider_name.clone(),
            definition_id: request.definition_id.clone(),
            event_type: request
                .signal
                .as_ref()
                .map(|signal| signal.name.clone())
                .unwrap_or_default(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(SignalWorkflowRunResponse {
            run: Some(WorkflowRun {
                id: "run-2".to_string(),
                provider_name: request.provider_name,
                definition_id: request.definition_id,
                workflow_key: request.workflow_key.clone(),
                input: request.input,
                definition_generation: request.expected_definition_generation,
                ..Default::default()
            }),
            signal: request.signal,
            started_run: true,
            workflow_key: request.workflow_key,
        }))
    }

    async fn deliver_event(
        &self,
        request: GrpcRequest<DeliverWorkflowProviderEventRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowEvent>, Status> {
        let relay_token = relay_token(&request);
        let request = request.into_inner();
        let event = request
            .event
            .ok_or_else(|| Status::invalid_argument("missing event"))?;
        self.record(SeenRequest {
            method: "deliver-event".to_string(),
            context_subject_id: context_subject_id(&request.context),
            relay_token,
            provider_name: request.provider_name,
            event_type: event.r#type.clone(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(event))
    }
}

impl TestWorkflowServer {
    fn record(&self, request: SeenRequest) {
        self.seen.lock().expect("seen lock").push(request);
    }
}

#[tokio::test]
async fn workflow_connects_over_unix_socket_and_uses_current_rpcs() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-workflow-access.sock");
    let server = TestWorkflowServer::default();
    let serve_task = serve_workflow(&socket, server.clone()).await;
    let _host_socket = helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, &socket);
    let _host_token = helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_TOKEN, "relay-token");

    let request = Request {
        idempotency_key: "default-key".to_string(),
        ..Default::default()
    };
    let mut workflow = gestalt::with_request_context(
        Some(request_context("user:workflow-access")),
        Workflow::connect(&request),
    )
    .await
    .expect("connect workflow");

    let definition = workflow
        .apply_definition(WorkflowApplyDefinition {
            provider_name: "temporal".to_string(),
            spec: Some(gestalt::WorkflowDefinitionSpec {
                id: "definition-42".to_string(),
                target: Some(app_target()),
                activations: vec![WorkflowActivation {
                    id: "hourly".to_string(),
                    trigger: Some(workflow_activation::Trigger::Schedule(
                        WorkflowScheduleActivation {
                            cron: "0 * * * *".to_string(),
                            timezone: "America/New_York".to_string(),
                        },
                    )),
                    ..Default::default()
                }],
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("apply definition");
    assert_eq!(definition.id, "definition-42");
    assert_eq!(definition.generation, 7);

    let listed = workflow.list_definitions().await.expect("list definitions");
    assert_eq!(listed.definitions[0].id, "definition-42");

    let fetched = workflow
        .get_definition(WorkflowGetDefinition {
            definition_id: "definition-42".to_string(),
        })
        .await
        .expect("get definition");
    assert_eq!(fetched.id, "definition-42");

    let paused = workflow
        .set_definition_paused(WorkflowSetDefinitionPaused {
            definition_id: "definition-42".to_string(),
            paused: true,
            ..Default::default()
        })
        .await
        .expect("set definition paused");
    assert!(paused.paused);

    let activation_paused = workflow
        .set_activation_paused(WorkflowSetActivationPaused {
            definition_id: "definition-42".to_string(),
            activation_id: "hourly".to_string(),
            paused: true,
            ..Default::default()
        })
        .await
        .expect("set activation paused");
    assert_eq!(activation_paused.activations[0].id, "hourly");

    let started = workflow
        .start_run(WorkflowStartRun {
            provider_name: "temporal".to_string(),
            workflow_key: "repo:toolshed".to_string(),
            definition_id: "definition-42".to_string(),
            input: Some(serde_json::json!({"github": {"number": 1}})),
            expected_definition_generation: 7,
            ..Default::default()
        })
        .await
        .expect("start run");
    assert_eq!(started.id, "run-1");
    assert_eq!(started.definition_generation, 7);

    let runs = workflow
        .list_runs(WorkflowListRuns {
            target_app: "github".to_string(),
            ..Default::default()
        })
        .await
        .expect("list runs");
    assert_eq!(runs.next_page_token, "next");

    let run = workflow
        .get_run(WorkflowGetRun {
            run_id: "run-1".to_string(),
        })
        .await
        .expect("get run");
    assert_eq!(run.id, "run-1");

    let signal_response = workflow
        .signal_run(WorkflowSignalRun {
            run_id: "run-1".to_string(),
            signal: Some(WorkflowSignal {
                name: "comment".to_string(),
                payload: Some(helpers::struct_from_json(serde_json::json!({
                    "channel": "C123"
                }))),
                ..Default::default()
            }),
        })
        .await
        .expect("signal run");
    assert_eq!(signal_response.signal.expect("signal").name, "comment");

    let signal_or_start = workflow
        .signal_or_start_run(WorkflowSignalOrStartRun {
            provider_name: "temporal".to_string(),
            workflow_key: "thread:C123:123".to_string(),
            definition_id: "definition-42".to_string(),
            input: Some(serde_json::json!({"thread": {"ts": "123.456"}})),
            expected_definition_generation: 7,
            signal: Some(WorkflowSignal {
                name: "message".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("signal or start");
    assert!(signal_or_start.started_run);

    let events = workflow
        .get_run_events(WorkflowGetRunEvents {
            run_id: "run-1".to_string(),
        })
        .await
        .expect("get run events");
    assert_eq!(events.events[0].step_id, "review");

    let output = workflow
        .get_run_output(WorkflowGetRunOutput {
            run_id: "run-1".to_string(),
        })
        .await
        .expect("get run output");
    assert_eq!(
        output.output,
        Some(helpers::json_to_prost(&serde_json::json!({"ok": true})))
    );

    let canceled = workflow
        .cancel_run(WorkflowCancelRun {
            run_id: "run-1".to_string(),
            reason: "done testing".to_string(),
        })
        .await
        .expect("cancel run");
    assert_eq!(canceled.status_message, "done testing");

    let delivered = workflow
        .deliver_event(WorkflowDeliverEvent {
            provider_name: "temporal".to_string(),
            app_name: "github".to_string(),
            event: Some(WorkflowEvent {
                id: "evt_1".to_string(),
                source: "github".to_string(),
                spec_version: "1.0".to_string(),
                r#type: "github.pull_request".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("deliver event");
    assert_eq!(delivered.id, "evt_1");

    workflow
        .delete_definition(WorkflowDeleteDefinition {
            definition_id: "definition-42".to_string(),
        })
        .await
        .expect("delete definition");

    assert_eq!(
        server
            .idempotency_keys
            .lock()
            .expect("idempotency_keys lock")
            .clone(),
        vec![
            "default-key".to_string(),
            "default-key".to_string(),
            "default-key".to_string()
        ]
    );
    assert_eq!(
        server
            .start_inputs
            .lock()
            .expect("start_inputs lock")
            .clone(),
        vec![serde_json::json!({"github": {"number": 1.0}})]
    );

    let seen = server.seen.lock().expect("seen lock").clone();
    assert!(
        seen.iter()
            .all(|request| request.context_subject_id == "user:workflow-access")
    );
    assert!(
        seen.iter()
            .all(|request| request.relay_token == "relay-token")
    );
    assert_eq!(
        seen.iter()
            .map(|request| request.method.as_str())
            .collect::<Vec<_>>(),
        vec![
            "apply-definition",
            "list-definitions",
            "get-definition",
            "set-definition-paused",
            "set-activation-paused",
            "start-run",
            "list-runs",
            "get-run",
            "signal-run",
            "signal-or-start-run",
            "get-run-events",
            "get-run-output",
            "cancel-run",
            "deliver-event",
            "delete-definition",
        ]
    );

    let signal_or_start_request = server
        .signal_or_start_requests
        .lock()
        .expect("signal_or_start_requests lock")
        .first()
        .expect("signal or start request")
        .clone();
    assert_eq!(signal_or_start_request.definition_id, "definition-42");
    assert_eq!(signal_or_start_request.expected_definition_generation, 7);
    assert_eq!(
        signal_or_start_request
            .input
            .as_ref()
            .map(support_protocol::json_from_struct),
        Some(serde_json::json!({"thread": {"ts": "123.456"}}))
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn request_workflow_uses_embedded_context_and_idempotency_key() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-request-workflow.sock");
    let server = TestWorkflowServer::default();
    let serve_task = serve_workflow(&socket, server.clone()).await;
    let _host_socket = helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, &socket);

    let request = Request {
        idempotency_key: "request-key".to_string(),
        ..Default::default()
    };
    let mut workflow = gestalt::with_request_context(
        Some(request_context("user:request-workflow")),
        request.workflow(),
    )
    .await
    .expect("request workflow");
    workflow
        .start_run(WorkflowStartRun {
            provider_name: "temporal".to_string(),
            workflow_key: "wf-key".to_string(),
            definition_id: "definition-42".to_string(),
            ..Default::default()
        })
        .await
        .expect("start run");

    assert_eq!(
        server
            .idempotency_keys
            .lock()
            .expect("idempotency_keys lock")
            .clone(),
        vec!["request-key".to_string()]
    );
    assert_eq!(
        server
            .seen
            .lock()
            .expect("seen lock")
            .first()
            .expect("seen request")
            .context_subject_id,
        "user:request-workflow"
    );

    serve_task.abort();
    let _ = serve_task.await;
}

async fn serve_workflow(socket: &Path, server: TestWorkflowServer) -> tokio::task::JoinHandle<()> {
    let listener = UnixListener::bind(socket).expect("bind unix listener");
    tokio::spawn(async move {
        Server::builder()
            .add_service(WorkflowProviderServer::new(server))
            .serve_with_incoming(UnixListenerStream::new(listener))
            .await
            .expect("serve workflow");
    })
}

fn app_target() -> BoundWorkflowTarget {
    BoundWorkflowTarget {
        steps: vec![WorkflowStep {
            id: "review".to_string(),
            action: Some(workflow_step::Action::App(WorkflowStepAppCall {
                name: "github".to_string(),
                operation: "pullRequests.review".to_string(),
                ..Default::default()
            })),
            ..Default::default()
        }],
    }
}

fn request_context(subject_id: &str) -> RequestContext {
    RequestContext {
        subject: Some(SubjectContext {
            id: subject_id.to_string(),
            ..Default::default()
        }),
        ..Default::default()
    }
}

fn context_subject_id(context: &Option<RequestContext>) -> String {
    context
        .as_ref()
        .and_then(|context| context.subject.as_ref())
        .map(|subject| subject.id.clone())
        .unwrap_or_default()
}

fn relay_token<T>(request: &GrpcRequest<T>) -> String {
    request
        .metadata()
        .get(RELAY_HEADER)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default()
        .to_string()
}
