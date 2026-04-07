use std::ffi::OsString;
use std::io::{BufRead, BufReader, Read};
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, OnceLock};

use assert_cmd::Command;
use gestalt::http;
use gestalt::output::Format;
use mockito::{Matcher, Server};
use predicates::prelude::*;
use reqwest::{Method, StatusCode, header};
use tempfile::TempDir;

const TEST_TOKEN: &str = "test-token";

macro_rules! json_mock {
    ($server:expr, $method:expr, $path:expr, $status:expr) => {
        $server
            .mock($method.as_str(), $path)
            .with_status(usize::from($status.as_u16()))
            .with_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    };
}

macro_rules! authed_json_mock {
    ($server:expr, $method:expr, $path:expr, $status:expr) => {
        json_mock!($server, $method, $path, $status).match_header(
            header::AUTHORIZATION.as_str(),
            Matcher::Exact(test_bearer()),
        )
    };
}

fn create_client(server: &Server) -> gestalt::api::ApiClient {
    gestalt::api::ApiClient::new(&server.url(), TEST_TOKEN).unwrap()
}

fn test_bearer() -> String {
    format!("Bearer {TEST_TOKEN}")
}

fn env_lock() -> std::sync::MutexGuard<'static, ()> {
    static LOCK: OnceLock<Mutex<()>> = OnceLock::new();
    LOCK.get_or_init(|| Mutex::new(())).lock().unwrap()
}

