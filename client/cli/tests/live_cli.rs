use std::fs;
use std::net::TcpListener;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Output, Stdio};
use std::sync::OnceLock;
use std::thread;
use std::time::{Duration, Instant};

use reqwest::blocking::Client;
use serde_json::{json, Value};
use tempfile::TempDir;

fn repo_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../..")
        .canonicalize()
        .expect("canonicalize repo root")
}

fn gestaltd_binary() -> &'static Path {
    static BIN: OnceLock<PathBuf> = OnceLock::new();

    BIN.get_or_init(|| {
        let dir = TempDir::new().expect("create gestaltd build dir");
        let bin = dir.path().join("gestaltd");
        let output = Command::new("go")
            .args(["build", "-o"])
            .arg(&bin)
            .arg("./cmd/gestaltd")
            .current_dir(repo_root())
            .output()
            .expect("run go build for gestaltd");
        assert!(
            output.status.success(),
            "go build gestaltd failed\nstdout:\n{}\nstderr:\n{}",
            String::from_utf8_lossy(&output.stdout),
            String::from_utf8_lossy(&output.stderr)
        );
        let _ = dir.keep();
        bin
    })
    .as_path()
}

fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .expect("bind free port")
        .local_addr()
        .expect("read local addr")
        .port()
}

fn config_body(dir: &Path, port: u16) -> String {
    let db_path = dir.join("gestalt.db");
    format!(
        r#"auth:
  provider: google
  config:
    client_id: test-client
    client_secret: test-secret
    redirect_url: http://127.0.0.1:{port}/auth/callback
datastore:
  provider: sqlite
  config:
    path: {db_path}
server:
  port: {port}
  base_url: http://127.0.0.1:{port}
  dev_mode: true
  encryption_key: cli-live-tests
"#,
        db_path = db_path.display(),
    )
}

struct LiveServer {
    _dir: TempDir,
    child: Child,
    base_url: String,
}

impl LiveServer {
    fn start() -> Self {
        let dir = TempDir::new().expect("create server tempdir");
        let port = free_port();
        let config_path = dir.path().join("config.yaml");
        fs::write(&config_path, config_body(dir.path(), port)).expect("write server config");

        let child = Command::new(gestaltd_binary())
            .arg("--config")
            .arg(&config_path)
            .current_dir(repo_root())
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .expect("spawn gestaltd");

        let server = Self {
            _dir: dir,
            child,
            base_url: format!("http://127.0.0.1:{port}"),
        };
        server.wait_for_ready();
        server
    }

    fn wait_for_ready(&self) {
        let client = Client::builder()
            .timeout(Duration::from_secs(1))
            .build()
            .expect("build readiness client");
        let deadline = Instant::now() + Duration::from_secs(30);

        while Instant::now() < deadline {
            if let Ok(resp) = client.get(format!("{}/ready", self.base_url)).send() {
                if resp.status().is_success() {
                    return;
                }
            }
            thread::sleep(Duration::from_millis(200));
        }

        panic!("gestaltd did not become ready at {}", self.base_url);
    }

    fn dev_session_token(&self) -> String {
        let client = Client::builder().build().expect("build client");
        let response = client
            .post(format!("{}/api/dev-login", self.base_url))
            .json(&json!({ "email": "cli-live@gestalt.dev" }))
            .send()
            .expect("POST /api/dev-login");
        assert!(
            response.status().is_success(),
            "dev login failed with {}",
            response.status()
        );

        response
            .headers()
            .get_all("set-cookie")
            .iter()
            .filter_map(|value| value.to_str().ok())
            .find_map(|value| {
                value
                    .strip_prefix("session_token=")
                    .map(|rest| rest.split(';').next().unwrap_or(rest).to_string())
            })
            .expect("session_token cookie")
    }
}

