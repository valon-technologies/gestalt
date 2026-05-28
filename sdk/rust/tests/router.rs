#[path = "../src/generated.rs"]
mod generated;

#[allow(dead_code)]
mod helpers;

use std::collections::BTreeMap;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};

use generated::v1::app_provider_client::AppProviderClient;
use generated::v1::{
    CredentialContext, ExecuteRequest, RequestContext, StartProviderRequest, SubjectContext,
};
use hyper_util::rt::tokio::TokioIo;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use serde_json::{Map as JsonMap, Value as JsonValue, json};
use tokio::net::UnixStream;
use tonic::Request as GrpcRequest;
use tonic::codegen::async_trait;
use tonic::transport::{Channel, Endpoint};
use tower::service_fn;

use gestalt::{Operation, Provider, Request, Response, Router, ok};

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

fn test_router() -> gestalt::Result<gestalt::Router<TestProvider>> {
    gestalt::Router::new().register(
        Operation::<EchoInput, EchoOutput>::new("echo"),
        |_: Arc<TestProvider>, input: EchoInput, _request: Request| async move {
            Ok::<Response<EchoOutput>, std::convert::Infallible>(ok(EchoOutput {
                message: input.message,
            }))
        },
    )
}

gestalt::export_provider!(constructor = TestProvider::default, router = test_router);

async fn integration_client(socket: PathBuf) -> AppProviderClient<Channel> {
    let channel = Endpoint::try_from("http://[::]:50051")
        .expect("endpoint")
        .connect_with_connector(service_fn(move |_| {
            let socket = socket.clone();
            async move { UnixStream::connect(socket).await.map(TokioIo::new) }
        }))
        .await
        .expect("connect channel");
    AppProviderClient::new(channel)
}

#[tokio::test]
async fn executes_registered_operation() {
    assert_eq!(Request::default().connection_param("missing"), None);

    let router = Router::new()
        .register(
            Operation::<EchoInput, EchoOutput>::new("echo").description("Echo the message"),
            |_: Arc<TestProvider>, input: EchoInput, _request: Request| async move {
                Ok::<Response<EchoOutput>, std::convert::Infallible>(
                    ok(EchoOutput {
                        message: input.message,
                    })
                    .with_header("Location", "/echo"),
                )
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
    assert_eq!(
        result.headers.get("Location").map(Vec::as_slice),
        Some(&["/echo".to_owned()][..])
    );
    assert_eq!(result.body, r#"{"message":"hello"}"#);
}

#[test]
fn catalog_includes_parameters() {
    let router = Router::<TestProvider>::new()
        .register(
            Operation::<EchoInput, EchoOutput>::new("echo")
                .read_only(true)
                .allowed_roles(vec!["viewer".to_owned(), "admin".to_owned()]),
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
    assert_eq!(catalog.operations[0].allowed_roles, vec!["viewer", "admin"]);
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
    subject_id: String,
    subject_email: String,
    agent_subject_email: String,
    credential_mode: String,
    idempotency_key: String,
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
        subject_id: request.subject.id,
        subject_email: request.subject.email,
        agent_subject_email: request.agent_subject.email,
        credential_mode: request.credential.mode,
        idempotency_key: request.idempotency_key,
    })
    .with_header("Location", format!("/greet/{name}")))
}

async fn fail(
    _provider: Arc<ErrorTestProvider>,
    _input: EmptyInput,
    _request: gestalt::Request,
) -> gestalt::Result<gestalt::Response<GreetOutput>> {
    Err(gestalt::Error::internal("boom"))
}

async fn implicit_internal(
    _provider: Arc<ErrorTestProvider>,
    _input: EmptyInput,
    _request: gestalt::Request,
) -> gestalt::Result<gestalt::Response<GreetOutput>> {
    Err(std::io::Error::other("disk exploded").into())
}

async fn not_found(
    _provider: Arc<ErrorTestProvider>,
    _input: EmptyInput,
    _request: gestalt::Request,
) -> gestalt::Result<gestalt::Response<GreetOutput>> {
    Err(gestalt::Error::not_found("record not found"))
}

async fn explicit_500(
    _provider: Arc<ErrorTestProvider>,
    _input: EmptyInput,
    _request: gestalt::Request,
) -> gestalt::Result<gestalt::Response<GreetOutput>> {
    Err(gestalt::Error::with_status(500, "service unavailable"))
}

async fn panic_op(
    _provider: Arc<ErrorTestProvider>,
    _input: EmptyInput,
    _request: gestalt::Request,
) -> gestalt::Result<gestalt::Response<GreetOutput>> {
    panic!("boom")
}

#[derive(Default)]
struct HiddenLifecycleProvider;

#[async_trait]
impl gestalt::Provider for HiddenLifecycleProvider {
    async fn configure(
        &self,
        _name: &str,
        _config: JsonMap<String, JsonValue>,
    ) -> gestalt::Result<()> {
        Err(std::io::Error::other("disk exploded").into())
    }

    fn supports_session_catalog(&self) -> bool {
        true
    }

    async fn catalog_for_request(
        &self,
        _request: &gestalt::Request,
    ) -> gestalt::Result<Option<gestalt::Catalog>> {
        Err(std::io::Error::other("catalog exploded").into())
    }
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
            gestalt::Operation::<EmptyInput, GreetOutput>::new("implicit_error"),
            implicit_internal,
        )?
        .register(
            gestalt::Operation::<EmptyInput, GreetOutput>::new("not_found"),
            not_found,
        )?
        .register(
            gestalt::Operation::<EmptyInput, GreetOutput>::new("explicit_500"),
            explicit_500,
        )?
        .register(
            gestalt::Operation::<EmptyInput, GreetOutput>::new("panic"),
            panic_op,
        )
}

