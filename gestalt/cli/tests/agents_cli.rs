mod support;

use serde_json::Value;
use std::collections::VecDeque;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::{Arc, Mutex, mpsc};
use std::time::{Duration, Instant};
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
        "toolSource":"native_search",
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
                "plugin":"roadmap",
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

#[test]
fn test_cli_runs_interactive_agent_session() {
    let mut server = Server::new();
    let _session = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/agent/sessions",
        StatusCode::CREATED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"provider":"managed","model":"gpt-5.4"}"#.to_string(),
    ))
    .with_body(SESSION_JSON)
    .create();
    let _turn = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/agent/sessions/session-1/turns",
        StatusCode::CREATED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"hello\nthere"}],"toolRefs":[{"plugin":"linear","operation":"searchIssues"}]}"#.to_string(),
    ))
    .with_body(TURN_JSON)
    .create();
    let _events = server
        .mock(
            Method::GET.as_str(),
            "/api/v1/agent/turns/turn-1/events/stream?after=0&limit=100&until=blocked_or_terminal",
        )
        .match_header(header::AUTHORIZATION.as_str(), Matcher::Exact(test_bearer()))
        .with_status(usize::from(StatusCode::OK.as_u16()))
        .with_header(header::CONTENT_TYPE.as_str(), "text/event-stream")
        .with_body(
            "data: {\"seq\":1,\"type\":\"tool.started\",\"data\":{\"toolName\":\"lookup\",\"arguments\":{\"ticket\":\"INC-42\"}}}\n\n\
             data: {\"seq\":2,\"type\":\"tool.completed\",\"data\":{\"toolName\":\"lookup\",\"status\":200,\"output\":{\"ok\":true}}}\n\n\
             data: {\"seq\":3,\"type\":\"assistant.completed\",\"data\":{\"text\":\"hello back\"}}\n\n\
             data: {\"seq\":4,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        )
        .create();
    let _get_turn = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/agent/turns/turn-1",
        StatusCode::OK
    )
    .with_body(
        r#"{
            "id":"turn-1",
            "sessionId":"session-1",
            "provider":"managed",
            "model":"gpt-5.4",
            "status":"succeeded",
            "outputText":"hello back",
            "structuredOutput":{"summary":"ok"}
        }"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["agent", "--provider", "managed", "--model", "gpt-5.4"])
        .args(["--tool", "linear:searchIssues"])
        .write_stdin("hello\\\nthere\n/quit\n")
        .assert()
        .success()
        .stdout(predicate::str::contains("tool> lookup started"))
        .stdout(predicate::str::contains("tool> lookup completed (200)"))
        .stdout(predicate::str::contains("assistant> hello back"))
        .stdout(predicate::str::contains("structured>"))
        .stdout(predicate::str::contains(r#""summary": "ok""#))
        .stderr(predicate::str::contains(
            "Session session-1 [managed / gpt-5.4]",
        ))
        .stderr(predicate::str::contains("Type /quit to exit."));
}

#[cfg(unix)]
#[test]
fn test_cli_runs_tty_agent_session_with_full_screen_ui() {
    let server = ScriptedServer::spawn(vec![
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions",
            r#"{"provider":"managed","model":"gpt-5.4"}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            SESSION_JSON,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-1/turns",
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"hello tui"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-tty",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-tty/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"agent.message.delta\",\"data\":{\"text\":\"hello\"}}\n\n\
             data: {\"seq\":2,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-tty",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-tty",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"hello from tui"
            }"#,
        ),
    ]);

    let home = tempfile::tempdir().unwrap();
    let mut session = spawn_tty_cli(
        home.path(),
        server.url(),
        &["agent", "--provider", "managed", "--model", "gpt-5.4"],
    );
    let mut output = String::new();
    session.wait_for(&mut output, "\x1b[?1049h");
    session.wait_for(&mut output, "Session");
    session.write("hello tui\r");
    session.wait_for(&mut output, "assistant>");
    session.wait_for(&mut output, "hello");
    session.write("/quit\r");
    session.wait_for_exit();

    server.assert_finished();
}

