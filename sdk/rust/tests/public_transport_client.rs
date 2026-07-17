//! Tests for the public Gestalt transport client.

use std::sync::{Arc, RwLock};

use base64::Engine;
use gestalt::public::auth::{Auth, BearerAuth, NoAuth};
use gestalt::public::client::{Transport, create_gestalt_client};
use gestalt::public::generated::app::AppInvokeRequest;
use gestalt::public::generated::app_client::AppClient;
use gestalt::public::rest_transport::RestTransport;
use gestalt::rpc_support::gestalt_error_code;
use serde_json::json;
use wiremock::matchers::{method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

#[tokio::test]
async fn create_client_requires_address() {
    let result = create_gestalt_client("", NoAuth, Transport::Rest).await;
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
        .respond_with(ResponseTemplate::new(200).set_body_json(body))
        .mount(&server)
        .await;

    let client = AppClient::new(RestTransport::new(
        server.uri(),
        Arc::new(BearerAuth::new("test-token")),
    ));
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

    let client = create_gestalt_client(server.uri(), BearerAuth::new("token"), Transport::Rest)
        .await
        .expect("client");
    let gestalt::public::client::GestaltClient::Rest(app) = client else {
        panic!("expected REST client");
    };
    let result = app
        .invoke(AppInvokeRequest {
            app: "example".to_string(),
            operation: "sync".to_string(),
            ..Default::default()
        })
        .await
        .expect("invoke");
    assert_eq!(result, json!({"ok": true}));
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
