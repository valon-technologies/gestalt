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
        "display":{"kind":"status","phase":"started","label":"turn","text":"running"},
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
        "display":{"kind":"text","phase":"delta","text":"risk is dependency churn"},
        "createdAt":"2026-04-22T00:00:02Z"
    },
    {
        "id":"evt-3",
        "turnId":"turn-1",
        "seq":3,
        "type":"tool.progress",
        "source":"managed",
        "visibility":"public",
        "display":{"kind":"tool","phase":"progress","action":"Indexing","label":"lookup","ref":"call-lookup"},
        "createdAt":"2026-04-22T00:00:03Z"
    },
    {
        "id":"evt-4",
        "turnId":"turn-1",
        "seq":4,
        "type":"tool.completed",
        "source":"managed",
        "visibility":"public",
        "data":{"output":{"secret":"RAW_OUTPUT_SENTINEL"}},
        "display":{"kind":"tool","phase":"completed","label":"lookup","ref":"call-lookup","text":"200","output":{"ok":true}},
        "createdAt":"2026-04-22T00:00:04Z"
    },
    {
        "id":"evt-5",
        "turnId":"turn-1",
        "seq":5,
        "type":"turn.failed",
        "source":"managed",
        "visibility":"public",
        "data":{"error":"fallback failure"},
        "display":{"kind":"error","phase":"failed","label":"turn","error":"display failure"},
        "createdAt":"2026-04-22T00:00:05Z"
    },
    {
        "id":"evt-6",
        "turnId":"turn-1",
        "seq":6,
        "type":"turn.failed",
        "source":"managed",
        "visibility":"public",
        "data":{"debug":"RAW_FAILED_SENTINEL"},
        "display":{"kind":123,"text":"bad failure display"},
        "createdAt":"2026-04-22T00:00:06Z"
    },
    {
        "id":"evt-7",
        "turnId":"turn-1",
        "seq":7,
        "type":"provider.secret",
        "source":"managed",
        "visibility":"private",
        "data":{"secret":"RAW_PRIVATE_SENTINEL"},
        "display":{"kind":123,"text":"hidden"},
        "createdAt":"2026-04-22T00:00:07Z"
    },
    {
        "id":"evt-8",
        "turnId":"turn-1",
        "seq":8,
        "type":"assistant.completed",
        "source":"managed",
        "visibility":"private",
        "data":{"text":"private known fallback"},
        "display":{"kind":123,"text":"bad display"},
        "createdAt":"2026-04-22T00:00:08Z"
    }
]"#;

const TURN_TRANSCRIPT_JSON: &str = r#"{
    "id":"turn-1",
    "sessionId":"session-1",
    "provider":"managed",
    "model":"gpt-5.4",
    "status":"succeeded",
    "messages":[
        {"role":"system","text":"Be concise."},
        {"role":"user","text":"Summarize the roadmap risk."},
        {"role":"user","parts":[
            {"text":"Include filter "},
            {"json":{"priority":"high"}}
        ]}
    ],
    "outputText":"historical answer",
    "structuredOutput":{"summary":"done"},
    "createdAt":"2026-04-22T00:00:00Z",
    "executionRef":"turn-1"
}"#;

const TURN_TRANSCRIPT_EVENTS_PAGE_1_JSON: &str = r#"[
    {
        "id":"evt-t1",
        "turnId":"turn-1",
        "seq":1,
        "type":"provider.tool",
        "source":"managed",
        "visibility":"public",
        "data":{"arguments":{"secret":"RAW_TRANSCRIPT_INPUT"}},
        "display":{"kind":"tool","phase":"started","label":"lookup","ref":"call-lookup","input":{"ticket":"INC-42"}},
        "createdAt":"2026-04-22T00:00:01Z"
    },
    {
        "id":"evt-t2",
        "turnId":"turn-1",
        "seq":2,
        "type":"provider.tool",
        "source":"managed",
        "visibility":"public",
        "data":{"output":{"secret":"RAW_TRANSCRIPT_OUTPUT"}},
        "display":{"kind":"tool","phase":"completed","label":"lookup","ref":"call-lookup","text":"200","output":{"ok":true}},
        "createdAt":"2026-04-22T00:00:02Z"
    }
]"#;

