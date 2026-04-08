use std::sync::Arc;

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

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

mod exported {
    use super::*;

    pub struct ExportedProvider;

    #[async_trait]
    impl Provider for ExportedProvider {}

    pub fn new() -> ExportedProvider {
        ExportedProvider
    }

    pub fn router() -> gestalt_plugin_sdk::Result<Router<ExportedProvider>> {
        Router::new().register(
            Operation::<EchoInput, EchoOutput>::new("echo"),
            |_: Arc<ExportedProvider>, input: EchoInput, _request: Request| async move {
                Ok::<Response<EchoOutput>, std::convert::Infallible>(ok(EchoOutput {
                    message: input.message,
                }))
            },
        )
    }

    gestalt_plugin_sdk::export_provider!(constructor = new, router = router);
}

#[test]
fn export_macro_generates_stable_functions() {
    let _serve: fn(&str) -> gestalt_plugin_sdk::Result<()> = exported::__gestalt_serve;
    let _write: fn(&str, &str) -> gestalt_plugin_sdk::Result<()> =
        exported::__gestalt_write_catalog;
}
