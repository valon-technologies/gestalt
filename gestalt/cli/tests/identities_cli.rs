mod support;

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
fn test_cli_gets_identity() {
    let mut server = Server::new();
    let _get = authed_json_mock!(server, Method::GET, "/api/v1/identities/id-1", StatusCode::OK)
        .with_body(
            r#"{"id":"id-1","displayName":"Release Bot","role":"admin","createdAt":"2026-04-15T00:00:00Z","updatedAt":"2026-04-16T00:00:00Z"}"#,
        )
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["identities", "get", "id-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("Release Bot"))
        .stdout(predicate::str::contains("2026-04-16T00:00:00Z"));
}

#[test]
fn test_cli_get_identity_preserves_provider_passthrough_json() {
    let mut server = Server::new();
    let _get = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/identities/read",
        StatusCode::OK
    )
    .with_body(r#"{"operation":"read","ok":true}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["--format", "json", "identities", "get", "read"])
        .assert()
        .success()
        .stdout(predicate::str::contains(r#""operation": "read""#))
        .stdout(predicate::str::contains(r#""ok": true"#));
}

#[test]
fn test_cli_updates_identity() {
    let mut server = Server::new();
    let _update = authed_json_mock!(server, Method::PATCH, "/api/v1/identities/id-1", StatusCode::OK)
        .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
        .match_body(Matcher::JsonString(
            r#"{"displayName":"Release Automation"}"#.to_string(),
        ))
        .with_body(
            r#"{"id":"id-1","displayName":"Release Automation","role":"admin","createdAt":"2026-04-15T00:00:00Z","updatedAt":"2026-04-16T00:00:00Z"}"#,
        )
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "--format",
            "json",
            "identities",
            "update",
            "id-1",
            "--name",
            "Release Automation",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains(
            r#""displayName": "Release Automation""#,
        ))
        .stdout(predicate::str::contains(
            r#""updatedAt": "2026-04-16T00:00:00Z""#,
        ));
}

#[test]
fn test_cli_update_identity_preserves_provider_passthrough_json() {
    let mut server = Server::new();
    let _update = authed_json_mock!(
        server,
        Method::PATCH,
        "/api/v1/identities/update",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"displayName":"Updated"}"#.to_string(),
    ))
    .with_body(r#"{"operation":"update","accepted":true}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "--format",
            "json",
            "identities",
            "update",
            "update",
            "--name",
            "Updated",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains(r#""operation": "update""#))
        .stdout(predicate::str::contains(r#""accepted": true"#));
}

#[test]
fn test_cli_deletes_identity() {
    let mut server = Server::new();
    let _delete = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/identities/id-1",
        StatusCode::OK
    )
    .with_body(r#"{"status":"deleted"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["identities", "delete", "id-1"])
        .assert()
        .success()
        .stderr(predicate::str::contains("Identity id-1 deleted."));
}

#[test]
fn test_cli_delete_identity_falls_back_for_provider_passthrough_table_output() {
    let mut server = Server::new();
    let _delete = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/identities/delete",
        StatusCode::OK
    )
    .with_body(r#"{"operation":"delete","ok":true}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["identities", "delete", "delete"])
        .assert()
        .success()
        .stdout(predicate::str::contains("operation"))
        .stdout(predicate::str::contains("delete"))
        .stderr(predicate::str::contains("Identity delete deleted.").not());
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
        r#"{"subjectId":"user:user-2","email":"viewer@example.test","role":"viewer","createdAt":"2026-04-15T00:00:00Z","updatedAt":"2026-04-15T00:00:00Z"}"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url())
        .args([
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
        .stdout(predicate::str::contains("Subject ID"))
        .stdout(predicate::str::contains("user:user-2"))
        .stdout(predicate::str::contains("viewer"));
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
fn test_cli_identities_help_lists_first_class_subcommands() {
    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .args(["identities", "--help"])
        .assert()
        .success()
        .stdout(predicate::str::contains(
            "Manage workspace-owned identities",
        ))
        .stdout(predicate::str::contains("list"))
        .stdout(predicate::str::contains("get"))
        .stdout(predicate::str::contains("update"))
        .stdout(predicate::str::contains("members"))
        .stdout(predicate::str::contains("grants"))
        .stdout(predicate::str::contains("tokens"));
}

#[test]
fn test_cli_identity_tokens_help_calls_out_role_requirements() {
    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .args(["identities", "tokens", "--help"])
        .assert()
        .success()
        .stdout(predicate::str::contains(
            "List API tokens for an identity (`viewer` or higher)",
        ))
        .stdout(predicate::str::contains(
            "Create an API token for an identity (`viewer` or higher)",
        ))
        .stdout(predicate::str::contains(
            "Revoke an API token owned by an identity (`editor` or higher)",
        ));
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
fn test_cli_list_identity_tokens_json_preserves_scopes() {
    let mut server = Server::new();
    let _tokens = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/identities/agent-1/tokens",
        StatusCode::OK
    )
    .with_body(
        r#"[{"id":"itok-1","name":"deploy-bot","scopes":"github slack","permissions":[{"plugin":"github"}],"createdAt":"2026-04-15T00:00:00Z"}]"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "--format",
            "json",
            "identities",
            "tokens",
            "list",
            "agent-1",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains(r#""scopes": "github slack""#))
        .stdout(predicate::str::contains(r#""plugin": "github""#));
}

#[test]
fn test_cli_list_identity_tokens_falls_back_for_provider_passthrough_table_output() {
    let mut server = Server::new();
    let _tokens = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/identities/agent-1/tokens",
        StatusCode::OK
    )
    .with_body(r#"{"operation":"list_tokens","ok":true}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["identities", "tokens", "list", "agent-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("operation"))
        .stdout(predicate::str::contains("list_tokens"));
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