const TURN_TRANSCRIPT_EVENTS_PAGE_2_JSON: &str = r#"[
    {
        "id":"evt-t3",
        "turnId":"turn-1",
        "seq":3,
        "type":"provider.text",
        "source":"managed",
        "visibility":"public",
        "data":{"text":"RAW_TRANSCRIPT_TEXT"},
        "display":{"kind":"text","phase":"completed","text":"historical answer"},
        "createdAt":"2026-04-22T00:00:03Z"
    },
    {
        "id":"evt-t4",
        "turnId":"turn-1",
        "seq":4,
        "type":"provider.secret",
        "source":"managed",
        "visibility":"private",
        "data":{"secret":"RAW_TRANSCRIPT_PRIVATE"},
        "display":{"kind":"error","phase":"failed","text":"hidden private display"},
        "createdAt":"2026-04-22T00:00:04Z"
    },
    {
        "id":"evt-t5",
        "turnId":"turn-1",
        "seq":5,
        "type":"turn.completed",
        "source":"managed",
        "visibility":"public",
        "data":{"status":"succeeded"},
        "display":{"kind":"status","phase":"completed","text":"done"},
        "createdAt":"2026-04-22T00:00:05Z"
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
        "metadata":{"ticket":"TASK-123"},
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
fn test_cli_lists_agent_turn_events_table_uses_display_summary() {
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
            "agent", "turns", "events", "list", "turn-1", "--after", "0", "--limit", "10",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("Display"))
        .stdout(predicate::str::contains("started:"))
        .stdout(predicate::str::contains("running"))
        .stdout(predicate::str::contains("assistant"))
        .stdout(predicate::str::contains("dependency"))
        .stdout(predicate::str::contains("Indexing"))
        .stdout(predicate::str::contains("lookup"))
        .stdout(predicate::str::contains("completed"))
        .stdout(predicate::str::contains(r#"{"ok"#))
        .stdout(predicate::str::contains("failed:"))
        .stdout(predicate::str::contains("display"))
        .stdout(predicate::str::contains("failure"))
        .stdout(predicate::str::contains("turn.failed"))
        .stdout(predicate::str::contains("private"))
        .stdout(predicate::str::contains("known"))
        .stdout(predicate::str::contains("fallback"))
        .stdout(predicate::str::contains("RAW_OUTPUT_SENTINEL").not())
        .stdout(predicate::str::contains("RAW_FAILED_SENTINEL").not())
        .stdout(predicate::str::contains("RAW_PRIVATE_SENTINEL").not())
        .stdout(predicate::str::contains("bad display").not())
        .stdout(predicate::str::contains("bad failure display").not())
        .stdout(predicate::str::contains("fallback failure").not());
}

#[test]
fn test_cli_renders_agent_turn_transcript_from_display_events() {
    let mut server = Server::new();
    let _turn = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/agent/turns/turn-1",
        StatusCode::OK
    )
    .with_body(TURN_TRANSCRIPT_JSON)
    .create();
    let _events = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/agent/turns/turn-1/events?after=0&limit=100",
        StatusCode::OK
    )
    .with_body(TURN_TRANSCRIPT_EVENTS_PAGE_1_JSON)
    .create();
    let _events_page_2 = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/agent/turns/turn-1/events?after=2&limit=100",
        StatusCode::OK
    )
    .with_body(TURN_TRANSCRIPT_EVENTS_PAGE_2_JSON)
    .create();
    let _events_done = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/agent/turns/turn-1/events?after=5&limit=100",
        StatusCode::OK
    )
    .with_body("[]")
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["agent", "turns", "transcript", "turn-1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("system> Be concise."))
        .stdout(predicate::str::contains("you> Summarize the roadmap risk."))
        .stdout(predicate::str::contains(
            r#"you> Include filter {"priority":"high"}"#,
        ))
        .stdout(predicate::str::contains(
            r#"tool> lookup started {"ticket":"INC-42"}"#,
        ))
        .stdout(predicate::str::contains(
            r#"tool> lookup completed (200) {"ok":true}"#,
        ))
        .stdout(predicate::str::contains("assistant> historical answer"))
        .stdout(predicate::str::contains("turn> completed (done)"))
        .stdout(predicate::str::contains("structured>"))
        .stdout(predicate::str::contains(r#""summary": "done""#))
        .stdout(predicate::str::contains("RAW_TRANSCRIPT_INPUT").not())
        .stdout(predicate::str::contains("RAW_TRANSCRIPT_OUTPUT").not())
        .stdout(predicate::str::contains("RAW_TRANSCRIPT_TEXT").not())
        .stdout(predicate::str::contains("RAW_TRANSCRIPT_PRIVATE").not())
        .stdout(predicate::str::contains("hidden private display").not());
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
        r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"hello\nthere"}],"toolRefs":[{"plugin":"tracker","operation":"searchItems"}]}"#.to_string(),
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
        .args(["--tool", "tracker:searchItems"])
        .write_stdin("hello\\\nthere\n")
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
        .stderr(predicate::str::contains(
            "Press Ctrl-C or send EOF to exit.",
        ));
}

