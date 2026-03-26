use std::collections::{HashMap, HashSet};
use std::fs;
use std::io::Write;
use std::net::TcpListener;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, OnceLock};
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

use reqwest::blocking::Client;
use reqwest::header::{COOKIE, SET_COOKIE};
use serde_json::Value;
use tempfile::TempDir;
use tiny_http::{Header, Method, Response, Server, StatusCode};
use url::Url;

static GESTALTD_BIN: OnceLock<PathBuf> = OnceLock::new();

pub struct CliOutput {
    pub status: std::process::ExitStatus,
    pub stdout: String,
    pub stderr: String,
}

impl CliOutput {
    pub fn assert_success(&self) {
        assert!(
            self.status.success(),
            "expected success, got {}\nstdout:\n{}\nstderr:\n{}",
            self.status,
            self.stdout,
            self.stderr
        );
    }

    pub fn assert_failure(&self) {
        assert!(
            !self.status.success(),
            "expected failure, got success\nstdout:\n{}\nstderr:\n{}",
            self.stdout,
            self.stderr
        );
    }

    pub fn stdout_json(&self) -> Value {
        serde_json::from_str(&self.stdout).expect("stdout should be valid JSON")
    }
}

pub struct CliEnv {
    _temp: TempDir,
    home: PathBuf,
    xdg_config_home: PathBuf,
    config_root: PathBuf,
    pub project_dir: PathBuf,
}

impl CliEnv {
    pub fn new() -> Self {
        let temp = tempfile::tempdir().expect("create temp dir");
        let home = temp.path().join("home");
        let xdg_config_home = temp.path().join("xdg-config");
        let config_root = if cfg!(target_os = "macos") {
            home.join("Library").join("Application Support")
        } else {
            xdg_config_home.clone()
        };
        let project_dir = temp.path().join("project");
        fs::create_dir_all(&home).expect("create home");
        fs::create_dir_all(&xdg_config_home).expect("create xdg config");
        fs::create_dir_all(&config_root).expect("create config root");
        fs::create_dir_all(&project_dir).expect("create project dir");

        Self {
            _temp: temp,
            home,
            xdg_config_home,
            config_root,
            project_dir,
        }
    }

    pub fn credentials_path(&self) -> PathBuf {
        self.config_root.join("gestalt").join("credentials.json")
    }

    pub fn config_path(&self) -> PathBuf {
        self.config_root.join("gestalt").join("config.json")
    }

    pub fn write_credentials(&self, api_url: &str, session_token: &str) {
        let path = self.credentials_path();
        fs::create_dir_all(path.parent().expect("credentials parent")).expect("create config dir");
        fs::write(
            &path,
            serde_json::json!({
                "api_url": api_url,
                "session_token": session_token,
            })
            .to_string(),
        )
        .expect("write credentials");
    }

    pub fn write_project_url(&self, dir: &Path, url: &str) {
        fs::write(
            dir.join(".gestalt.json"),
            serde_json::json!({
                "url": url,
            })
            .to_string(),
        )
        .expect("write project config");
    }

    pub fn run(&self, args: &[&str]) -> CliOutput {
        self.run_in(&self.project_dir, args, None, &[])
    }

    pub fn run_in(
        &self,
        cwd: &Path,
        args: &[&str],
        stdin: Option<&str>,
        extra_env: &[(&str, &str)],
    ) -> CliOutput {
        let mut cmd = Command::new(env!("CARGO_BIN_EXE_gestalt"));
        cmd.args(args)
            .current_dir(cwd)
            .env("HOME", &self.home)
            .env("XDG_CONFIG_HOME", &self.xdg_config_home)
            .env_remove("GESTALT_API_KEY")
            .env_remove("GESTALT_URL")
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());

        for (key, value) in extra_env {
            cmd.env(key, value);
        }

        if stdin.is_some() {
            cmd.stdin(Stdio::piped());
        }

        let mut child = cmd.spawn().expect("spawn gestalt CLI");
        if let Some(input) = stdin {
            let mut handle = child.stdin.take().expect("stdin handle");
            handle.write_all(input.as_bytes()).expect("write CLI stdin");
        }

        let output = child.wait_with_output().expect("wait for CLI");
        CliOutput {
            status: output.status,
            stdout: String::from_utf8(output.stdout).expect("utf8 stdout"),
            stderr: String::from_utf8(output.stderr).expect("utf8 stderr"),
        }
    }
}

pub struct TestServer {
    pub base_url: String,
    _oidc: OidcServer,
    _temp: TempDir,
    child: Child,
}

