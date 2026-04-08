use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};

use async_trait::async_trait;
use gestalt::proto::v1::provider_plugin_server::ProviderPlugin;
use gestalt::proto::v1::{
    ExecuteRequest, GetSessionCatalogRequest, PostConnectRequest, StartProviderRequest,
};
use gestalt_plugin_sdk as gestalt;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use serde_json::{Map as JsonMap, Value as JsonValue, json};
use tonic::{Code, Request as GrpcRequest};

#[derive(Default)]
struct TestProvider {
    greeting: Mutex<String>,
    configured_name: Mutex<String>,
    supports_session_catalog: bool,
    empty_session_catalog: bool,
}

impl TestProvider {
    fn with_session_catalog() -> Self {
        Self {
            supports_session_catalog: true,
            ..Self::default()
        }
    }

    fn with_empty_session_catalog() -> Self {
        Self {
            supports_session_catalog: true,
            empty_session_catalog: true,
            ..Self::default()
        }
    }
}

#[async_trait]
impl gestalt::Provider for TestProvider {
    async fn configure(
        &self,
        name: &str,
        config: JsonMap<String, JsonValue>,
    ) -> gestalt::Result<()> {
        *self.configured_name.lock().expect("configured_name lock") = name.to_owned();
        let greeting = config
            .get("greeting")
            .and_then(JsonValue::as_str)
            .unwrap_or("Hello")
            .to_owned();
        *self.greeting.lock().expect("greeting lock") = greeting;
        Ok(())
    }

    fn supports_session_catalog(&self) -> bool {
        self.supports_session_catalog
    }

    async fn catalog_for_request(
        &self,
        request: &gestalt::Request,
    ) -> gestalt::Result<Option<gestalt::Catalog>> {
        if !self.supports_session_catalog {
            return Ok(None);
        }
        if self.empty_session_catalog {
            return Ok(None);
        }
        Ok(Some(gestalt::Catalog {
            name: format!("session-{}", request.token),
            operations: vec![gestalt::CatalogOperation {
                id: "session_op".to_owned(),
                method: "GET".to_owned(),
                ..gestalt::CatalogOperation::default()
            }],
            ..gestalt::Catalog::default()
        }))
    }
}

#[derive(Deserialize, JsonSchema)]
struct GreetInput {
    name: Option<String>,
}

#[derive(Serialize, JsonSchema)]
struct GreetOutput {
    message: String,
    api_key: String,
}

#[derive(Deserialize, JsonSchema)]
struct EmptyInput {}

async fn greet(
    provider: Arc<TestProvider>,
    input: GreetInput,
    request: gestalt::Request,
) -> gestalt::Result<gestalt::Response<GreetOutput>> {
    let greeting = provider.greeting.lock().expect("greeting lock").clone();
    let name = input.name.unwrap_or_else(|| "World".to_owned());
    Ok(gestalt::ok(GreetOutput {
        message: format!("{greeting}, {name}!"),
        api_key: request.connection_param("api_key").to_owned(),
    }))
}

async fn fail(
    _provider: Arc<TestProvider>,
    _input: EmptyInput,
    _request: gestalt::Request,
) -> gestalt::Result<gestalt::Response<GreetOutput>> {
    Err(gestalt::Error::internal("boom"))
}

async fn panic_op(
    _provider: Arc<TestProvider>,
    _input: EmptyInput,
    _request: gestalt::Request,
) -> gestalt::Result<gestalt::Response<GreetOutput>> {
    panic!("boom")
}

fn router() -> gestalt::Result<gestalt::Router<TestProvider>> {
    gestalt::Router::new()
        .register(
            gestalt::Operation::<GreetInput, GreetOutput>::new("greet")
                .method("GET")
                .description("Return a greeting message")
                .read_only(true),
            greet,
        )?
        .register(
            gestalt::Operation::<EmptyInput, GreetOutput>::new("error"),
            fail,
        )?
        .register(
            gestalt::Operation::<EmptyInput, GreetOutput>::new("panic"),
            panic_op,
        )
}

#[tokio::test]
async fn get_metadata_reports_protocol_window() {
    let server =
        gestalt::ProviderServer::new(Arc::new(TestProvider::default()), router().expect("router"));
    let response = server
        .get_metadata(GrpcRequest::new(()))
        .await
        .expect("get metadata")
        .into_inner();

    assert!(!response.supports_session_catalog);
    assert_eq!(
        response.min_protocol_version,
        gestalt::CURRENT_PROTOCOL_VERSION
    );
    assert_eq!(
        response.max_protocol_version,
        gestalt::CURRENT_PROTOCOL_VERSION
    );
}

#[tokio::test]
async fn start_provider_configures_provider_and_echoes_protocol_version() {
    let provider = Arc::new(TestProvider::default());
    let server = gestalt::ProviderServer::new(Arc::clone(&provider), router().expect("router"));

    let response = server
        .start_provider(GrpcRequest::new(StartProviderRequest {
            name: "my-instance".to_owned(),
            config: Some(json_struct(json!({ "greeting": "Hi" }))),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION,
        }))
        .await
        .expect("start provider")
        .into_inner();

    assert_eq!(response.protocol_version, gestalt::CURRENT_PROTOCOL_VERSION);
    assert_eq!(
        *provider
            .configured_name
            .lock()
            .expect("configured_name lock"),
        "my-instance"
    );
    assert_eq!(*provider.greeting.lock().expect("greeting lock"), "Hi");
}