#[test]
fn test_cli_runs_interactive_agent_session_with_display_events() {
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
        r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"display events"}]}"#.to_string(),
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
            "data: {\"seq\":1,\"type\":\"provider.tool\",\"visibility\":\"public\",\"display\":{\"kind\":\"tool\",\"phase\":\"started\",\"label\":\"lookup\",\"ref\":\"call-lookup\",\"input\":{\"ticket\":\"INC-42\"}}}\n\n\
             data: {\"seq\":2,\"type\":\"provider.tool\",\"visibility\":\"public\",\"display\":{\"kind\":\"tool\",\"phase\":\"progress\",\"action\":\"Searching\",\"label\":\"lookup\",\"ref\":\"call-lookup\",\"text\":\"halfway\"}}\n\n\
             data: {\"seq\":3,\"type\":\"provider.tool\",\"visibility\":\"public\",\"display\":{\"kind\":\"tool\",\"phase\":\"completed\",\"label\":\"lookup\",\"ref\":\"call-lookup\",\"text\":\"200\",\"output\":{\"ok\":true}}}\n\n\
             data: {\"seq\":4,\"type\":\"provider.tool\",\"visibility\":\"public\",\"data\":{\"arguments\":{\"secret\":\"RAW_INPUT_SENTINEL\"}},\"display\":{\"kind\":\"tool\",\"phase\":\"started\",\"label\":\"redacted\",\"ref\":\"call-redacted\"}}\n\n\
             data: {\"seq\":5,\"type\":\"provider.tool\",\"visibility\":\"public\",\"data\":{\"output\":{\"secret\":\"RAW_OUTPUT_SENTINEL\"}},\"display\":{\"kind\":\"tool\",\"phase\":\"completed\",\"label\":\"redacted\",\"ref\":\"call-redacted\",\"text\":\"done\"}}\n\n\
             data: {\"seq\":6,\"type\":\"provider.text\",\"visibility\":\"public\",\"display\":{\"kind\":\"text\",\"phase\":\"delta\",\"text\":\"hello\"}}\n\n\
             data: {\"seq\":7,\"type\":\"provider.text\",\"visibility\":\"public\",\"display\":{\"kind\":\"text\",\"phase\":\"delta\",\"text\":\"\\n  display\"}}\n\n\
             data: {\"seq\":8,\"type\":\"assistant.completed\",\"visibility\":\"public\",\"data\":{\"text\":\"hello\\n  display\"},\"display\":{\"kind\":\"text\",\"phase\":\"completed\"}}\n\n\
             data: {\"seq\":9,\"type\":\"assistant.completed\",\"visibility\":\"public\",\"data\":{\"text\":\"raw fallback\"},\"display\":{\"kind\":123,\"text\":42}}\n\n\
             data: {\"seq\":10,\"type\":\"assistant.completed\",\"visibility\":\"public\",\"data\":{\"text\":\"missing display text fallback\"},\"display\":{\"kind\":\"text\",\"phase\":\"completed\"}}\n\n\
             data: {\"seq\":11,\"type\":\"provider.status\",\"visibility\":\"public\",\"display\":{\"kind\":\"status\",\"phase\":\"completed\",\"text\":\"tools synced\"}}\n\n\
             data: {\"seq\":12,\"type\":\"provider.secret\",\"visibility\":\"private\",\"display\":{\"kind\":\"error\",\"phase\":\"failed\",\"text\":\"hidden secret\"}}\n\n\
             data: {\"seq\":13,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
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
            "outputText":"hello\n  display"
        }"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["agent", "--provider", "managed", "--model", "gpt-5.4"])
        .write_stdin("display events\n")
        .assert()
        .success()
        .stdout(predicate::str::contains(
            r#"tool> lookup started {"ticket":"INC-42"}"#,
        ))
        .stdout(predicate::str::contains("tool> Searching lookup: halfway"))
        .stdout(predicate::str::contains(
            r#"tool> lookup completed (200) {"ok":true}"#,
        ))
        .stdout(predicate::str::contains("tool> redacted started"))
        .stdout(predicate::str::contains("tool> redacted completed (done)"))
        .stdout(predicate::str::contains("assistant> hello\n  display"))
        .stdout(predicate::str::contains("assistant> raw fallback"))
        .stdout(predicate::str::contains(
            "assistant> missing display text fallback",
        ))
        .stdout(predicate::str::contains("turn> completed (tools synced)"))
        .stdout(predicate::str::contains("RAW_INPUT_SENTINEL").not())
        .stdout(predicate::str::contains("RAW_OUTPUT_SENTINEL").not())
        .stdout(predicate::str::contains("hidden secret").not());
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
            "data: {\"seq\":1,\"type\":\"agent.message.delta\",\"data\":{\"text\":\"**hello**\"}}\n\n\
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
    std::fs::create_dir_all(home.path().join(".git")).unwrap();
    std::fs::write(
        home.path().join(".git").join("HEAD"),
        "ref: refs/heads/footer-branch\n",
    )
    .unwrap();
    let mut session = spawn_tty_cli(
        home.path(),
        server.url(),
        &["agent", "--provider", "managed", "--model", "gpt-5.4"],
    );
    let mut output = String::new();
    session.wait_for(&mut output, "\x1b[?1049h");
    session.wait_for(&mut output, "Session");
    session.wait_for(&mut output, "managed/gpt-5.4");
    session.wait_for(&mut output, "footer-branch");
    session.write("hello tui\r");
    session.wait_for(&mut output, "hello");
    session.wait_for(&mut output, "Brewed for");
    session.write("\x03");
    session.wait_for(&mut output, "Resume with: gestalt agent resume session-1");
    session.wait_for_exit();

    assert!(
        output.contains("› hello tui") && output.contains("●"),
        "TTY transcript did not render highlighted user input and assistant bullet:\n{output}"
    );
    assert!(
        !output.contains("**hello**"),
        "TTY legacy assistant fallback did not preserve markdown rendering:\n{output}"
    );
    assert!(
        output.contains("managed/gpt-5.4")
            && output.contains("~")
            && output.contains("footer-branch")
            && output.contains("session session-1"),
        "TTY footer did not render local model/session/path/branch metadata:\n{output}"
    );
    assert!(
        !output.contains("You")
            && !output.contains("Assistant")
            && !output.contains("you>")
            && !output.contains("assistant>"),
        "TTY transcript still rendered legacy prompt labels:\n{output}"
    );
    server.assert_finished();
}