struct EnvGuard(Vec<(&'static str, Option<OsString>)>);

impl EnvGuard {
    fn new(config_root: &Path) -> Self {
        let saved = vec![
            ("HOME", std::env::var_os("HOME")),
            ("XDG_CONFIG_HOME", std::env::var_os("XDG_CONFIG_HOME")),
            ("GESTALT_URL", std::env::var_os("GESTALT_URL")),
            (
                gestalt::api::ENV_API_KEY,
                std::env::var_os(gestalt::api::ENV_API_KEY),
            ),
        ];
        unsafe {
            std::env::set_var("HOME", config_root);
            std::env::set_var("XDG_CONFIG_HOME", config_root);
            std::env::remove_var("GESTALT_URL");
            std::env::remove_var(gestalt::api::ENV_API_KEY);
        }
        Self(saved)
    }
}

struct CurrentDirGuard {
    saved: PathBuf,
}

impl CurrentDirGuard {
    fn new(path: &Path) -> Self {
        let saved = std::env::current_dir().unwrap();
        std::env::set_current_dir(path).unwrap();
        Self { saved }
    }
}

impl Drop for CurrentDirGuard {
    fn drop(&mut self) {
        std::env::set_current_dir(&self.saved).unwrap();
    }
}

impl Drop for EnvGuard {
    fn drop(&mut self) {
        for (key, value) in self.0.drain(..) {
            unsafe {
                match value {
                    Some(value) => std::env::set_var(key, value),
                    None => std::env::remove_var(key),
                }
            }
        }
    }
}

#[derive(Default)]
struct LoginFlowState {
    callback_port: Option<u16>,
    expected_state: Option<String>,
    browser_response_html: Option<String>,
}

struct HttpRequest {
    method: Method,
    target: String,
    body: Vec<u8>,
}

struct LoginServer {
    base_url: String,
    state: Arc<Mutex<LoginFlowState>>,
    handle: std::thread::JoinHandle<()>,
}

fn spawn_login_server() -> LoginServer {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let base_url = format!("http://{}", listener.local_addr().unwrap());
    let state = Arc::new(Mutex::new(LoginFlowState::default()));
    let server_state = Arc::clone(&state);
    let server_url = base_url.clone();
    let handle = std::thread::spawn(move || {
        let mut workers = Vec::new();
        for _ in 0..3 {
            let (stream, _) = listener.accept().unwrap();
            let state = Arc::clone(&server_state);
            let base_url = server_url.clone();
            workers.push(std::thread::spawn(move || {
                handle_login_request(stream, state, &base_url);
            }));
        }
        for worker in workers {
            worker.join().unwrap();
        }
    });
    LoginServer {
        base_url,
        state,
        handle,
    }
}

fn handle_login_request(mut stream: TcpStream, state: Arc<Mutex<LoginFlowState>>, base_url: &str) {
    let request = read_http_request(&mut stream);
    match request.target.as_str() {
        "/api/v1/auth/login" if request.method == Method::POST => {
            let body: serde_json::Value = serde_json::from_slice(&request.body).unwrap();
            let mut state = state.lock().unwrap();
            state.callback_port = Some(body["callback_port"].as_u64().unwrap() as u16);
            state.expected_state = Some(body["state"].as_str().unwrap().to_string());
            write_http_response(
                &mut stream,
                StatusCode::OK,
                http::APPLICATION_JSON,
                &format!(r#"{{"url":"{base_url}/browser-login"}}"#),
            );
        }
        "/browser-login" if request.method == Method::GET => {
            let (callback_port, expected_state) = {
                let state = state.lock().unwrap();
                (
                    state.callback_port.expect("missing callback port"),
                    state.expected_state.clone().expect("missing state"),
                )
            };
            let callback_url =
                format!("http://127.0.0.1:{callback_port}/?code=test-code&state={expected_state}");
            let html = reqwest::blocking::get(callback_url)
                .unwrap()
                .text()
                .unwrap();
            state.lock().unwrap().browser_response_html = Some(html);
            write_http_response(&mut stream, StatusCode::OK, http::TEXT_PLAIN, "ok");
        }
        target
            if request.method == Method::GET
                && target.starts_with("/api/v1/auth/login/callback?") =>
        {
            let url = url::Url::parse(&format!("http://localhost{target}")).unwrap();
            let params = url
                .query_pairs()
                .collect::<std::collections::HashMap<_, _>>();
            let expected_state = state.lock().unwrap().expected_state.clone().unwrap();
            assert_eq!(params.get("code").map(|v| v.as_ref()), Some("test-code"));
            assert_eq!(params.get("cli").map(|v| v.as_ref()), Some("1"));
            assert_eq!(
                params.get("state").map(|v| v.as_ref()),
                Some(expected_state.as_str())
            );
            write_http_response(
                &mut stream,
                StatusCode::OK,
                http::APPLICATION_JSON,
                r#"{"token":"cli-secret","id":"tok-123"}"#,
            );
        }
        _ => panic!("unexpected request: {} {}", request.method, request.target),
    }
}

fn read_http_request(stream: &mut TcpStream) -> HttpRequest {
    let mut reader = BufReader::new(stream.try_clone().unwrap());
    let mut request_line = String::new();
    reader.read_line(&mut request_line).unwrap();

    let mut content_length = 0usize;
    loop {
        let mut line = String::new();
        reader.read_line(&mut line).unwrap();
        if line == "\r\n" {
            break;
        }
        if let Some((name, value)) = line.split_once(':')
            && name.eq_ignore_ascii_case(header::CONTENT_LENGTH.as_str())
        {
            content_length = value.trim().parse().unwrap();
        }
    }

    let mut body = vec![0; content_length];
    reader.read_exact(&mut body).unwrap();

    let mut parts = request_line.split_whitespace();
    HttpRequest {
        method: Method::from_bytes(parts.next().unwrap().as_bytes()).unwrap(),
        target: parts.next().unwrap().to_string(),
        body,
    }
}

fn write_http_response(stream: &mut TcpStream, status: StatusCode, content_type: &str, body: &str) {
    http::write_response(&mut *stream, status, content_type, body, &[]).unwrap();
}

fn cli_command(home: &Path) -> Command {
    std::fs::create_dir_all(home).unwrap();
    let mut cmd = Command::cargo_bin("gestalt").unwrap();
    cmd.env("HOME", home)
        .env("XDG_CONFIG_HOME", home.join("xdg-config"))
        .env_remove(gestalt::api::ENV_API_KEY)
        .env_remove("GESTALT_URL");
    cmd
}

fn run_cli(server: &Server, args: &[&str]) -> std::process::Output {
    let dir = tempfile::tempdir().unwrap();
    std::process::Command::new(env!("CARGO_BIN_EXE_gestalt"))
        .current_dir(dir.path())
        .env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url())
        .args(args)
        .output()
        .unwrap()
}

fn cli_command_for_server(home: &Path, server: &Server) -> Command {
    let mut cmd = cli_command(home);
    cmd.env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url());
    cmd
}

#[test]
fn test_list_integrations() {
    let mut server = Server::new();
    let mock = authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
        .with_body(
            r#"[{"name":"github","display_name":"GitHub","description":"GitHub integration"}]"#,
        )
        .create();

    let client = create_client(&server);
    let resp = client.get("/api/v1/integrations").unwrap();

    mock.assert();
    let items = resp.as_array().unwrap();
    assert_eq!(items.len(), 1);
    assert_eq!(items[0]["name"], "github");
}

#[test]
fn test_list_tokens() {
    let mut server = Server::new();
    let mock = authed_json_mock!(server, Method::GET, "/api/v1/tokens", StatusCode::OK)
        .with_body(
            r#"[{"id":"1","name":"my-token","scopes":"","created_at":"2025-01-01T00:00:00Z"}]"#,
        )
        .create();

    let client = create_client(&server);
    let resp = client.get("/api/v1/tokens").unwrap();

    mock.assert();
    let items = resp.as_array().unwrap();
    assert_eq!(items.len(), 1);
    assert_eq!(items[0]["name"], "my-token");
}

#[test]
fn test_create_token() {
    let mut server = Server::new();
    let mock = authed_json_mock!(server, Method::POST, "/api/v1/tokens", StatusCode::CREATED)
        .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
        .match_body(Matcher::JsonString(r#"{"name":"cli-token"}"#.to_string()))
        .with_body(r#"{"id":"2","name":"cli-token","token":"plaintext-secret"}"#)
        .create();

    let client = create_client(&server);
    let body = serde_json::json!({"name": "cli-token"});
    let resp = client.post("/api/v1/tokens", &body).unwrap();

    mock.assert();
    assert_eq!(resp["token"], "plaintext-secret");
}

#[test]
fn test_revoke_token() {
    let mut server = Server::new();
    let mock = authed_json_mock!(server, Method::DELETE, "/api/v1/tokens/42", StatusCode::OK)
        .with_body(r#"{"status":"revoked"}"#)
        .create();

    let client = create_client(&server);
    let resp = client.delete("/api/v1/tokens/42").unwrap();

    mock.assert();
    assert_eq!(resp["status"], "revoked");
}

#[test]
fn test_execute_operation() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/github/search_code",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .with_body(r#"{"results":[]}"#)
    .create();

    let client = create_client(&server);
    let body = serde_json::json!({"query": "hello"});
    let resp = client.post("/api/v1/github/search_code", &body).unwrap();

    mock.assert();
    assert_eq!(resp["results"], serde_json::json!([]));
}

#[test]
fn test_error_response() {
    let mut server = Server::new();
    let mock = json_mock!(
        server,
        Method::GET,
        "/api/v1/tokens",
        StatusCode::UNAUTHORIZED
    )
    .with_body(r#"{"error":"missing authorization header"}"#)
    .create();

    let client = create_client(&server);
    let result = client.get("/api/v1/tokens");

    mock.assert();
    assert!(result.is_err());
    let err = result.unwrap_err().to_string();
    assert!(err.contains("missing authorization header"));

    let home = TempDir::new().unwrap();
    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/auth_svc/operations",
        StatusCode::OK
    )
    .with_body(single_operation_catalog("list_items"))
    .create();

    let _invoke_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/auth_svc/list_items",
        StatusCode::PRECONDITION_FAILED
    )
    .with_body(
        r#"{"error":"no token stored for integration \"auth_svc\"; connect via OAuth first"}"#,
    )
    .create();

    cli_command_for_server(home.path(), &server)
        .args(["invoke", "auth_svc", "list_items"])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "failed to invoke auth_svc.list_items: integration \"auth_svc\" is not connected. Connect it first with `gestalt integrations connect auth_svc`",
        ))
        .stderr(predicate::str::contains("OAuth first").not());
}

#[test]
fn test_error_response_nested_message() {
    let mut server = Server::new();
    let mock = json_mock!(
        server,
        Method::GET,
        "/api/v1/tokens",
        StatusCode::BAD_REQUEST
    )
    .with_body(r#"{"error":{"message":"invalid parameter: limit"}}"#)
    .create();

    let client = create_client(&server);
    let result = client.get("/api/v1/tokens");

    mock.assert();
    assert!(result.is_err());
    let err = result.unwrap_err().to_string();
    assert!(err.contains("invalid parameter: limit"));
}

#[test]
fn test_list_operations_formats_parameters() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
        .with_body(
            r#"[{
                "id": "do_thing",
                "description": "Does a thing",
                "method": "POST",
                "parameters": [
                    {"name": "id", "type": "string", "location": "path", "required": true, "description": "The ID"},
                    {"name": "mode", "type": "string", "location": "query", "required": false, "description": "Mode"}
                ]
            },{
                "id": "workflowStateCreate",
                "description": "Creates a workflow state",
                "method": "POST",
                "parameters": [
                    {"name": "input", "type": "object{name!, position, teamId!}", "required": true}
                ]
            },{
                "id": "save_comment",
                "description": "Create or update a comment",
                "method": "POST",
                "parameters": [
                    {"name": "body", "type": "string", "required": true},
                    {"name": "issueId", "type": "string", "required": true}
                ]
            }]"#,
        )
        .create();

    let output = run_cli(&server, &["invoke", "test_svc"]);
    mock.assert();
    assert!(
        output.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(stdout.contains("id=<string> [path]"), "stdout: {stdout}");
    assert!(stdout.contains("mode=<string> [query]"), "stdout: {stdout}");
    assert!(stdout.contains("workflowStateCreate"), "stdout: {stdout}");
    assert!(stdout.contains("object{name!,"), "stdout: {stdout}");
    assert!(stdout.contains("position, teamId!}>"), "stdout: {stdout}");
    assert!(stdout.contains("-p body=<string>"), "stdout: {stdout}",);
    assert!(stdout.contains("issueId=<string>"), "stdout: {stdout}",);
    assert!(
        stdout.matches("(required)").count() >= 3,
        "stdout: {stdout}"
    );
}

