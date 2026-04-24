mod support;

use support::*;

const SESSION_JSON: &str = r#"{
    "id":"session-1",
    "provider":"managed",
    "model":"gpt-5.4",
    "state":"active",
    "clientRef":"cli-session-1",
    "createdAt":"2026-04-22T00:00:00Z",
    "updatedAt":"2026-04-22T00:00:00Z"
}"#;

const TURN_JSON: &str = r#"{
    "id":"turn-1",
    "sessionId":"session-1",
    "provider":"managed",
    "model":"gpt-5.4",
    "status":"running",
    "messages":[
        {"role":"system","text":"Be concise."},
        {"role":"user","text":"Summarize the roadmap risk."}
    ],
    "createdAt":"2026-04-22T00:00:00Z",
    "executionRef":"turn-1"
}"#;

const TURN_EVENTS_JSON: &str = r#"[
    {
        "id":"evt-1",
        "turnId":"turn-1",
        "seq":1,
        "type":"turn.started",
        "source":"managed",
        "visibility":"public",
        "data":{"status":"running"},
        "createdAt":"2026-04-22T00:00:01Z"
    },
    {
        "id":"evt-2",
        "turnId":"turn-1",
        "seq":2,
        "type":"agent.message.delta",
        "source":"managed",
        "visibility":"public",
        "data":{"text":"risk is dependency churn"},
        "createdAt":"2026-04-22T00:00:02Z"
    }
]"#;

#[test]
fn test_cli_creates_agent_session() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/agent/sessions",
        StatusCode::CREATED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{
                "provider":"managed",
                "model":"gpt-5.4",
                "clientRef":"cli-session-1",
                "idempotencyKey":"session-create-1"
            }"#
        .to_string(),
    ))
    .with_body(SESSION_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "agent",
            "sessions",
            "create",
            "--provider",
            "managed",
            "--model",
            "gpt-5.4",
            "--client-ref",
            "cli-session-1",
            "--idempotency-key",
            "session-create-1",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("session-1"))
        .stdout(predicate::str::contains("managed"));
}

#[test]
fn test_cli_creates_agent_turn_from_input() {
    let request_json = r#"{
        "model":"gpt-5.4",
        "toolSource":"explicit",
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
        "/api/v1/agent/sessions/session-1/turns",
        StatusCode::CREATED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(request_json.to_string()))
    .with_body(TURN_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    let request_path = home.path().join("agent-turn-request.json");
    std::fs::write(&request_path, request_json).unwrap();

    cli_command_for_server(home.path(), &server)
        .args([
            "agent",
            "turns",
            "create",
            "session-1",
            "--input",
            request_path.to_str().unwrap(),
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("turn-1"));
}

#[test]
fn test_cli_rejects_agent_turn_create_without_messages() {
    let mut server = Server::new();
    let create = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/agent/sessions/session-1/turns",
        StatusCode::CREATED
    )
    .expect(0)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["agent", "turns", "create", "session-1"])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "agent turns create requires at least one message",
        ));

    create.assert();
}

#[test]
fn test_cli_lists_agent_sessions_with_server_side_filters() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/agent/sessions?provider=managed&state=active",
        StatusCode::OK
    )
    .with_body(
        r#"[
                {"id":"session-a","provider":"managed","model":"gpt-5.4","state":"active","updatedAt":"2026-04-22T00:00:00Z"}
            ]"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "agent",
            "sessions",
            "list",
            "--provider",
            "managed",
            "--state",
            "active",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("session-a"));
}

#[test]
fn test_cli_gets_agent_turn() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/agent/turns/turn-1",
        StatusCode::OK
    )
    .with_body(TURN_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["agent", "turns", "get", "turn-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("turn-1"))
        .stdout(predicate::str::contains("gpt-5.4"));
}

#[test]
fn test_cli_cancels_agent_turn() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/agent/turns/turn-1/cancel",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(r#"{"reason":"stop now"}"#.to_string()))
    .with_body(
        r#"{
                    "id":"turn-1",
                    "sessionId":"session-1",
                    "provider":"managed",
                    "model":"gpt-5.4",
                    "status":"canceled",
                    "createdAt":"2026-04-22T00:00:00Z"
                }"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["agent", "turns", "cancel", "turn-1", "--reason", "stop now"])
        .assert()
        .success()
        .stdout(predicate::str::contains("canceled"));
}

#[test]
fn test_cli_lists_agent_turn_events() {
    let mut server = Server::new();
    let _mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/agent/turns/turn-1/events?after=0&limit=10",
        StatusCode::OK
    )
    .with_body(TURN_EVENTS_JSON)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "--format", "json", "agent", "turns", "events", "list", "turn-1", "--after", "0",
            "--limit", "10",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("agent.message.delta"))
        .stdout(predicate::str::contains("risk is dependency churn"));
}

#[test]
fn test_cli_streams_agent_turn_events() {
    let mut server = Server::new();
    let _mock = server
        .mock(
            Method::GET.as_str(),
            "/api/v1/agent/turns/turn-1/events/stream?after=2&limit=1",
        )
        .match_header(header::AUTHORIZATION.as_str(), Matcher::Exact(test_bearer()))
        .with_status(usize::from(StatusCode::OK.as_u16()))
        .with_header(header::CONTENT_TYPE.as_str(), "text/event-stream")
        .with_body(
            "id: 3\nevent: turn.completed\ndata: {\"seq\":3,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        )
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "agent", "turns", "events", "stream", "turn-1", "--after", "2", "--limit", "1",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("event: turn.completed"))
        .stdout(predicate::str::contains(r#""status":"succeeded""#));
}
