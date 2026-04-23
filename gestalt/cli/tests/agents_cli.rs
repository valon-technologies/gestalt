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

const RUN_EVENTS_JSON: &str = r#"[
    {
        "id":"evt-1",
        "runId":"run-1",
        "seq":1,
        "type":"agent.run.started",
        "source":"managed",
        "visibility":"public",
        "data":{"status":"running"},
        "createdAt":"2026-04-22T00:00:01Z"
    },
    {
        "id":"evt-2",
        "runId":"run-1",
        "seq":2,
        "type":"agent.message.delta",
        "source":"managed",
        "visibility":"public",
        "data":{"text":"risk is dependency churn"},
        "createdAt":"2026-04-22T00:00:02Z"
    }
]"#;

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
fn test_cli_creates_agent_run_from_request_file() {
    let request_json = r#"{
        "provider":"managed",
        "model":"gpt-5.4",
        "toolSource":"explicit",
        "sessionRef":"session-1",
        "metadata":{"ticket":"AIT-123"},
        "providerOptions":{"temperature":0.2},
        "responseSchema":{
            "type":"object",
            "properties":{"summary":{"type":"string"}},
            "required":["summary"]
        },
        "messages":[
            {"role":"user","text":"Summarize the roadmap risk."}
        ],
        "toolRefs":[
            {
                "pluginName":"roadmap",
                "operation":"sync",
                "connection":"workspace",
                "instance":"prod",
                "title":"Roadmap sync",
                "description":"Read roadmap state"
            }
        ]
    }"#;
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/agent/runs",
        StatusCode::CREATED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(request_json.to_string()))
    .with_body(RUN_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    let request_path = home.path().join("agent-request.json");
    std::fs::write(&request_path, request_json).unwrap();

    cli_command_for_server(home.path(), &server)
        .args([
            "agent",
            "runs",
            "create",
            "--request-file",
            request_path.to_str().unwrap(),
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("run-1"));
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

#[test]
fn test_cli_lists_agent_run_events() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/agent/runs/run-1/events?after=0&limit=10",
        StatusCode::OK
    )
    .with_body(RUN_EVENTS_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "--format", "json", "agent", "runs", "events", "list", "run-1", "--after", "0",
            "--limit", "10",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("agent.message.delta"))
        .stdout(predicate::str::contains("risk is dependency churn"));
}

#[test]
fn test_cli_streams_agent_run_events() {
    let mut server = Server::new();
    let _mock = server
        .mock(
            Method::GET.as_str(),
            "/api/v1/agent/runs/run-1/events/stream?after=2&limit=1",
        )
        .match_header(header::AUTHORIZATION.as_str(), Matcher::Exact(test_bearer()))
        .with_status(usize::from(StatusCode::OK.as_u16()))
        .with_header(header::CONTENT_TYPE.as_str(), "text/event-stream")
        .with_body(
            "id: 3\nevent: agent.run.completed\ndata: {\"seq\":3,\"type\":\"agent.run.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        )
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "agent", "runs", "events", "stream", "run-1", "--after", "2", "--limit", "1",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("event: agent.run.completed"))
        .stdout(predicate::str::contains(r#""status":"succeeded""#));
}