#[test]
fn test_list_operations_json_format() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
        .with_body(
            r#"[{
                "id": "do_thing",
                "description": "Does a thing",
                "method": "POST",
                "parameters": [
                    {"name": "id", "type": "string", "location": "path", "required": true, "description": "The ID"}
                ]
            }]"#,
        )
        .create();

    let client = create_client(&server);
    let result = gestalt::commands::invoke::list_operations(&client, "test_svc", Format::Json);

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_list_operations_empty_parameters() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
        .with_body(r#"[{"id": "list_items", "description": "Lists items", "method": "GET", "parameters": []}]"#)
        .create();

    let client = create_client(&server);
    let result = gestalt::commands::invoke::list_operations(&client, "test_svc", Format::Table);

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_start_oauth() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/start-oauth",
        StatusCode::OK
    )
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let client = create_client(&server);
    let body = serde_json::json!({"integration": "github"});
    let resp = client.post("/api/v1/auth/start-oauth", &body).unwrap();

    mock.assert();
    assert_eq!(resp["url"], "https://example.com/oauth");
    assert_eq!(resp["state"], "abc123");
}

#[test]
fn test_auth_login_stores_credentials_and_serves_styled_browser_page() {
    let _lock = env_lock();
    let tempdir = tempfile::tempdir().unwrap();
    let _env = EnvGuard::new(tempdir.path());
    let server = spawn_login_server();
    let browser = Arc::new(Mutex::new(None));
    let browser_handle = Arc::clone(&browser);

    gestalt::commands::auth::login_with_browser_opener(Some(&server.base_url), |url| {
        let url = url.to_string();
        *browser_handle.lock().unwrap() = Some(std::thread::spawn(move || {
            reqwest::blocking::get(url).unwrap();
        }));
        Ok(())
    })
    .unwrap();

    let LoginServer {
        base_url,
        state,
        handle,
    } = server;
    browser
        .lock()
        .unwrap()
        .take()
        .expect("browser thread missing")
        .join()
        .unwrap();
    handle.join().unwrap();

    let html = state.lock().unwrap().browser_response_html.clone().unwrap();
    assert!(html.contains("<small>Gestalt</small>"));
    assert!(html.contains("Login successful"));
    assert!(html.contains("radial-gradient(140% 90% at 50% 100%"));
    assert!(!html.contains("Gestalt CLI"));
    assert!(!html.contains("CLI login complete"));
    assert!(!html.contains("class=\"pill\""));

    let credentials_path = dirs::config_dir()
        .unwrap()
        .join("gestalt")
        .join("credentials.json");
    let credentials: serde_json::Value =
        serde_json::from_str(&std::fs::read_to_string(credentials_path).unwrap()).unwrap();
    assert_eq!(credentials["api_url"], base_url);
    assert_eq!(credentials["api_token"], "cli-secret");
    assert_eq!(credentials["api_token_id"], "tok-123");
}