#[cfg(unix)]
#[test]
fn test_cli_tty_groups_tool_activity_rows() {
    let long_output = format!("visible-prefix-{}-hidden-tail", "x".repeat(180));
    let stream = format!(
        "data: {{\"seq\":1,\"type\":\"tool.started\",\"data\":{{\"toolCallId\":\"call-lookup\",\"toolName\":\"lookup\",\"arguments\":{{\"ticket\":\"INC-42\"}}}}}}\n\n\
         data: {{\"seq\":2,\"type\":\"tool.completed\",\"data\":{{\"toolCallId\":\"call-lookup\",\"toolName\":\"lookup\",\"statusCode\":200,\"output\":{{\"summary\":\"{long_output}\"}}}}}}\n\n\
         data: {{\"seq\":3,\"type\":\"tool.started\",\"data\":{{\"toolCallId\":\"call-deploy\",\"toolName\":\"deploy\",\"input\":{{\"environment\":\"prod\"}}}}}}\n\n\
         data: {{\"seq\":4,\"type\":\"tool.failed\",\"data\":{{\"toolCallId\":\"call-deploy\",\"toolName\":\"deploy\",\"error\":\"denied by policy\"}}}}\n\n\
         data: {{\"seq\":5,\"type\":\"assistant.completed\",\"data\":{{\"text\":\"tools done\"}}}}\n\n\
         data: {{\"seq\":6,\"type\":\"turn.completed\",\"data\":{{\"status\":\"succeeded\"}}}}\n\n"
    );
    let server = ScriptedServer::spawn(vec![
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions",
            r#"{"provider":"managed","model":"gpt-5.4"}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            SESSION_JSON,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-1/turns",
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"use tools"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-tools",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-tools/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            &stream,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-tools",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-tools",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"tools done"
            }"#,
        ),
    ]);

    let home = tempfile::tempdir().unwrap();
    let mut session = spawn_tty_cli(
        home.path(),
        server.url(),
        &["agent", "--provider", "managed", "--model", "gpt-5.4"],
    );
    let mut output = String::new();
    session.wait_for(&mut output, "Session");
    session.write("use tools\r");
    session.wait_for(&mut output, "lookup");
    session.wait_for(&mut output, "completed");
    session.wait_for(&mut output, "200");
    session.wait_for(&mut output, "visible-prefix");
    session.wait_for(&mut output, "deploy");
    session.wait_for(&mut output, "failed");
    session.wait_for(&mut output, "denied");
    session.wait_for(&mut output, "done");
    session.write("/quit\r");
    session.wait_for_exit();

    assert!(
        !output.contains("hidden-tail"),
        "tool output preview was not truncated:\n{output}"
    );
    server.assert_finished();
}

#[cfg(unix)]
#[test]
fn test_cli_tty_reconciles_unmatched_tool_activity_rows() {
    let server = ScriptedServer::spawn(vec![
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions",
            r#"{"provider":"managed","model":"gpt-5.4"}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            SESSION_JSON,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-1/turns",
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"ambiguous tools"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-ambiguous-tools",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-ambiguous-tools/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"tool.started\",\"data\":{\"toolName\":\"search\",\"arguments\":{\"query\":\"single\"}}}\n\n\
             data: {\"seq\":2,\"type\":\"tool.completed\",\"data\":{\"toolName\":\"search\",\"statusCode\":200,\"output\":{\"matches\":1}}}\n\n\
             data: {\"seq\":3,\"type\":\"tool.started\",\"data\":{\"toolName\":\"lookup\",\"arguments\":{\"ticket\":\"INC-1\"}}}\n\n\
             data: {\"seq\":4,\"type\":\"tool.started\",\"data\":{\"toolName\":\"lookup\",\"arguments\":{\"ticket\":\"INC-2\"}}}\n\n\
             data: {\"seq\":5,\"type\":\"tool.completed\",\"data\":{\"toolName\":\"lookup\",\"statusCode\":200,\"output\":{\"ticket\":\"INC-2\"}}}\n\n\
             data: {\"seq\":6,\"type\":\"tool.completed\",\"data\":{\"toolName\":\"audit\",\"statusCode\":204,\"output\":{\"ok\":true}}}\n\n\
             data: {\"seq\":7,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-ambiguous-tools",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-ambiguous-tools",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded"
            }"#,
        ),
    ]);

    let home = tempfile::tempdir().unwrap();
    let mut session = spawn_tty_cli(
        home.path(),
        server.url(),
        &["agent", "--provider", "managed", "--model", "gpt-5.4"],
    );
    let mut output = String::new();
    session.wait_for(&mut output, "Session");
    session.write("ambiguous tools\r");
    session.wait_for(&mut output, "search");
    session.wait_for(&mut output, "completed");
    session.wait_for(&mut output, "lookup");
    session.wait_for(&mut output, "ended");
    session.wait_for(&mut output, "audit");
    session.write("/quit\r");
    session.wait_for_exit();

    assert!(
        !output.contains("audit completed (204, 0ms)"),
        "terminal-only tool row rendered a synthetic elapsed time:\n{output}"
    );
    server.assert_finished();
}

