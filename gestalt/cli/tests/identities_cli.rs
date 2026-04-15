mod support;

use std::sync::{Arc, Mutex};

use support::*;

#[test]
fn test_cli_lists_identities() {
    let mut server = Server::new();
    let _identities = authed_json_mock!(server, Method::GET, "/api/v1/identities", StatusCode::OK)
        .with_body(
            r#"[{"id":"id-1","displayName":"Release Bot","role":"admin","createdAt":"2026-04-15T00:00:00Z","updatedAt":"2026-04-15T00:00:00Z"}]"#,
        )
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url())
        .args(["identities", "list"])
        .assert()
        .success()
        .stdout(predicate::str::contains("Release Bot"))
        .stdout(predicate::str::contains("admin"));
}

#[test]
fn test_cli_creates_identity() {
    let mut server = Server::new();
    let _create =
        authed_json_mock!(server, Method::POST, "/api/v1/identities", StatusCode::CREATED)
            .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
            .match_body(Matcher::JsonString(
                r#"{"displayName":"Release Bot"}"#.to_string(),
            ))
            .with_body(
                r#"{"id":"id-1","displayName":"Release Bot","role":"admin","createdAt":"2026-04-15T00:00:00Z","updatedAt":"2026-04-15T00:00:00Z"}"#,
            )
            .create();

    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url())
        .args([
            "--format",
            "json",
            "identities",
            "create",
            "--name",
            "Release Bot",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains(r#""displayName": "Release Bot""#))
        .stdout(predicate::str::contains(r#""role": "admin""#));
}

#[test]
fn test_cli_adds_identity_member() {
    let mut server = Server::new();
    let _member = authed_json_mock!(
        server,
        Method::PUT,
        "/api/v1/identities/id-1/members",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"email":"viewer@example.test","role":"viewer"}"#.to_string(),
    ))
    .with_body(
        r#"{"userId":"user-2","email":"viewer@example.test","role":"viewer","createdAt":"2026-04-15T00:00:00Z","updatedAt":"2026-04-15T00:00:00Z"}"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url())
        .args([
            "--format",
            "json",
            "identities",
            "members",
            "add",
            "id-1",
            "viewer@example.test",
            "--role",
            "viewer",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains(
            r#""email": "viewer@example.test""#,
        ))
        .stdout(predicate::str::contains(r#""role": "viewer""#));
}

#[test]
fn test_cli_removes_identity_member_with_path_segment_encoding() {
    let mut server = Server::new();
    let _remove = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/identities/id-1/members/space%20cadet",
        StatusCode::OK
    )
    .with_body(r#"{"status":"deleted"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url())
        .args(["identities", "members", "remove", "id-1", "space cadet"])
        .assert()
        .success()
        .stderr(predicate::str::contains(
            "Removed member space cadet from identity id-1.",
        ));
}

#[test]
fn test_cli_sets_identity_grant() {
    let mut server = Server::new();
    let _grant = authed_json_mock!(
        server,
        Method::PUT,
        "/api/v1/identities/id-1/grants/github",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"operations":["issues.list","pulls.list"]}"#.to_string(),
    ))
    .with_body(
        r#"{"plugin":"github","operations":["issues.list","pulls.list"],"createdAt":"2026-04-15T00:00:00Z","updatedAt":"2026-04-15T00:00:00Z"}"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url())
        .args([
            "--format",
            "json",
            "identities",
            "grants",
            "set",
            "id-1",
            "github",
            "--operation",
            "issues.list",
            "--operation",
            "pulls.list",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains(r#""plugin": "github""#))
        .stdout(predicate::str::contains(r#""issues.list""#))
        .stdout(predicate::str::contains(r#""pulls.list""#));
}

#[test]
fn test_cli_lists_explicit_empty_grant_operations_as_plugin_wide_access() {
    let mut server = Server::new();
    let _grants = authed_json_mock!(server, Method::GET, "/api/v1/identities/id-1/grants", StatusCode::OK)
        .with_body(
            r#"[{"plugin":"github","operations":[],"createdAt":"2026-04-15T00:00:00Z","updatedAt":"2026-04-15T00:00:00Z"}]"#,
        )
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url())
        .args(["identities", "grants", "list", "id-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("github"))
        .stdout(predicate::str::contains("all"));
}

#[test]
fn test_cli_lists_identity_tokens() {
    let mut server = Server::new();
    let _tokens = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/identities/agent-1/tokens",
        StatusCode::OK
    )
    .with_body(
        r#"[{"id":"itok-1","name":"deploy-bot","permissions":[{"plugin":"github"},{"plugin":"slack","operations":["chat.postMessage","channels.history"]}],"createdAt":"2026-04-15T00:00:00Z"}]"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["identities", "tokens", "list", "agent-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("deploy-bot"))
        .stdout(predicate::str::contains("github"))
        .stdout(predicate::str::contains("slack:chat.po"))
        .stdout(predicate::str::contains("channels.hi"));
}

#[test]
fn test_cli_creates_identity_token() {
    let mut server = Server::new();
    let _create = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/identities/agent-1/tokens",
        StatusCode::CREATED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"name":"deploy-bot","permissions":[{"plugin":"github"},{"plugin":"slack","operations":["chat.postMessage","channels.history"]}]}"#.to_string(),
    ))
    .with_body(r#"{"id":"itok-2","name":"deploy-bot","token":"plaintext-secret"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "identities",
            "tokens",
            "create",
            "agent-1",
            "--name",
            "deploy-bot",
            "--permission",
            "github",
            "--permission",
            "slack:chat.postMessage,channels.history",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("plaintext-secret"))
        .stderr(predicate::str::contains("Token created"));
}

#[test]
fn test_cli_revokes_identity_token() {
    let mut server = Server::new();
    let _revoke = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/identities/agent-1/tokens/itok-2",
        StatusCode::OK
    )
    .with_body(r#"{"status":"revoked"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["identities", "tokens", "revoke", "agent-1", "itok-2"])
        .assert()
        .success()
        .stderr(predicate::str::contains(
            "Token itok-2 for identity agent-1 revoked.",
        ));
}

#[test]
fn test_cli_lists_identity_connections() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/identities/agent-1/integrations",
        StatusCode::OK
    )
    .with_body(
        r#"[{"name":"widget_metrics","description":"Metrics and logs","connected":true},{"name":"slack","description":"Team chat","connected":false}]"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["identities", "connections", "list", "agent-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("widget_metrics"))
        .stdout(predicate::str::contains("Metrics and logs"))
        .stdout(predicate::str::contains("slack"))
        .stdout(predicate::str::contains("yes"))
        .stdout(predicate::str::contains("no"));
}

#[test]
fn test_cli_connects_identity_connection_via_manual_auth() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/identities/agent-1/integrations",
        StatusCode::OK
    )
    .with_body(
        r#"[{
            "name":"widget_metrics",
            "displayName":"Widget Metrics",
            "description":"Metrics and logs",
            "authTypes":["manual"],
            "connectionParams":{"region":{"description":"API region","default":"us-east","required":true}},
            "credentialFields":[{"name":"api_key","label":"API key","description":"Use a personal API key"}]
        }]"#,
    )
    .create();
    let _connect = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/identities/agent-1/auth/connect-manual",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"workspace","connectionParams":{"region":"eu-west"},"credential":"wm-key","instance":"team-a","integration":"widget_metrics"}"#.to_string(),
    ))
    .with_body(r#"{"status":"connected","integration":"widget_metrics"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "identities",
            "connections",
            "connect",
            "agent-1",
            "widget_metrics",
            "--connection",
            "workspace",
            "--instance",
            "team-a",
        ])
        .write_stdin("eu-west\nwm-key\n")
        .assert()
        .success()
        .stderr(predicate::str::contains("API region"))
        .stderr(predicate::str::contains("API key"))
        .stderr(predicate::str::contains(
            "Connected widget_metrics for identity agent-1.",
        ));
}

#[test]
fn test_cli_disconnects_identity_connection_with_connection_and_instance() {
    let mut server = Server::new();
    let _disconnect = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/identities/agent-1/integrations/widget_metrics?connection=workspace&instance=team-a",
        StatusCode::OK
    )
    .with_body(r#"{"status":"disconnected"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "identities",
            "connections",
            "disconnect",
            "agent-1",
            "widget_metrics",
            "--connection",
            "workspace",
            "--instance",
            "team-a",
        ])
        .assert()
        .success()
        .stderr(predicate::str::contains(
            "Disconnected widget_metrics from identity agent-1.",
        ));
}

