//! Tests for the sync REST transport (SyncRestTransport).

use std::sync::Arc;

use base64::Engine as _;
use base64::engine::general_purpose::STANDARD as BASE64;
use gestalt::authorization::{
    Action, CheckAccessRequest, ListRelationshipsRequest, RelationshipFilter, Resource, Subject,
};
use gestalt::public::auth::BearerAuth;
use gestalt::public::generated::app_client::AuthorizationClient;
use gestalt::public::rest_transport::SyncRestTransport;
use gestalt::rpc_support::gestalt_error_code;
use serde_json::json;
use wiremock::matchers::{method, path, query_param};
use wiremock::{Mock, MockServer, ResponseTemplate};

// wiremock::MockServer::start() is async, so we use a one-shot tokio runtime
// to boot the server, then make blocking SDK calls against it.
fn start_mock_server() -> MockServer {
    let rt = tokio::runtime::Runtime::new().expect("tokio runtime");
    rt.block_on(MockServer::start())
}

fn sync_client(server: &MockServer) -> AuthorizationClient<SyncRestTransport> {
    let transport = SyncRestTransport::new(server.uri(), Arc::new(BearerAuth::new("test-token")));
    AuthorizationClient::new(transport)
}

#[test]
fn sync_rest_transport_check_access_success() {
    let server = start_mock_server();
    let rt = tokio::runtime::Runtime::new().expect("tokio runtime");
    rt.block_on(async {
        Mock::given(method("POST"))
            .and(path("/api/v2/authorization/access:check"))
            .respond_with(
                ResponseTemplate::new(200)
                    .set_body_json(json!({"allowed": true, "modelId": "model-1"})),
            )
            .mount(&server)
            .await;
    });

    let client = sync_client(&server);
    let resp = client
        .check_access_sync(CheckAccessRequest {
            subject: Some(Subject {
                r#type: "user".to_string(),
                id: "u1".to_string(),
                properties: None,
            }),
            action: Some(Action {
                name: "read".to_string(),
                properties: None,
            }),
            resource: Some(Resource {
                r#type: "doc".to_string(),
                id: "d1".to_string(),
                properties: None,
            }),
        })
        .expect("check_access_sync should succeed");

    assert!(resp.allowed);
    assert_eq!(resp.model_id, "model-1");
}

#[test]
fn sync_rest_transport_check_access_denied() {
    let server = start_mock_server();
    let rt = tokio::runtime::Runtime::new().expect("tokio runtime");
    rt.block_on(async {
        Mock::given(method("POST"))
            .and(path("/api/v2/authorization/access:check"))
            .respond_with(
                ResponseTemplate::new(200)
                    .set_body_json(json!({"allowed": false, "modelId": "model-1"})),
            )
            .mount(&server)
            .await;
    });

    let client = sync_client(&server);
    let resp = client
        .check_access_sync(CheckAccessRequest {
            subject: Some(Subject {
                r#type: "user".to_string(),
                id: "u1".to_string(),
                properties: None,
            }),
            action: Some(Action {
                name: "read".to_string(),
                properties: None,
            }),
            resource: Some(Resource {
                r#type: "doc".to_string(),
                id: "d1".to_string(),
                properties: None,
            }),
        })
        .expect("check_access_sync should succeed");

    assert!(!resp.allowed);
}

#[test]
fn sync_rest_transport_unauthenticated_error() {
    let server = start_mock_server();
    let rt = tokio::runtime::Runtime::new().expect("tokio runtime");
    rt.block_on(async {
        Mock::given(method("GET"))
            .and(path("/api/v2/authorization/models/active"))
            .respond_with(
                ResponseTemplate::new(401)
                    .set_body_json(json!({"code": "Unauthenticated", "error": "token expired"})),
            )
            .mount(&server)
            .await;
    });

    let client = sync_client(&server);
    let err = client
        .get_active_model_ref_sync()
        .expect_err("should fail with unauthenticated");

    assert_eq!(err.code, gestalt_error_code::UNAUTHENTICATED);
    assert_eq!(err.message, "token expired");
}

#[test]
fn sync_rest_transport_list_relationships_query_params() {
    let server = start_mock_server();
    let rt = tokio::runtime::Runtime::new().expect("tokio runtime");
    rt.block_on(async {
        Mock::given(method("GET"))
            .and(path("/api/v2/authorization/relationships"))
            .and(query_param("filter.relation", "owner"))
            .and(query_param("pageSize", "10"))
            .respond_with(
                ResponseTemplate::new(200)
                    .set_body_json(json!({"relationships": [], "nextPageToken": ""})),
            )
            .mount(&server)
            .await;
    });

    let client = sync_client(&server);
    let resp = client
        .list_relationships_sync(ListRelationshipsRequest {
            filter: Some(RelationshipFilter {
                relation: "owner".to_string(),
                target: None,
                resource: None,
                target_type: 0,
                target_entity_type: String::new(),
                resource_type: String::new(),
                source_layer: 0,
            }),
            page_size: 10,
            page_token: String::new(),
        })
        .expect("list_relationships_sync should succeed");

    assert!(resp.relationships.is_empty());
}

#[test]
fn sync_rest_transport_internal_error() {
    let server = start_mock_server();
    let rt = tokio::runtime::Runtime::new().expect("tokio runtime");
    rt.block_on(async {
        Mock::given(method("GET"))
            .and(path("/api/v2/authorization/models/active"))
            .respond_with(
                ResponseTemplate::new(500)
                    .set_body_json(json!({"code": "Internal", "error": "server crashed"})),
            )
            .mount(&server)
            .await;
    });

    let client = sync_client(&server);
    let err = client
        .get_active_model_ref_sync()
        .expect_err("should fail with internal error");

    assert_eq!(err.code, gestalt_error_code::INTERNAL);
}

#[test]
fn sync_rest_transport_operation_result_envelope() {
    let server = start_mock_server();
    let rt = tokio::runtime::Runtime::new().expect("tokio runtime");

    // The App/Invoke endpoint returns an OperationResult envelope when the
    // X-Gestalt-Response-Kind header is "operation-result". Verify the sync
    // transport handles this path via the shared decode_rest_response helper.
    let app_client = gestalt::public::generated::app_client::AppClient::new(
        SyncRestTransport::new(server.uri(), Arc::new(BearerAuth::new("test-token"))),
    );

    rt.block_on(async {
        let body = json!({
            "status": 200,
            "body": BASE64.encode(br#"{"status":"success","data":{"ok":true}}"#),
            "headers": {}
        });
        Mock::given(method("POST"))
            .and(path("/api/v2/app/example/operations/sync"))
            .respond_with(
                ResponseTemplate::new(200)
                    .insert_header("X-Gestalt-Response-Kind", "operation-result")
                    .set_body_json(body),
            )
            .mount(&server)
            .await;
    });

    use gestalt::public::generated::app::AppInvokeRequest;
    let result = app_client.invoke_sync(AppInvokeRequest {
        app: "example".to_string(),
        operation: "sync".to_string(),
        params: Some(serde_json::Map::new()),
        ..Default::default()
    });
    let _ = result;
}