#[cfg(unix)]
#[test]
fn test_cli_tty_help_and_prompt_history() {
    let server = ScriptedServer::spawn(vec![
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions",
            r#"{"provider":"managed","model":"gpt-5.4"}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            SESSION_JSON,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-1/turns",
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"remember this"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-history-1",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-history-1/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"assistant.completed\",\"data\":{\"text\":\"first response\"}}\n\n\
             data: {\"seq\":2,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-history-1",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-history-1",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"first response"
            }"#,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-1/turns",
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"remember this"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-history-2",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-history-2/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"assistant.completed\",\"data\":{\"text\":\"second response\"}}\n\n\
             data: {\"seq\":2,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-history-2",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-history-2",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"second response"
            }"#,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-1/turns",
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"draft text"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-history-3",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-history-3/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"assistant.completed\",\"data\":{\"text\":\"third response\"}}\n\n\
             data: {\"seq\":2,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-history-3",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-history-3",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"third response"
            }"#,
        ),
    ]);

    let home = tempfile::tempdir().unwrap();
    let mut session = spawn_tty_cli(
        home.path(),
        server.url(),
        &["agent", "--provider", "managed", "--model", "gpt-5.4"],
    );
    let mut output = String::new();
    session.wait_for(&mut output, "Session");
    session.write("/help\r");
    session.wait_for(&mut output, "recalls");
    session.write("remember this\r");
    session.wait_for(&mut output, "first");
    session.write("\x1b[A\r");
    session.wait_for(&mut output, "second");
    session.write("draft text\x1b[A\x1b[B\r");
    session.wait_for(&mut output, "third");
    session.write("/quit\r");
    session.wait_for_exit();

    server.assert_finished();
}

#[cfg(unix)]
#[test]
fn test_cli_tty_scrolls_multiline_help_rows() {
    let server = ScriptedServer::spawn(vec![ExpectedRequest::json(
        Method::POST,
        "/api/v1/agent/sessions",
        r#"{"provider":"managed","model":"gpt-5.4"}"#,
        StatusCode::CREATED,
        http::APPLICATION_JSON,
        SESSION_JSON,
    )]);

    let home = tempfile::tempdir().unwrap();
    let mut session = spawn_tty_cli_with_size(
        home.path(),
        server.url(),
        &["agent", "--provider", "managed", "--model", "gpt-5.4"],
        8,
        80,
    );
    let mut output = String::new();
    session.wait_for(&mut output, "Session");
    session.write("/help\r");
    session.wait_for(&mut output, "cancels");
    session.write("\x1b[5~\x1b[5~");
    session.wait_for(&mut output, "Commands");
    session.write("/quit\r");
    session.wait_for_exit();

    server.assert_finished();
}

#[cfg(unix)]
#[test]
fn test_cli_tty_wraps_wide_transcript_text_by_display_width() {
    let server = ScriptedServer::spawn(vec![
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions",
            r#"{"provider":"managed","model":"gpt-5.4"}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            SESSION_JSON,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-1/turns",
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"wide text"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-wide",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-wide/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"assistant.completed\",\"data\":{\"text\":\"一二三四五六七八九十👩‍💻👨‍💻\"}}\n\n\
             data: {\"seq\":2,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-wide",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-wide",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"一二三四五六七八九十👩‍💻👨‍💻"
            }"#,
        ),
    ]);

    let home = tempfile::tempdir().unwrap();
    let mut session = spawn_tty_cli_with_size(
        home.path(),
        server.url(),
        &["agent", "--provider", "managed", "--model", "gpt-5.4"],
        8,
        30,
    );
    let mut output = String::new();
    session.wait_for(&mut output, "Session");
    session.write("wide text\r");
    session.wait_for(&mut output, "👨‍💻");
    session.write("/quit\r");
    session.wait_for_exit();

    server.assert_finished();
}

