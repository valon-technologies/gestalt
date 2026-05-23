mod support;

use support::*;

const SCHEDULE_JSON: &str = r#"{
    "id":"sched-1",
    "provider":"test-provider",
    "cron":"0 0 * * *",
    "timezone":"UTC",
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
        "steps":[{
            "id":"doit",
            "app":{
                "name":"dummy",
                "operation":"doit",
                "input":{"literal":{"k":"v"}}
            }
        }]
    },
    "paused":false,
    "createdAt":"2026-04-20T00:00:00Z",
    "updatedAt":"2026-04-20T00:00:00Z"
}"#;

const AGENT_TRIGGER_JSON: &str = r#"{
    "id":"trg-agent",
    "provider":"local",
    "match":{"type":"roadmap.item.updated","source":"roadmap","subject":"item"},
    "target":{
        "steps":[{
            "id":"summarize",
            "agent":{
                "provider":"simple",
                "model":"fast",
                "prompt":{"template":"Summarize the updated roadmap item."},
                "tools":[{"app":"roadmap","operation":"items.get"}]
            }
        }]
    },
    "paused":false,
    "createdAt":"2026-04-20T00:00:00Z",
    "updatedAt":"2026-04-20T00:00:00Z"
}"#;

const MULTI_STEP_SCHEDULE_JSON: &str = r#"{
    "id":"sched-multi",
    "provider":"test-provider",
    "cron":"0 0 * * *",
    "timezone":"UTC",
    "target":{
        "steps":[
            {
                "id":"summarize",
                "agent":{
                    "provider":"simple",
                    "prompt":{"template":"Summarize the updated roadmap item."}
                }
            },
            {
                "id":"notify",
                "app":{
                    "name":"dummy",
                    "operation":"notify",
                    "input":{"literal":{"k":"v"}}
                }
            }
        ]
    },
    "paused":false,
    "createdAt":"2026-04-20T00:00:00Z",
    "updatedAt":"2026-04-20T00:00:00Z",
    "nextRunAt":"2026-04-21T00:00:00Z"
}"#;

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
        .args(["workflow", "schedules", "list"])
        .assert()
        .success()
        .stdout(predicate::str::contains("sched-1"))
        .stdout(predicate::str::contains("dummy"))
        .stdout(predicate::str::contains("doit"));

    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/schedules",
        StatusCode::OK
    )
    .with_body(format!("[{SCHEDULE_JSON}]"))
    .create();

    cli_command_for_server(home.path(), &server)
        .args(["workflows", "schedules", "list"])
        .assert()
        .success()
        .stdout(predicate::str::contains("sched-1"));
}