#[test]
fn test_connect_includes_connection_and_instance() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(r#"[{"name":"github","auth_types":["oauth"],"connected":false}]"#)
            .create();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/start-oauth",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"workspace","instance":"team-a","integration":"github"}"#.to_string(),
    ))
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::integrations::connect_with_browser_opener(
        &client,
        "github",
        Some("workspace"),
        Some("team-a"),
        |_| Ok(()),
    );

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_connect_omits_null_connection_and_instance() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(r#"[{"name":"github","auth_types":["oauth"],"connected":false}]"#)
            .create();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/start-oauth",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"integration":"github"}"#.to_string(),
    ))
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::integrations::connect_with_browser_opener(
        &client,
        "github",
        None,
        None,
        |_| Ok(()),
    );

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_manual_connect_uses_prompted_credentials_and_connection_params() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
        .with_body(
            r#"[{
                "name":"datadog",
                "display_name":"Datadog",
                "description":"Metrics and logs",
                "auth_types":["manual"],
                "connection_params":{"site":{"description":"Datadog site","default":"datadoghq.com","required":true}},
                "credential_fields":[{"name":"api_key","label":"API key","description":"Use a personal API key","help_url":"https://docs.example.com/datadog"}]
            }]"#,
        )
        .create();
    let _connect = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/connect-manual",
        StatusCode::OK
    )
        .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
        .match_body(Matcher::JsonString(
            r#"{"connection_params":{"site":"datadoghq.eu"},"credential":"dd-key","integration":"datadog"}"#.to_string(),
        ))
        .with_body(r#"{"status":"connected","integration":"datadog"}"#)
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["integrations", "connect", "datadog"])
        .write_stdin("datadoghq.eu\ndd-key\n")
        .assert()
        .success()
        .stderr(predicate::str::contains("Datadog site"))
        .stderr(predicate::str::contains("API key"))
        .stderr(predicate::str::contains("Connected datadog."));
}

