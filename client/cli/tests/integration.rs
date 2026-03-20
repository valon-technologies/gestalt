use mockito::{Matcher, Server};

fn create_client(server: &Server) -> gestalt::api::ApiClient {
    gestalt::api::ApiClient::new(&server.url(), "test-token").unwrap()
}

#[test]
fn test_list_integrations() {
    let mut server = Server::new();
    let mock = server
        .mock("GET", "/api/v1/integrations")
        .match_header("Authorization", "Bearer test-token")
        .with_status(200)
        .with_header("Content-Type", "application/json")
        .with_body(
            r#"[{"name":"github","display_name":"GitHub","description":"GitHub integration"}]"#,
        )
        .create();

    let client = create_client(&server);
    let resp = client.get("/api/v1/integrations").unwrap();

    mock.assert();
    let items = resp.as_array().unwrap();
    assert_eq!(items.len(), 1);
    assert_eq!(items[0]["name"], "github");
}

#[test]
fn test_list_tokens() {
    let mut server = Server::new();
    let mock = server
        .mock("GET", "/api/v1/tokens")
        .match_header("Authorization", "Bearer test-token")
        .with_status(200)
        .with_header("Content-Type", "application/json")
        .with_body(
            r#"[{"id":"1","name":"my-token","scopes":"","created_at":"2025-01-01T00:00:00Z"}]"#,
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
    let mock = server
        .mock("POST", "/api/v1/tokens")
        .match_header("Authorization", "Bearer test-token")
        .match_header("Content-Type", "application/json")
        .match_body(Matcher::JsonString(r#"{"name":"cli-token"}"#.to_string()))
        .with_status(201)
        .with_header("Content-Type", "application/json")
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
    let mock = server
        .mock("DELETE", "/api/v1/tokens/42")
        .match_header("Authorization", "Bearer test-token")
        .with_status(200)
        .with_header("Content-Type", "application/json")
        .with_body(r#"{"status":"revoked"}"#)
        .create();

    let client = create_client(&server);
    let resp = client.delete("/api/v1/tokens/42").unwrap();

    mock.assert();
    assert_eq!(resp["status"], "revoked");
}

#[test]
fn test_execute_operation() {
    let mut server = Server::new();
    let mock = server
        .mock("POST", "/api/v1/github/search_code")
        .match_header("Authorization", "Bearer test-token")
        .match_header("Content-Type", "application/json")
        .with_status(200)
        .with_header("Content-Type", "application/json")
        .with_body(r#"{"results":[]}"#)
        .create();

    let client = create_client(&server);
    let body = serde_json::json!({"query": "hello"});
    let resp = client.post("/api/v1/github/search_code", &body).unwrap();

    mock.assert();
    assert_eq!(resp["results"], serde_json::json!([]));
}

#[test]
fn test_error_response() {
    let mut server = Server::new();
    let mock = server
        .mock("GET", "/api/v1/tokens")
        .with_status(401)
        .with_header("Content-Type", "application/json")
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
fn test_start_oauth() {
    let mut server = Server::new();
    let mock = server
        .mock("POST", "/api/v1/auth/start-oauth")
        .match_header("Authorization", "Bearer test-token")
        .with_status(200)
        .with_header("Content-Type", "application/json")
        .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
        .create();

    let client = create_client(&server);
    let body = serde_json::json!({"integration": "github"});
    let resp = client.post("/api/v1/auth/start-oauth", &body).unwrap();

    mock.assert();
    assert_eq!(resp["url"], "https://example.com/oauth");
    assert_eq!(resp["state"], "abc123");
}
