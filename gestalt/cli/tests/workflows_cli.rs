mod support;

use support::*;

const RUN_JSON: &str = r#"{
    "id":"run-1",
    "provider":"test-provider",
    "status":"succeeded",
    "target":{
        "plugin":{
            "name":"dummy",
            "operation":"doit",
            "input":{"k":"v"}
        }
    },
    "trigger":{"kind":"schedule","scheduleId":"sched-1"},
    "createdAt":"2026-04-20T00:00:00Z",
    "startedAt":"2026-04-20T00:01:00Z",
    "completedAt":"2026-04-20T00:02:00Z",
    "statusMessage":"done",
    "resultBody":"{\"ok\":true}"
}"#;

const PUBLISHED_EVENT_JSON: &str = r#"{
    "status":"published",
    "event":{
        "id":"evt-1",
        "type":"roadmap.item.updated",
        "source":"roadmap",
        "subject":"item",
        "specVersion":"1.0",
        "time":"2026-04-21T00:00:00Z",
        "data":{"id":"item-1"},
        "extensions":{"traceId":"trace-1"}
    }
}"#;

#[test]
fn test_cli_lists_runs() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(server, Method::GET, "/api/v1/workflow/runs", StatusCode::OK)
        .with_body(format!("[{RUN_JSON}]"))
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["workflow", "runs", "list"])
        .assert()
        .success()
        .stdout(predicate::str::contains("run-1"))
        .stdout(predicate::str::contains("dummy"))
        .stdout(predicate::str::contains("succeeded"));
}

#[test]
fn test_cli_list_runs_filters() {
    let body = r#"[
        {"id":"run-a","status":"running","target":{"plugin":{"name":"alpha","operation":"x"}},"trigger":{"kind":"manual"}},
        {"id":"run-b","status":"failed","target":{"plugin":{"name":"beta","operation":"y"}},"trigger":{"kind":"event","triggerId":"evt-1"}}
    ]"#;
    let mut server = Server::new();
    let _mock = authed_json_mock!(server, Method::GET, "/api/v1/workflow/runs", StatusCode::OK)
        .with_body(body)
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflow", "runs", "list", "--plugin", "beta", "--status", "failed",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("run-b"))
        .stdout(predicate::str::contains("run-a").not());
}

#[test]
fn test_cli_gets_run() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/runs/run-1",
        StatusCode::OK
    )
    .with_body(RUN_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["workflow", "runs", "get", "run-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("run-1"))
        .stdout(predicate::str::contains("succeeded"));
}

#[test]
fn test_cli_cancels_run() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/workflow/runs/run-1/cancel",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"reason":"operator requested"}"#.to_string(),
    ))
    .with_body(
        r#"{
            "id":"run-1",
            "provider":"test-provider",
            "status":"canceled",
            "target":{"plugin":{"name":"dummy","operation":"doit"}},
            "statusMessage":"operator requested"
        }"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflow",
            "runs",
            "cancel",
            "run-1",
            "--reason",
            "operator requested",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("run-1"))
        .stdout(predicate::str::contains("canceled"));
}

#[test]
fn test_cli_publishes_event() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/workflow/events",
        StatusCode::ACCEPTED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{
            "type":"roadmap.item.updated",
            "source":"roadmap",
            "subject":"item",
            "dataContentType":"application/json",
            "data":{"id":"item-1"},
            "extensions":{"traceId":"trace-1"}
        }"#
        .to_string(),
    ))
    .with_body(PUBLISHED_EVENT_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflow",
            "events",
            "publish",
            "--type",
            "roadmap.item.updated",
            "--source",
            "roadmap",
            "--subject",
            "item",
            "--data-content-type",
            "application/json",
            "-p",
            "id=item-1",
            "-e",
            "traceId=trace-1",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("evt-1"))
        .stdout(predicate::str::contains("roadmap.item.updated"));
}
