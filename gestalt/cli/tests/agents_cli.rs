mod support;

use serde_json::Value;
use std::collections::VecDeque;
use std::io::{BufRead, BufReader, Read};
use std::net::{TcpListener, TcpStream};
use std::sync::{Arc, Mutex};
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
        r#"{"model":"gpt-5.4","messages":[{"role":"user","text":"hello there"}]}"#.to_string(),
    ))
    .with_body(TURN_JSON)
    .create();
    let _events = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/agent/turns/turn-1/events?after=0&limit=100",
        StatusCode::OK
    )
    .with_body(
        r#"[
            {"seq":1,"type":"assistant.completed","data":{"text":"hello back"}},
            {"seq":2,"type":"turn.completed","data":{"status":"succeeded"}}
        ]"#,
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
            "outputText":"hello back"
        }"#,
    )
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["agent", "--provider", "managed", "--model", "gpt-5.4"])
        .write_stdin("hello there\n/quit\n")
        .assert()
        .success()
        .stdout(predicate::str::contains("assistant> hello back"))
        .stderr(predicate::str::contains(
            "Session session-1 [managed / gpt-5.4]",
        ))
        .stderr(predicate::str::contains("Type /quit to exit."));
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
            "/api/v1/agent/turns/turn-input/events?after=0&limit=100",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"[
                {"seq":1,"type":"turn.started","data":{"status":"running"}},
                {"seq":2,"type":"interaction.requested","data":{"interaction_id":"interaction-2"}}
            ]"#,
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
            "/api/v1/agent/turns/turn-input/events?after=2&limit=100",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"[
                {"seq":3,"type":"interaction.resolved","data":{"interaction_id":"interaction-2"}},
                {"seq":4,"type":"assistant.completed","data":{"text":"incident INC-42 summarized"}},
                {"seq":5,"type":"turn.completed","data":{"status":"succeeded"}}
            ]"#,
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
            "/api/v1/agent/turns/turn-approval/events?after=0&limit=100",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"[
                {"seq":1,"type":"turn.started","data":{"status":"running"}},
                {"seq":2,"type":"interaction.requested","data":{"interaction_id":"interaction-1"}}
            ]"#,
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
            "/api/v1/agent/turns/turn-approval/events?after=2&limit=100",
            StatusCode::OK,
            http::APPLICATION_JSON,
            r#"[
                {"seq":3,"type":"interaction.resolved","data":{"interaction_id":"interaction-1"}},
                {"seq":4,"type":"assistant.completed","data":{"text":"deployment approved"}},
                {"seq":5,"type":"turn.completed","data":{"status":"succeeded"}}
            ]"#,
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
        }
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