#[cfg(unix)]
#[test]
fn test_cli_tty_queues_prompt_while_turn_is_running() {
    let server = ScriptedServer::spawn(vec![
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions",
            r#"{"provider":"managed","model":"gpt-5.4"}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            SESSION_JSON,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-1/turns",
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"slow turn"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-queued-1",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-queued-1/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"assistant.completed\",\"data\":{\"text\":\"done-one\"}}\n\n\
             data: {\"seq\":2,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        )
        .with_response_delay(Duration::from_secs(2)),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-queued-1",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-queued-1",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"done-one"
            }"#,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-1/turns",
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"pending turn"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-queued-2",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-queued-2/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"assistant.completed\",\"data\":{\"text\":\"done-two\"}}\n\n\
             data: {\"seq\":2,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-queued-2",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-queued-2",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"done-two"
            }"#,
        ),
    ]);

    let home = tempfile::tempdir().unwrap();
    let mut session = spawn_tty_cli(
        home.path(),
        server.url(),
        &["agent", "--provider", "managed", "--model", "gpt-5.4"],
    );
    let mut output = String::new();
    session.wait_for(&mut output, "Session");
    session.write("slow turn\r");
    session.wait_for(&mut output, "working");
    session.write("pending turn\r");
    session.wait_for(&mut output, "queued");
    session.wait_for(&mut output, "done-one");
    session.wait_for(&mut output, "done-two");
    session.write("/quit\r");
    session.wait_for_exit();

    server.assert_finished();
}

#[cfg(unix)]
#[test]
fn test_cli_tty_turn_boundary_stops_stale_streaming_transcript_item() {
    let server = ScriptedServer::spawn(vec![
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions",
            r#"{"provider":"managed","model":"gpt-5.4"}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            SESSION_JSON,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-1/turns",
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"first turn"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-first",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-first/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"assistant.delta\",\"data\":{\"text\":\"alpha\"}}\n\n\
             data: {\"seq\":2,\"type\":\"turn.failed\",\"data\":{}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-first",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-first",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"failed"
            }"#,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-1/turns",
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"second turn"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-second",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-second/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"assistant.delta\",\"data\":{\"text\":\"beta\"}}\n\n\
             data: {\"seq\":2,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-second",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-second",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"beta"
            }"#,
        ),
    ]);

    let home = tempfile::tempdir().unwrap();
    let mut session = spawn_tty_cli(
        home.path(),
        server.url(),
        &["agent", "--provider", "managed", "--model", "gpt-5.4"],
    );
    let mut output = String::new();
    session.wait_for(&mut output, "Session");
    session.write("first turn\r");
    session.wait_for(&mut output, "alpha");
    session.write("second turn\r");
    session.wait_for(&mut output, "beta");
    session.write("/quit\r");
    session.wait_for_exit();

    assert!(
        !output.contains("alphabeta"),
        "assistant output from separate turns was merged:\n{output}"
    );
    server.assert_finished();
}

