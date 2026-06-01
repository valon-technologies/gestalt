mod support;

use support::*;

#[test]
fn test_cli_authorization_subjects_create_uses_public_subject_api() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/authorization/subjects",
        StatusCode::CREATED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"displayName":"Release Bot","id":"release-bot"}"#.to_string(),
    ))
    .with_body(
        r#"{"id":"release-bot","subjectId":"service_account:release-bot","kind":"service_account","displayName":"Release Bot"}"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "--format",
            "json",
            "authz",
            "subjects",
            "create",
            "release-bot",
            "--display-name",
            "Release Bot",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("service_account:release-bot"));

    mock.assert();
}

#[test]
fn test_cli_authorization_subjects_members_set_encodes_subject_path() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::PUT,
        "/api/v1/authorization/subjects/service_account%3Arelease-bot/members",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"role":"editor","subjectId":"user:alice"}"#.to_string(),
    ))
    .with_body(r#"{"subjectId":"user:alice","role":"editor","email":"alice@example.com"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "--format",
            "json",
            "authz",
            "subjects",
            "members",
            "set",
            "service_account:release-bot",
            "--subject-id",
            "user:alice",
            "--role",
            "editor",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("alice@example.com"));

    mock.assert();
}

#[test]
fn test_cli_authorization_subject_member_remove_path_encodes_space_as_percent_20() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/authorization/subjects/service_account%3Arelease-bot/members/user%3Aalice%20smith",
        StatusCode::OK
    )
    .with_body(r#"{"status":"deleted"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "--format",
            "json",
            "authz",
            "subjects",
            "members",
            "remove",
            "release-bot",
            "user:alice smith",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("deleted"));

    mock.assert();
}

#[test]
fn test_cli_authorization_subjects_tokens_create_groups_native_permissions() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/authorization/subjects/service_account%3Arelease-bot/tokens",
        StatusCode::CREATED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"name":"deploy","permissions":[{"operations":["issues.create"],"app":"github"},{"operations":["chat.postMessage"],"app":"slack"}]}"#.to_string(),
    ))
    .with_body(r#"{"id":"tok-1","name":"deploy","token":"plain-secret"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "authz",
            "subjects",
            "tokens",
            "create",
            "release-bot",
            "--name",
            "deploy",
            "--permission",
            "github:issues.create",
            "--permission",
            "slack:chat.postMessage",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("plain-secret"));

    mock.assert();
}

#[test]
fn test_cli_authorization_subjects_grants_set_surfaces_pending_reload() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::PUT,
        "/api/v1/authorization/subjects/service_account%3Arelease-bot/grants/github",
        StatusCode::ACCEPTED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(r#"{"role":"viewer"}"#.to_string()))
    .with_body(
        r#"{"status":"persisted_pending_reload","grant":{"app":"github","role":"viewer","source":"dynamic","mutable":true},"reloaded":false}"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "authz",
            "subjects",
            "grants",
            "set",
            "release-bot",
            "github",
            "--role",
            "viewer",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("github"))
        .stdout(predicate::str::contains("viewer"))
        .stderr(predicate::str::contains("snapshot has not reloaded"));

    mock.assert();
}

#[test]
fn test_cli_authorization_grants_list_uses_public_api() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/authorization/grants",
        StatusCode::OK
    )
    .with_body(
        r#"[{"id":"app/github","owner":{"kind":"app","app":"github"},"version":3,"relationships":[],"resourceTypes":{}}]"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["authz", "grants", "list"])
        .assert()
        .success()
        .stdout(predicate::str::contains("app/github"));

    mock.assert();
}

#[test]
fn test_cli_authorization_grants_get_encodes_grant_id() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/authorization/grants/app%2Fgithub",
        StatusCode::OK
    )
    .with_body(
        r#"{"id":"app/github","owner":{"kind":"app","app":"github"},"version":3,"relationships":[],"resourceTypes":{}}"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["authz", "grants", "get", "app/github"])
        .assert()
        .success()
        .stdout(predicate::str::contains("app/github"));

    mock.assert();
}

#[test]
fn test_cli_authorization_grants_put_reads_json_file() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::PUT,
        "/api/v1/authorization/grants/app%2Fgithub",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"id":"app/github","owner":{"kind":"app","app":"github"},"version":4,"resourceTypes":{},"relationships":[]}"#.to_string(),
    ))
    .with_body(
        r#"{"id":"app/github","owner":{"kind":"app","app":"github"},"version":4,"relationships":[],"resourceTypes":{}}"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    let grant_file = home.path().join("grant.json");
    std::fs::write(
        &grant_file,
        r#"{"id":"app/github","owner":{"kind":"app","app":"github"},"version":4,"resourceTypes":{},"relationships":[]}"#,
    )
    .unwrap();

    cli_command_for_server(home.path(), &server)
        .args([
            "authz",
            "grants",
            "put",
            "app/github",
            "--file",
            grant_file.to_str().unwrap(),
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("app/github"));

    mock.assert();
}

#[test]
fn test_cli_authorization_grants_delete_encodes_grant_id() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/authorization/grants/app%2Fgithub",
        StatusCode::OK
    )
    .with_body(r#"{"status":"deleted","persisted":true,"reloaded":true}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "--format",
            "json",
            "authz",
            "grants",
            "delete",
            "app/github",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("deleted"));

    mock.assert();
}

