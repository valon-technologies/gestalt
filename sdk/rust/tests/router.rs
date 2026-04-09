#[allow(dead_code)]
mod helpers;

use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};

use gestalt_plugin_sdk as gestalt;
use gestalt_plugin_sdk::proto::v1::integration_provider_server::IntegrationProvider;
use gestalt_plugin_sdk::proto::v1::{ExecuteRequest, StartProviderRequest};
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use serde_json::{Map as JsonMap, Value as JsonValue, json};
use tonic::Request as GrpcRequest;
use tonic::codegen::async_trait;

use gestalt_plugin_sdk::{Operation, Provider, Request, Response, Router, ok};

#[derive(Default)]
struct TestProvider;

#[async_trait]
impl Provider for TestProvider {}

#[derive(Deserialize, schemars::JsonSchema)]
struct EchoInput {
    #[schemars(description = "Message to echo")]
    message: String,
}

#[derive(Serialize, schemars::JsonSchema)]
struct EchoOutput {
    message: String,
}

#[tokio::test]
async fn executes_registered_operation() {
    assert_eq!(Request::default().connection_param("missing"), None);

    let router = Router::new()
        .register(
            Operation::<EchoInput, EchoOutput>::new("echo").description("Echo the message"),
            |_: Arc<TestProvider>, input: EchoInput, _request: Request| async move {
                Ok::<Response<EchoOutput>, std::convert::Infallible>(ok(EchoOutput {
                    message: input.message,
                }))
            },
        )
        .expect("register operation");

    let result = router
        .execute(
            Arc::new(TestProvider),
            "echo",
            serde_json::json!({ "message": "hello" }),
            Request::default(),
        )
        .await;

    assert_eq!(result.status, 200);
    assert_eq!(result.body, r#"{"message":"hello"}"#);
}

#[test]
fn catalog_includes_parameters() {
    let router = Router::<TestProvider>::new()
        .register(
            Operation::<EchoInput, EchoOutput>::new("echo").read_only(true),
            |_: Arc<TestProvider>, input: EchoInput, _request: Request| async move {
                Ok::<Response<EchoOutput>, std::convert::Infallible>(ok(EchoOutput {
                    message: input.message,
                }))
            },
        )
        .expect("register operation")
        .with_name("example");

    let catalog = router.catalog();
    assert_eq!(catalog.name, "example");
    assert_eq!(catalog.operations.len(), 1);
    assert_eq!(catalog.operations[0].parameters.len(), 1);
    assert_eq!(catalog.operations[0].parameters[0].name, "message");
    assert!(catalog.operations[0].read_only);
}

#[derive(Default)]
struct ErrorTestProvider {
    greeting: Mutex<String>,
}

#[async_trait]
impl gestalt::Provider for ErrorTestProvider {
    async fn configure(
        &self,
        _name: &str,
        config: JsonMap<String, JsonValue>,
    ) -> gestalt::Result<()> {
        let greeting = config
            .get("greeting")
            .and_then(JsonValue::as_str)
            .unwrap_or("Hello")
            .to_owned();
        *self.greeting.lock().expect("greeting lock") = greeting;
        Ok(())
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
    provider: Arc<ErrorTestProvider>,
    input: GreetInput,
    request: gestalt::Request,
) -> gestalt::Result<gestalt::Response<GreetOutput>> {
    let greeting = provider.greeting.lock().expect("greeting lock").clone();
    let name = input.name.unwrap_or_else(|| "World".to_owned());
    Ok(gestalt::ok(GreetOutput {
        message: format!("{greeting}, {name}!"),
        api_key: request
            .connection_param("api_key")
            .unwrap_or_default()
            .to_owned(),
    }))
}

async fn fail(
    _provider: Arc<ErrorTestProvider>,
    _input: EmptyInput,
    _request: gestalt::Request,
) -> gestalt::Result<gestalt::Response<GreetOutput>> {
    Err(gestalt::Error::internal("boom"))
}

async fn panic_op(
    _provider: Arc<ErrorTestProvider>,
    _input: EmptyInput,
    _request: gestalt::Request,
) -> gestalt::Result<gestalt::Response<GreetOutput>> {
    panic!("boom")
}

fn error_test_router() -> gestalt::Result<gestalt::Router<ErrorTestProvider>> {
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
async fn execute_handles_success_decode_errors_handler_errors_and_panics() {
    let provider = Arc::new(ErrorTestProvider::default());
    let server =
        gestalt::ProviderServer::new(Arc::clone(&provider), error_test_router().expect("router"));
    server
        .start_provider(GrpcRequest::new(StartProviderRequest {
            name: "test".to_owned(),
            config: Some(helpers::struct_from_json(json!({ "greeting": "Hi" }))),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION,
        }))
        .await
        .expect("start provider");

    let success = server
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "greet".to_owned(),
            params: Some(helpers::struct_from_json(json!({ "name": "Ada" }))),
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
            params: Some(helpers::struct_from_json(json!({ "name": 7 }))),
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