#[test]
fn test_identity_connect_starts_oauth_with_connection_and_instance() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/identities/agent-1/integrations",
        StatusCode::OK
    )
    .with_body(r#"[{"name":"acme_crm","authTypes":["oauth"],"connected":false}]"#)
    .create();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/identities/agent-1/auth/start-oauth",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"workspace","instance":"team-a","integration":"acme_crm"}"#.to_string(),
    ))
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::plugins::connect_identity_with_browser_opener(
        &client,
        "agent-1",
        "acme_crm",
        Some("workspace"),
        Some("team-a"),
        |_| Ok(()),
    );

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_identity_connect_prefers_oauth_when_manual_also_exists() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/identities/agent-1/integrations",
        StatusCode::OK
    )
    .with_body(r#"[{"name":"acme_crm","authTypes":["oauth","manual"],"connected":false}]"#)
    .create();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/identities/agent-1/auth/start-oauth",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"integration":"acme_crm"}"#.to_string(),
    ))
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let client = create_client(&server);
    let opened_url = Arc::new(Mutex::new(None));
    let opened_url_handle = Arc::clone(&opened_url);
    let result = gestalt::commands::plugins::connect_identity_with_browser_opener(
        &client,
        "agent-1",
        "acme_crm",
        None,
        None,
        move |url| {
            *opened_url_handle.lock().unwrap() = Some(url.to_string());
            Ok(())
        },
    );

    mock.assert();
    assert!(result.is_ok());
    assert_eq!(
        opened_url.lock().unwrap().as_deref(),
        Some("https://example.com/oauth")
    );
}
