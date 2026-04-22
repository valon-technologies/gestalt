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

const TRIGGER_JSON: &str = r#"{
    "id":"trg-1",
    "provider":"test-provider",
    "match":{"type":"dummy.event","source":"dummy","subject":"item"},
    "target":{
        "plugin":"dummy",
        "operation":"doit",
        "input":{"k":"v"}
    },
    "paused":false,
    "createdAt":"2026-04-20T00:00:00Z",
    "updatedAt":"2026-04-20T00:00:00Z"
}"#;

const RUN_JSON: &str = r#"{
    "id":"run-1",
    "provider":"test-provider",
    "status":"succeeded",
    "target":{
        "plugin":"dummy",
        "operation":"doit",
        "input":{"k":"v"}
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

#[test]
fn test_cli_lists_event_triggers() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/event-triggers",
        StatusCode::OK
    )
    .with_body(format!("[{TRIGGER_JSON}]"))
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["workflows", "triggers", "list"])
        .assert()
        .success()
        .stdout(predicate::str::contains("trg-1"))
        .stdout(predicate::str::contains("dummy"))
        .stdout(predicate::str::contains("doit"));
}

#[test]
fn test_cli_list_event_triggers_filters() {
    let body = r#"[
        {"id":"trg-a","match":{"type":"alpha.created"},"target":{"plugin":"alpha","operation":"x"},"paused":false},
        {"id":"trg-b","match":{"type":"beta.failed"},"target":{"plugin":"beta","operation":"y"},"paused":false}
    ]"#;
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/event-triggers",
        StatusCode::OK
    )
    .with_body(body)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflows",
            "triggers",
            "list",
            "--plugin",
            "beta",
            "--type",
            "beta.failed",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("trg-b"))
        .stdout(predicate::str::contains("trg-a").not());
}

#[test]
fn test_cli_gets_event_trigger() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/event-triggers/trg-1",
        StatusCode::OK
    )
    .with_body(TRIGGER_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["workflows", "triggers", "get", "trg-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("trg-1"))
        .stdout(predicate::str::contains("dummy"));
}

#[test]
fn test_cli_creates_event_trigger() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/workflow/event-triggers",
        StatusCode::CREATED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{
            "match":{"type":"dummy.event","source":"dummy","subject":"item"},
            "target":{"plugin":"dummy","operation":"doit","input":{"channel":"C1","text":"hi"}},
            "paused":false
        }"#
        .to_string(),
    ))
    .with_body(TRIGGER_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflows",
            "triggers",
            "create",
            "--type",
            "dummy.event",
            "--source",
            "dummy",
            "--subject",
            "item",
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
        .stdout(predicate::str::contains("trg-1"));
}

#[test]
fn test_cli_updates_event_trigger_merges_existing_fields() {
    let mut server = Server::new();
    let _get = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/event-triggers/trg-1",
        StatusCode::OK
    )
    .with_body(TRIGGER_JSON)
    .create();

    let _put = authed_json_mock!(
        server,
        Method::PUT,
        "/api/v1/workflow/event-triggers/trg-1",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{
            "provider":"test-provider",
            "match":{"type":"dummy.event.updated","source":"dummy","subject":"item"},
            "target":{"plugin":"dummy","operation":"doit","input":{"k":"v"}},
            "paused":true
        }"#
        .to_string(),
    ))
    .with_body(TRIGGER_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflows",
            "triggers",
            "update",
            "trg-1",
            "--type",
            "dummy.event.updated",
            "--paused",
        ])
        .assert()
        .success();
}

#[test]
fn test_cli_deletes_event_trigger() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/workflow/event-triggers/trg-1",
        StatusCode::OK
    )
    .with_body(r#"{"status":"deleted"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["workflows", "triggers", "delete", "trg-1"])
        .assert()
        .success()
        .stderr(predicate::str::contains("Workflow trigger trg-1 deleted."));
}

#[test]
fn test_cli_pauses_event_trigger() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/workflow/event-triggers/trg-1/pause",
        StatusCode::OK
    )
    .with_body(TRIGGER_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["workflows", "triggers", "pause", "trg-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("trg-1"));
}

#[test]
fn test_cli_resumes_event_trigger() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/workflow/event-triggers/trg-1/resume",
        StatusCode::OK
    )
    .with_body(TRIGGER_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["workflows", "triggers", "resume", "trg-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("trg-1"));
}

#[test]
fn test_cli_lists_runs() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(server, Method::GET, "/api/v1/workflow/runs", StatusCode::OK)
        .with_body(format!("[{RUN_JSON}]"))
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["workflows", "runs", "list"])
        .assert()
        .success()
        .stdout(predicate::str::contains("run-1"))
        .stdout(predicate::str::contains("dummy"))
        .stdout(predicate::str::contains("succeeded"));
}

#[test]
fn test_cli_list_runs_filters() {
    let body = r#"[
        {"id":"run-a","status":"running","target":{"plugin":"alpha","operation":"x"},"trigger":{"kind":"manual"}},
        {"id":"run-b","status":"failed","target":{"plugin":"beta","operation":"y"},"trigger":{"kind":"event","triggerId":"evt-1"}}
    ]"#;
    let mut server = Server::new();
    let _mock = authed_json_mock!(server, Method::GET, "/api/v1/workflow/runs", StatusCode::OK)
        .with_body(body)
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflows",
            "runs",
            "list",
            "--plugin",
            "beta",
            "--status",
            "failed",
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
        .args(["workflows", "runs", "get", "run-1"])
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
            "target":{"plugin":"dummy","operation":"doit"},
            "statusMessage":"operator requested"
        }"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflows",
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
            "workflows",
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
