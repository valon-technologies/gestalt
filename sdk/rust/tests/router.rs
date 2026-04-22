#[allow(dead_code)]
mod helpers;

use std::collections::BTreeMap;
use std::fs;
use std::sync::{Arc, Mutex};

use gestalt::proto::v1::integration_provider_server::IntegrationProvider;
use gestalt::proto::v1::{
    CredentialContext, ExecuteRequest, RequestContext, StartProviderRequest, SubjectContext,
};
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use serde_json::{Map as JsonMap, Value as JsonValue, json};
use tonic::Request as GrpcRequest;
use tonic::codegen::async_trait;

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

fn http_manifest_router() -> gestalt::Result<gestalt::Router<TestProvider>> {
    gestalt::Router::new()
        .security_scheme(
            "signed",
            gestalt::HTTPSecurityScheme::hmac()
                .secret_env("REQUEST_SIGNING_SECRET")
                .signature_header("X-Request-Signature")
                .signature_prefix("v0=")
                .payload_template("v0:{header:X-Request-Timestamp}:{raw_body}")
                .timestamp_header("X-Request-Timestamp")
                .max_age_seconds(300),
        )
        .http_binding(
            "command",
            gestalt::HTTPBinding::new("/command", "post", "signed", "echo")
                .request_body(
                    gestalt::HTTPRequestBody::new()
                        .required(true)
                        .content_type("application/x-www-form-urlencoded"),
                )
                .ack(gestalt::HTTPAck::new().status(200).body(json!({
                    "status": "accepted",
                }))),
        )
        .register(
            Operation::<EchoInput, EchoOutput>::new("echo"),
            |_: Arc<TestProvider>, input: EchoInput, _request: Request| async move {
                Ok::<Response<EchoOutput>, std::convert::Infallible>(ok(EchoOutput {
                    message: input.message,
                }))
            },
        )
}

gestalt::export_provider!(
    constructor = TestProvider::default,
    router = http_manifest_router
);

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

#[test]
fn router_includes_manifest_metadata() {
    let router = http_manifest_router().expect("router");
    let metadata = router.manifest_metadata();

    assert_eq!(
        metadata.security_schemes["signed"].kind,
        Some(gestalt::HTTPSecuritySchemeType::Hmac)
    );
    assert_eq!(metadata.http["command"].path, "/command");
    assert_eq!(metadata.http["command"].method, "POST");
    assert_eq!(metadata.http["command"].security, "signed");
    assert_eq!(metadata.http["command"].target, "echo");
    assert_eq!(
        metadata.http["command"]
            .request_body
            .as_ref()
            .expect("request body")
            .content
            .len(),
        1
    );
}

#[tokio::test]
async fn serve_provider_writes_catalog_and_manifest_metadata_when_requested() {
    let _env_lock = helpers::env_lock().lock().await;
    let catalog_path = helpers::temp_socket("router-catalog.json");
    let metadata_path = helpers::temp_socket("router-manifest-metadata.yaml");
    let _catalog_guard = helpers::EnvGuard::set("GESTALT_PLUGIN_WRITE_CATALOG", &catalog_path);
    let _metadata_guard =
        helpers::EnvGuard::set(gestalt::ENV_WRITE_MANIFEST_METADATA, &metadata_path);
    let _name_guard = helpers::EnvGuard::set("GESTALT_PLUGIN_NAME", "manifest-provider");

    gestalt::runtime::serve_provider(
        Arc::new(TestProvider),
        http_manifest_router().expect("router"),
    )
    .await
    .expect("serve provider");

    let catalog = fs::read_to_string(&catalog_path).expect("read catalog");
    assert!(catalog.contains("\"name\": \"manifest-provider\""));
    assert!(catalog.contains("\"id\": \"echo\""));

    let metadata = fs::read_to_string(&metadata_path).expect("read manifest metadata");
    assert!(metadata.contains("securitySchemes:"));
    assert!(metadata.contains("type: hmac"));
    assert!(metadata.contains("env: REQUEST_SIGNING_SECRET"));
    assert!(metadata.contains("signatureHeader: X-Request-Signature"));
    assert!(metadata.contains("http:"));
    assert!(metadata.contains("path: /command"));
    assert!(metadata.contains("target: echo"));
}

