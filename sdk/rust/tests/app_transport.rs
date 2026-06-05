#[path = "../src/generated.rs"]
mod generated;

#[allow(dead_code)]
mod helpers;

use std::collections::BTreeMap;
use std::path::Path;
use std::sync::{Arc, Mutex};

use generated::v1::app_server::{App as ProtoApp, AppServer};
use generated::v1::{
    AppInvokeGraphQlRequest, AppInvokeRequest, OperationResult,
    RequestContext as ProviderRequestContext,
};
use gestalt::proto::v1::{RequestContext, SubjectContext};
use gestalt::{App, InvokeGraphQLOptions, InvokeOptions, Request};
use prost_types::Struct;
use serde::Serialize;
use tokio::net::{TcpListener, UnixListener};
use tokio_stream::wrappers::{TcpListenerStream, UnixListenerStream};
use tonic::codegen::async_trait;
use tonic::transport::Server;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

#[derive(Serialize)]
struct IssueParams {
    issue: i32,
    labels: Vec<String>,
}

#[derive(Serialize)]
struct BadNumberParams {
    score: f64,
}

#[derive(Clone, Debug, Default, PartialEq)]
struct SeenRequest {
    context_subject_id: String,
    plugin: String,
    operation: String,
    params: Option<Struct>,
    connection: String,
    instance: String,
    idempotency_key: String,
    credential_mode: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
struct SeenGraphQlRequest {
    context_subject_id: String,
    plugin: String,
    document: String,
    variables: Option<Struct>,
    connection: String,
    instance: String,
    idempotency_key: String,
}

#[derive(Clone, Default)]
struct TestAppServer {
    seen_invokes: Arc<Mutex<Vec<SeenRequest>>>,
    seen_graphql_invokes: Arc<Mutex<Vec<SeenGraphQlRequest>>>,
    seen_relay_tokens: Arc<Mutex<Vec<String>>>,
}

#[async_trait]
impl ProtoApp for TestAppServer {
    async fn invoke(
        &self,
        request: GrpcRequest<AppInvokeRequest>,
    ) -> std::result::Result<GrpcResponse<OperationResult>, Status> {
        let relay_tokens = request
            .metadata()
            .get_all("x-gestalt-host-service-relay-token")
            .iter()
            .filter_map(|value| value.to_str().ok())
            .map(ToOwned::to_owned)
            .collect::<Vec<_>>();
        let request = request.into_inner();
        self.seen_relay_tokens
            .lock()
            .expect("lock seen relay tokens")
            .extend(relay_tokens);
        self.seen_invokes
            .lock()
            .expect("lock seen invokes")
            .push(SeenRequest {
                context_subject_id: context_subject_id(&request.context),
                plugin: request.app.clone(),
                operation: request.operation.clone(),
                params: request.params.clone(),
                connection: request.connection.clone(),
                instance: request.instance.clone(),
                idempotency_key: request.idempotency_key.clone(),
                credential_mode: request.credential_mode.clone(),
            });

        Ok(GrpcResponse::new(OperationResult {
            status: 207,
            headers: BTreeMap::from([(
                "Location".to_string(),
                generated::v1::StringList {
                    values: vec!["https://example.test/created".to_string()],
                },
            )]),
            body: serde_json::json!({
                "context_subject_id": context_subject_id(&request.context),
                "app": request.app,
                "operation": request.operation,
                "params": request.params.map(struct_to_json).unwrap_or_else(|| serde_json::json!({})),
				"connection": request.connection,
				"instance": request.instance,
				"idempotency_key": request.idempotency_key,
				"credential_mode": request.credential_mode,
			})
			.to_string(),
		}))
    }

    async fn invoke_graph_ql(
        &self,
        request: GrpcRequest<AppInvokeGraphQlRequest>,
    ) -> std::result::Result<GrpcResponse<OperationResult>, Status> {
        let request = request.into_inner();
        self.seen_graphql_invokes
            .lock()
            .expect("lock seen graphql invokes")
            .push(SeenGraphQlRequest {
                context_subject_id: context_subject_id(&request.context),
                plugin: request.app.clone(),
                document: request.document.clone(),
                variables: request.variables.clone(),
                connection: request.connection.clone(),
                instance: request.instance.clone(),
                idempotency_key: request.idempotency_key.clone(),
            });

        Ok(GrpcResponse::new(OperationResult {
            status: 208,
            headers: BTreeMap::new(),
            body: serde_json::json!({
                "context_subject_id": context_subject_id(&request.context),
                "app": request.app,
                "document": request.document,
                "variables": request.variables.map(struct_to_json).unwrap_or_else(|| serde_json::json!({})),
                "connection": request.connection,
                "instance": request.instance,
                "idempotency_key": request.idempotency_key,
            })
            .to_string(),
        }))
    }
}

#[tokio::test]
async fn app_connects_over_unix_socket_and_sends_request_context() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-plugin-app.sock");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, socket.as_os_str());

