#[allow(dead_code)]
mod helpers;

mod support_protocol;

use std::path::Path;
use std::sync::{Arc, Mutex};

use gestalt::Workflow;
use gestalt::app::{
    RequestContext as NativeRequestContext, SubjectContext as NativeSubjectContext,
};
use gestalt::proto::v1::workflow_server::{Workflow as ProtoWorkflowProvider, WorkflowServer};
use gestalt::proto::v1::{
    ApplyWorkflowProviderDefinitionRequest, BoundWorkflowTarget, CancelWorkflowProviderRunRequest,
    DeleteWorkflowProviderDefinitionRequest, DeliverWorkflowProviderEventRequest,
    GetWorkflowProviderDefinitionRequest, GetWorkflowProviderRunEventsRequest,
    GetWorkflowProviderRunOutputRequest, GetWorkflowProviderRunRequest,
    ListWorkflowProviderDefinitionsRequest, ListWorkflowProviderDefinitionsResponse,
    ListWorkflowProviderRunsRequest, ListWorkflowProviderRunsResponse, RequestContext,
    SetWorkflowProviderActivationPausedRequest, SetWorkflowProviderDefinitionPausedRequest,
    SignalOrStartWorkflowProviderRunRequest, SignalWorkflowProviderRunRequest,
    SignalWorkflowRunResponse, StartWorkflowProviderRunRequest, WorkflowActivation,
    WorkflowDefinition, WorkflowEvent, WorkflowRun, WorkflowRunEvent, WorkflowRunStatus,
    WorkflowStep, WorkflowStepAppCall, workflow_step,
};
use gestalt::workflow::{
    BoundWorkflowTarget as NativeBoundWorkflowTarget,
    StartWorkflowProviderRunRequest as NativeStartRunRequest,
    WorkflowActivation as NativeWorkflowActivation,
    WorkflowActivationTrigger as NativeWorkflowActivationTrigger,
    WorkflowDefinitionSpec as NativeWorkflowDefinitionSpec, WorkflowEvent as NativeWorkflowEvent,
    WorkflowScheduleActivation as NativeWorkflowScheduleActivation,
    WorkflowSignal as NativeWorkflowSignal, WorkflowStep as NativeWorkflowStep,
    WorkflowStepAction as NativeWorkflowStepAction,
    WorkflowStepAppCall as NativeWorkflowStepAppCall, workflow_run_status,
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
            provider_name: "temporal".to_string(),
            definition_id: spec.id.clone(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(WorkflowDefinition {
            id: spec.id,
            generation: 7,
            target: spec.target,
            activations: spec.activations,
            paused: spec.paused,
            provider_name: "temporal".to_string(),
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
            provider_name: "temporal".to_string(),
            definition_id: request.definition_id.clone(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(WorkflowRun {
            id: "run-1".to_string(),
            status: WorkflowRunStatus::Pending as i32,
            provider_name: "temporal".to_string(),
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
            provider_name: "temporal".to_string(),
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
                provider_name: "temporal".to_string(),
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

    let mut workflow = Workflow::connect()
        .await
        .expect("connect workflow")
        .with_context(request_context_native("user:workflow-access"));

    let definition = workflow
        .apply_definition(
            "default".to_string(),
            "default-key".to_string(),
            Some(NativeWorkflowDefinitionSpec {
                id: "definition-42".to_string(),
                target: Some(app_target_native()),
                activations: vec![NativeWorkflowActivation {
                    id: "hourly".to_string(),
                    trigger: Some(NativeWorkflowActivationTrigger::Schedule(
                        NativeWorkflowScheduleActivation {
                            cron: "0 * * * *".to_string(),
                            timezone: "America/New_York".to_string(),
                        },
                    )),
                    ..Default::default()
                }],
                ..Default::default()
            }),
        )
        .await
        .expect("apply definition");
    assert_eq!(definition.id, "definition-42");
    assert_eq!(definition.generation, 7);

    let listed = workflow
        .list_definitions("default".to_string())
        .await
        .expect("list definitions");
    assert_eq!(listed[0].id, "definition-42");

    let fetched = workflow
        .get_definition("default".to_string(), "definition-42".to_string())
        .await
        .expect("get definition");
    assert_eq!(fetched.id, "definition-42");

    let paused = workflow
        .set_definition_paused("default".to_string(), "definition-42".to_string(), true)
        .await
        .expect("set definition paused");
    assert!(paused.paused);

    let activation_paused = workflow
        .set_activation_paused(
            "default".to_string(),
            "definition-42".to_string(),
            "hourly".to_string(),
            true,
        )
        .await
        .expect("set activation paused");
    assert_eq!(activation_paused.activations[0].id, "hourly");

    let run_input = serde_json::json!({"github": {"number": 1}});
    let started = workflow
        .start_run(
            "default-key".to_string(),
            "repo:toolshed".to_string(),
            "default".to_string(),
            "definition-42".to_string(),
            7,
            Some(run_input.as_object().expect("run input object").clone()),
        )
        .await
        .expect("start run");
    assert_eq!(started.id, "run-1");
    assert_eq!(started.definition_generation, 7);

    let runs = workflow
        .list_runs(
            "default".to_string(),
            0,
            String::new(),
            workflow_run_status::WORKFLOW_RUN_STATUS_UNSPECIFIED,
            "github".to_string(),
        )
        .await
        .expect("list runs");
    assert_eq!(runs.next_page_token, "next");

    let run = workflow
        .get_run("default".to_string(), "run-1".to_string())
        .await
        .expect("get run");
    assert_eq!(run.id, "run-1");

    let signal_payload = serde_json::json!({ "channel": "C123" });
    let signal_response = workflow
        .signal_run(
            "default".to_string(),
            "run-1".to_string(),
            Some(NativeWorkflowSignal {
                name: "comment".to_string(),
                payload: Some(
                    signal_payload
                        .as_object()
                        .expect("signal payload object")
                        .clone(),
                ),
                ..Default::default()
            }),
        )
        .await
        .expect("signal run");
    assert_eq!(signal_response.signal.expect("signal").name, "comment");

    let signal_or_start_input = serde_json::json!({"thread": {"ts": "123.456"}});
    let signal_or_start = workflow
        .signal_or_start_run(
            "thread:C123:123".to_string(),
            "default-key".to_string(),
            "default".to_string(),
            "definition-42".to_string(),
            7,
            Some(NativeWorkflowSignal {
                name: "message".to_string(),
                ..Default::default()
            }),
            Some(
                signal_or_start_input
                    .as_object()
                    .expect("signal or start input object")
                    .clone(),
            ),
        )
        .await
        .expect("signal or start");
    assert!(signal_or_start.started_run);

    let events = workflow
        .get_run_events("default".to_string(), "run-1".to_string())
        .await
        .expect("get run events");
    assert_eq!(events[0].step_id, "review");

    let output = workflow
        .get_run_output("default".to_string(), "run-1".to_string())
        .await
        .expect("get run output");
    assert_eq!(output, Some(serde_json::json!({"ok": true})));

    let canceled = workflow
        .cancel_run(
            "default".to_string(),
            "run-1".to_string(),
            "done testing".to_string(),
        )
        .await
        .expect("cancel run");
    assert_eq!(canceled.status_message, "done testing");

    let delivered = workflow
        .deliver_event(
            "default".to_string(),
            Some(NativeWorkflowEvent {
                id: "evt_1".to_string(),
                source: "github".to_string(),
                spec_version: "1.0".to_string(),
                r#type: "github.pull_request".to_string(),
                ..Default::default()
            }),
        )
        .await
        .expect("deliver event");
    assert_eq!(delivered.id, "evt_1");

    workflow
        .delete_definition("default".to_string(), "definition-42".to_string())
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
async fn start_run_raw_injects_default_context_when_unset() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-request-workflow.sock");
    let server = TestWorkflowServer::default();
    let serve_task = serve_workflow(&socket, server.clone()).await;
    let _host_socket = helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, &socket);

    let mut workflow = Workflow::connect()
        .await
        .expect("connect workflow")
        .with_context(request_context_native("user:request-workflow"));
    workflow
        .start_run_raw(NativeStartRunRequest {
            workflow_key: "wf-key".to_string(),
            definition_id: "definition-42".to_string(),
            idempotency_key: "request-key".to_string(),
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
            .add_service(WorkflowServer::new(server))
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

fn app_target_native() -> NativeBoundWorkflowTarget {
    NativeBoundWorkflowTarget {
        steps: vec![NativeWorkflowStep {
            id: "review".to_string(),
            action: Some(NativeWorkflowStepAction::App(NativeWorkflowStepAppCall {
                name: "github".to_string(),
                operation: "pullRequests.review".to_string(),
                ..Default::default()
            })),
            ..Default::default()
        }],
    }
}

fn request_context_native(subject_id: &str) -> NativeRequestContext {
    NativeRequestContext {
        subject: Some(NativeSubjectContext {
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