impl Drop for LiveServer {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

fn cli(temp_home: &Path) -> Command {
    let mut cmd = Command::new(env!("CARGO_BIN_EXE_gestalt"));
    cmd.env("HOME", temp_home);
    cmd.env("XDG_CONFIG_HOME", temp_home);
    cmd
}

fn config_store_paths(temp_home: &Path) -> [PathBuf; 2] {
    [
        temp_home.join("gestalt"),
        temp_home.join("Library/Application Support/gestalt"),
    ]
}

fn active_config_store_dir(temp_home: &Path) -> PathBuf {
    run_ok(cli(temp_home).args(["config", "set", "url", "https://probe.example.com"]));

    let dir = config_store_paths(temp_home)
        .into_iter()
        .find(|dir| dir.join("config.json").exists())
        .expect("active config dir");

    run_ok(cli(temp_home).args(["config", "unset", "url"]));
    dir
}

fn write_credentials(temp_home: &Path, api_url: &str, session_token: &str) {
    let body = json!({
        "api_url": api_url,
        "session_token": session_token,
    });

    let dir = active_config_store_dir(temp_home);
    fs::create_dir_all(&dir).expect("create config dir");
    fs::write(
        dir.join("credentials.json"),
        serde_json::to_vec_pretty(&body).expect("serialize credentials"),
    )
    .expect("write credentials");
}

fn run_ok(cmd: &mut Command) -> Output {
    let output = cmd.output().expect("run command");
    assert!(
        output.status.success(),
        "command failed: {:?}\nstdout:\n{}\nstderr:\n{}",
        cmd,
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );
    output
}

fn run_json(cmd: &mut Command) -> Value {
    let output = run_ok(cmd);
    serde_json::from_slice(&output.stdout).expect("parse stdout as json")
}

fn run_err(cmd: &mut Command) -> String {
    let output = cmd.output().expect("run command");
    assert!(
        !output.status.success(),
        "expected failure for {:?}\nstdout:\n{}\nstderr:\n{}",
        cmd,
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );
    String::from_utf8_lossy(&output.stderr).to_string()
}

#[test]
fn config_commands_round_trip() {
    let home = TempDir::new().unwrap();

    run_ok(cli(home.path()).args(["config", "set", "url", "gestalt.example.com"]));

    let json = run_json(cli(home.path()).args(["--format", "json", "config", "get", "url"]));
    assert_eq!(json["url"], "https://gestalt.example.com");

    let json = run_json(cli(home.path()).args(["--format", "json", "config", "list"]));
    assert_eq!(json["url"], "https://gestalt.example.com");

    run_ok(cli(home.path()).args(["config", "unset", "url"]));

    let json = run_json(cli(home.path()).args(["--format", "json", "config", "get", "url"]));
    assert!(json["url"].is_null());
}

#[test]
fn live_integrations_and_invoke_use_real_server() {
    let server = LiveServer::start();
    let home = TempDir::new().unwrap();
    let token = server.dev_session_token();

    let integrations = run_json(cli(home.path()).env("GESTALT_API_KEY", &token).args([
        "--format",
        "json",
        "--url",
        &server.base_url,
        "integrations",
        "list",
    ]));
    assert!(integrations
        .as_array()
        .unwrap()
        .iter()
        .any(|item| item["name"] == "echo"));

    let operations = run_json(cli(home.path()).env("GESTALT_API_KEY", &token).args([
        "--format",
        "json",
        "--url",
        &server.base_url,
        "invoke",
        "echo",
    ]));
    assert_eq!(operations[0]["Name"], "echo");

    let result = run_json(cli(home.path()).env("GESTALT_API_KEY", &token).args([
        "--format",
        "json",
        "--url",
        &server.base_url,
        "invoke",
        "echo",
        "echo",
        "--param",
        "message=hello",
        "--param",
        "count=2",
    ]));
    assert_eq!(result["message"], "hello");
    assert_eq!(result["count"], "2");
}

#[test]
fn live_tokens_commands_cover_create_list_and_revoke() {
    let server = LiveServer::start();
    let home = TempDir::new().unwrap();
    let token = server.dev_session_token();

    let created = run_json(cli(home.path()).env("GESTALT_API_KEY", &token).args([
        "--format",
        "json",
        "--url",
        &server.base_url,
        "tokens",
        "create",
        "--name",
        "cli-live-token",
    ]));
    let token_id = created["id"].as_str().unwrap().to_string();
    assert!(!created["token"].as_str().unwrap().is_empty());

    let listed = run_json(cli(home.path()).env("GESTALT_API_KEY", &token).args([
        "--format",
        "json",
        "--url",
        &server.base_url,
        "tokens",
        "list",
    ]));
    assert!(listed
        .as_array()
        .unwrap()
        .iter()
        .any(|item| item["id"] == token_id));

    let revoked = run_json(cli(home.path()).env("GESTALT_API_KEY", &token).args([
        "--format",
        "json",
        "--url",
        &server.base_url,
        "tokens",
        "revoke",
        &token_id,
    ]));
    assert_eq!(revoked["status"], "revoked");

    let listed = run_json(cli(home.path()).env("GESTALT_API_KEY", &token).args([
        "--format",
        "json",
        "--url",
        &server.base_url,
        "tokens",
        "list",
    ]));
    assert!(!listed
        .as_array()
        .unwrap()
        .iter()
        .any(|item| item["id"] == token_id));
}

#[test]
fn stored_session_and_project_url_work_without_env_token() {
    let server = LiveServer::start();
    let home = TempDir::new().unwrap();
    let project = TempDir::new().unwrap();
    let token = server.dev_session_token();

    write_credentials(home.path(), &server.base_url, &token);
    fs::write(
        project.path().join(".gestalt.json"),
        serde_json::to_vec_pretty(&json!({ "url": server.base_url })).unwrap(),
    )
    .unwrap();
    let nested = project.path().join("nested/deeper");
    fs::create_dir_all(&nested).unwrap();

    let integrations = run_json(cli(home.path()).current_dir(&nested).args([
        "--format",
        "json",
        "integrations",
        "list",
    ]));
    assert!(integrations
        .as_array()
        .unwrap()
        .iter()
        .any(|item| item["name"] == "echo"));
}

#[test]
fn invalid_env_token_shows_precedence_hint() {
    let server = LiveServer::start();
    let home = TempDir::new().unwrap();
    let valid_token = server.dev_session_token();

    write_credentials(home.path(), &server.base_url, &valid_token);

    let stderr = run_err(
        cli(home.path())
            .env("GESTALT_API_KEY", "definitely-invalid-token")
            .args(["--format", "json", "tokens", "list"]),
    );
    assert!(stderr.contains("GESTALT_API_KEY"));
    assert!(stderr.contains("unset it to use your session token"));
}

#[test]
fn auth_logout_removes_stored_credentials() {
    let server = LiveServer::start();
    let home = TempDir::new().unwrap();
    let valid_token = server.dev_session_token();

    write_credentials(home.path(), &server.base_url, &valid_token);
    run_ok(cli(home.path()).args(["auth", "logout"]));

    let active_dir = active_config_store_dir(home.path());
    assert!(!active_dir.join("credentials.json").exists());
}
