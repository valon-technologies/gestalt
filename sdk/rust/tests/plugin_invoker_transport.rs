#[allow(dead_code)]
mod helpers;

use std::path::Path;
use std::sync::{Arc, Mutex};

use gestalt::proto::v1::plugin_invoker_server::{
    PluginInvoker as ProtoPluginInvoker, PluginInvokerServer,
};
use gestalt::proto::v1::{OperationResult, PluginInvokeRequest};
use gestalt::{ENV_PLUGIN_INVOKER_SOCKET, InvokeOptions, PluginInvoker, Request};
use prost_types::Struct;
use tokio::net::UnixListener;
use tokio_stream::wrappers::UnixListenerStream;
use tonic::codegen::async_trait;
use tonic::transport::Server;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

#[derive(Clone, Debug, Default, PartialEq)]
struct SeenRequest {
    request_handle: String,
    plugin: String,
    operation: String,
    params: Option<Struct>,
    connection: String,
    instance: String,
}

#[derive(Clone, Default)]
struct TestPluginInvokerServer {
    seen: Arc<Mutex<Vec<SeenRequest>>>,
}

#[async_trait]
impl ProtoPluginInvoker for TestPluginInvokerServer {
    async fn invoke(
        &self,
        request: GrpcRequest<PluginInvokeRequest>,
    ) -> std::result::Result<GrpcResponse<OperationResult>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            request_handle: request.request_handle.clone(),
            plugin: request.plugin.clone(),
            operation: request.operation.clone(),
            params: request.params.clone(),
            connection: request.connection.clone(),
            instance: request.instance.clone(),
        });

        Ok(GrpcResponse::new(OperationResult {
            status: 207,
            body: serde_json::json!({
                "request_handle": request.request_handle,
                "plugin": request.plugin,
                "operation": request.operation,
                "params": request.params.map(struct_to_json).unwrap_or_else(|| serde_json::json!({})),
                "connection": request.connection,
                "instance": request.instance,
            })
            .to_string(),
        }))
    }
}

#[tokio::test]
async fn plugin_invoker_connects_over_unix_socket_and_sends_request_handle() {
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

    let mut invoker = PluginInvoker::connect("handle-123")
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
            "request_handle": "handle-123",
            "plugin": "github",
            "operation": "get_issue",
            "params": { "issue": 42.0, "labels": ["bug"] },
            "connection": "work",
            "instance": "secondary",
        })
    );

    let seen = server.seen.lock().expect("lock seen").clone();
    assert_eq!(seen.len(), 1);
    assert_eq!(
        seen[0],
        SeenRequest {
            request_handle: "handle-123".to_string(),
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
async fn request_invoker_uses_embedded_request_handle() {
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
        request_handle: "handle-embedded".to_string(),
        ..Request::default()
    };
    let mut invoker = request.invoker().await.expect("request invoker");
    let response = invoker
        .invoke("linear", "search_issues", serde_json::json!({}), None)
        .await
        .expect("invoke nested operation");

    assert_eq!(response.status, 207);

    let seen = server.seen.lock().expect("lock seen").clone();
    assert_eq!(seen.len(), 1);
    assert_eq!(seen[0].request_handle, "handle-embedded");
    assert_eq!(seen[0].plugin, "linear");
    assert_eq!(seen[0].operation, "search_issues");
    assert_eq!(seen[0].connection, "");
    assert_eq!(seen[0].instance, "");

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