#[cfg(unix)]
#[test]
fn test_cli_tty_secret_interaction_masks_and_requires_input() {
    let server = ScriptedServer::spawn(vec![
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions",
            r#"{"provider":"managed","model":"gpt-5.4"}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            SESSION_JSON,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-1/turns",
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"needs secret"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-secret",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-secret/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"turn.started\",\"data\":{\"status\":\"running\"}}\n\n\
             data: {\"seq\":2,\"type\":\"interaction.requested\",\"data\":{\"interaction_id\":\"interaction-secret\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-secret",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-secret",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"waiting_for_input",
                "statusMessage":"waiting for input"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-secret/interactions",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"[
                {
                    "id":"interaction-secret",
                    "turnId":"turn-secret",
                    "type":"input",
                    "state":"pending",
                    "title":"API token",
                    "prompt":"Enter the deployment token.",
                    "request":{"required":true,"secret":true}
                }
            ]"#,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/turns/turn-secret/interactions/interaction-secret/resolve",
            r#"{"resolution":{"response":"supersecret"}}"#,
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"interaction-secret",
                "turnId":"turn-secret",
                "type":"input",
                "state":"resolved",
                "resolution":{"response":"supersecret"}
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-secret/events/stream?after=2&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":3,\"type\":\"interaction.resolved\",\"data\":{\"interaction_id\":\"interaction-secret\"}}\n\n\
             data: {\"seq\":4,\"type\":\"assistant.completed\",\"data\":{\"text\":\"token accepted\"}}\n\n\
             data: {\"seq\":5,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-secret",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-secret",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"token accepted"
            }"#,
        ),
    ]);

    let home = tempfile::tempdir().unwrap();
    let mut session = spawn_tty_cli(
        home.path(),
        server.url(),
        &["agent", "--provider", "managed", "--model", "gpt-5.4"],
    );
    let mut output = String::new();
    session.wait_for(&mut output, "Session");
    session.write("needs secret\r");
    session.wait_for(&mut output, "API token");
    session.write("\r");
    session.wait_for(&mut output, "A value is required.");
    session.write("supersecret\r");
    session.wait_for(&mut output, "assistant>");
    session.wait_for(&mut output, "accepted");
    session.write("/quit\r");
    session.wait_for_exit();

    assert!(
        !output.contains("supersecret"),
        "secret input was rendered in TTY output:\n{output}"
    );
    server.assert_finished();
}

#[test]
fn test_cli_resumes_latest_active_agent_session() {
    let server = ScriptedServer::spawn(vec![
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/sessions?provider=managed&state=active",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"[
                {
                    "id":"session-old",
                    "provider":"managed",
                    "model":"gpt-5.3",
                    "state":"active",
                    "createdAt":"2026-04-21T00:00:00Z",
                    "updatedAt":"2026-04-22T00:00:03Z",
                    "lastTurnAt":"2026-04-22T00:00:10Z"
                },
                {
                    "id":"session-new",
                    "provider":"managed",
                    "model":"gpt-5.4",
                    "state":"active",
                    "createdAt":"2026-04-22T00:00:00Z",
                    "updatedAt":"2026-04-22T00:00:03.1Z",
                    "lastTurnAt":"2026-04-22T00:00:10.1Z"
                }
            ]"#,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-new/turns",
            r#"{"messages":[{"role":"user","text":"continue plan"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-resumed",
                "sessionId":"session-new",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-resumed/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"assistant.completed\",\"data\":{\"text\":\"continued\"}}\n\n\
             data: {\"seq\":2,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-resumed",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-resumed",
                "sessionId":"session-new",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"continued"
            }"#,
        ),
    ]);

    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url())
        .args([
            "agent",
            "--continue",
            "--provider",
            "managed",
            "--message",
            "continue plan",
        ])
        .write_stdin("/quit\n")
        .assert()
        .success()
        .stdout(predicate::str::contains("assistant> continued"))
        .stderr(predicate::str::contains(
            "Session session-new [managed / gpt-5.4]",
        ));

    server.assert_finished();
}

#[test]
fn test_cli_resume_fails_without_active_agent_session() {
    let server = ScriptedServer::spawn(vec![ExpectedRequest::text(
        Method::GET,
        "/api/v1/agent/sessions?state=active",
        StatusCode::OK,
        http::APPLICATION_JSON,
        "[]",
    )]);

    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url())
        .args(["agent", "--resume"])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "no active agent sessions found; omit --resume to create one",
        ));

    server.assert_finished();
}

#[test]
fn test_cli_rejects_interactive_agent_flags_with_subcommands() {
    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .args(["agent", "--resume", "sessions", "list"])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "--resume can only be used with interactive `gestalt agent`",
        ));
}

#[test]
fn test_cli_agent_help_describes_resume_provider_filter() {
    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .args(["agent", "--help"])
        .assert()
        .success()
        .stdout(predicate::str::contains("--resume"))
        .stdout(predicate::str::contains("--continue"))
        .stdout(predicate::str::contains("provider filter when resuming"))
        .stdout(predicate::str::contains("--ui").not());
}

