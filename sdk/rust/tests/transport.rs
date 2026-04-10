#[allow(dead_code)]
mod helpers;

use std::sync::{Arc, Mutex};

use gestalt::proto::v1::integration_provider_client::IntegrationProviderClient;
use gestalt::proto::v1::{
    ExecuteRequest, GetSessionCatalogRequest, PostConnectRequest, StartProviderRequest,
};
use gestalt::{Catalog, CatalogOperation, Operation, Provider, Request, Response, Router, ok};
use hyper_util::rt::tokio::TokioIo;
use tokio::net::UnixStream;
use tonic::Code;
use tonic::codegen::async_trait;
use tonic::transport::Endpoint;
use tower::service_fn;

#[derive(Default)]
struct TestProvider {
    greeting: Mutex<String>,
}

#[async_trait]
impl Provider for TestProvider {
    async fn configure(
        &self,
        _name: &str,
        config: serde_json::Map<String, serde_json::Value>,
    ) -> gestalt::Result<()> {
        let greeting = config
            .get("greeting")
            .and_then(serde_json::Value::as_str)
            .unwrap_or("Hello")
            .to_string();
        *self.greeting.lock().expect("lock greeting") = greeting;
        Ok(())
    }

    fn supports_session_catalog(&self) -> bool {
        true
    }

    async fn catalog_for_request(&self, request: &Request) -> gestalt::Result<Option<Catalog>> {
        Ok(Some(Catalog {
            name: "session-example".to_string(),
            display_name: request
                .connection_param("tenant")
                .unwrap_or_default()
                .to_string(),
            description: String::new(),
            icon_svg: String::new(),
            operations: vec![CatalogOperation {
                id: "private_echo".to_string(),
                method: "POST".to_string(),
                title: String::new(),
                description: String::new(),
                input_schema: String::new(),
                output_schema: String::new(),
                annotations: None,
                parameters: Vec::new(),
                required_scopes: Vec::new(),
                tags: Vec::new(),
                read_only: false,
                visible: None,
                transport: String::new(),
            }],
        }))
    }
}

#[derive(serde::Deserialize, schemars::JsonSchema)]
struct Input {
    name: String,
}

#[derive(serde::Serialize, schemars::JsonSchema)]
struct Output {
    message: String,
}

#[tokio::test]
async fn serves_provider_requests_over_unix_socket() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-sdk.sock");
    let _socket_guard = helpers::EnvGuard::set(gestalt::ENV_PROVIDER_SOCKET, socket.as_os_str());

    let router = Router::new()
        .register(
            Operation::<Input, Output>::new("greet"),
            |provider: Arc<TestProvider>, input: Input, _request: Request| async move {
                let greeting = provider.greeting.lock().expect("lock greeting").clone();
                Ok::<Response<Output>, std::convert::Infallible>(ok(Output {
                    message: format!("{greeting}, {}!", input.name),
                }))
            },
        )
        .expect("register operation");

    let provider = Arc::new(TestProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_router = router.clone();
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_provider(serve_provider, serve_router)
            .await
            .expect("serve provider");
    });

    helpers::wait_for_socket(&socket).await;

    let channel = Endpoint::try_from("http://[::]:50051")
        .expect("endpoint")
        .connect_with_connector(service_fn({
            let socket = socket.clone();
            move |_| {
                let socket = socket.clone();
                async move { UnixStream::connect(socket).await.map(TokioIo::new) }
            }
        }))
        .await
        .expect("connect channel");
    let mut client = IntegrationProviderClient::new(channel);

    let metadata = client
        .get_metadata(())
        .await
        .expect("get metadata")
        .into_inner();
    assert!(metadata.supports_session_catalog);
    assert_eq!(
        metadata.min_protocol_version,
        gestalt::CURRENT_PROTOCOL_VERSION
    );
    assert_eq!(
        metadata.max_protocol_version,
        gestalt::CURRENT_PROTOCOL_VERSION
    );

    let started = client
        .start_provider(StartProviderRequest {
            name: "example".to_string(),
            config: Some(helpers::struct_from_json(
                serde_json::json!({ "greeting": "Hi" }),
            )),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION,
        })
        .await
        .expect("start provider")
        .into_inner();
    assert_eq!(started.protocol_version, gestalt::CURRENT_PROTOCOL_VERSION);

    let response = client
        .execute(ExecuteRequest {
            operation: "greet".to_string(),
            params: Some(helpers::struct_from_json(
                serde_json::json!({ "name": "Rust" }),
            )),
            token: String::new(),
            connection_params: Default::default(),
            invocation_id: String::new(),
        })
        .await
        .expect("execute")
        .into_inner();

    assert_eq!(response.status, 200);
    assert_eq!(response.body, r#"{"message":"Hi, Rust!"}"#);

    let session_catalog = client
        .get_session_catalog(GetSessionCatalogRequest {
            token: "tok".to_string(),
            connection_params: [("tenant".to_string(), "acme".to_string())]
                .into_iter()
                .collect(),
            invocation_id: String::new(),
        })
        .await
        .expect("session catalog")
        .into_inner();
    let catalog = session_catalog.catalog.expect("session catalog");
    assert_eq!(catalog.name, "session-example");
    assert_eq!(catalog.display_name, "acme");

    let err = client
        .post_connect(PostConnectRequest::default())
        .await
        .expect_err("post connect should be unimplemented");
    assert_eq!(err.code(), Code::Unimplemented);

    serve_task.abort();
    let _ = serve_task.await;
}