#[tokio::test]
async fn execute_handles_success_decode_errors_handler_errors_and_panics() {
    let provider = Arc::new(TestProvider::default());
    let server = gestalt::ProviderServer::new(Arc::clone(&provider), router().expect("router"));
    server
        .start_provider(GrpcRequest::new(StartProviderRequest {
            name: "test".to_owned(),
            config: Some(json_struct(json!({ "greeting": "Hi" }))),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION,
        }))
        .await
        .expect("start provider");

    let success = server
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "greet".to_owned(),
            params: Some(json_struct(json!({ "name": "Ada" }))),
            token: "tok".to_owned(),
            connection_params: BTreeMap::from([("api_key".to_owned(), "secret".to_owned())]),
            invocation_id: String::new(),
        }))
        .await
        .expect("execute greet")
        .into_inner();
    assert_eq!(success.status, 200);
    assert_eq!(success.body, r#"{"message":"Hi, Ada!","api_key":"secret"}"#);

    let unknown = server
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "missing".to_owned(),
            ..ExecuteRequest::default()
        }))
        .await
        .expect("execute missing")
        .into_inner();
    assert_eq!(unknown.status, 404);
    assert_eq!(unknown.body, r#"{"error":"unknown operation"}"#);

    let decode = server
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "greet".to_owned(),
            params: Some(json_struct(json!({ "name": 7 }))),
            ..ExecuteRequest::default()
        }))
        .await
        .expect("execute decode")
        .into_inner();
    assert_eq!(decode.status, 400);
    assert!(decode.body.contains("decode params for"));
    assert!(decode.body.contains("greet"));

    let handler_error = server
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "error".to_owned(),
            ..ExecuteRequest::default()
        }))
        .await
        .expect("execute error")
        .into_inner();
    assert_eq!(handler_error.status, 500);
    assert_eq!(handler_error.body, r#"{"error":"boom"}"#);

    let panic = server
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "panic".to_owned(),
            ..ExecuteRequest::default()
        }))
        .await
        .expect("execute panic")
        .into_inner();
    assert_eq!(panic.status, 500);
    assert_eq!(panic.body, r#"{"error":"boom"}"#);
}

#[tokio::test]
async fn session_catalog_and_post_connect_match_go_behavior() {
    let unsupported =
        gestalt::ProviderServer::new(Arc::new(TestProvider::default()), router().expect("router"));
    let error = unsupported
        .get_session_catalog(GrpcRequest::new(GetSessionCatalogRequest {
            token: "tok".to_owned(),
            ..GetSessionCatalogRequest::default()
        }))
        .await
        .expect_err("session catalog should be unimplemented");
    assert_eq!(error.code(), Code::Unimplemented);

    let supported = gestalt::ProviderServer::new(
        Arc::new(TestProvider::with_session_catalog()),
        router().expect("router"),
    );
    let catalog = supported
        .get_session_catalog(GrpcRequest::new(GetSessionCatalogRequest {
            token: "tok".to_owned(),
            connection_params: BTreeMap::from([("api_key".to_owned(), "secret".to_owned())]),
            invocation_id: String::new(),
        }))
        .await
        .expect("get session catalog")
        .into_inner();
    assert!(catalog.catalog_json.contains(r#""name":"session-tok""#));
    assert!(catalog.catalog_json.contains(r#""transport":"plugin""#));

    let empty_catalog = gestalt::ProviderServer::new(
        Arc::new(TestProvider::with_empty_session_catalog()),
        router().expect("router"),
    );
    let empty = empty_catalog
        .get_session_catalog(GrpcRequest::new(GetSessionCatalogRequest {
            token: "tok".to_owned(),
            ..GetSessionCatalogRequest::default()
        }))
        .await
        .expect("empty session catalog")
        .into_inner();
    assert_eq!(empty.catalog_json, "");

    let post_connect = supported
        .post_connect(GrpcRequest::new(PostConnectRequest::default()))
        .await
        .expect_err("post connect should be unimplemented");
    assert_eq!(post_connect.code(), Code::Unimplemented);
}

fn json_struct(value: JsonValue) -> prost_types::Struct {
    let JsonValue::Object(fields) = value else {
        panic!("expected object value");
    };

    prost_types::Struct {
        fields: fields
            .into_iter()
            .map(|(key, value)| (key, json_value(value)))
            .collect(),
    }
}

fn json_value(value: JsonValue) -> prost_types::Value {
    use prost_types::value::Kind;

    prost_types::Value {
        kind: Some(match value {
            JsonValue::Null => Kind::NullValue(prost_types::NullValue::NullValue as i32),
            JsonValue::Bool(flag) => Kind::BoolValue(flag),
            JsonValue::Number(number) => {
                Kind::NumberValue(number.as_f64().expect("json number should fit in f64"))
            }
            JsonValue::String(text) => Kind::StringValue(text),
            JsonValue::Array(items) => Kind::ListValue(prost_types::ListValue {
                values: items.into_iter().map(json_value).collect(),
            }),
            JsonValue::Object(fields) => Kind::StructValue(prost_types::Struct {
                fields: fields
                    .into_iter()
                    .map(|(key, value)| (key, json_value(value)))
                    .collect(),
            }),
        }),
    }
}
