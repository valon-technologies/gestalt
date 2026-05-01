#[allow(dead_code)]
mod helpers;

use std::path::Path;
use std::sync::{Arc, Mutex};

use gestalt::proto::v1::plugin_runtime_log_host_server::{
    PluginRuntimeLogHost as ProtoPluginRuntimeLogHost, PluginRuntimeLogHostServer,
};
use gestalt::proto::v1::{
    AppendPluginRuntimeLogsRequest, AppendPluginRuntimeLogsResponse, PluginRuntimeLogStream,
};
use gestalt::{
    ENV_RUNTIME_LOG_HOST_SOCKET, ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN, ENV_RUNTIME_SESSION_ID,
    RuntimeLogHost, RuntimeLogStream,
};
use tokio::net::UnixListener;
use tokio_stream::wrappers::UnixListenerStream;
use tonic::codegen::async_trait;
use tonic::transport::Server;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

#[derive(Clone, Default)]
struct TestRuntimeLogHostServer {
    requests: Arc<Mutex<Vec<AppendPluginRuntimeLogsRequest>>>,
    relay_tokens: Arc<Mutex<Vec<String>>>,
}

#[async_trait]
impl ProtoPluginRuntimeLogHost for TestRuntimeLogHostServer {
    async fn append_logs(
        &self,
        request: GrpcRequest<AppendPluginRuntimeLogsRequest>,
    ) -> std::result::Result<GrpcResponse<AppendPluginRuntimeLogsResponse>, Status> {
        if let Some(token) = request.metadata().get("x-gestalt-host-service-relay-token") {
            self.relay_tokens
                .lock()
                .expect("lock relay tokens")
                .push(token.to_str().expect("relay token ascii").to_string());
        }
        let request = request.into_inner();
        let last_seq = request
            .logs
            .last()
            .map(|entry| entry.source_seq)
            .unwrap_or_default();
        self.requests.lock().expect("lock requests").push(request);
        Ok(GrpcResponse::new(AppendPluginRuntimeLogsResponse {
            last_seq,
        }))
    }
}

#[tokio::test]
async fn runtime_log_host_appends_logs_and_forwards_relay_token() {
    let _env_guard = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("runtime-log-host.sock");
    let server = TestRuntimeLogHostServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task =
        tokio::spawn(async move { serve_runtime_log_host(serve_server, &serve_socket).await });
    helpers::wait_for_socket(&socket).await;

    let _socket_guard = helpers::EnvGuard::set(ENV_RUNTIME_LOG_HOST_SOCKET, socket.as_os_str());
    let _token_guard =
        helpers::EnvGuard::set(ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN, "relay-token-rust");
    let _session_guard = helpers::EnvGuard::set(ENV_RUNTIME_SESSION_ID, "runtime-session-1");

    let mut host = RuntimeLogHost::connect().await.expect("connect");
    let response = host
        .append_current_entry(
            RuntimeLogStream::Runtime,
            "runtime boot\n",
            Some(helpers::timestamp_now()),
            7,
        )
        .await
        .expect("append runtime log");
    host.append_current_stderr("stderr line\n")
        .await
        .expect("append stderr log");

    assert_eq!(response.last_seq, 7);
    let requests = server.requests.lock().expect("lock requests").clone();
    assert_eq!(requests.len(), 2);
    assert_eq!(requests[0].session_id, "runtime-session-1");
    assert_eq!(requests[0].logs[0].message, "runtime boot\n");
    assert_eq!(
        requests[0].logs[0].stream,
        PluginRuntimeLogStream::Runtime as i32
    );
    assert_eq!(requests[0].logs[0].source_seq, 7);
    assert!(requests[0].logs[0].observed_at.is_some());
    assert_eq!(requests[1].logs[0].message, "stderr line\n");
    assert_eq!(requests[1].session_id, "runtime-session-1");
    assert_eq!(
        requests[1].logs[0].stream,
        PluginRuntimeLogStream::Stderr as i32
    );
    assert_eq!(requests[1].logs[0].source_seq, 8);

    let relay_tokens = server
        .relay_tokens
        .lock()
        .expect("lock relay tokens")
        .clone();
    assert_eq!(
        relay_tokens,
        vec![
            "relay-token-rust".to_string(),
            "relay-token-rust".to_string()
        ]
    );

    serve_task.abort();
    let _ = serve_task.await;
}

async fn serve_runtime_log_host(
    server: TestRuntimeLogHostServer,
    socket: &Path,
) -> std::result::Result<(), tonic::transport::Error> {
    let _ = std::fs::remove_file(socket);
    let listener = UnixListener::bind(socket).expect("bind unix listener");

    Server::builder()
        .add_service(PluginRuntimeLogHostServer::new(server))
        .serve_with_incoming(UnixListenerStream::new(listener))
        .await
}
