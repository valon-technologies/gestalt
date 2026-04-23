mod support;

use support::*;

const RUN_JSON: &str = r#"{
    "id":"run-1",
    "provider":"managed",
    "model":"gpt-5.4",
    "status":"running",
    "messages":[
        {"role":"system","text":"Be concise."},
        {"role":"user","text":"Summarize the roadmap risk."}
    ],
    "sessionRef":"session-1",
    "createdAt":"2026-04-22T00:00:00Z",
    "executionRef":"run-1"
}"#;

#[test]
fn test_cli_creates_agent_run() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/agent/runs",
        StatusCode::CREATED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{
                "provider":"managed",
                "model":"gpt-5.4",
                "idempotencyKey":"agent-create-1",
                "messages":[
                    {"role":"system","text":"Be concise."},
                    {"role":"user","text":"Summarize the roadmap risk."}
                ],
                "toolRefs":[
                    {"pluginName":"roadmap","operation":"sync"}
                ]
            }"#
        .to_string(),
    ))
    .with_body(RUN_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "agent",
            "runs",
            "create",
            "--provider",
            "managed",
            "--model",
            "gpt-5.4",
            "--system",
            "Be concise.",
            "--message",
            "Summarize the roadmap risk.",
            "--tool",
            "roadmap:sync",
            "--idempotency-key",
            "agent-create-1",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("run-1"))
        .stdout(predicate::str::contains("managed"));
}

#[test]
fn test_cli_lists_agent_runs_with_server_side_filters() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/agent/runs?provider=managed&status=running",
        StatusCode::OK
    )
        .with_body(
            r#"[
                {"id":"run-a","provider":"managed","model":"gpt-5.4","status":"running","createdAt":"2026-04-22T00:00:00Z"}
            ]"#,
        )
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "agent",
            "runs",
            "list",
            "--provider",
            "managed",
            "--status",
            "running",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("run-a"));
}

#[test]
fn test_cli_gets_agent_run() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/agent/runs/run-1",
        StatusCode::OK
    )
    .with_body(RUN_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["agent", "runs", "get", "run-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("run-1"))
        .stdout(predicate::str::contains("gpt-5.4"));
}

#[test]
fn test_cli_cancels_agent_run() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/agent/runs/run-1/cancel",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(r#"{"reason":"stop now"}"#.to_string()))
    .with_body(
        r#"{
                    "id":"run-1",
                    "provider":"managed",
                    "model":"gpt-5.4",
                    "status":"canceled",
                    "createdAt":"2026-04-22T00:00:00Z"
                }"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["agent", "runs", "cancel", "run-1", "--reason", "stop now"])
        .assert()
        .success()
        .stdout(predicate::str::contains("canceled"));
}
