mod support;

use support::*;

const RUN_JSON: &str = r#"{
    "id":"run-1",
    "provider":"test-provider",
    "status":"succeeded",
    "target":{
        "steps":[{
            "id":"doit",
            "app":{
                "name":"dummy",
                "operation":"doit",
                "input":{"literal":{"k":"v"}}
            }
        }]
    },
    "trigger":{"kind":"schedule","activationId":"hourly"},
    "createdAt":"2026-04-20T00:00:00Z",
    "startedAt":"2026-04-20T00:01:00Z",
    "completedAt":"2026-04-20T00:02:00Z",
    "statusMessage":"done",
    "output":{"ok":true}
}"#;

const DELIVERED_EVENT_JSON: &str = r#"{
    "status":"delivered",
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
        .with_body(format!(
            r#"{{"runs":[{RUN_JSON}],"nextPageToken":"next-1"}}"#
        ))
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
    let body = r#"{
        "runs":[{"id":"run-b","status":"failed","target":{"steps":[{"id":"y","app":{"name":"beta","operation":"y"}}]},"trigger":{"kind":"event","activationId":"github_pr"}}],
        "nextPageToken":"next-filtered"
    }"#;
    let mut server = Server::new();
    let _mock = authed_json_mock!(server, Method::GET, "/api/v1/workflow/runs", StatusCode::OK)
        .match_query(Matcher::AllOf(vec![
            Matcher::UrlEncoded("app".into(), "beta".into()),
            Matcher::UrlEncoded("status".into(), "failed".into()),
            Matcher::UrlEncoded("pageSize".into(), "25".into()),
            Matcher::UrlEncoded("pageToken".into(), "cursor-1".into()),
        ]))
        .with_body(body)
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflow",
            "runs",
            "list",
            "--app",
            "beta",
            "--status",
            "failed",
            "--page-size",
            "25",
            "--page-token",
            "cursor-1",
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
            "target":{"steps":[{"id":"doit","app":{"name":"dummy","operation":"doit"}}]},
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
fn test_cli_delivers_event() {
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
    .with_body(DELIVERED_EVENT_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflow",
            "events",
            "deliver",
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
