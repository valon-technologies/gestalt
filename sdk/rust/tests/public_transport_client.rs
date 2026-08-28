//! Tests for the public Gestalt transport client.

use std::sync::{Arc, RwLock};

use base64::Engine;
use gestalt::public::auth::{Auth, BearerAuth, NoAuth};
use gestalt::public::client::create_rest_gestalt_client;
use gestalt::public::generated::app::AppInvokeRequest;
use gestalt::public::generated::app_client::AppClient;
use gestalt::public::rest_transport::RestTransport;
use gestalt::rpc_support::gestalt_error_code;
use serde_json::json;
use wiremock::matchers::{header, method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

#[tokio::test]
async fn create_client_requires_address() {
    let result = create_rest_gestalt_client("", NoAuth).await;
    let Err(err) = result else {
        panic!("expected empty address to fail");
    };
    assert_eq!(err.code, gestalt_error_code::INVALID_ARGUMENT);
}

#[tokio::test]
async fn bearer_auth_rotates_under_concurrent_calls() {
    let token = Arc::new(RwLock::new("token-a".to_string()));
    let auth = BearerAuth::shared(Arc::clone(&token));
    assert_eq!(
        auth.authorization_header().as_deref(),
        Some("Bearer token-a")
    );

    {
        let mut guard = token.write().expect("lock");
        *guard = "token-b".to_string();
    }
    assert_eq!(
        auth.authorization_header().as_deref(),
        Some("Bearer token-b")
    );
}

#[tokio::test]
async fn rest_transport_invoke_success() {
    let server = MockServer::start().await;
    let body = json!({
        "status": 200,
        "body": base64::engine::general_purpose::STANDARD.encode(
            br#"{"status":"success","data":{"ok":true}}"#
        ),
        "headers": {}
    });
    Mock::given(method("POST"))
        .and(path("/api/v2/app/example/operations/sync"))
        .and(header("X-Gestalt-Client", "cli"))
        .respond_with(ResponseTemplate::new(200).set_body_json(body))
        .mount(&server)
        .await;

    let client = AppClient::new(
        RestTransport::new(server.uri(), Arc::new(BearerAuth::new("test-token")))
            .with_gestalt_client_kind("cli"),
    );
    let result = client
        .invoke(AppInvokeRequest {
            app: "example".to_string(),
            operation: "sync".to_string(),
            params: Some(json!({"ok": true}).as_object().cloned().unwrap()),
            ..Default::default()
        })
        .await
        .expect("invoke");
    assert_eq!(result, json!({"ok": true}));
}

#[tokio::test]
async fn rest_transport_platform_error() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .and(path("/api/v2/app/example/operations/sync"))
        .respond_with(ResponseTemplate::new(401).set_body_json(json!({
            "code": "Unauthenticated",
            "error": "Not authenticated"
        })))
        .mount(&server)
        .await;

    let client = AppClient::new(RestTransport::new(server.uri(), Arc::new(NoAuth)));
    let err = client
        .invoke(AppInvokeRequest {
            app: "example".to_string(),
            operation: "sync".to_string(),
            ..Default::default()
        })
        .await
        .expect_err("platform error");
    let gestalt::public::generated::invoke_support::InvokeError::Transport(gerr) = err else {
        panic!("expected transport error");
    };
    assert_eq!(gerr.code, 16);
    assert_eq!(gerr.message, "Not authenticated");
}

#[tokio::test]
async fn create_rest_client_factory() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .and(path("/api/v2/app/example/operations/sync"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "status": 200,
            "body": base64::engine::general_purpose::STANDARD.encode(
                br#"{"status":"success","data":{"ok":true}}"#
            ),
            "headers": {}
        })))
        .mount(&server)
        .await;

    let client = create_rest_gestalt_client(server.uri(), BearerAuth::new("token"))
        .await
        .expect("client");
    let result = client
        .app
        .invoke(AppInvokeRequest {
            app: "example".to_string(),
            operation: "sync".to_string(),
            ..Default::default()
        })
        .await
        .expect("invoke");
    assert_eq!(result, json!({"ok": true}));
}

#[path = "../src/generated.rs"]
mod generated;