#[test]
fn test_cli_resumes_existing_agent_session_and_resolves_input_interaction() {
    let server = ScriptedServer::spawn(vec![
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/sessions/session-2",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"session-2",
                "provider":"managed",
                "model":"gpt-5.4",
                "state":"active"
            }"#,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-2/turns",
            r#"{"messages":[{"role":"user","text":"need incident context"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-input",
                "sessionId":"session-2",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-input/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"turn.started\",\"data\":{\"status\":\"running\"}}\n\n\
             data: {\"seq\":2,\"type\":\"interaction.requested\",\"data\":{\"interaction_id\":\"interaction-2\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-input",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-input",
                "sessionId":"session-2",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"waiting_for_input",
                "statusMessage":"waiting for input"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-input/interactions",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"[
                {
                    "id":"interaction-2",
                    "turnId":"turn-input",
                    "type":"input",
                    "state":"pending",
                    "title":"Incident ID",
                    "prompt":"Provide the incident identifier to summarize.",
                    "request":{"default":"INC-42","required":true}
                }
            ]"#,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/turns/turn-input/interactions/interaction-2/resolve",
            r#"{"resolution":{"response":"INC-42"}}"#,
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"interaction-2",
                "turnId":"turn-input",
                "type":"input",
                "state":"resolved",
                "resolution":{"response":"INC-42"}
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-input/events/stream?after=2&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":3,\"type\":\"interaction.resolved\",\"data\":{\"interaction_id\":\"interaction-2\"}}\n\n\
             data: {\"seq\":4,\"type\":\"assistant.completed\",\"data\":{\"text\":\"incident INC-42 summarized\"}}\n\n\
             data: {\"seq\":5,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-input",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-input",
                "sessionId":"session-2",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"incident INC-42 summarized"
            }"#,
        ),
    ]);

    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url())
        .args(["agent", "--session", "session-2"])
        .write_stdin("need incident context\n\n/quit\n")
        .assert()
        .success()
        .stdout(predicate::str::contains(
            "interaction> requested (interaction-2)",
        ))
        .stdout(predicate::str::contains(
            "interaction> resolved (interaction-2)",
        ))
        .stdout(predicate::str::contains(
            "assistant> incident INC-42 summarized",
        ))
        .stderr(predicate::str::contains(
            "Session session-2 [managed / gpt-5.4]",
        ))
        .stderr(predicate::str::contains("Incident ID"))
        .stderr(predicate::str::contains(
            "Provide the incident identifier to summarize.",
        ))
        .stderr(predicate::str::contains("Value [INC-42]:"));

    server.assert_finished();
}

#[test]
fn test_cli_resolves_agent_interaction_in_interactive_mode() {
    let server = ScriptedServer::spawn(vec![
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions",
            r#"{"provider":"managed","model":"gpt-5.4"}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            SESSION_JSON,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-1/turns",
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"approve deployment"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-approval",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-approval/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"turn.started\",\"data\":{\"status\":\"running\"}}\n\n\
             data: {\"seq\":2,\"type\":\"interaction.requested\",\"data\":{\"interaction_id\":\"interaction-1\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-approval",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-approval",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"waiting_for_input",
                "statusMessage":"waiting for input"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-approval/interactions",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"[
                {
                    "id":"interaction-1",
                    "turnId":"turn-approval",
                    "type":"approval",
                    "state":"pending",
                    "title":"Approve deployment",
                    "prompt":"Allow the deployment to continue?",
                    "request":{"environment":"prod"}
                }
            ]"#,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/turns/turn-approval/interactions/interaction-1/resolve",
            r#"{"resolution":{"approved":true}}"#,
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"interaction-1",
                "turnId":"turn-approval",
                "type":"approval",
                "state":"resolved",
                "resolution":{"approved":true}
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-approval/events/stream?after=2&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":3,\"type\":\"interaction.resolved\",\"data\":{\"interaction_id\":\"interaction-1\"}}\n\n\
             data: {\"seq\":4,\"type\":\"assistant.completed\",\"data\":{\"text\":\"deployment approved\"}}\n\n\
             data: {\"seq\":5,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-approval",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-approval",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"deployment approved"
            }"#,
        ),
    ]);

    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url())
        .args(["agent", "--provider", "managed", "--model", "gpt-5.4"])
        .write_stdin("approve deployment\nY\n/quit\n")
        .assert()
        .success()
        .stdout(predicate::str::contains(
            "interaction> requested (interaction-1)",
        ))
        .stdout(predicate::str::contains(
            "interaction> resolved (interaction-1)",
        ))
        .stdout(predicate::str::contains("assistant> deployment approved"))
        .stderr(predicate::str::contains("Approve deployment"))
        .stderr(predicate::str::contains("Approve? [Y/n]:"));

    server.assert_finished();
}