#[test]
fn test_cli_authorization_subjects_tokens_list_table_shows_permissions() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/authorization/subjects/service_account%3Arelease-bot/tokens",
        StatusCode::OK
    )
    .with_body(
        r#"[{"id":"tok-1","name":"deploy","scopes":"","permissions":[{"app":"github","operations":["issues.list"]}],"createdAt":"2026-05-12T00:00:00Z"}]"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["authz", "subjects", "tokens", "list", "release-bot"])
        .assert()
        .success()
        .stdout(predicate::str::contains("github"))
        .stdout(predicate::str::contains("issues.list"));

    mock.assert();
}

#[test]
fn test_cli_authorization_apps_members_set_uses_management_api() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::PUT,
        "/api/v1/authorization/apps/github/members",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"role":"triage","subjectId":"service_account:release-bot"}"#.to_string(),
    ))
    .with_body(
        r#"{"status":"ok","persisted":true,"reloaded":true,"membership":{"role":"triage","source":"dynamic","effective":true,"selectorKind":"subject_id","selectorValue":"service_account:release-bot"}}"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "--format",
            "json",
            "authz",
            "apps",
            "members",
            "set",
            "github",
            "--subject-id",
            "service_account:release-bot",
            "--role",
            "triage",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("persisted"));

    mock.assert();
}

#[test]
fn test_cli_authorization_relationships_list_maps_debug_filters() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/authorization/relationships?pageSize=10&pageToken=next&subjectType=subject&subjectId=user%3Aalice&relation=viewer&resourceType=app_dynamic&resourceId=github&modelId=model-1",
        StatusCode::OK
    )
    .with_body(
        r#"{"modelId":"model-1","relationships":[{"subject":{"type":"subject","id":"user:alice"},"relation":"viewer","resource":{"type":"app_dynamic","id":"github"},"managed":true}]}"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "--format",
            "json",
            "authz",
            "relationships",
            "list",
            "--page-size",
            "10",
            "--page-token",
            "next",
            "--subject-type",
            "subject",
            "--subject-id",
            "user:alice",
            "--relation",
            "viewer",
            "--resource-type",
            "app_dynamic",
            "--resource-id",
            "github",
            "--model-id",
            "model-1",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("app_dynamic"));

    mock.assert();
}

#[test]
fn test_cli_authorization_subjects_integrations_manual_pending_selection_uses_returned_url() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/authorization/subjects/service_account%3Arelease-bot/apps",
        StatusCode::OK
    )
    .with_body(
        r#"[{"name":"manual-svc","displayName":"Manual Service","connections":[{"name":"workspace","authTypes":["manual"],"credentialFields":[{"name":"token","label":"Token"}]}]}]"#,
    )
    .create();
    let _connect = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/authorization/subjects/service_account%3Arelease-bot/auth/connect-manual",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"workspace","credential":"abc123","integration":"manual-svc"}"#
            .to_string(),
    ))
    .with_body(
        r#"{
            "status":"selection_required",
            "integration":"manual-svc",
            "selectionUrl":"/api/v1/auth/pending-subject-selection?scope=subject",
            "pendingToken":"pending-123",
            "candidates":[
                {"id":"site-a","name":"Site A"},
                {"id":"site-b","name":"Site B"}
            ]
        }"#,
    )
    .create();
    let select = server
        .mock(
            Method::POST.as_str(),
            "/api/v1/auth/pending-subject-selection?scope=subject",
        )
        .match_header(
            header::AUTHORIZATION.as_str(),
            Matcher::Exact(test_bearer()),
        )
        .match_header(
            header::CONTENT_TYPE.as_str(),
            Matcher::Regex(format!("{}.*", http::APPLICATION_X_WWW_FORM_URLENCODED)),
        )
        .match_body(Matcher::Exact(
            "pending_token=pending-123&candidate_index=1".to_string(),
        ))
        .with_status(usize::from(StatusCode::OK.as_u16()))
        .with_header(header::CONTENT_TYPE.as_str(), http::TEXT_HTML)
        .with_body("<html>ok</html>")
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "authz",
            "subjects",
            "integrations",
            "connect",
            "service_account:release-bot",
            "manual-svc",
        ])
        .write_stdin("abc123\n2\n")
        .assert()
        .success()
        .stderr(predicate::str::contains("Connected manual-svc (Site B)"));

    select.assert();
}
