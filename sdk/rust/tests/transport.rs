use std::ffi::OsString;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use async_trait::async_trait;
use gestalt_plugin_sdk::proto::v1::provider_plugin_client::ProviderPluginClient;
use gestalt_plugin_sdk::proto::v1::{
    ExecuteRequest, GetSessionCatalogRequest, PostConnectRequest, StartProviderRequest,
};
use gestalt_plugin_sdk::{
    Catalog, CatalogOperation, Operation, Provider, Request, Response, Router, ok,
};
use prost_types::{Struct, Value};
use tokio::net::UnixStream;
use tonic::Code;
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
    ) -> gestalt_plugin_sdk::Result<()> {
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

    async fn catalog_for_request(
        &self,
        request: &Request,
    ) -> gestalt_plugin_sdk::Result<Option<Catalog>> {
        Ok(Some(Catalog {
            name: "session-example".to_string(),
            display_name: request.connection_param("tenant").to_string(),
            description: String::new(),
            icon_svg: String::new(),
            operations: vec![CatalogOperation {
                id: "private_echo".to_string(),
                method: "POST".to_string(),
                title: String::new(),
                description: String::new(),
                input_schema: None,
                output_schema: None,
                annotations: None,
                parameters: Vec::new(),
                required_scopes: Vec::new(),
                tags: Vec::new(),
                read_only: false,
                visible: None,
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
    let socket = temp_socket("gestalt-rust-sdk.sock");
    let _socket_guard = EnvGuard::set(gestalt_plugin_sdk::ENV_PLUGIN_SOCKET, socket.as_os_str());

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
        gestalt_plugin_sdk::runtime::serve_provider(serve_provider, serve_router)
            .await
            .expect("serve provider");
    });

    wait_for_socket(&socket).await;

    let channel = Endpoint::try_from("http://[::]:50051")
        .expect("endpoint")
        .connect_with_connector(service_fn({
            let socket = socket.clone();
            move |_| UnixStream::connect(socket.clone())
        }))
        .await
        .expect("connect channel");
    let mut client = ProviderPluginClient::new(channel);

    let metadata = client
        .get_metadata(())
        .await
        .expect("get metadata")
        .into_inner();
    assert!(metadata.supports_session_catalog);
    assert_eq!(
        metadata.min_protocol_version,
        gestalt_plugin_sdk::CURRENT_PROTOCOL_VERSION
    );
    assert_eq!(
        metadata.max_protocol_version,
        gestalt_plugin_sdk::CURRENT_PROTOCOL_VERSION
    );

    let started = client
        .start_provider(StartProviderRequest {
            name: "example".to_string(),
            config: Some(struct_from_json(serde_json::json!({ "greeting": "Hi" }))),
            protocol_version: gestalt_plugin_sdk::CURRENT_PROTOCOL_VERSION,
        })
        .await
        .expect("start provider")
        .into_inner();
    assert_eq!(
        started.protocol_version,
        gestalt_plugin_sdk::CURRENT_PROTOCOL_VERSION
    );

    let response = client
        .execute(ExecuteRequest {
            operation: "greet".to_string(),
            params: Some(struct_from_json(serde_json::json!({ "name": "Rust" }))),
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
    assert!(session_catalog.catalog_json.contains("session-example"));
    assert!(session_catalog.catalog_json.contains("acme"));

    let err = client
        .post_connect(PostConnectRequest::default())
        .await
        .expect_err("post connect should be unimplemented");
    assert_eq!(err.code(), Code::Unimplemented);

    serve_task.abort();
    let _ = serve_task.await;
}

struct EnvGuard {
    key: &'static str,
    previous: Option<OsString>,
}

impl EnvGuard {
    fn set(key: &'static str, value: impl AsRef<std::ffi::OsStr>) -> Self {
        let previous = std::env::var_os(key);
        unsafe {
            std::env::set_var(key, value);
        }
        Self { key, previous }
    }
}

impl Drop for EnvGuard {
    fn drop(&mut self) {
        unsafe {
            if let Some(previous) = &self.previous {
                std::env::set_var(self.key, previous);
            } else {
                std::env::remove_var(self.key);
            }
        }
    }
}

fn struct_from_json(value: serde_json::Value) -> Struct {
    let object = value.as_object().expect("json object");
    Struct {
        fields: object
            .iter()
            .map(|(key, value)| (key.clone(), json_to_prost(value)))
            .collect(),
    }
}

fn json_to_prost(value: &serde_json::Value) -> Value {
    use prost_types::value::Kind;

    let kind = match value {
        serde_json::Value::Null => Kind::NullValue(0),
        serde_json::Value::Bool(boolean) => Kind::BoolValue(*boolean),
        serde_json::Value::Number(number) => Kind::NumberValue(number.as_f64().expect("f64")),
        serde_json::Value::String(string) => Kind::StringValue(string.clone()),
        serde_json::Value::Array(items) => Kind::ListValue(prost_types::ListValue {
            values: items.iter().map(json_to_prost).collect(),
        }),
        serde_json::Value::Object(object) => Kind::StructValue(Struct {
            fields: object
                .iter()
                .map(|(key, value)| (key.clone(), json_to_prost(value)))
                .collect(),
        }),
    };
    Value { kind: Some(kind) }
}

async fn wait_for_socket(path: &Path) {
    let deadline = tokio::time::Instant::now() + Duration::from_secs(3);
    while tokio::time::Instant::now() < deadline {
        if path.exists() {
            return;
        }
        tokio::time::sleep(Duration::from_millis(25)).await;
    }
    panic!("socket {} was not created", path.display());
}

fn temp_socket(name: &str) -> PathBuf {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("unix epoch")
        .as_nanos();
    std::env::temp_dir().join(format!("{nanos}-{name}"))
}