#[cfg(unix)]
#[test]
fn test_cli_tty_resume_model_override_updates_visible_model_and_turn_payload() {
    let server = ScriptedServer::spawn(vec![
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/sessions/session-1",
            StatusCode::OK,
            http::APPLICATION_JSON,
            SESSION_JSON,
        ),
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions/session-1/turns",
            r#"{"model":"gpt-5.5","messages":[{"role":"user","text":"override model"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-override-model",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.5",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-override-model/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"assistant.completed\",\"data\":{\"text\":\"override acknowledged\"}}\n\n\
             data: {\"seq\":2,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-override-model",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-override-model",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.5",
                "status":"succeeded",
                "outputText":"override acknowledged"
            }"#,
        ),
    ]);

    let home = tempfile::tempdir().unwrap();
    let mut session = spawn_tty_cli(
        home.path(),
        server.url(),
        &["agent", "resume", "session-1", "--model", "gpt-5.5"],
    );
    let mut output = String::new();
    session.wait_for(&mut output, "Session");
    session.wait_for(&mut output, "managed/gpt-5.5");
    session.write("override model\r");
    session.wait_for(&mut output, "override acknowledged");
    session.wait_for(&mut output, "Brewed for");
    session.write("\x03");
    session.wait_for(&mut output, "Resume with: gestalt agent resume session-1");
    session.wait_for_exit();

    assert!(
        output.contains("managed/gpt-5.5"),
        "TTY did not render resumed model override:\n{output}"
    );
    server.assert_finished();
}