#[tokio::test]
async fn grpc_transport_invoke_success() {
    use generated::v1::app_server::{App, AppServer};
    use generated::v1::{AppInvokeGraphQlRequest, AppInvokeRequest, OperationResult};
    use gestalt::public::grpc_transport::GrpcTransport;
    use tokio_stream::wrappers::TcpListenerStream;
    use tonic::transport::Server;
    use tonic::{Request, Response, Status};

    struct StubApp;

    #[tonic::async_trait]
    impl App for StubApp {
        type InvokeStreamStream = std::pin::Pin<
            Box<
                dyn tonic::codegen::tokio_stream::Stream<
                        Item = std::result::Result<generated::v1::InvokeFrame, Status>,
                    > + Send
                    + 'static,
            >,
        >;

        async fn invoke(
            &self,
            _request: Request<AppInvokeRequest>,
        ) -> Result<Response<OperationResult>, Status> {
            Ok(Response::new(OperationResult {
                status: 200,
                body: br#"{"status":"success","data":{"ok":true}}"#.to_vec(),
                ..Default::default()
            }))
        }

        async fn invoke_stream(
            &self,
            _request: Request<AppInvokeRequest>,
        ) -> Result<Response<Self::InvokeStreamStream>, Status> {
            Err(Status::unimplemented("unused"))
        }

        async fn invoke_graph_ql(
            &self,
            _request: Request<AppInvokeGraphQlRequest>,
        ) -> Result<Response<OperationResult>, Status> {
            Err(Status::unimplemented("unused"))
        }
    }

    let listener = tokio::net::TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind");
    let addr = listener.local_addr().expect("addr");
    tokio::spawn(async move {
        Server::builder()
            .add_service(AppServer::new(StubApp))
            .serve_with_incoming(TcpListenerStream::new(listener))
            .await
            .expect("serve");
    });

    let channel = tonic::transport::Endpoint::from_shared(format!("http://{addr}"))
        .expect("endpoint")
        .connect()
        .await
        .expect("connect");
    let client = AppClient::new(GrpcTransport::new(channel, Arc::new(NoAuth)));
    let result = client
        .invoke(gestalt::public::generated::app::AppInvokeRequest {
            app: "example".to_string(),
            operation: "sync".to_string(),
            ..Default::default()
        })
        .await
        .expect("invoke");
    assert_eq!(result, json!({"ok": true}));
}

#[tokio::test]
async fn rest_transport_rejects_grpc_only_method() {
    use gestalt::public::generated::metadata::Empty;
    use gestalt::public::generated::metadata::METHOD_INDEXED_D_B_GET;
    use gestalt::public::generated::unary_transport::UnaryTransport;

    let transport = RestTransport::new("http://example.com".to_string(), Arc::new(NoAuth));
    let mut response = Empty::default();
    let err = transport
        .unary(&METHOD_INDEXED_D_B_GET, &Empty::default(), &mut response)
        .await
        .expect_err("grpc-only method");
    assert_eq!(err.code, gestalt_error_code::INVALID_ARGUMENT);
}

#[tokio::test]
async fn agent_get_session_uses_rest_path() {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/v2/agent/sessions/sess-1"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({"session": {}})))
        .mount(&server)
        .await;

    let client = create_rest_gestalt_client(server.uri(), NoAuth)
        .await
        .expect("client");
    client
        .agent
        .get_session(
            gestalt::public::generated::agent::GetAgentProviderSessionRequest {
                session_id: "sess-1".to_string(),
                ..Default::default()
            },
        )
        .await
        .expect("get session");
}

#[tokio::test]
async fn authorization_get_active_model_ref_empty_input() {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/v2/authorization/models/active"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({})))
        .mount(&server)
        .await;

    let client = create_rest_gestalt_client(server.uri(), NoAuth)
        .await
        .expect("client");
    client
        .authorization
        .get_active_model_ref()
        .await
        .expect("get active model ref");
}

#[tokio::test]
async fn rest_transport_times_out() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .and(path("/api/v2/app/example/operations/sync"))
        .respond_with(
            ResponseTemplate::new(200)
                .set_delay(std::time::Duration::from_millis(200))
                .set_body_json(json!({"status": 200, "body": "", "headers": {}})),
        )
        .mount(&server)
        .await;

    let transport = RestTransport::new(server.uri(), Arc::new(NoAuth))
        .with_timeout(std::time::Duration::from_millis(50));
    let client = AppClient::new(transport);
    let err = client
        .invoke(AppInvokeRequest {
            app: "example".to_string(),
            operation: "sync".to_string(),
            ..Default::default()
        })
        .await
        .expect_err("timeout");
    let gestalt::public::generated::invoke_support::InvokeError::Transport(gerr) = err else {
        panic!("expected transport error");
    };
    assert_eq!(gerr.code, gestalt_error_code::DEADLINE_EXCEEDED);
}
