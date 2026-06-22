mod support;

use support::*;

#[test]
fn test_list_tokens() {
    let mut server = Server::new();
    let mock = authed_json_mock!(server, Method::GET, "/api/v1/tokens", StatusCode::OK)
        .with_body(
            r#"[{"id":"grant-1","name":"grant-1","scopes":["my-app"],"createdAt":"2025-01-01T00:00:00Z","expiresAt":"2026-01-01T00:00:00Z"}]"#,
        )
        .create();

    let output = run_cli(&server, &["auth", "token", "list"]);
    mock.assert();
    assert!(
        output.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&output.stderr)
    );

    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(stdout.contains("grant-1"));
    assert!(stdout.contains("my-app"));
    assert!(stdout.contains("2025-01-01T00:00:00Z"));
    assert!(stdout.contains("2026-01-01T00:00:00Z"));
}

#[test]
fn test_list_tokens_without_expires_at() {
    let mut server = Server::new();
    let mock = authed_json_mock!(server, Method::GET, "/api/v1/tokens", StatusCode::OK)
        .with_body(
            r#"[{"id":"grant-2","name":"grant-2","scopes":["my-app"],"createdAt":"2025-01-01T00:00:00Z"}]"#,
        )
        .create();

    let output = run_cli(&server, &["auth", "token", "list"]);
    mock.assert();
    assert!(
        output.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&output.stderr)
    );

    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(stdout.contains("grant-2"));
    assert!(stdout.contains("never"));
}

#[test]
fn test_create_token() {
    let mut server = Server::new();
    let mock = authed_json_mock!(server, Method::POST, "/api/v1/tokens", StatusCode::CREATED)
        .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
        .match_body(Matcher::JsonString(
            r#"{"name":"cli-token","scopes":"my-app","expiresIn":2592000}"#.to_string(),
        ))
        .with_body(
            r#"{"id":"2","name":"cli-token","token":"plaintext-secret","scopes":["my-app"]}"#,
        )
        .create();

    let client = create_client(&server);
    let resp = client
        .create_api_token("cli-token", "my-app", 30 * 24 * 3600)
        .unwrap();

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