#[cfg(unix)]
#[test]
fn test_cli_tty_renders_display_tool_activity_rows() {
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
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"display tools"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-display-tools",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-display-tools/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"provider.tool\",\"visibility\":\"public\",\"data\":{\"arguments\":{\"secret\":\"RAW_TUI_INPUT\"}},\"display\":{\"kind\":\"tool\",\"phase\":\"started\",\"action\":\"Running\",\"label\":\"lookup\",\"ref\":\"call-lookup\",\"input\":{\"query\":\"sample\"}}}\n\n\
             data: {\"seq\":2,\"type\":\"provider.tool\",\"visibility\":\"public\",\"display\":{\"kind\":\"tool\",\"phase\":\"progress\",\"label\":\"lookup\",\"ref\":\"call-lookup\",\"text\":\"halfway\"}}\n\n\
             data: {\"seq\":3,\"type\":\"provider.tool\",\"visibility\":\"public\",\"display\":{\"kind\":\"tool\",\"phase\":\"progress\",\"label\":\"lookup\",\"ref\":\"call-lookup\",\"text\":\"almost done\"}}\n\n\
             data: {\"seq\":4,\"type\":\"provider.tool\",\"visibility\":\"public\",\"data\":{\"output\":{\"secret\":\"RAW_TUI_OUTPUT\"}},\"display\":{\"kind\":\"tool\",\"phase\":\"completed\",\"action\":\"Ran\",\"label\":\"lookup\",\"ref\":\"call-lookup\",\"text\":\"200\",\"output\":{\"count\":1}}}\n\n\
             data: {\"seq\":5,\"type\":\"provider.status\",\"visibility\":\"public\",\"display\":{\"kind\":\"status\",\"phase\":\"completed\",\"text\":\"sync complete\"}}\n\n\
             data: {\"seq\":6,\"type\":\"provider.text\",\"visibility\":\"public\",\"display\":{\"kind\":\"text\",\"phase\":\"delta\",\"text\":\"tui\"}}\n\n\
             data: {\"seq\":7,\"type\":\"assistant.completed\",\"visibility\":\"public\",\"data\":{\"text\":\"tui suffix\"},\"display\":{\"kind\":\"text\",\"phase\":\"completed\"}}\n\n\
             data: {\"seq\":8,\"type\":\"provider.text\",\"visibility\":\"public\",\"display\":{\"kind\":\"text\",\"phase\":\"completed\",\"text\":\"display done\"}}\n\n\
             data: {\"seq\":9,\"type\":\"assistant.completed\",\"visibility\":\"public\",\"data\":{\"text\":\"raw tui fallback\"},\"display\":{\"kind\":123,\"text\":42}}\n\n\
             data: {\"seq\":10,\"type\":\"provider.secret\",\"visibility\":\"private\",\"display\":{\"kind\":\"text\",\"phase\":\"completed\",\"text\":\"hidden secret\"}}\n\n\
             data: {\"seq\":11,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-display-tools",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-display-tools",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"display done"
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
    session.write("display tools\r");
    session.wait_for(&mut output, "lookup");
    session.wait_for(&mut output, "Ran");
    session.wait_for(&mut output, "200");
    session.wait_for(&mut output, "count");
    session.wait_for(&mut output, "suffix");
    session.wait_for(&mut output, "done");
    session.wait_for(&mut output, "fallback");
    session.write("\x03");
    session.wait_for_exit();

    assert!(
        !output.contains("hidden secret"),
        "unknown private display event was rendered:\n{output}"
    );
    assert!(
        !output.contains("RAW_TUI_INPUT") && !output.contains("RAW_TUI_OUTPUT"),
        "display tool rendering leaked raw tool payload data:\n{output}"
    );
    assert!(
        output.contains("●")
            && output.contains("Ran")
            && output.contains("lookup")
            && output.contains("input")
            && output.contains("output")
            && output.contains("└─")
            && !output.contains("Tool")
            && !output.contains("tool>"),
        "TTY tool transcript did not render inline activity rows:\n{output}"
    );
    assert!(
        !output.contains("ended"),
        "tool progress events left duplicate running tool rows:\n{output}"
    );
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
         data: {{\"seq\":4,\"type\":\"tool.failed\",\"data\":{{\"toolCallId\":\"call-deploy\",\"toolName\":\"deploy\",\"error\":\"denied by policy\\ncontact admin\"}}}}\n\n\
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
    session.wait_for(&mut output, "contact admin");
    session.wait_for(&mut output, "done");
    session.write("\x03");
    session.wait_for_exit();

    assert!(
        !output.contains("hidden-tail"),
        "tool output preview was not truncated:\n{output}"
    );
    assert!(
        output.contains("●")
            && output.contains("lookup")
            && output.contains("├─ input")
            && output.contains("└─ output")
            && output.contains("deploy")
            && output.contains("└─ error")
            && output.contains("contact admin")
            && !output.contains("policycontact")
            && !output.contains("Tool"),
        "TTY tool transcript did not render structured activity rows:\n{output}"
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
    session.wait_for(&mut output, "completed");
    session.wait_for(&mut output, "lookup");
    session.wait_for(&mut output, "ended");
    session.wait_for(&mut output, "audit");
    session.write("\x03");
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
            "data: {\"seq\":1,\"type\":\"assistant.completed\",\"data\":{\"text\":\"beta response\"}}\n\n\
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
                "outputText":"beta response"
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
            "data: {\"seq\":1,\"type\":\"assistant.completed\",\"data\":{\"text\":\"gamma response\"}}\n\n\
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
                "outputText":"gamma response"
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
    session.wait_for(&mut output, "Show commands");
    session.write("remember this\r");
    session.wait_for(&mut output, "first");
    output.clear();
    session.write("\x1b[A\r");
    session.wait_for(&mut output, "working");
    output.clear();
    session.wait_for(&mut output, "ready");
    output.clear();
    session.write("draft text\x1b[A\x1b[B\r");
    session.wait_for(&mut output, "working");
    output.clear();
    session.wait_for(&mut output, "ready");
    session.write("\x03");
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
    session.wait_for(&mut output, "PgUp/PgDn");
    output.clear();
    session.write("\x1b[5~\x1b[5~");
    session.wait_for(&mut output, "Commands");
    output.clear();
    session.write("\x1b[6~\x1b[6~");
    session.wait_for(&mut output, "Ctrl-C");
    session.write("\x03");
    session.wait_for_exit();

    server.assert_finished();
}

#[cfg(unix)]
#[test]
fn test_cli_tty_renders_markdown_like_assistant_content() {
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
            r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"render markdown"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-markdown",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-markdown/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"assistant.completed\",\"data\":{\"text\":\"RAW_MARKDOWN_FALLBACK\"},\"display\":{\"kind\":\"text\",\"phase\":\"completed\",\"format\":\"markdown\",\"text\":\"**Fixture Heading**\\nUse `lookupItems` with _active_ filters and [Reference](https://example.test).\\n```json\\n{\\\"ok\\\":true}\\n```\\nKeep record_id and __init__ literal.\"}}\n\n\
             data: {\"seq\":2,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-markdown",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-markdown",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.4",
                "status":"succeeded",
                "outputText":"**Fixture Heading**\nUse `lookupItems` with _active_ filters and [Reference](https://example.test).\n```json\n{\"ok\":true}\n```\nKeep record_id and __init__ literal."
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
    session.write("render markdown\r");
    session.wait_for(&mut output, "Fixture Heading");
    session.wait_for(&mut output, "lookupItems");
    session.wait_for(&mut output, "active");
    session.wait_for(&mut output, "Reference");
    session.wait_for(&mut output, "https://example.test");
    session.wait_for(&mut output, "ok");
    session.wait_for(&mut output, "record_id");
    session.wait_for(&mut output, "__init__");
    session.wait_for(&mut output, "Brewed for");
    session.write("\x03");
    session.wait_for_exit();

    assert!(
        !output.contains("**Fixture Heading**")
            && !output.contains("`lookupItems`")
            && !output.contains("_active_")
            && !output.contains("[Reference]")
            && !output.contains("```")
            && !output.contains("RAW_MARKDOWN_FALLBACK"),
        "TTY assistant markdown syntax was not rendered away:\n{output}"
    );
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
    session.write("\x03");
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
    output.clear();
    session.wait_for(&mut output, "done-one");
    session.wait_for(&mut output, "Brewed for");
    session.wait_for(&mut output, "› pending turn");
    session.wait_for(&mut output, "done-two");
    session.wait_for(&mut output, "Brewed for");
    session.write("\x03");
    session.wait_for_exit();

    let done_one = output
        .find("done-one")
        .unwrap_or_else(|| panic!("queued TTY output did not include first response:\n{output}"));
    let first_elapsed = output[done_one..]
        .find("Brewed for")
        .map(|index| done_one + index)
        .unwrap_or_else(|| {
            panic!("queued TTY output did not include first elapsed row:\n{output}")
        });
    let queued_prompt = output
        .find("› pending turn")
        .unwrap_or_else(|| panic!("queued TTY output did not include queued prompt:\n{output}"));
    let done_two = output
        .find("done-two")
        .unwrap_or_else(|| panic!("queued TTY output did not include second response:\n{output}"));
    let second_elapsed = output[done_two..]
        .find("Brewed for")
        .map(|index| done_two + index)
        .unwrap_or_else(|| {
            panic!("queued TTY output did not include second elapsed row:\n{output}")
        });
    assert!(
        done_one < first_elapsed && first_elapsed < queued_prompt && queued_prompt < done_two,
        "queued prompt rendered before the active turn completion marker:\n{output}"
    );
    assert!(
        done_two < second_elapsed,
        "second turn completion marker did not follow second response:\n{output}"
    );
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
    session.write("\x03");
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
    session.wait_for(&mut output, "accepted");
    session.wait_for(&mut output, "Brewed for");
    session.write("\x03");
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
            "resume",
            "--provider",
            "managed",
            "--message",
            "continue plan",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("assistant> continued"))
        .stdout(predicate::str::contains(
            "Resume with: gestalt agent resume session-new",
        ))
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
        .args(["agent", "resume"])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "no active agent sessions found; use `gestalt agent` to create one",
        ));

    server.assert_finished();
}