impl TestServer {
    pub fn start() -> Self {
        let temp = tempfile::tempdir().expect("create temp dir");
        let port = free_port();
        let base_url = format!("http://127.0.0.1:{port}");
        let oidc = OidcServer::start("cli-user@gestalt.dev", "CLI User");
        let config_path = temp.path().join("config.yaml");
        fs::write(
            &config_path,
            format!(
                r#"auth:
  provider: oidc
  config:
    issuer_url: {issuer_url}
    client_id: test-client
    client_secret: test-secret
    redirect_url: {base_url}/api/v1/auth/login/callback
    session_secret: cli-session-secret
datastore:
  provider: sqlite
  config:
    path: {db_path}
server:
  port: {port}
  base_url: {base_url}
  dev_mode: true
  encryption_key: cli-encryption-key
"#,
                issuer_url = oidc.base_url,
                db_path = temp.path().join("gestalt.db").display(),
            ),
        )
        .expect("write gestaltd config");

        let child = Command::new(gestaltd_binary())
            .arg("--config")
            .arg(&config_path)
            .current_dir(repo_root())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .expect("spawn gestaltd");

        let mut server = Self {
            base_url,
            _oidc: oidc,
            _temp: temp,
            child,
        };
        server.wait_ready();
        server
    }

    pub fn dev_login(&self, email: &str) -> String {
        let client = Client::builder()
            .timeout(Duration::from_secs(5))
            .build()
            .expect("build reqwest client");
        let response = client
            .post(format!("{}/api/dev-login", self.base_url))
            .json(&serde_json::json!({ "email": email }))
            .send()
            .expect("dev login request");
        assert!(
            response.status().is_success(),
            "dev login failed: {}",
            response.status()
        );
        let cookie = response
            .headers()
            .get(SET_COOKIE)
            .and_then(|value| value.to_str().ok())
            .expect("session cookie header");
        cookie
            .strip_prefix("session_token=")
            .and_then(|value| value.split(';').next())
            .expect("session token cookie")
            .to_string()
    }

    pub fn create_api_token(&self, session_token: &str, name: &str) -> Value {
        let client = Client::builder()
            .timeout(Duration::from_secs(5))
            .build()
            .expect("build reqwest client");
        let response = client
            .post(format!("{}/api/v1/tokens", self.base_url))
            .header(COOKIE, format!("session_token={session_token}"))
            .json(&serde_json::json!({ "name": name }))
            .send()
            .expect("create api token");
        assert!(
            response.status().is_success(),
            "create api token failed: {}",
            response.status()
        );
        response.json().expect("api token response json")
    }

    fn wait_ready(&mut self) {
        let client = Client::builder()
            .timeout(Duration::from_millis(500))
            .build()
            .expect("build reqwest client");
        let deadline = Instant::now() + Duration::from_secs(20);
        while Instant::now() < deadline {
            if let Some(status) = self.child.try_wait().expect("check gestaltd status") {
                panic!("gestaltd exited early with status {status}");
            }
            if let Ok(response) = client.get(format!("{}/ready", self.base_url)).send() {
                if response.status().is_success() {
                    return;
                }
            }
            thread::sleep(Duration::from_millis(100));
        }
        panic!("timed out waiting for gestaltd readiness");
    }
}