#[test]
fn test_manual_connect_prompts_for_connection_and_finishes_candidate_selection() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
        .with_body(
            r#"[{
                "name":"manual-svc",
                "display_name":"Manual Service",
                "auth_types":["manual"],
                "connections":[
                    {"name":"workspace","credential_fields":[{"name":"token","label":"Workspace token"}]},
                    {"name":"plugin","auth_types":["oauth"]}
                ]
            }]"#,
        )
        .create();
    let _connect = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/connect-manual",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"workspace","credential":"abc123","integration":"manual-svc"}"#
            .to_string(),
    ))
    .with_body(
        r#"{
                "status":"selection_required",
                "integration":"manual-svc",
                "selection_url":"/api/v1/auth/pending-connection",
                "pending_token":"pending-123",
                "candidates":[
                    {"id":"site-a","name":"Site A"},
                    {"id":"site-b","name":"Site B"}
                ]
            }"#,
    )
    .create();
    let _select = server
        .mock(Method::POST.as_str(), "/api/v1/auth/pending-connection")
        .match_header(
            header::AUTHORIZATION.as_str(),
            Matcher::Exact(test_bearer()),
        )
        .match_header(
            header::CONTENT_TYPE.as_str(),
            Matcher::Regex(format!("{}.*", http::APPLICATION_X_WWW_FORM_URLENCODED)),
        )
        .match_body(Matcher::Exact(
            "pending_token=pending-123&candidate_index=1".to_string(),
        ))
        .with_status(usize::from(StatusCode::OK.as_u16()))
        .with_header(header::CONTENT_TYPE.as_str(), http::TEXT_HTML)
        .with_body("<html>ok</html>")
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["integrations", "connect", "manual-svc"])
        .write_stdin("1\nabc123\n2\n")
        .assert()
        .success()
        .stderr(predicate::str::contains(
            "Select a Manual Service connection:",
        ))
        .stderr(predicate::str::contains("Workspace token"))
        .stderr(predicate::str::contains(
            "Gestalt found more than one manual-svc connection. Choose one to save:",
        ))
        .stderr(predicate::str::contains("Connected manual-svc (Site B)"));
}