#[derive(Clone)]
struct ExpectedRequest {
    method: Method,
    path: String,
    body: Option<Value>,
    status: StatusCode,
    content_type: String,
    response_body: String,
    response_delay: Duration,
}

impl ExpectedRequest {
    fn json(
        method: Method,
        path: &str,
        body: &str,
        status: StatusCode,
        content_type: &str,
        response_body: &str,
    ) -> Self {
        Self {
            method,
            path: path.to_string(),
            body: Some(serde_json::from_str(body).unwrap()),
            status,
            content_type: content_type.to_string(),
            response_body: response_body.to_string(),
            response_delay: Duration::from_millis(0),
        }
    }

    fn text(
        method: Method,
        path: &str,
        status: StatusCode,
        content_type: &str,
        response_body: &str,
    ) -> Self {
        Self {
            method,
            path: path.to_string(),
            body: None,
            status,
            content_type: content_type.to_string(),
            response_body: response_body.to_string(),
            response_delay: Duration::from_millis(0),
        }
    }

    fn with_response_delay(mut self, delay: Duration) -> Self {
        self.response_delay = delay;
        self
    }
}

struct ScriptedServer {
    base_url: String,
    expected: Arc<Mutex<VecDeque<ExpectedRequest>>>,
    handle: std::thread::JoinHandle<()>,
}

impl ScriptedServer {
    fn spawn(expected: Vec<ExpectedRequest>) -> Self {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let base_url = format!("http://{}", listener.local_addr().unwrap());
        let expected = Arc::new(Mutex::new(VecDeque::from(expected)));
        let expected_for_thread = Arc::clone(&expected);
        let handle = std::thread::spawn(move || {
            loop {
                if expected_for_thread.lock().unwrap().is_empty() {
                    break;
                }
                let (stream, _) = listener.accept().unwrap();
                handle_scripted_request(stream, &expected_for_thread);
            }
        });
        Self {
            base_url,
            expected,
            handle,
        }
    }

    fn url(&self) -> &str {
        &self.base_url
    }

    fn assert_finished(self) {
        self.handle.join().unwrap();
        let remaining = self.expected.lock().unwrap();
        assert!(
            remaining.is_empty(),
            "scripted server still had pending requests: {}",
            remaining.len()
        );
    }
}

fn handle_scripted_request(stream: TcpStream, expected: &Arc<Mutex<VecDeque<ExpectedRequest>>>) {
    let mut stream = stream;
    let request = read_scripted_http_request(&mut stream);
    let next = expected.lock().unwrap().pop_front().unwrap();
    assert_eq!(request.method, next.method);
    assert_eq!(request.target, next.path);
    assert_eq!(
        request.authorization.as_deref().unwrap_or_default(),
        test_bearer()
    );
    match next.body {
        Some(expected_body) => {
            let actual: Value = serde_json::from_slice(&request.body).unwrap();
            assert_eq!(actual, expected_body);
        }
        None => assert!(request.body.is_empty(), "unexpected request body"),
    }
    if !next.response_delay.is_zero() {
        std::thread::sleep(next.response_delay);
    }
    http::write_response(
        &mut stream,
        next.status,
        &next.content_type,
        &next.response_body,
        &[],
    )
    .unwrap();
}

struct ScriptedHttpRequest {
    method: Method,
    target: String,
    authorization: Option<String>,
    body: Vec<u8>,
}

#[cfg(unix)]
struct TtyCliSession {
    writer: Box<dyn Write + Send>,
    reader: Option<std::thread::JoinHandle<()>>,
    rx: mpsc::Receiver<String>,
    child: Box<dyn portable_pty::Child + Send + Sync>,
}

#[cfg(unix)]
impl TtyCliSession {
    fn write(&mut self, input: &str) {
        self.writer.write_all(input.as_bytes()).unwrap();
        self.writer.flush().unwrap();
    }

