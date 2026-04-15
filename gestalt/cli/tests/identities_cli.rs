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
