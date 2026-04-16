mod support;

use support::*;

#[test]
fn test_list_tokens() {
    let mut server = Server::new();
    let mock = authed_json_mock!(server, Method::GET, "/api/v1/tokens", StatusCode::OK)
        .with_body(
            r#"[{"id":"1","name":"my-token","scopes":"","createdAt":"2025-01-01T00:00:00Z"}]"#,
        )
        .create();

    let client = create_client(&server);
    let resp = client.get("/api/v1/tokens").unwrap();

    mock.assert();
    let items = resp.as_array().unwrap();
    assert_eq!(items.len(), 1);
    assert_eq!(items[0]["name"], "my-token");
}

#[test]
fn test_create_token() {
    let mut server = Server::new();
    let mock = authed_json_mock!(server, Method::POST, "/api/v1/tokens", StatusCode::CREATED)
        .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
        .match_body(Matcher::JsonString(r#"{"name":"cli-token"}"#.to_string()))
        .with_body(r#"{"id":"2","name":"cli-token","token":"plaintext-secret"}"#)
        .create();

    let client = create_client(&server);
    let body = serde_json::json!({"name": "cli-token"});
    let resp = client.post("/api/v1/tokens", &body).unwrap();

    mock.assert();
    assert_eq!(resp["token"], "plaintext-secret");
}

#[test]
fn test_revoke_token() {
    let mut server = Server::new();
    let mock = authed_json_mock!(server, Method::DELETE, "/api/v1/tokens/42", StatusCode::OK)
        .with_body(r#"{"status":"revoked"}"#)
        .create();

    let client = create_client(&server);
    let resp = client.delete("/api/v1/tokens/42").unwrap();

    mock.assert();
    assert_eq!(resp["status"], "revoked");
}

#[test]
fn test_error_response() {
    let mut server = Server::new();
    let mock = json_mock!(
        server,
        Method::GET,
        "/api/v1/tokens",
        StatusCode::UNAUTHORIZED
    )
    .with_body(r#"{"error":"missing authorization header"}"#)
    .create();

    let client = create_client(&server);
    let result = client.get("/api/v1/tokens");

    mock.assert();
    assert!(result.is_err());
    let err = result.unwrap_err().to_string();
    assert!(err.contains("missing authorization header"));
}

#[test]
fn test_error_response_nested_message() {
    let mut server = Server::new();
    let mock = json_mock!(
        server,
        Method::GET,
        "/api/v1/tokens",
        StatusCode::BAD_REQUEST
    )
    .with_body(r#"{"error":{"message":"invalid parameter: limit"}}"#)
    .create();

    let client = create_client(&server);
    let result = client.get("/api/v1/tokens");

    mock.assert();
    assert!(result.is_err());
    let err = result.unwrap_err().to_string();
    assert!(err.contains("invalid parameter: limit"));
}

#[test]
fn test_connection_error_shows_actionable_message() {
    let client = gestalt::api::ApiClient::new("http://127.0.0.1:1", TEST_TOKEN).unwrap();
    let err = client.get("/api/v1/tokens").unwrap_err().to_string();
    assert!(
        err.contains("could not reach server at http://127.0.0.1:1"),
        "unexpected error: {err}"
    );
    assert!(err.contains("gestalt auth status"));
}
