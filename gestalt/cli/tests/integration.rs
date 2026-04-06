use std::ffi::OsString;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::Path;
use std::sync::{Arc, Mutex, OnceLock};

use assert_cmd::Command;
use gestalt::output::Format;
use mockito::{Matcher, Server};
use predicates::prelude::*;
use tempfile::TempDir;

fn create_client(server: &Server) -> gestalt::api::ApiClient {
    gestalt::api::ApiClient::new(&server.url(), "test-token").unwrap()
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
            (
                gestalt::api::ENV_API_KEY,
                std::env::var_os(gestalt::api::ENV_API_KEY),
            ),
        ];
        unsafe {
            std::env::set_var("HOME", config_root);
            std::env::set_var("XDG_CONFIG_HOME", config_root);
            std::env::remove_var(gestalt::api::ENV_API_KEY);
        }
        Self(saved)
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
    method: String,
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
    match (request.method.as_str(), request.target.as_str()) {
        ("POST", "/api/v1/auth/login") => {
            let body: serde_json::Value = serde_json::from_slice(&request.body).unwrap();
            let mut state = state.lock().unwrap();
            state.callback_port = Some(body["callback_port"].as_u64().unwrap() as u16);
            state.expected_state = Some(body["state"].as_str().unwrap().to_string());
            write_http_response(
                &mut stream,
                "200 OK",
                "application/json",
                &format!(r#"{{"url":"{base_url}/browser-login"}}"#),
            );
        }
        ("GET", "/browser-login") => {
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
            write_http_response(&mut stream, "200 OK", "text/plain", "ok");
        }
        ("GET", target) if target.starts_with("/api/v1/auth/login/callback?") => {
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
                "200 OK",
                "application/json",
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
            && name.eq_ignore_ascii_case("Content-Length")
        {
            content_length = value.trim().parse().unwrap();
        }
    }

    let mut body = vec![0; content_length];
    reader.read_exact(&mut body).unwrap();

    let mut parts = request_line.split_whitespace();
    HttpRequest {
        method: parts.next().unwrap().to_string(),
        target: parts.next().unwrap().to_string(),
        body,
    }
}

fn write_http_response(stream: &mut TcpStream, status: &str, content_type: &str, body: &str) {
    write!(
        stream,
        "HTTP/1.1 {status}\r\nContent-Type: {content_type}; charset=utf-8\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
        body.len()
    )
    .unwrap();
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
        .env("GESTALT_API_KEY", "test-token")
        .arg("--url")
        .arg(server.url())
        .args(args)
        .output()
        .unwrap()
}

#[test]
fn test_list_integrations() {
    let mut server = Server::new();
    let mock = server
        .mock("GET", "/api/v1/integrations")
        .match_header("Authorization", "Bearer test-token")
        .with_status(200)
        .with_header("Content-Type", "application/json")
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
    let mock = server
        .mock("GET", "/api/v1/tokens")
        .match_header("Authorization", "Bearer test-token")
        .with_status(200)
        .with_header("Content-Type", "application/json")
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
    let mock = server
        .mock("POST", "/api/v1/tokens")
        .match_header("Authorization", "Bearer test-token")
        .match_header("Content-Type", "application/json")
        .match_body(Matcher::JsonString(r#"{"name":"cli-token"}"#.to_string()))
        .with_status(201)
        .with_header("Content-Type", "application/json")
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
    let mock = server
        .mock("DELETE", "/api/v1/tokens/42")
        .match_header("Authorization", "Bearer test-token")
        .with_status(200)
        .with_header("Content-Type", "application/json")
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
    let mock = server
        .mock("POST", "/api/v1/github/search_code")
        .match_header("Authorization", "Bearer test-token")
        .match_header("Content-Type", "application/json")
        .with_status(200)
        .with_header("Content-Type", "application/json")
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
    let mock = server
        .mock("GET", "/api/v1/tokens")
        .with_status(401)
        .with_header("Content-Type", "application/json")
        .with_body(r#"{"error":"missing authorization header"}"#)
        .create();

    let client = create_client(&server);
    let result = client.get("/api/v1/tokens");

    mock.assert();
    assert!(result.is_err());
    let err = result.unwrap_err().to_string();
    assert!(err.contains("missing authorization header"));
}

#[test]
fn test_error_response_nested_message() {
    let mut server = Server::new();
    let mock = server
        .mock("GET", "/api/v1/tokens")
        .with_status(400)
        .with_header("Content-Type", "application/json")
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
    let mock = server
        .mock("GET", "/api/v1/integrations/test_svc/operations")
        .match_header("Authorization", "Bearer test-token")
        .with_status(200)
        .with_header("Content-Type", "application/json")
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
    let mock = server
        .mock("GET", "/api/v1/integrations/test_svc/operations")
        .with_status(200)
        .with_header("Content-Type", "application/json")
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
    let mock = server
        .mock("GET", "/api/v1/integrations/test_svc/operations")
        .with_status(200)
        .with_header("Content-Type", "application/json")
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
    let mock = server
        .mock("POST", "/api/v1/auth/start-oauth")
        .match_header("Authorization", "Bearer test-token")
        .with_status(200)
        .with_header("Content-Type", "application/json")
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
    let mock = server
        .mock("POST", "/api/v1/auth/start-oauth")
        .match_header("Authorization", "Bearer test-token")
        .match_header("Content-Type", "application/json")
        .match_body(Matcher::JsonString(
            r#"{"connection":"workspace","instance":"team-a","integration":"github"}"#.to_string(),
        ))
        .with_status(200)
        .with_header("Content-Type", "application/json")
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
    let mock = server
        .mock("POST", "/api/v1/auth/start-oauth")
        .match_header("Authorization", "Bearer test-token")
        .match_header("Content-Type", "application/json")
        .match_body(Matcher::JsonString(
            r#"{"integration":"github"}"#.to_string(),
        ))
        .with_status(200)
        .with_header("Content-Type", "application/json")
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

#[test]
fn test_invoke_with_connection_and_instance() {
    let mut server = Server::new();

    let _catalog_mock = server
        .mock("GET", "/api/v1/integrations/test_svc/operations")
        .with_status(200)
        .with_header("Content-Type", "application/json")
        .with_body(catalog_body())
        .create();

    let invoke_mock = server
        .mock("POST", "/api/v1/test_svc/do_thing")
        .match_header("Content-Type", "application/json")
        .match_body(Matcher::JsonString(
            r#"{"_connection":"workspace","_instance":"team-a","name":"test"}"#.to_string(),
        ))
        .with_status(200)
        .with_header("Content-Type", "application/json")
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
}

#[test]
fn test_describe_operation() {
    let mut server = Server::new();

    let mock = server
        .mock("GET", "/api/v1/integrations/test_svc/operations")
        .with_status(200)
        .with_header("Content-Type", "application/json")
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
        .stderr(predicate::str::contains("url = https://localhost:9999"));

    let mut get_cmd = cli_command(home.path());
    get_cmd.args(["--format", "json", "config", "get", "url"]);
    get_cmd.assert().success().stdout(predicate::str::contains(
        "\"url\": \"https://localhost:9999\"",
    ));
}

#[test]
fn test_cli_integrations_list_table_output() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();
    let mock = server
        .mock("GET", "/api/v1/integrations")
        .match_header("Authorization", "Bearer test-token")
        .with_status(200)
        .with_header("Content-Type", "application/json")
        .with_body(
            r#"[{"name":"github","description":"GitHub integration with a longer description","connected":true}]"#,
        )
        .create();

    let mut cmd = cli_command(home.path());
    cmd.env("GESTALT_API_KEY", "test-token")
        .args(["--url", &server.url(), "integrations", "list"]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("GITHUB").or(predicate::str::contains("github")))
        .stdout(predicate::str::contains("GitHub integration"))
        .stdout(predicate::str::contains("CONNECTED").or(predicate::str::contains("connected")));

    mock.assert();
}

#[test]
fn test_cli_invoke_table_output_renders_collection_and_metadata() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();

    let _catalog_mock = server
        .mock("GET", "/api/v1/integrations/bigquery/operations")
        .match_header("Authorization", "Bearer test-token")
        .with_status(200)
        .with_header("Content-Type", "application/json")
        .with_body(
            r#"[{
                "id": "list_datasets",
                "description": "List datasets",
                "method": "POST",
                "parameters": [
                    {"name": "project_id", "type": "string", "location": "query", "required": true}
                ]
            }]"#,
        )
        .create();

    let invoke_mock = server
        .mock("POST", "/api/v1/bigquery/list_datasets")
        .match_header("Authorization", "Bearer test-token")
        .match_header("Content-Type", "application/json")
        .match_body(Matcher::JsonString(
            r#"{"project_id":"serviceone"}"#.to_string(),
        ))
        .with_status(200)
        .with_header("Content-Type", "application/json")
        .with_body(
            r#"{
                "datasets": [
                    {
                        "datasetReference": {"datasetId": "analytics", "projectId": "serviceone"},
                        "id": "serviceone:analytics",
                        "kind": "bigquery#dataset",
                        "labels": {"owner": {"name": "platform"}},
                        "location": "US",
                        "type": "DEFAULT"
                    },
                    {
                        "datasetReference": {"datasetId": "analytics_core", "projectId": "serviceone"},
                        "id": "serviceone:analytics_core",
                        "kind": "bigquery#dataset",
                        "labels": {"owner": {"name": "warehouse"}},
                        "location": "US",
                        "type": "DEFAULT"
                    }
                ],
                "etag": "abc123",
                "kind": "bigquery#datasetList"
            }"#,
        )
        .create();

    let mut cmd = cli_command(home.path());
    cmd.env("GESTALT_API_KEY", "test-token").args([
        "--url",
        &server.url(),
        "invoke",
        "bigquery",
        "list_datasets",
        "-p",
        "project_id=serviceone",
    ]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("DATASETS"))
        .stdout(predicate::str::contains("DATASETREFERENCE.DATASETID"))
        .stdout(predicate::str::contains("DATASETREFERENCE.PROJECTID"))
        .stdout(predicate::str::contains("{\"name\":\"platform\"}"))
        .stdout(predicate::str::contains("LABELS.OWNER.NAME").not())
        .stdout(predicate::str::contains("analytics_core"))
        .stdout(predicate::str::contains("serviceone:analytics"))
        .stdout(predicate::str::contains("METADATA"))
        .stdout(predicate::str::contains("bigquery#datasetList"))
        .stdout(predicate::str::contains("abc123"))
        .stdout(predicate::str::contains("\"datasetReference\"").not());

    invoke_mock.assert();
}

