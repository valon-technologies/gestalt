//! Conformance tests for the generated JSON operation-envelope decode,
//! driven by the shared fixtures in sdk/testdata/app_invoke. The fixture
//! suite is the normative spec of the envelope semantics across all four
//! SDK languages.

use gestalt::rpc_support::gestalt_error_code;
use gestalt::{decode_app_result, decode_graphql_result, error_for_status, is_success};

fn fixture(name: &str) -> Vec<u8> {
    let path = format!(
        "{}/../testdata/app_invoke/{name}",
        env!("CARGO_MANIFEST_DIR")
    );
    std::fs::read(&path).unwrap_or_else(|err| panic!("read fixture {path}: {err}"))
}

#[test]
fn decodes_app_results_per_fixture_suite() {
    let decode =
        |name: &str, status: i32| decode_app_result("github", "get_issue", status, &fixture(name));

    assert_eq!(
        decode("success_envelope.json", 200).expect("success envelope"),
        serde_json::json!({ "id": 1 })
    );
    assert_eq!(
        decode("plain_ok.json", 200).expect("plain ok"),
        serde_json::json!({ "pull_request": { "id": 123, "title": "Fix transport" } })
    );
    assert_eq!(
        decode("empty_body.json", 200).expect("empty body"),
        serde_json::json!({})
    );
    assert_eq!(
        decode("success_missing_data.json", 200).expect("success without data"),
        serde_json::json!({ "status": "success", "ok": true })
    );
    assert_eq!(
        decode("success_null_data.json", 200).expect("null data"),
        serde_json::Value::Null
    );
    assert_eq!(
        decode("unknown_status.json", 200).expect("unknown status"),
        serde_json::json!({ "status": "pending", "data": { "id": 2 } })
    );
    assert_eq!(
        decode("non_string_status.json", 200).expect("non-string status"),
        serde_json::json!({ "status": true, "data": { "id": 3 } })
    );
    assert_eq!(
        decode("array_ok.json", 200).expect("array"),
        serde_json::json!([1, 2, 3])
    );
    assert_eq!(
        decode("primitive_ok.json", 200).expect("primitive"),
        serde_json::json!("ok")
    );

    let envelope = decode("error_envelope.json", 200).expect_err("error envelope");
    assert_eq!(envelope.app, "github");
    assert_eq!(envelope.operation, "get_issue");
    assert!(envelope.status.is_none());
    assert_eq!(envelope.reason.as_deref(), Some("missing_credential"));
    assert_eq!(envelope.gestalt_code(), gestalt_error_code::UNKNOWN);

    let http = decode("http_401.json", 401).expect_err("http error");
    assert_eq!(http.status, Some(401));
    assert_eq!(http.reason.as_deref(), Some("unauthorized"));
    assert_eq!(http.gestalt_code(), gestalt_error_code::UNAUTHENTICATED);

    let redirect = decode("http_302.json", 302).expect_err("redirect");
    assert_eq!(redirect.status, Some(302));
    assert_eq!(redirect.gestalt_code(), gestalt_error_code::UNKNOWN);

    let invalid = decode("invalid_json.txt", 200).expect_err("invalid json");
    assert_eq!(invalid.message, "app invoke response is not valid JSON");
    assert_eq!(invalid.gestalt_code(), gestalt_error_code::INTERNAL);
}

#[test]
fn error_for_status_matches_the_http_error_decode() {
    let body = fixture("http_401.json");
    let err = error_for_status("github", "get_issue", 401, &body).expect_err("http 401");
    let decoded =
        decode_app_result("github", "get_issue", 401, &body).expect_err("decode http 401");
    assert_eq!(err.app, decoded.app);
    assert_eq!(err.operation, decoded.operation);
    assert_eq!(err.status, Some(401));
    assert_eq!(err.reason.as_deref(), Some("unauthorized"));
    assert_eq!(err.message, "unauthorized");
    assert_eq!(err.gestalt_code(), gestalt_error_code::UNAUTHENTICATED);
    assert_eq!(err.body, decoded.body);
    assert_eq!(err.raw_body, decoded.raw_body);

    let invalid = error_for_status("github", "get_issue", 500, &fixture("invalid_json.txt"))
        .expect_err("http 500 with non-JSON body");
    assert_eq!(invalid.status, Some(500));
    assert_eq!(invalid.message, "app invoke failed with status 500");
    assert!(invalid.body.is_none());

    error_for_status("github", "get_issue", 200, &body).expect("2xx passes");

    let redirect = fixture("http_302.json");
    let redirect_err =
        error_for_status("github", "get_issue", 302, &redirect).expect_err("3xx fails");
    assert_eq!(redirect_err.status, Some(302));
    let decoded_redirect =
        decode_app_result("github", "get_issue", 302, &redirect).expect_err("decode 302");
    assert_eq!(decoded_redirect.status, Some(302));
}

#[test]
fn is_success_covers_exactly_the_2xx_range() {
    assert!(!is_success(199));
    assert!(is_success(200));
    assert!(is_success(204));
    assert!(is_success(299));
    assert!(!is_success(300));
    assert!(!is_success(404));
    assert!(!is_success(-1));
}

#[test]
fn decodes_graphql_results_per_fixture_suite() {
    let decode = |name: &str| decode_graphql_result("linear", 200, &fixture(name));

    assert_eq!(
        decode("graphql_ok.json").expect("graphql ok"),
        serde_json::json!({ "data": { "viewer": { "id": "user-1" } }, "errors": [] })
    );
    assert_eq!(
        decode("graphql_malformed_errors.json").expect("malformed errors pass through"),
        serde_json::json!({ "data": { "viewer": null }, "errors": { "message": "not an array" } })
    );

    let errors = decode("graphql_errors.json").expect_err("errors array");
    assert_eq!(errors.reason.as_deref(), Some("graphql_errors"));
    assert_eq!(errors.operation, "graphql");
    assert_eq!(errors.gestalt_code(), gestalt_error_code::INTERNAL);

    decode("graphql_success_envelope_errors.json").expect_err("errors behind success envelope");
}

#[test]
fn operation_result_json_classifies_invalid_body_as_internal() {
    use gestalt::OperationResult;

    let result = OperationResult {
        status: 200,
        headers: Default::default(),
        body: b"not-json".to_vec(),
    };
    let err = result.json().expect_err("invalid json body");
    assert_eq!(err.message, "operation result body is not valid JSON");
    assert_eq!(err.gestalt_code(), gestalt_error_code::INTERNAL);
}