#[test]
fn test_cli_list_schedules_filters_by_app() {
    let body = r#"[
        {"id":"sched-a","provider":"p","cron":"* * * * *","target":{"steps":[{"id":"x","app":{"name":"alpha","operation":"x"}}]},"paused":false},
        {"id":"sched-b","provider":"p","cron":"* * * * *","target":{"steps":[{"id":"y","app":{"name":"beta","operation":"y"}}]},"paused":false},
        {"id":"sched-c","provider":"p","cron":"* * * * *","target":{"steps":[{"id":"inspect","agent":{"provider":"simple","prompt":{"template":"Inspect"}}},{"id":"z","app":{"name":"beta","operation":"z"}}]},"paused":false}
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
        .args(["workflow", "schedules", "list", "--app", "beta"])
        .assert()
        .success()
        .stdout(predicate::str::contains("sched-b"))
        .stdout(predicate::str::contains("sched-c"))
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
        .args(["workflow", "schedules", "get", "sched-1"])
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
            "target":{"steps":[{"id":"doit","app":{"name":"dummy","operation":"doit","input":{"literal":{"channel":"C1","text":"hi"}}}}]},
            "paused":false
        }"#
        .to_string(),
    ))
    .with_body(SCHEDULE_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflow",
            "schedules",
            "create",
            "--cron",
            "0 */5 * * *",
            "--timezone",
            "UTC",
            "--app",
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
            "target":{"steps":[{"id":"doit","app":{"name":"dummy","operation":"doit","input":{"literal":{"k":"v"}}}}]},
            "paused":true
        }"#
        .to_string(),
    ))
    .with_body(SCHEDULE_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflow",
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
fn test_cli_updates_single_app_schedule_preserves_step_fields() {
    let existing = r#"{
        "id":"sched-custom",
        "provider":"test-provider",
        "cron":"0 0 * * *",
        "timezone":"UTC",
        "target":{
            "steps":[{
                "id":"custom-step",
                "inputs":{"item":{"runInput":"item"}},
                "app":{
                    "name":"dummy",
                    "operation":"doit",
                    "credentialMode":"user",
                    "input":{"literal":{"k":"v"}}
                },
                "when":{"value":{"runInput":"enabled"},"equals":true},
                "timeoutSeconds":30,
                "metadata":{"owner":"ops"}
            }]
        },
        "paused":false
    }"#;
    let mut server = Server::new();
    let _get = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/schedules/sched-custom",
        StatusCode::OK
    )
    .with_body(existing)
    .create();

    let _put = authed_json_mock!(
        server,
        Method::PUT,
        "/api/v1/workflow/schedules/sched-custom",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{
            "cron":"0 0 * * *",
            "timezone":"UTC",
            "target":{
                "steps":[{
                    "id":"custom-step",
                    "inputs":{"item":{"runInput":"item"}},
                    "app":{
                        "name":"dummy",
                        "operation":"doit.updated",
                        "credentialMode":"user",
                        "connection":"prod",
                        "input":{"literal":{"k":"v"}}
                    },
                    "when":{"value":{"runInput":"enabled"},"equals":true},
                    "timeoutSeconds":30,
                    "metadata":{"owner":"ops"}
                }]
            },
            "paused":false
        }"#
        .to_string(),
    ))
    .with_body(existing)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflow",
            "schedules",
            "update",
            "sched-custom",
            "--operation",
            "doit.updated",
            "--connection",
            "prod",
        ])
        .assert()
        .success();
}

#[test]
fn test_cli_updates_schedule_preserves_multistep_target_without_target_flags() {
    let mut server = Server::new();
    let _get = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/schedules/sched-multi",
        StatusCode::OK
    )
    .with_body(MULTI_STEP_SCHEDULE_JSON)
    .create();

    let _put = authed_json_mock!(
        server,
        Method::PUT,
        "/api/v1/workflow/schedules/sched-multi",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{
            "cron":"15 * * * *",
            "timezone":"UTC",
            "target":{
                "steps":[
                    {"id":"summarize","agent":{"provider":"simple","prompt":{"template":"Summarize the updated roadmap item."}}},
                    {"id":"notify","app":{"name":"dummy","operation":"notify","input":{"literal":{"k":"v"}}}}
                ]
            },
            "paused":false
        }"#
        .to_string(),
    ))
    .with_body(MULTI_STEP_SCHEDULE_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflow",
            "schedules",
            "update",
            "sched-multi",
            "--cron",
            "15 * * * *",
        ])
        .assert()
        .success();
}