    let server = TestAppServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_app(serve_server, &serve_socket)
            .await
            .expect("serve app");
    });

    helpers::wait_for_socket(&socket).await;

    let request = Request {
        context: Some(request_context("user:app-access")),
        ..Default::default()
    };
    let mut app = App::connect(&request).await.expect("connect app");
    let response = app
        .invoke(
            "github",
            "get_issue",
            IssueParams {
                issue: 42,
                labels: vec!["bug".to_string()],
            },
            Some(InvokeOptions {
                connection: "work".to_string(),
                instance: "secondary".to_string(),
                idempotency_key: " issue-42-create ".to_string(),
                credential_mode: "none".to_string(),
            }),
        )
        .await
        .expect("invoke nested operation");

    assert_eq!(response.status, 207);
    assert_eq!(
        response.headers.get("Location"),
        Some(&vec!["https://example.test/created".to_string()])
    );
    assert_eq!(
        serde_json::from_str::<serde_json::Value>(&response.body).expect("parse response"),
        serde_json::json!({
            "context_subject_id": "user:app-access",
            "app": "github",
            "operation": "get_issue",
            "params": { "issue": 42.0, "labels": ["bug"] },
            "connection": "work",
            "instance": "secondary",
            "idempotency_key": "issue-42-create",
            "credential_mode": "none",
        })
    );
    let err = app
        .invoke("github", "bad", "not-object", None)
        .await
        .expect_err("non-object params should fail");
    assert!(
        err.to_string().contains("must serialize to a JSON object"),
        "unexpected error: {err}"
    );
    let err = app
        .invoke("github", "bad", BadNumberParams { score: f64::NAN }, None)
        .await
        .expect_err("non-finite params should fail");
    assert!(
        err.to_string().contains("finite"),
        "unexpected error: {err}"
    );
    let err = app
        .invoke(
            "github",
            "bad",
            BTreeMap::from([(1_i32, "not-a-string-key".to_string())]),
            None,
        )
        .await
        .expect_err("non-string map keys should fail");
    assert!(
        err.to_string().contains("map keys must be strings"),
        "unexpected error: {err}"
    );

    let seen = server
        .seen_invokes
        .lock()
        .expect("lock seen invokes")
        .clone();
    assert_eq!(seen.len(), 1);
    assert_eq!(
        seen[0],
        SeenRequest {
            context_subject_id: "user:app-access".to_string(),
            plugin: "github".to_string(),
            operation: "get_issue".to_string(),
            params: Some(helpers::struct_from_json(
                serde_json::json!({ "issue": 42, "labels": ["bug"] }),
            )),
            connection: "work".to_string(),
            instance: "secondary".to_string(),
            idempotency_key: "issue-42-create".to_string(),
            credential_mode: "none".to_string(),
        }
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn request_app_uses_embedded_context() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-request-app.sock");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, socket.as_os_str());

    let server = TestAppServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_app(serve_server, &serve_socket)
            .await
            .expect("serve app");
    });

    helpers::wait_for_socket(&socket).await;

    let request = Request {
        context: Some(request_context("user:request-app")),
        ..Default::default()
    };
    let mut app = request.app().await.expect("request app");
    let response = app
        .invoke("linear", "search_issues", serde_json::json!({}), None)
        .await
        .expect("invoke nested operation");

    assert_eq!(response.status, 207);

    let seen = server
        .seen_invokes
        .lock()
        .expect("lock seen invokes")
        .clone();
    assert_eq!(seen.len(), 1);
    assert_eq!(seen[0].context_subject_id, "user:request-app");
    assert_eq!(seen[0].plugin, "linear");
    assert_eq!(seen[0].operation, "search_issues");
    assert_eq!(seen[0].connection, "");
    assert_eq!(seen[0].instance, "");

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn app_connects_over_tcp_and_forwards_relay_token() {
    let _env_lock = helpers::env_lock().lock().await;
    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind tcp listener");
    let address = listener.local_addr().expect("local addr");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, format!("tcp://{address}"));
    let _token_guard = helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_TOKEN, "relay-token-rust");

    let server = TestAppServer::default();
    let serve_server = server.clone();
    let serve_task = tokio::spawn(async move {
        Server::builder()
            .add_service(AppServer::new(serve_server))
            .serve_with_incoming(TcpListenerStream::new(listener))
            .await
            .expect("serve app over tcp");
    });

    let request = Request {
        context: Some(request_context("user:app-access")),
        ..Default::default()
    };
    let mut app = App::connect(&request).await.expect("connect app");
    let response = app
        .invoke("github", "plain_text", serde_json::json!({}), None)
        .await
        .expect("invoke nested operation");

    assert_eq!(response.status, 207);
    let seen_tokens = server
        .seen_relay_tokens
        .lock()
        .expect("lock seen relay tokens")
        .clone();
    assert_eq!(seen_tokens, vec!["relay-token-rust".to_string()]);

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn app_invokes_graphql_surface() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-graphql-app.sock");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, socket.as_os_str());

    let server = TestAppServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_app(serve_server, &serve_socket)
            .await
            .expect("serve app");
    });

    helpers::wait_for_socket(&socket).await;

    let request = Request {
        context: Some(request_context("user:graphql-app-access")),
        ..Default::default()
    };
    let mut app = App::connect(&request)
        .await
        .expect("connect app");
    let response = app
        .invoke_graphql(
            "linear",
            "  query Viewer($team: String!) { viewer(team: $team) { id } }  ",
            Some(serde_json::json!({ "team": "eng" })),
            Some(InvokeGraphQLOptions {
                connection: "workspace".to_string(),
                instance: "secondary".to_string(),
                idempotency_key: " graphql-call-42 ".to_string(),
            }),
        )
        .await
        .expect("invoke graphql surface");

    assert_eq!(response.status, 208);
    assert_eq!(
        serde_json::from_str::<serde_json::Value>(&response.body).expect("parse response"),
        serde_json::json!({
            "context_subject_id": "user:graphql-app-access",
            "app": "linear",
            "document": "query Viewer($team: String!) { viewer(team: $team) { id } }",
            "variables": { "team": "eng" },
            "connection": "workspace",
            "instance": "secondary",
            "idempotency_key": "graphql-call-42",
        })
    );

    let seen = server
        .seen_graphql_invokes
        .lock()
        .expect("lock seen graphql invokes")
        .clone();
    assert_eq!(seen.len(), 1);
    assert_eq!(
        seen[0],
        SeenGraphQlRequest {
            context_subject_id: "user:graphql-app-access".to_string(),
            plugin: "linear".to_string(),
            document: "query Viewer($team: String!) { viewer(team: $team) { id } }".to_string(),
            variables: Some(helpers::struct_from_json(
                serde_json::json!({ "team": "eng" })
            )),
            connection: "workspace".to_string(),
            instance: "secondary".to_string(),
            idempotency_key: "graphql-call-42".to_string(),
        }
    );

    serve_task.abort();
    let _ = serve_task.await;
}