    fn wait_for(&mut self, output: &mut String, needle: &str) {
        let deadline = Instant::now() + Duration::from_secs(10);
        while !output.contains(needle) {
            let now = Instant::now();
            assert!(
                now < deadline,
                "timed out waiting for {needle:?}; output was:\n{output}"
            );
            let timeout = deadline
                .saturating_duration_since(now)
                .min(Duration::from_millis(100));
            match self.rx.recv_timeout(timeout) {
                Ok(chunk) => output.push_str(&chunk),
                Err(mpsc::RecvTimeoutError::Timeout) => {}
                Err(mpsc::RecvTimeoutError::Disconnected) => {
                    panic!("PTY reader closed before seeing {needle:?}; output was:\n{output}")
                }
            }
        }
    }

    fn wait_for_exit(&mut self) {
        let deadline = Instant::now() + Duration::from_secs(10);
        loop {
            if let Some(status) = self.child.try_wait().unwrap() {
                assert!(status.success(), "TTY CLI exited with {status:?}");
                if let Some(reader) = self.reader.take() {
                    let _ = reader.join();
                }
                return;
            }
            if Instant::now() >= deadline {
                let _ = self.child.kill();
                panic!("timed out waiting for TTY CLI to exit");
            }
            std::thread::sleep(Duration::from_millis(50));
        }
    }
}

#[cfg(unix)]
fn spawn_tty_cli(home: &std::path::Path, url: &str, args: &[&str]) -> TtyCliSession {
    spawn_tty_cli_with_size(home, url, args, 24, 100)
}

#[cfg(unix)]
fn spawn_tty_cli_with_size(
    home: &std::path::Path,
    url: &str,
    args: &[&str],
    rows: u16,
    cols: u16,
) -> TtyCliSession {
    use portable_pty::{CommandBuilder, PtySize, native_pty_system};

    std::fs::create_dir_all(home).unwrap();
    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(PtySize {
            rows,
            cols,
            pixel_width: 0,
            pixel_height: 0,
        })
        .unwrap();
    let mut cmd = CommandBuilder::new(env!("CARGO_BIN_EXE_gestalt"));
    cmd.cwd(home);
    cmd.env("HOME", home);
    cmd.env("XDG_CONFIG_HOME", home.join("xdg-config"));
    cmd.env("GESTALT_API_KEY", TEST_TOKEN);
    cmd.env_remove("GESTALT_URL");
    cmd.arg("--url");
    cmd.arg(url);
    cmd.args(args);

    let child = pair.slave.spawn_command(cmd).unwrap();
    drop(pair.slave);
    let mut reader = pair.master.try_clone_reader().unwrap();
    let writer = pair.master.take_writer().unwrap();
    let (tx, rx) = mpsc::channel();
    let reader = std::thread::spawn(move || {
        let mut buffer = [0; 4096];
        loop {
            match reader.read(&mut buffer) {
                Ok(0) => return,
                Ok(read) => {
                    let chunk = String::from_utf8_lossy(&buffer[..read]).to_string();
                    if tx.send(chunk).is_err() {
                        return;
                    }
                }
                Err(_) => return,
            }
        }
    });

    TtyCliSession {
        writer,
        reader: Some(reader),
        rx,
        child,
    }
}

fn read_scripted_http_request(stream: &mut TcpStream) -> ScriptedHttpRequest {
    let mut reader = BufReader::new(stream.try_clone().unwrap());
    let mut request_line = String::new();
    reader.read_line(&mut request_line).unwrap();

    let mut content_length = 0usize;
    let mut authorization = None;
    loop {
        let mut line = String::new();
        reader.read_line(&mut line).unwrap();
        if line == "\r\n" {
            break;
        }
        if let Some((name, value)) = line.split_once(':') {
            if name.eq_ignore_ascii_case(header::CONTENT_LENGTH.as_str()) {
                content_length = value.trim().parse().unwrap();
            }
            if name.eq_ignore_ascii_case(header::AUTHORIZATION.as_str()) {
                authorization = Some(value.trim().to_string());
            }
        }
    }

    let mut body = vec![0; content_length];
    reader.read_exact(&mut body).unwrap();

    let mut parts = request_line.split_whitespace();
    ScriptedHttpRequest {
        method: Method::from_bytes(parts.next().unwrap().as_bytes()).unwrap(),
        target: parts.next().unwrap().to_string(),
        authorization,
        body,
    }
}