#[test]
fn test_oauth_connect_still_prefers_browser_flow_when_manual_also_exists() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(
                r#"[{
                "name":"github",
                "display_name":"GitHub",
                "auth_types":["oauth","manual"],
                "connection_params":{"site":{"description":"GitHub site","required":true}},
                "credential_fields":[{"name":"token","label":"Token"}]
            }]"#,
            )
            .create();
    let _oauth = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/start-oauth",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection_params":{"site":"github.valon.com"},"integration":"github"}"#.to_string(),
    ))
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    let browser_bin = tempfile::tempdir().unwrap();
    for command in ["open", "xdg-open"] {
        let path = browser_bin.path().join(command);
        std::fs::write(&path, "#!/bin/sh\nexit 0\n").unwrap();
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;

            let mut perms = std::fs::metadata(&path).unwrap().permissions();
            perms.set_mode(0o755);
            std::fs::set_permissions(&path, perms).unwrap();
        }
    }

    let path = std::env::join_paths(
        std::iter::once(browser_bin.path().to_path_buf()).chain(
            std::env::var_os("PATH")
                .into_iter()
                .flat_map(|value| std::env::split_paths(&value).collect::<Vec<_>>()),
        ),
    )
    .unwrap();

    cli_command_for_server(home.path(), &server)
        .env("PATH", path)
        .args(["integrations", "connect", "github"])
        .write_stdin("github.valon.com\n")
        .assert()
        .success()
        .stderr(predicate::str::contains("GitHub site"))
        .stderr(predicate::str::contains(
            "Opening browser to connect github...",
        ))
        .stderr(predicate::str::contains(
            "If the browser doesn't open, visit: https://example.com/oauth",
        ));
}

#[test]
fn test_manual_connect_falls_back_to_generic_credential_prompt() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(r#"[{"name":"manual-svc","auth_types":["manual"]}]"#)
            .create();
    let _connect = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/connect-manual",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"credential":"secret","integration":"manual-svc"}"#.to_string(),
    ))
    .with_body(r#"{"status":"connected","integration":"manual-svc"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["integrations", "connect", "manual-svc"])
        .write_stdin("secret\n")
        .assert()
        .success()
        .stderr(predicate::str::contains("\nCredential\n"))
        .stderr(predicate::str::contains("Connected manual-svc."));
}

#[test]
fn test_manual_connect_fails_when_stdin_closes_during_prompt() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(r#"[{"name":"manual-svc","auth_types":["manual"]}]"#)
            .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["integrations", "connect", "manual-svc"])
        .write_stdin("")
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "stdin closed while waiting for input",
        ));
}

fn catalog_body() -> &'static str {
    r#"[{
        "id": "do_thing",
        "title": "Do Thing",
        "description": "Does a thing",
        "method": "POST",
        "parameters": [
            {"name": "name", "type": "string", "location": "query", "required": true},
            {"name": "count", "type": "integer", "location": "query"},
            {"name": "tags", "type": "array", "location": "query"}
        ],
        "transport": "rest"
    }]"#
}