#[test]
fn test_cli_rejects_interactive_agent_flags_with_subcommands() {
    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .args(["agent", "--model", "gpt-5.5", "sessions", "list"])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "--model must be passed before a prompt or after `agent resume`",
        ));
}

#[test]
fn test_cli_agent_help_describes_resume_command() {
    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .args(["agent", "--help"])
        .assert()
        .success()
        .stdout(predicate::str::contains("resume"))
        .stdout(predicate::str::contains("--resume").not())
        .stdout(predicate::str::contains("--continue").not())
        .stdout(predicate::str::contains("--ui").not());

    cli_command(home.path())
        .args(["agent", "resume", "--help"])
        .assert()
        .success()
        .stdout(predicate::str::contains("[SESSION_ID]"))
        .stdout(predicate::str::contains("Provider filter when resuming"));
}

#[test]
fn test_cli_agent_model_slash_lists_configured_providers() {
    let server = ScriptedServer::spawn(vec![
        ExpectedRequest::json(
            Method::POST,
            "/api/v1/agent/sessions",
            r#"{"provider":"managed","model":"gpt-5.4"}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            SESSION_JSON,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/providers",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "providers":[
                    {"name":"managed","default":true},
                    {"name":"anthropic"}
                ]
            }"#,
        ),
    ]);

    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url())
        .args(["agent", "--provider", "managed", "--model", "gpt-5.4"])
        .write_stdin("/model\n")
        .assert()
        .success()
        .stdout(predicate::str::contains(
            "Resume with: gestalt agent resume session-1",
        ))
        .stderr(predicate::str::contains("current provider: managed"))
        .stderr(predicate::str::contains("current model: gpt-5.4"))
        .stderr(predicate::str::contains("managed (default)"))
        .stderr(predicate::str::contains("anthropic"));

    server.assert_finished();
}