#[test]
fn test_cli_rejects_partial_schedule_target_update_for_multistep_target() {
    let mut server = Server::new();
    let _get = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/schedules/sched-multi",
        StatusCode::OK
    )
    .with_body(MULTI_STEP_SCHEDULE_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflow",
            "schedules",
            "update",
            "sched-multi",
            "--operation",
            "notify.updated",
        ])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "cannot apply app step target flags to an existing non-app or multi-step schedule",
        ));
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
        .args(["workflow", "schedules", "delete", "sched-1"])
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
        .args(["workflow", "schedules", "pause", "sched-1"])
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
        .args(["workflow", "schedules", "resume", "sched-1"])
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
        .args(["--format", "json", "workflow", "schedules", "list"])
        .assert()
        .success()
        .stdout(predicate::str::contains(r#""id": "sched-1""#))
        .stdout(predicate::str::contains(r#""name": "dummy""#));
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
        .args(["workflow", "triggers", "list"])
        .assert()
        .success()
        .stdout(predicate::str::contains("trg-1"))
        .stdout(predicate::str::contains("dummy"))
        .stdout(predicate::str::contains("doit"));
}

#[test]
fn test_cli_list_event_triggers_filters() {
    let body = r#"[
        {"id":"trg-a","match":{"type":"alpha.created"},"target":{"steps":[{"id":"x","app":{"name":"alpha","operation":"x"}}]},"paused":false},
        {"id":"trg-b","match":{"type":"beta.failed"},"target":{"steps":[{"id":"y","app":{"name":"beta","operation":"y"}}]},"paused":false}
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
            "workflow",
            "triggers",
            "list",
            "--app",
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
        .args(["workflow", "triggers", "get", "trg-1"])
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
            "target":{"steps":[{"id":"doit","app":{"name":"dummy","operation":"doit","input":{"literal":{"channel":"C1","text":"hi"}}}}]},
            "paused":false
        }"#
        .to_string(),
    ))
    .with_body(TRIGGER_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflow",
            "triggers",
            "create",
            "--type",
            "dummy.event",
            "--source",
            "dummy",
            "--subject",
            "item",
            "--app",
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
fn test_cli_creates_event_trigger_from_target_file() {
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
            "provider":"local",
            "match":{"type":"roadmap.item.updated","source":"roadmap","subject":"item"},
            "target":{
                "steps":[{
                    "id":"summarize",
                    "agent":{
                        "provider":"simple",
                        "model":"fast",
                        "prompt":"Summarize the updated roadmap item.",
                        "tools":[{"app":"roadmap","operation":"items.get"}]
                    }
                }]
            },
            "paused":false
        }"#
        .to_string(),
    ))
    .with_body(AGENT_TRIGGER_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "--format",
            "json",
            "workflow",
            "triggers",
            "create",
            "--provider",
            "local",
            "--type",
            "roadmap.item.updated",
            "--source",
            "roadmap",
            "--subject",
            "item",
            "--target-file",
            "-",
        ])
        .write_stdin(
            r#"{
                "steps": [{
                    "id": "summarize",
                    "agent": {
                        "provider": "simple",
                        "model": "fast",
                        "prompt": "Summarize the updated roadmap item.",
                        "tools": [{"app": "roadmap", "operation": "items.get"}]
                    }
                }]
            }"#,
        )
        .assert()
        .success()
        .stdout(predicate::str::contains(r#""id": "trg-agent""#));
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
            "target":{"steps":[{"id":"doit","app":{"name":"dummy","operation":"doit","input":{"literal":{"k":"v"}}}}]},
            "paused":true
        }"#
        .to_string(),
    ))
    .with_body(TRIGGER_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflow",
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
fn test_cli_updates_event_trigger_preserves_agent_target_without_target_flags() {
    let mut server = Server::new();
    let _get = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/event-triggers/trg-agent",
        StatusCode::OK
    )
    .with_body(AGENT_TRIGGER_JSON)
    .create();

    let _put = authed_json_mock!(
        server,
        Method::PUT,
        "/api/v1/workflow/event-triggers/trg-agent",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{
            "provider":"local",
            "match":{"type":"roadmap.item.changed","source":"roadmap","subject":"item"},
            "target":{
                "steps":[{
                    "id":"summarize",
                    "agent":{
                        "provider":"simple",
                        "model":"fast",
                        "prompt":{"template":"Summarize the updated roadmap item."},
                        "tools":[{"app":"roadmap","operation":"items.get"}]
                    }
                }]
            },
            "paused":false
        }"#
        .to_string(),
    ))
    .with_body(AGENT_TRIGGER_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflow",
            "triggers",
            "update",
            "trg-agent",
            "--type",
            "roadmap.item.changed",
        ])
        .assert()
        .success();
}

#[test]
fn test_cli_rejects_partial_trigger_target_update_for_non_app_target() {
    let mut server = Server::new();
    let _get = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/workflow/event-triggers/trg-agent",
        StatusCode::OK
    )
    .with_body(AGENT_TRIGGER_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "workflow",
            "triggers",
            "update",
            "trg-agent",
            "--operation",
            "notify.updated",
        ])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "cannot apply app step target flags to an existing non-app or multi-step trigger",
        ));
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
        .args(["workflow", "triggers", "delete", "trg-1"])
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
        .args(["workflow", "triggers", "pause", "trg-1"])
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
        .args(["workflow", "triggers", "resume", "trg-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("trg-1"));
}

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
        "runs":[{"id":"run-b","status":"failed","target":{"steps":[{"id":"y","app":{"name":"beta","operation":"y"}}]},"trigger":{"kind":"event","triggerId":"evt-1"}}],
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