#[tokio::test]
async fn export_provider_writes_manifest_metadata_with_catalog_exports() {
    let _env_lock = helpers::env_lock().lock().await;
    let catalog_path = helpers::temp_socket("macro-catalog.json");
    let metadata_path = helpers::temp_socket("macro-manifest-metadata.yaml");
    let direct_metadata_path = helpers::temp_socket("direct-manifest-metadata.yaml");
    let _metadata_guard =
        helpers::EnvGuard::set(gestalt::ENV_WRITE_MANIFEST_METADATA, &metadata_path);

    __gestalt_write_catalog("macro-provider", catalog_path.to_str().expect("utf8 path"))
        .expect("write catalog");

    let catalog = fs::read_to_string(&catalog_path).expect("read macro catalog");
    assert!(catalog.contains("\"name\": \"macro-provider\""));

    let metadata = fs::read_to_string(&metadata_path).expect("read macro metadata");
    assert!(metadata.contains("securitySchemes:"));
    assert!(metadata.contains("target: echo"));

    __gestalt_write_manifest_metadata(
        "macro-provider",
        direct_metadata_path.to_str().expect("utf8 path"),
    )
    .expect("write direct manifest metadata");

    let direct_metadata =
        fs::read_to_string(&direct_metadata_path).expect("read direct manifest metadata");
    assert!(direct_metadata.contains("securitySchemes:"));
    assert!(direct_metadata.contains("application/x-www-form-urlencoded"));
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
    credential_mode: String,
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
        credential_mode: request.credential.mode,
    }))
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
            invocation_token: "token-123".to_owned(),
            context: Some(RequestContext {
                subject: Some(SubjectContext {
                    id: "user:user-123".to_owned(),
                    kind: "user".to_owned(),
                    ..Default::default()
                }),
                credential: Some(CredentialContext {
                    mode: "identity".to_owned(),
                    ..Default::default()
                }),
                access: None,
                workflow: None,
            }),
        }))
        .await
        .expect("execute greet")
        .into_inner();
    assert_eq!(success.status, 200);
    assert_eq!(
        success.body,
        r#"{"message":"Hi, Ada!","api_key":"secret","subject_id":"user:user-123","credential_mode":"identity"}"#
    );

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

    let implicit_handler_error = server
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "implicit_error".to_owned(),
            ..ExecuteRequest::default()
        }))
        .await
        .expect("execute implicit_error")
        .into_inner();
    assert_eq!(implicit_handler_error.status, 500);
    assert_eq!(implicit_handler_error.body, r#"{"error":"internal error"}"#);

    let not_found = server
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "not_found".to_owned(),
            ..ExecuteRequest::default()
        }))
        .await
        .expect("execute not_found")
        .into_inner();
    assert_eq!(not_found.status, 404);
    assert_eq!(not_found.body, r#"{"error":"record not found"}"#);

    let explicit_500 = server
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "explicit_500".to_owned(),
            ..ExecuteRequest::default()
        }))
        .await
        .expect("execute explicit_500")
        .into_inner();
    assert_eq!(explicit_500.status, 500);
    assert_eq!(explicit_500.body, r#"{"error":"service unavailable"}"#);

    let panic = server
        .execute(GrpcRequest::new(ExecuteRequest {
            operation: "panic".to_owned(),
            ..ExecuteRequest::default()
        }))
        .await
        .expect("execute panic")
        .into_inner();
    assert_eq!(panic.status, 500);
    assert_eq!(panic.body, r#"{"error":"internal error"}"#);
}

#[tokio::test]
async fn lifecycle_rpcs_sanitize_hidden_internal_errors() {
    let server = gestalt::ProviderServer::new(
        Arc::new(HiddenLifecycleProvider),
        gestalt::Router::<HiddenLifecycleProvider>::new(),
    );

    let configure_error = server
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

    let catalog_error = server
        .get_session_catalog(GrpcRequest::new(
            gestalt::proto::v1::GetSessionCatalogRequest::default(),
        ))
        .await
        .expect_err("get session catalog should fail");
    assert_eq!(catalog_error.code(), tonic::Code::Unknown);
    assert_eq!(catalog_error.message(), "session catalog: internal error");
}