#[tokio::test]
async fn execute_handles_success_decode_errors_handler_errors_and_panics() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("grr.sock");
    let _socket_guard = helpers::EnvGuard::set(gestalt::ENV_PROVIDER_SOCKET, socket.as_os_str());

    let provider = Arc::new(ErrorTestProvider::default());
    let router = error_test_router().expect("router");
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_provider(serve_provider, router)
            .await
            .expect("serve provider");
    });

    helpers::wait_for_socket(&socket).await;
    let mut client = integration_client(socket.clone()).await;

    client
        .start_provider(GrpcRequest::new(StartProviderRequest {
            name: "test".to_owned(),
            config: Some(helpers::struct_from_json(json!({ "greeting": "Hi" }))),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION,
        }))
        .await
        .expect("start provider");

    let success = client
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "greet".to_owned(),
            params: Some(helpers::struct_from_json(json!({ "name": "Ada" }))),
            token: "tok".to_owned(),
            connection_params: BTreeMap::from([("api_key".to_owned(), "secret".to_owned())]),
            invocation_id: String::new(),
            invocation_token: "token-123".to_owned(),
            context: Some(RequestContext {
                subject: Some(SubjectContext {
                    id: "user:user-123".to_owned(),
                    kind: "user".to_owned(),
                    email: "ada@example.com".to_owned(),
                    ..Default::default()
                }),
                agent_subject: Some(SubjectContext {
                    id: "user:user-456".to_owned(),
                    kind: "user".to_owned(),
                    email: "grace@example.com".to_owned(),
                    ..Default::default()
                }),
                credential: Some(CredentialContext {
                    mode: "user".to_owned(),
                    ..Default::default()
                }),
                access: None,
                workflow: None,
                host: None,
                ..Default::default()
            }),
            idempotency_key: " tool-call-123 ".to_owned(),
        }))
        .await
        .expect("execute greet")
        .into_inner();
    assert_eq!(success.status, 200);
    assert_eq!(
        success
            .headers
            .get("Location")
            .map(|header| header.values.as_slice()),
        Some(&["/greet/Ada".to_owned()][..])
    );
    assert_eq!(
        success.body,
        r#"{"message":"Hi, Ada!","api_key":"secret","subject_id":"user:user-123","subject_email":"ada@example.com","agent_subject_email":"grace@example.com","credential_mode":"user","idempotency_key":"tool-call-123"}"#
    );

    let unknown = client
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "missing".to_owned(),
            ..ExecuteRequest::default()
        }))
        .await
        .expect("execute missing")
        .into_inner();
    assert_eq!(unknown.status, 404);
    assert_eq!(unknown.body, r#"{"error":"unknown operation"}"#);

    let decode = client
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

    let handler_error = client
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "error".to_owned(),
            ..ExecuteRequest::default()
        }))
        .await
        .expect("execute error")
        .into_inner();
    assert_eq!(handler_error.status, 500);
    assert_eq!(handler_error.body, r#"{"error":"boom"}"#);

    let implicit_handler_error = client
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "implicit_error".to_owned(),
            ..ExecuteRequest::default()
        }))
        .await
        .expect("execute implicit_error")
        .into_inner();
    assert_eq!(implicit_handler_error.status, 500);
    assert_eq!(implicit_handler_error.body, r#"{"error":"internal error"}"#);

    let not_found = client
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "not_found".to_owned(),
            ..ExecuteRequest::default()
        }))
        .await
        .expect("execute not_found")
        .into_inner();
    assert_eq!(not_found.status, 404);
    assert_eq!(not_found.body, r#"{"error":"record not found"}"#);

    let explicit_500 = client
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "explicit_500".to_owned(),
            ..ExecuteRequest::default()
        }))
        .await
        .expect("execute explicit_500")
        .into_inner();
    assert_eq!(explicit_500.status, 500);
    assert_eq!(explicit_500.body, r#"{"error":"service unavailable"}"#);

    let panic = client
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "panic".to_owned(),
            ..ExecuteRequest::default()
        }))
        .await
        .expect("execute panic")
        .into_inner();
    assert_eq!(panic.status, 500);
    assert_eq!(panic.body, r#"{"error":"internal error"}"#);

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn lifecycle_rpcs_sanitize_hidden_internal_errors() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("grl.sock");
    let _socket_guard = helpers::EnvGuard::set(gestalt::ENV_PROVIDER_SOCKET, socket.as_os_str());

    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_provider(
            Arc::new(HiddenLifecycleProvider),
            gestalt::Router::<HiddenLifecycleProvider>::new(),
        )
        .await
        .expect("serve provider");
    });

    helpers::wait_for_socket(&socket).await;
    let mut client = integration_client(socket.clone()).await;

    let configure_error = client
        .start_provider(GrpcRequest::new(StartProviderRequest {
            name: "broken".to_owned(),
            config: None,
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION,
        }))
        .await
        .expect_err("start provider should fail");
    assert_eq!(configure_error.code(), tonic::Code::Unknown);
    assert_eq!(
        configure_error.message(),
        "configure provider: internal error"
    );

    let catalog_error = client
        .get_session_catalog(GrpcRequest::new(
            generated::v1::GetSessionCatalogRequest::default(),
        ))
        .await
        .expect_err("get session catalog should fail");
    assert_eq!(catalog_error.code(), tonic::Code::Unknown);
    assert_eq!(catalog_error.message(), "session catalog: internal error");

    serve_task.abort();
    let _ = serve_task.await;
}