fn single_operation_catalog(id: &str) -> String {
    format!(r#"[{{"id":"{id}"}}]"#)
}

#[test]
fn test_invoke_with_connection_and_instance() {
    let mut server = Server::new();

    let _catalog_mock = json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(catalog_body())
    .create();

    let invoke_mock = json_mock!(
        server,
        Method::POST,
        "/api/v1/test_svc/do_thing",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"_connection":"workspace","_instance":"team-a","name":"test"}"#.to_string(),
    ))
    .with_body(r#"{"ok": true}"#)
    .create();

    let params = vec![gestalt::params::ParamEntry {
        key: "name".to_string(),
        value: gestalt::params::ParamValue::StringVal("test".to_string()),
    }];

    let client = create_client(&server);
    let result = gestalt::commands::invoke::invoke(
        &client,
        "test_svc",
        "do_thing",
        &params,
        gestalt::commands::invoke::InvokeOptions {
            connection: Some("workspace"),
            instance: Some("team-a"),
            ..Default::default()
        },
        Format::Json,
    );

    invoke_mock.assert();
    assert!(result.is_ok());

    let _secondary_catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/other_svc/operations",
        StatusCode::OK
    )
    .with_body(single_operation_catalog("check_status"))
    .create();

    let secondary_invoke_mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/other_svc/check_status",
        StatusCode::PRECONDITION_FAILED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"_connection":"workspace","_instance":"team-a"}"#.to_string(),
    ))
    .with_body(r#"{"error":"no token stored for integration \"other_svc\" instance \"team-a\""}"#)
    .create();

    let err = format!(
        "{:#}",
        gestalt::commands::invoke::invoke(
            &client,
            "other_svc",
            "check_status",
            &[],
            gestalt::commands::invoke::InvokeOptions {
                connection: Some("workspace"),
                instance: Some("team-a"),
                ..Default::default()
            },
            Format::Json,
        )
        .unwrap_err()
    );

    secondary_invoke_mock.assert();
    assert!(err.contains(
        "Connect it first with `gestalt integrations connect other_svc --connection workspace --instance team-a`"
    ));
}

#[test]
fn test_describe_operation() {
    let mut server = Server::new();

    let mock = json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(catalog_body())
    .create();

    let client = create_client(&server);
    let result =
        gestalt::commands::describe::describe(&client, "test_svc", "do_thing", Format::Table);

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_cli_config_set_and_get_json() {
    let home = TempDir::new().unwrap();

    let mut set_cmd = cli_command(home.path());
    set_cmd.args(["config", "set", "url", "localhost:9999"]);
    set_cmd
        .assert()
        .success()
        .stderr(predicate::str::contains("url = http://localhost:9999"));

    let mut get_cmd = cli_command(home.path());
    get_cmd.args(["--format", "json", "config", "get", "url"]);
    get_cmd.assert().success().stdout(predicate::str::contains(
        "\"url\": \"http://localhost:9999\"",
    ));
}

#[test]
fn test_resolve_url_prefers_project_config_file() {
    let _lock = env_lock();
    let config_root = TempDir::new().unwrap();
    let _env = EnvGuard::new(config_root.path());
    let workspace = TempDir::new().unwrap();
    let repo_root = workspace.path().join("repo");
    let nested = repo_root.join("nested");

    std::fs::create_dir_all(repo_root.join(".gestalt")).unwrap();
    std::fs::create_dir_all(&nested).unwrap();
    std::fs::write(
        repo_root.join(".gestalt/config.json"),
        "{\n  \"url\": \"https://project.example.com\"\n}\n",
    )
    .unwrap();

    let _cwd = CurrentDirGuard::new(&nested);
    let resolved = gestalt::api::resolve_url(Some("localhost:9999")).unwrap();
    assert_eq!(resolved, "http://localhost:9999");

    let resolved = gestalt::api::resolve_url(None).unwrap();
    assert_eq!(resolved, "https://project.example.com");
}

#[test]
fn test_resolve_url_ignores_legacy_project_file_and_uses_global_config() {
    let _lock = env_lock();
    let config_root = TempDir::new().unwrap();
    let _env = EnvGuard::new(config_root.path());
    let workspace = TempDir::new().unwrap();
    let repo_root = workspace.path().join("repo");
    let nested = repo_root.join("nested");

    std::fs::create_dir_all(&nested).unwrap();
    std::fs::write(
        repo_root.join(".gestalt.json"),
        "{\n  \"url\": \"https://legacy.example.com\"\n}\n",
    )
    .unwrap();

    let mut set_cmd = cli_command(config_root.path());
    set_cmd.args(["config", "set", "url", "https://global.example.com"]);
    set_cmd.assert().success();

    let _cwd = CurrentDirGuard::new(&nested);
    let resolved = gestalt::api::resolve_url(None).unwrap();
    assert_eq!(resolved, "https://global.example.com");
}

#[test]
fn test_cli_integrations_list_table_output() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();
    let mock = authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
        .with_body(
            r#"[{"name":"github","description":"GitHub integration with a longer description","connected":true}]"#,
        )
        .create();

    let mut cmd = cli_command(home.path());
    cmd.env("GESTALT_API_KEY", TEST_TOKEN)
        .args(["--url", &server.url(), "integrations", "list"]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("GITHUB").or(predicate::str::contains("github")))
        .stdout(predicate::str::contains("GitHub integration"))
        .stdout(
            predicate::str::contains("Connected")
                .or(predicate::str::contains("CONNECTED"))
                .or(predicate::str::contains("connected")),
        );

    mock.assert();
}

