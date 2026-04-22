#[allow(dead_code)]
mod helpers;

use std::path::Path;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use gestalt::proto::v1::plugin_invoker_server::{
    PluginInvoker as ProtoPluginInvoker, PluginInvokerServer,
};
use gestalt::proto::v1::{
    ExchangeInvocationTokenRequest, ExchangeInvocationTokenResponse, OperationResult,
    PluginInvocationGrant, PluginInvokeGraphQlRequest, PluginInvokeRequest,
};
use gestalt::{
    ENV_PLUGIN_INVOKER_SOCKET, ENV_PLUGIN_INVOKER_SOCKET_TOKEN, InvocationGrant, InvokeOptions,
    PluginInvoker, Request,
};
use prost_types::Struct;
use tokio::net::{TcpListener, UnixListener};
use tokio_stream::wrappers::{TcpListenerStream, UnixListenerStream};
use tonic::codegen::async_trait;
use tonic::transport::Server;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

#[derive(Clone, Debug, Default, PartialEq)]
struct SeenRequest {
    invocation_token: String,
    plugin: String,
    operation: String,
    params: Option<Struct>,
    connection: String,
    instance: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
struct SeenGraphQlRequest {
    invocation_token: String,
    plugin: String,
    document: String,
    variables: Option<Struct>,
    connection: String,
    instance: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
struct SeenExchangeRequest {
    parent_invocation_token: String,
    grants: Vec<PluginInvocationGrant>,
    ttl_seconds: i64,
}

#[derive(Clone, Default)]
struct TestPluginInvokerServer {
    seen_invokes: Arc<Mutex<Vec<SeenRequest>>>,
    seen_graphql_invokes: Arc<Mutex<Vec<SeenGraphQlRequest>>>,
    seen_exchanges: Arc<Mutex<Vec<SeenExchangeRequest>>>,
    seen_relay_tokens: Arc<Mutex<Vec<String>>>,
}

#[async_trait]
impl ProtoPluginInvoker for TestPluginInvokerServer {
    async fn exchange_invocation_token(
        &self,
        request: GrpcRequest<ExchangeInvocationTokenRequest>,
    ) -> std::result::Result<GrpcResponse<ExchangeInvocationTokenResponse>, Status> {
        let request = request.into_inner();
        self.seen_exchanges
            .lock()
            .expect("lock seen exchanges")
            .push(SeenExchangeRequest {
                parent_invocation_token: request.parent_invocation_token,
                grants: request.grants,
                ttl_seconds: request.ttl_seconds,
            });

        Ok(GrpcResponse::new(ExchangeInvocationTokenResponse {
            invocation_token: "child-token-123".to_string(),
        }))
    }

    async fn invoke(
        &self,
        request: GrpcRequest<PluginInvokeRequest>,
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
                invocation_token: request.invocation_token.clone(),
                plugin: request.plugin.clone(),
                operation: request.operation.clone(),
                params: request.params.clone(),
                connection: request.connection.clone(),
                instance: request.instance.clone(),
            });

        Ok(GrpcResponse::new(OperationResult {
            status: 207,
            body: serde_json::json!({
                "invocation_token": request.invocation_token,
                "plugin": request.plugin,
                "operation": request.operation,
                "params": request.params.map(struct_to_json).unwrap_or_else(|| serde_json::json!({})),
                "connection": request.connection,
                "instance": request.instance,
            })
            .to_string(),
        }))
    }

    async fn invoke_graph_ql(
        &self,
        request: GrpcRequest<PluginInvokeGraphQlRequest>,
    ) -> std::result::Result<GrpcResponse<OperationResult>, Status> {
        let request = request.into_inner();
        self.seen_graphql_invokes
            .lock()
            .expect("lock seen graphql invokes")
            .push(SeenGraphQlRequest {
                invocation_token: request.invocation_token.clone(),
                plugin: request.plugin.clone(),
                document: request.document.clone(),
                variables: request.variables.clone(),
                connection: request.connection.clone(),
                instance: request.instance.clone(),
            });

        Ok(GrpcResponse::new(OperationResult {
            status: 208,
            body: serde_json::json!({
                "invocation_token": request.invocation_token,
                "plugin": request.plugin,
                "document": request.document,
                "variables": request.variables.map(struct_to_json).unwrap_or_else(|| serde_json::json!({})),
                "connection": request.connection,
                "instance": request.instance,
            })
            .to_string(),
        }))
    }
}