async fn serve_app(
    server: TestAppServer,
    socket: &Path,
) -> std::result::Result<(), tonic::transport::Error> {
    let _ = std::fs::remove_file(socket);
    let listener = UnixListener::bind(socket).expect("bind unix listener");

    Server::builder()
        .add_service(AppServer::new(server))
        .serve_with_incoming(UnixListenerStream::new(listener))
        .await
}

fn struct_to_json(value: Struct) -> serde_json::Value {
    serde_json::Value::Object(
        value
            .fields
            .into_iter()
            .map(|(key, value)| (key, prost_to_json(value)))
            .collect(),
    )
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

fn context_subject_id(context: &Option<ProviderRequestContext>) -> String {
    context
        .as_ref()
        .and_then(|context| context.subject.as_ref())
        .map(|subject| subject.id.clone())
        .unwrap_or_default()
}

fn prost_to_json(value: prost_types::Value) -> serde_json::Value {
    use prost_types::value::Kind;

    match value.kind {
        Some(Kind::NullValue(_)) => serde_json::Value::Null,
        Some(Kind::BoolValue(boolean)) => serde_json::Value::Bool(boolean),
        Some(Kind::NumberValue(number)) => serde_json::json!(number),
        Some(Kind::StringValue(string)) => serde_json::Value::String(string),
        Some(Kind::StructValue(object)) => struct_to_json(object),
        Some(Kind::ListValue(list)) => {
            serde_json::Value::Array(list.values.into_iter().map(prost_to_json).collect())
        }
        None => serde_json::Value::Null,
    }
}