impl Drop for TestServer {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

pub struct OidcServer {
    pub base_url: String,
    shutdown: Arc<AtomicBool>,
    handle: Option<JoinHandle<()>>,
}

impl OidcServer {
    fn start(email: &str, display_name: &str) -> Self {
        let server = Server::http("127.0.0.1:0").expect("start oidc server");
        let addr = server
            .server_addr()
            .to_ip()
            .expect("oidc server should listen on tcp");
        let base_url = format!("http://{}", addr);
        let shutdown = Arc::new(AtomicBool::new(false));
        let state = Arc::new(Mutex::new(OidcState {
            base_url: base_url.clone(),
            email: email.to_string(),
            display_name: display_name.to_string(),
            next_id: 0,
            codes: HashMap::new(),
            access_tokens: HashSet::new(),
        }));
        let shutdown_clone = shutdown.clone();
        let handle = thread::spawn(move || {
            while !shutdown_clone.load(Ordering::Relaxed) {
                match server.recv_timeout(Duration::from_millis(100)) {
                    Ok(Some(request)) => handle_oidc_request(request, &state),
                    Ok(None) => {}
                    Err(err) => panic!("oidc recv error: {err}"),
                }
            }
        });

        Self {
            base_url,
            shutdown,
            handle: Some(handle),
        }
    }
}

impl Drop for OidcServer {
    fn drop(&mut self) {
        self.shutdown.store(true, Ordering::Relaxed);
        if let Some(handle) = self.handle.take() {
            let _ = handle.join();
        }
    }
}

struct OidcState {
    base_url: String,
    email: String,
    display_name: String,
    next_id: u64,
    codes: HashMap<String, String>,
    access_tokens: HashSet<String>,
}

fn handle_oidc_request(mut request: tiny_http::Request, state: &Arc<Mutex<OidcState>>) {
    let base = {
        let guard = state.lock().expect("oidc state");
        guard.base_url.clone()
    };
    let url = Url::parse(&format!("{}{}", base, request.url())).expect("parse oidc request url");

    match (request.method().clone(), url.path()) {
        (Method::Get, "/.well-known/openid-configuration") => {
            let body = {
                let guard = state.lock().expect("oidc state");
                serde_json::json!({
                    "issuer": guard.base_url,
                    "authorization_endpoint": format!("{}/authorize", guard.base_url),
                    "token_endpoint": format!("{}/token", guard.base_url),
                    "userinfo_endpoint": format!("{}/userinfo", guard.base_url),
                    "jwks_uri": format!("{}/jwks", guard.base_url),
                })
            };
            respond_json(request, StatusCode(200), &body);
        }
        (Method::Get, "/authorize") => {
            let redirect_uri = url
                .query_pairs()
                .find(|(key, _)| key == "redirect_uri")
                .map(|(_, value)| value.into_owned())
                .expect("redirect_uri query param");
            let state_value = url
                .query_pairs()
                .find(|(key, _)| key == "state")
                .map(|(_, value)| value.into_owned())
                .unwrap_or_default();

            let mut guard = state.lock().expect("oidc state");
            guard.next_id += 1;
            let code = format!("code-{}", guard.next_id);
            let token = format!("oidc-access-token-{}", guard.next_id);
            guard.codes.insert(code.clone(), token.clone());
            guard.access_tokens.insert(token);

            let location = format!("{redirect_uri}?code={code}&state={state_value}");
            let response = Response::empty(StatusCode(302))
                .with_header(header("Location", &location))
                .with_header(header("Content-Length", "0"));
            request.respond(response).expect("respond authorize");
        }
        (Method::Post, "/token") => {
            let mut body = String::new();
            request
                .as_reader()
                .read_to_string(&mut body)
                .expect("read token request body");
            let form: HashMap<String, String> = url::form_urlencoded::parse(body.as_bytes())
                .into_owned()
                .collect();
            let code = form.get("code").cloned().unwrap_or_default();
            let token = {
                let guard = state.lock().expect("oidc state");
                guard.codes.get(&code).cloned()
            };
            match token {
                Some(access_token) => {
                    respond_json(
                        request,
                        StatusCode(200),
                        &serde_json::json!({
                            "access_token": access_token,
                            "token_type": "Bearer",
                            "expires_in": 3600,
                        }),
                    );
                }
                None => {
                    respond_json(
                        request,
                        StatusCode(400),
                        &serde_json::json!({
                            "error": "invalid_grant",
                            "error_description": "unknown authorization code",
                        }),
                    );
                }
            }
        }
        (Method::Get, "/userinfo") => {
            let auth_header = request
                .headers()
                .iter()
                .find(|header| header.field.equiv("Authorization"))
                .map(|header| header.value.as_str())
                .unwrap_or("");
            let token = auth_header.strip_prefix("Bearer ").unwrap_or("");
            let valid = {
                let guard = state.lock().expect("oidc state");
                guard.access_tokens.contains(token)
            };
            if !valid {
                respond_json(
                    request,
                    StatusCode(401),
                    &serde_json::json!({ "error": "unauthorized" }),
                );
                return;
            }
            let body = {
                let guard = state.lock().expect("oidc state");
                serde_json::json!({
                    "email": guard.email,
                    "name": guard.display_name,
                    "picture": "https://example.com/avatar.png",
                    "email_verified": true,
                })
            };
            respond_json(request, StatusCode(200), &body);
        }
        _ => {
            request
                .respond(Response::empty(StatusCode(404)))
                .expect("respond 404");
        }
    }
}

fn respond_json(request: tiny_http::Request, status: StatusCode, body: &Value) {
    let payload = serde_json::to_string(body).expect("serialize json response");
    let response = Response::from_string(payload)
        .with_status_code(status)
        .with_header(header("Content-Type", "application/json"));
    request.respond(response).expect("respond json");
}

fn header(name: &str, value: &str) -> Header {
    Header::from_bytes(name.as_bytes(), value.as_bytes()).expect("build header")
}

fn repo_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("client parent")
        .parent()
        .expect("repo root")
        .to_path_buf()
}

fn gestaltd_binary() -> &'static PathBuf {
    GESTALTD_BIN.get_or_init(|| {
        let dir = tempfile::tempdir().expect("create gestaltd build dir");
        let dir_path = dir.keep();
        let path = dir_path.join("gestaltd");
        let status = Command::new("go")
            .arg("build")
            .arg("-o")
            .arg(&path)
            .arg("./cmd/gestaltd")
            .current_dir(repo_root())
            .status()
            .expect("run go build");
        assert!(status.success(), "go build ./cmd/gestaltd failed");
        path
    })
}

fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .expect("bind ephemeral port")
        .local_addr()
        .expect("local addr")
        .port()
}
