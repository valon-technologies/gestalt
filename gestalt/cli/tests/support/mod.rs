#![allow(dead_code)]
#![allow(unused_imports)]

use std::ffi::OsString;
use std::io::{BufRead, BufReader, Read};
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, OnceLock};

pub(crate) use assert_cmd::Command;
pub(crate) use gestalt::http;
pub(crate) use gestalt::output::Format;
pub(crate) use mockito::{Matcher, Server};
pub(crate) use predicates::prelude::*;
pub(crate) use reqwest::{Method, StatusCode, header};
pub(crate) use tempfile::TempDir;

pub(crate) const TEST_TOKEN: &str = "test-token";
const LOGIN_FLOW_REQUEST_COUNT: usize = 4;

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

pub(crate) use authed_json_mock;
pub(crate) use json_mock;

pub(crate) fn create_client(server: &Server) -> gestalt::api::ApiClient {
    gestalt::api::ApiClient::new(&server.url(), TEST_TOKEN).unwrap()
}

pub(crate) fn test_bearer() -> String {
    format!("Bearer {TEST_TOKEN}")
}

pub(crate) fn env_lock() -> std::sync::MutexGuard<'static, ()> {
    static LOCK: OnceLock<Mutex<()>> = OnceLock::new();
    LOCK.get_or_init(|| Mutex::new(())).lock().unwrap()
}

pub(crate) struct EnvGuard(Vec<(&'static str, Option<OsString>)>);

impl EnvGuard {
    pub(crate) fn new(config_root: &Path) -> Self {
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
            std::env::set_var("XDG_CONFIG_HOME", config_root.join("xdg-config"));
            std::env::remove_var("GESTALT_URL");
            std::env::remove_var(gestalt::api::ENV_API_KEY);
        }
        Self(saved)
    }
}

pub(crate) struct CurrentDirGuard {
    saved: PathBuf,
}

impl CurrentDirGuard {
    pub(crate) fn new(path: &Path) -> Self {
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
pub(crate) struct LoginFlowState {
    pub(crate) callback_port: Option<u16>,
    pub(crate) expected_state: Option<String>,
    pub(crate) browser_response_html: Option<String>,
}

struct HttpRequest {
    method: Method,
    target: String,
    body: Vec<u8>,
}

pub(crate) struct LoginServer {
    pub(crate) base_url: String,
    pub(crate) state: Arc<Mutex<LoginFlowState>>,
    pub(crate) handle: std::thread::JoinHandle<()>,
}

pub(crate) fn spawn_login_server() -> LoginServer {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let base_url = format!("http://{}", listener.local_addr().unwrap());
    let login_response_url = format!("{base_url}/browser-login");
    let state = Arc::new(Mutex::new(LoginFlowState::default()));
    let server_state = Arc::clone(&state);
    let handle = std::thread::spawn(move || {
        let mut workers = Vec::new();
        for _ in 0..LOGIN_FLOW_REQUEST_COUNT {
            let (stream, _) = listener.accept().unwrap();
            let state = Arc::clone(&server_state);
            let login_response_url = login_response_url.clone();
            workers.push(std::thread::spawn(move || {
                handle_login_request(stream, state, &login_response_url);
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

fn handle_login_request(
    mut stream: TcpStream,
    state: Arc<Mutex<LoginFlowState>>,
    login_response_url: &str,
) {
    let request = read_http_request(&mut stream);
    match request.target.as_str() {
        "/api/v1/auth/info" if request.method == Method::GET => {
            write_http_response(
                &mut stream,
                StatusCode::OK,
                http::APPLICATION_JSON,
                r#"{"provider":"local","displayName":"local","loginSupported":true}"#,
            );
        }
        "/api/v1/auth/login" if request.method == Method::POST => {
            let body: serde_json::Value = serde_json::from_slice(&request.body).unwrap();
            let mut state = state.lock().unwrap();
            state.callback_port = Some(body["callbackPort"].as_u64().unwrap() as u16);
            state.expected_state = Some(body["state"].as_str().unwrap().to_string());
            write_http_response(
                &mut stream,
                StatusCode::OK,
                http::APPLICATION_JSON,
                &format!(r#"{{"url":"{login_response_url}"}}"#),
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

pub(crate) fn cli_command(home: &Path) -> Command {
    std::fs::create_dir_all(home).unwrap();
    let mut cmd = Command::cargo_bin("gestalt").unwrap();
    cmd.current_dir(home)
        .env("HOME", home)
        .env("XDG_CONFIG_HOME", home.join("xdg-config"))
        .env_remove(gestalt::api::ENV_API_KEY)
        .env_remove("GESTALT_URL");
    cmd
}

fn credentials_path(home: &Path) -> PathBuf {
    home.join("xdg-config")
        .join("gestalt")
        .join("credentials.json")
}

pub(crate) fn write_credentials(home: &Path, credentials: serde_json::Value) {
    let path = credentials_path(home);
    std::fs::create_dir_all(path.parent().unwrap()).unwrap();
    std::fs::write(path, serde_json::to_string_pretty(&credentials).unwrap()).unwrap();
}

pub(crate) fn run_cli(server: &Server, args: &[&str]) -> std::process::Output {
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

pub(crate) fn cli_command_for_server(home: &Path, server: &Server) -> Command {
    let mut cmd = cli_command(home);
    cmd.env("GESTALT_API_KEY", TEST_TOKEN)
        .arg("--url")
        .arg(server.url());
    cmd
}

pub(crate) fn write_cli_credentials(home: &Path, json: &str) {
    let path = credentials_path(home);
    std::fs::create_dir_all(path.parent().unwrap()).unwrap();
    std::fs::write(path, json).unwrap();
}

pub(crate) fn catalog_body() -> &'static str {
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

pub(crate) fn multi_operation_catalog() -> &'static str {
    r#"[
        {"id": "widgets.create", "description": "Create a widget", "method": "POST", "parameters": [
            {"name": "name", "type": "string", "required": true},
            {"name": "color", "type": "string", "required": true}
        ]},
        {"id": "widgets.delete", "description": "Delete a widget", "method": "POST", "parameters": []},
        {"id": "widgets.list", "description": "List widgets", "method": "GET", "parameters": []},
        {"id": "gadgets.fetch", "description": "Fetch a gadget", "method": "GET", "parameters": []},
        {"id": "gadgets.update", "description": "Update a gadget", "method": "POST", "parameters": []},
        {"id": "status.check", "description": "Check status", "method": "GET", "parameters": []},
        {"id": "widgets.bulk.create", "description": "Bulk create widgets", "method": "POST", "parameters": []},
        {"id": "widgets.bulk.delete", "description": "Bulk delete widgets", "method": "POST", "parameters": []}
    ]"#
}

pub(crate) fn single_operation_catalog(id: &str) -> String {
    format!(r#"[{{"id":"{id}"}}]"#)
}