#[test]
fn test_cli_agent_model_slash_sets_future_turn_model() {
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
            r#"{"model":"gpt-5.5","messages":[{"role":"user","text":"hello"}]}"#,
            StatusCode::CREATED,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-model",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.5",
                "status":"running"
            }"#,
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-model/events/stream?after=0&limit=100&until=blocked_or_terminal",
            StatusCode::OK,
            "text/event-stream",
            "data: {\"seq\":1,\"type\":\"assistant.completed\",\"data\":{\"text\":\"hello from 5.5\"}}\n\n\
             data: {\"seq\":2,\"type\":\"turn.completed\",\"data\":{\"status\":\"succeeded\"}}\n\n",
        ),
        ExpectedRequest::text(
            Method::GET,
            "/api/v1/agent/turns/turn-model",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"{
                "id":"turn-model",
                "sessionId":"session-1",
                "provider":"managed",
                "model":"gpt-5.5",
                "status":"succeeded",
                "outputText":"hello from 5.5"
            }"#,
        ),
    ]);

    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url())
        .args(["agent", "--provider", "managed", "--model", "gpt-5.4"])
        .write_stdin("/model gpt-5.5\nhello\n")
        .assert()
        .success()
        .stdout(predicate::str::contains("assistant> hello from 5.5"))
        .stderr(predicate::str::contains(
            "model gpt-5.5 selected for future turns",
        ));

    server.assert_finished();
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
        .args(["agent", "resume", "session-2"])
        .write_stdin("need incident context\n\n")
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
        .write_stdin("approve deployment\nY\n")
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
