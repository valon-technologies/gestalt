mod support;

use support::*;

const SCHEDULE_JSON: &str = r#"{
    "id":"sched-1",
    "provider":"test-provider",
    "cron":"0 0 * * *",
    "timezone":"UTC",
    "target":{
        "plugin":"dummy",
        "operation":"doit",
        "input":{"k":"v"}
    },
    "paused":false,
    "createdAt":"2026-04-20T00:00:00Z",
    "updatedAt":"2026-04-20T00:00:00Z",
    "nextRunAt":"2026-04-21T00:00:00Z"
}"#;

#[test]
fn test_cli_lists_schedules() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/schedules",
        StatusCode::OK
    )
    .with_body(format!("[{SCHEDULE_JSON}]"))
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["workflows", "schedules", "list"])
        .assert()
        .success()
        .stdout(predicate::str::contains("sched-1"))
        .stdout(predicate::str::contains("dummy"))
        .stdout(predicate::str::contains("doit"));
}

#[test]
fn test_cli_list_schedules_filters_by_plugin() {
    let body = r#"[
        {"id":"sched-a","provider":"p","cron":"* * * * *","target":{"plugin":"alpha","operation":"x"},"paused":false},
        {"id":"sched-b","provider":"p","cron":"* * * * *","target":{"plugin":"beta","operation":"y"},"paused":false}
    ]"#;
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/schedules",
        StatusCode::OK
    )
    .with_body(body)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["workflows", "schedules", "list", "--plugin", "beta"])
        .assert()
        .success()
        .stdout(predicate::str::contains("sched-b"))
        .stdout(predicate::str::contains("sched-a").not());
}

#[test]
fn test_cli_gets_schedule() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/schedules/sched-1",
        StatusCode::OK
    )
    .with_body(SCHEDULE_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["workflows", "schedules", "get", "sched-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("sched-1"))
        .stdout(predicate::str::contains("dummy"));
}

#[test]
fn test_cli_creates_schedule() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/workflow/schedules",
        StatusCode::CREATED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{
            "cron":"0 */5 * * *",
            "timezone":"UTC",
            "target":{"plugin":"dummy","operation":"doit","input":{"channel":"C1","text":"hi"}},
            "paused":false
        }"#
        .to_string(),
    ))
    .with_body(SCHEDULE_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflows",
            "schedules",
            "create",
            "--cron",
            "0 */5 * * *",
            "--timezone",
            "UTC",
            "--plugin",
            "dummy",
            "--operation",
            "doit",
            "-p",
            "channel=C1",
            "-p",
            "text=hi",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("sched-1"));
}

#[test]
fn test_cli_updates_schedule_merges_existing_fields() {
    let mut server = Server::new();
    let _get = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/schedules/sched-1",
        StatusCode::OK
    )
    .with_body(SCHEDULE_JSON)
    .create();

    let _put = authed_json_mock!(
        server,
        Method::PUT,
        "/api/v1/workflow/schedules/sched-1",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{
            "cron":"15 * * * *",
            "timezone":"UTC",
            "target":{"plugin":"dummy","operation":"doit","input":{"k":"v"}},
            "paused":true
        }"#
        .to_string(),
    ))
    .with_body(SCHEDULE_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflows",
            "schedules",
            "update",
            "sched-1",
            "--cron",
            "15 * * * *",
            "--paused",
        ])
        .assert()
        .success();
}

#[test]
fn test_cli_deletes_schedule() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/workflow/schedules/sched-1",
        StatusCode::OK
    )
    .with_body(r#"{"status":"deleted"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["workflows", "schedules", "delete", "sched-1"])
        .assert()
        .success()
        .stderr(predicate::str::contains(
            "Workflow schedule sched-1 deleted.",
        ));
}

#[test]
fn test_cli_pauses_schedule() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/workflow/schedules/sched-1/pause",
        StatusCode::OK
    )
    .with_body(SCHEDULE_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["workflows", "schedules", "pause", "sched-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("sched-1"));
}

#[test]
fn test_cli_resumes_schedule() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/workflow/schedules/sched-1/resume",
        StatusCode::OK
    )
    .with_body(SCHEDULE_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["workflows", "schedules", "resume", "sched-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("sched-1"));
}

#[test]
fn test_cli_list_schedules_json_format() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/schedules",
        StatusCode::OK
    )
    .with_body(format!("[{SCHEDULE_JSON}]"))
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["--format", "json", "workflows", "schedules", "list"])
        .assert()
        .success()
        .stdout(predicate::str::contains(r#""id": "sched-1""#))
        .stdout(predicate::str::contains(r#""plugin": "dummy""#));
}