#[test]
fn test_cli_invoke_merges_file_params_and_selects_output() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();
    let input_file = home.path().join("input.json");
    std::fs::write(&input_file, r#"{"count":1,"name":"from-file"}"#).unwrap();

    let _catalog_mock = server
        .mock("GET", "/api/v1/integrations/test_svc/operations")
        .match_header("Authorization", "Bearer test-token")
        .with_status(200)
        .with_header("Content-Type", "application/json")
        .with_body(catalog_body())
        .create();

    let invoke_mock = server
        .mock("POST", "/api/v1/test_svc/do_thing")
        .match_header("Authorization", "Bearer test-token")
        .match_header("Content-Type", "application/json")
        .match_body(Matcher::JsonString(
            r#"{"count":42,"name":"override","tags":["one","two"]}"#.to_string(),
        ))
        .with_status(200)
        .with_header("Content-Type", "application/json")
        .with_body(r#"{"result":{"id":"1"}}"#)
        .create();

    let mut cmd = cli_command(home.path());
    cmd.env("GESTALT_API_KEY", "test-token").args([
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
fn test_cli_invoke_rejects_duplicate_scalar_params() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();
    let _catalog_mock = server
        .mock("GET", "/api/v1/integrations/test_svc/operations")
        .match_header("Authorization", "Bearer test-token")
        .with_status(200)
        .with_header("Content-Type", "application/json")
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