#[test]
fn test_cli_invoke_merges_file_params_and_selects_output() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();
    let input_file = home.path().join("input.json");
    std::fs::write(&input_file, r#"{"count":1,"name":"from-file"}"#).unwrap();

    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(catalog_body())
    .create();

    let invoke_mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/test_svc/do_thing",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"count":42,"name":"override","tags":["one","two"]}"#.to_string(),
    ))
    .with_body(r#"{"result":{"id":"1"}}"#)
    .create();

    let mut cmd = cli_command(home.path());
    cmd.env("GESTALT_API_KEY", TEST_TOKEN).args([
        "--url",
        &server.url(),
        "--format",
        "json",
        "invoke",
        "test_svc",
        "do_thing",
        "-p",
        "name=override",
        "-p",
        "count:=42",
        "-p",
        "tags=one",
        "-p",
        "tags=two",
        "--input-file",
        input_file.to_str().unwrap(),
        "--select",
        "result.id",
    ]);
    cmd.assert().success().stdout("\"1\"\n");

    invoke_mock.assert();
}

#[test]
fn test_cli_invoke_table_keeps_nested_json_inline() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();
    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(catalog_body())
    .create();

    let invoke_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/test_svc/do_thing",
        StatusCode::OK
    )
    .with_body(
        r#"{
                "id":"abc123",
                "status":"ok",
                "user":{"name":"Amy","email":"amy@example.com"},
                "labels":["prod","urgent"],
                "jobs":[{"id":"j1","state":"done"},{"id":"j2","state":"running"}]
            }"#,
    )
    .create();

    let mut cmd = cli_command(home.path());
    cmd.env("GESTALT_API_KEY", TEST_TOKEN).args([
        "--url",
        &server.url(),
        "invoke",
        "test_svc",
        "do_thing",
    ]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("user"))
        .stdout(predicate::str::contains(
            r#"{"email":"amy@example.com","name":"Amy"}"#,
        ))
        .stdout(predicate::str::contains("labels"))
        .stdout(predicate::str::contains(r#"["prod","urgent"]"#))
        .stdout(predicate::str::contains("jobs"))
        .stdout(predicate::str::contains(
            r#"[{"id":"j1","state":"done"},{"id":"j2","state":"running"}]"#,
        ));

    invoke_mock.assert();
}

#[test]
fn test_cli_invoke_rejects_duplicate_scalar_params() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();
    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(catalog_body())
    .create();

    let mut cmd = cli_command(home.path());
    cmd.env("GESTALT_API_KEY", "test-token").args([
        "--url",
        &server.url(),
        "invoke",
        "test_svc",
        "do_thing",
        "-p",
        "name=first",
        "-p",
        "name=second",
    ]);
    cmd.assert().failure().stderr(predicate::str::contains(
        "parameter 'name' is not an array type but was specified multiple times",
    ));
}