#[tokio::test]
async fn plugin_invoker_connects_over_unix_socket_and_sends_invocation_token() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-plugin-invoker.sock");
    let _socket_guard = helpers::EnvGuard::set(ENV_PLUGIN_INVOKER_SOCKET, socket.as_os_str());

    let server = TestPluginInvokerServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_plugin_invoker(serve_server, &serve_socket)
            .await
            .expect("serve plugin invoker");
    });

    helpers::wait_for_socket(&socket).await;

    let mut invoker = PluginInvoker::connect("token-123")
        .await
        .expect("connect invoker");
    let response = invoker
        .invoke(
            "github",
            "get_issue",
            serde_json::json!({ "issue": 42, "labels": ["bug"] }),
            Some(InvokeOptions {
                connection: "work".to_string(),
                instance: "secondary".to_string(),
            }),
        )
        .await
        .expect("invoke nested operation");

    assert_eq!(response.status, 207);
    assert_eq!(
        serde_json::from_str::<serde_json::Value>(&response.body).expect("parse response"),
        serde_json::json!({
            "invocation_token": "token-123",
            "plugin": "github",
            "operation": "get_issue",
            "params": { "issue": 42.0, "labels": ["bug"] },
            "connection": "work",
            "instance": "secondary",
        })
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
            invocation_token: "token-123".to_string(),
            plugin: "github".to_string(),
            operation: "get_issue".to_string(),
            params: Some(helpers::struct_from_json(
                serde_json::json!({ "issue": 42, "labels": ["bug"] }),
            )),
            connection: "work".to_string(),
            instance: "secondary".to_string(),
        }
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn request_invoker_uses_embedded_invocation_token() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-request-invoker.sock");
    let _socket_guard = helpers::EnvGuard::set(ENV_PLUGIN_INVOKER_SOCKET, socket.as_os_str());

    let server = TestPluginInvokerServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_plugin_invoker(serve_server, &serve_socket)
            .await
            .expect("serve plugin invoker");
    });

    helpers::wait_for_socket(&socket).await;

    let request = Request {
        invocation_token: "token-embedded".to_string(),
        ..Request::default()
    };
    let mut invoker = request.invoker().await.expect("request invoker");
    let response = invoker
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
    assert_eq!(seen[0].invocation_token, "token-embedded");
    assert_eq!(seen[0].plugin, "linear");
    assert_eq!(seen[0].operation, "search_issues");
    assert_eq!(seen[0].connection, "");
    assert_eq!(seen[0].instance, "");

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn plugin_invoker_connects_over_tcp_and_forwards_relay_token() {
    let _env_lock = helpers::env_lock().lock().await;
    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind tcp listener");
    let address = listener.local_addr().expect("local addr");
    let _socket_guard =
        helpers::EnvGuard::set(ENV_PLUGIN_INVOKER_SOCKET, format!("tcp://{address}"));
    let _token_guard = helpers::EnvGuard::set(ENV_PLUGIN_INVOKER_SOCKET_TOKEN, "relay-token-rust");

    let server = TestPluginInvokerServer::default();
    let serve_server = server.clone();
    let serve_task = tokio::spawn(async move {
        Server::builder()
            .add_service(PluginInvokerServer::new(serve_server))
            .serve_with_incoming(TcpListenerStream::new(listener))
            .await
            .expect("serve plugin invoker over tcp");
    });

    let mut invoker = PluginInvoker::connect("tcp-token-123")
        .await
        .expect("connect invoker");
    let response = invoker
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
async fn plugin_invoker_invokes_graphql_surface() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-graphql-invoker.sock");
    let _socket_guard = helpers::EnvGuard::set(ENV_PLUGIN_INVOKER_SOCKET, socket.as_os_str());

    let server = TestPluginInvokerServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_plugin_invoker(serve_server, &serve_socket)
            .await
            .expect("serve plugin invoker");
    });

    helpers::wait_for_socket(&socket).await;

    let mut invoker = PluginInvoker::connect("graphql-token-123")
        .await
        .expect("connect invoker");
    let response = invoker
        .invoke_graphql(
            "linear",
            "  query Viewer($team: String!) { viewer(team: $team) { id } }  ",
            Some(serde_json::json!({ "team": "eng" })),
            Some(InvokeOptions {
                connection: "workspace".to_string(),
                instance: "secondary".to_string(),
            }),
        )
        .await
        .expect("invoke graphql surface");

    assert_eq!(response.status, 208);
    assert_eq!(
        serde_json::from_str::<serde_json::Value>(&response.body).expect("parse response"),
        serde_json::json!({
            "invocation_token": "graphql-token-123",
            "plugin": "linear",
            "document": "query Viewer($team: String!) { viewer(team: $team) { id } }",
            "variables": { "team": "eng" },
            "connection": "workspace",
            "instance": "secondary",
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
            invocation_token: "graphql-token-123".to_string(),
            plugin: "linear".to_string(),
            document: "query Viewer($team: String!) { viewer(team: $team) { id } }".to_string(),
            variables: Some(helpers::struct_from_json(
                serde_json::json!({ "team": "eng" })
            )),
            connection: "workspace".to_string(),
            instance: "secondary".to_string(),
        }
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn plugin_invoker_exchanges_invocation_tokens_with_grants_and_ttl() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-exchange-invoker.sock");
    let _socket_guard = helpers::EnvGuard::set(ENV_PLUGIN_INVOKER_SOCKET, socket.as_os_str());

    let server = TestPluginInvokerServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_plugin_invoker(serve_server, &serve_socket)
            .await
            .expect("serve plugin invoker");
    });

    helpers::wait_for_socket(&socket).await;

    let mut invoker = PluginInvoker::connect("parent-token-123")
        .await
        .expect("connect invoker");
    let child_token = invoker
        .exchange_invocation_token(
            &[
                InvocationGrant {
                    plugin: " github ".to_string(),
                    operations: vec![
                        " get_issue ".to_string(),
                        String::new(),
                        "list_labels".to_string(),
                    ],
                    surfaces: Vec::new(),
                    all_operations: false,
                },
                InvocationGrant {
                    plugin: " linear ".to_string(),
                    operations: Vec::new(),
                    surfaces: vec![" GraphQL ".to_string(), String::new(), "MCP".to_string()],
                    all_operations: false,
                },
                InvocationGrant {
                    plugin: "google_sheets".to_string(),
                    operations: Vec::new(),
                    surfaces: Vec::new(),
                    all_operations: true,
                },
                InvocationGrant {
                    plugin: "   ".to_string(),
                    operations: vec!["ignored".to_string()],
                    surfaces: vec!["rest".to_string()],
                    all_operations: true,
                },
            ],
            Some(Duration::from_millis(500)),
        )
        .await
        .expect("exchange invocation token");

    assert_eq!(child_token, "child-token-123");

    let seen = server
        .seen_exchanges
        .lock()
        .expect("lock seen exchanges")
        .clone();
    assert_eq!(seen.len(), 1);
    assert_eq!(
        seen[0],
        SeenExchangeRequest {
            parent_invocation_token: "parent-token-123".to_string(),
            grants: vec![
                PluginInvocationGrant {
                    plugin: "github".to_string(),
                    operations: vec!["get_issue".to_string(), "list_labels".to_string()],
                    surfaces: Vec::new(),
                    all_operations: false,
                },
                PluginInvocationGrant {
                    plugin: "linear".to_string(),
                    operations: Vec::new(),
                    surfaces: vec!["graphql".to_string(), "mcp".to_string()],
                    all_operations: false,
                },
                PluginInvocationGrant {
                    plugin: "google_sheets".to_string(),
                    operations: Vec::new(),
                    surfaces: Vec::new(),
                    all_operations: true,
                },
            ],
            ttl_seconds: 1,
        }
    );

    serve_task.abort();
    let _ = serve_task.await;
}

async fn serve_plugin_invoker(
    server: TestPluginInvokerServer,
    socket: &Path,
) -> std::result::Result<(), tonic::transport::Error> {
    let _ = std::fs::remove_file(socket);
    let listener = UnixListener::bind(socket).expect("bind unix listener");

    Server::builder()
        .add_service(PluginInvokerServer::new(server))
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
