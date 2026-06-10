//! Conformance tests for the generated JSON operation-envelope decode,
//! driven by the shared fixtures in sdk/testdata/app_invoke. The fixture
//! suite is the normative spec of the envelope semantics across all four
//! SDK languages.

use gestalt::{decode_app_result, decode_graphql_result};

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

    let http = decode("http_401.json", 401).expect_err("http error");
    assert_eq!(http.status, Some(401));

    let invalid = decode("invalid_json.txt", 200).expect_err("invalid json");
    assert_eq!(invalid.message, "app invoke response is not valid JSON");
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
    assert_eq!(errors.code.as_deref(), Some("graphql_errors"));
    assert_eq!(errors.operation, "graphql");

    decode("graphql_success_envelope_errors.json").expect_err("errors behind success envelope");
}
