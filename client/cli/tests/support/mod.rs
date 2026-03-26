use std::collections::BTreeMap;
use std::fs::{self, File};
use std::io::{BufRead, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Output, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, OnceLock};
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

use anyhow::{bail, Context, Result};
use tempfile::TempDir;

const TEST_EMAIL: &str = "cli-user@example.com";

pub struct TestEnv {
    _tempdir: TempDir,
    base_url: String,
    server: Child,
    _oidc_server: HttpServer,
    _upstream_server: HttpServer,
}

pub struct CliHome {
    tempdir: TempDir,
}

pub struct CliOutput {
    pub status: i32,
    pub stdout: String,
    pub stderr: String,
}

struct HttpServer {
    base_url: String,
    stop: Arc<AtomicBool>,
    handle: Option<JoinHandle<()>>,
}

struct TestRequest {
    method: String,
    target: String,
    path: String,
    query: BTreeMap<String, String>,
    headers: BTreeMap<String, String>,
    body: Vec<u8>,
}

struct TestResponse {
    status: u16,
    headers: Vec<(String, String)>,
    body: Vec<u8>,
}

impl TestEnv {
    pub fn new() -> Self {
        Self::try_new().unwrap()
    }

    fn try_new() -> Result<Self> {
        let tempdir = tempfile::tempdir().context("create tempdir")?;
        let server_port = free_port()?;
        let base_url = format!("http://127.0.0.1:{server_port}");
        let redirect_url = format!("{base_url}/auth/callback");

        let upstream_server = start_upstream_server();
        let oidc_server = start_oidc_server(&redirect_url);

        let config_path = tempdir.path().join("config.yaml");
        let db_path = tempdir.path().join("gestalt.db");
        fs::write(
            &config_path,
            format!(
                "auth:\n  provider: oidc\n  config:\n    issuer_url: {oidc_url}\n    client_id: test-client\n    client_secret: test-secret\n    redirect_url: {redirect_url}\n    session_secret: test-session-secret\n    display_name: Test SSO\ndatastore:\n  provider: sqlite\n  config:\n    path: {db_path}\nserver:\n  port: {server_port}\n  base_url: {base_url}\n  dev_mode: true\n  encryption_key: test-encryption-key\nintegrations:\n  restapi:\n    display_name: REST API\n    description: Test API\n    manual_auth: true\n    upstreams:\n      - type: rest\n        url: {upstream_url}/openapi.json\n",
                oidc_url = oidc_server.base_url,
                upstream_url = upstream_server.base_url,
                db_path = db_path.display(),
            ),
        )
        .context("write config")?;

        let stdout_log = tempdir.path().join("gestaltd.stdout.log");
        let stderr_log = tempdir.path().join("gestaltd.stderr.log");
        let stdout = File::create(&stdout_log).context("create stdout log")?;
        let stderr = File::create(&stderr_log).context("create stderr log")?;

        let mut server = Command::new(gestaltd_binary()?)
            .current_dir(repo_root()?)
            .arg("dev")
            .arg("--config")
            .arg(&config_path)
            .stdout(Stdio::from(stdout))
            .stderr(Stdio::from(stderr))
            .spawn()
            .context("spawn gestaltd")?;

        wait_for_server(&base_url, &mut server, &stdout_log, &stderr_log)?;

        Ok(Self {
            _tempdir: tempdir,
            base_url,
            server,
            _oidc_server: oidc_server,
            _upstream_server: upstream_server,
        })
    }

    pub fn base_url(&self) -> &str {
        &self.base_url
    }

    pub fn new_home(&self) -> CliHome {
        CliHome::new().unwrap()
    }

    pub fn login(&self, home: &CliHome) -> CliOutput {
        let mut child = home
            .command(
                &["auth", "login"],
                &[("GESTALT_URL", self.base_url())],
                None,
                None,
            )
            .unwrap();

        let mut stderr = String::new();
        let mut stdout = String::new();
        let mut browser_url = None;

        {
            let stderr_pipe = child.stderr.take().expect("stderr should be piped");
            let mut reader = std::io::BufReader::new(stderr_pipe);
            loop {
                let mut line = String::new();
                let read = reader.read_line(&mut line).unwrap();
                if read == 0 {
                    break;
                }
                if let Some(url) = line.strip_prefix("If the browser doesn't open, visit: ") {
                    browser_url = Some(url.trim().to_string());
                }
                stderr.push_str(&line);
                if browser_url.is_some() {
                    break;
                }
            }

            let url = browser_url.as_deref().expect("login URL was not printed");
            simulate_browser(url).unwrap();
            reader.read_to_string(&mut stderr).unwrap();
        }

        if let Some(mut stdout_pipe) = child.stdout.take() {
            stdout_pipe.read_to_string(&mut stdout).unwrap();
        }
        let status = child.wait().unwrap();
        CliOutput {
            status: status.code().unwrap_or(-1),
            stdout,
            stderr,
        }
    }

    pub fn dev_session_token(&self) -> String {
        let client = reqwest::blocking::Client::builder()
            .timeout(Duration::from_secs(5))
            .build()
            .unwrap();
        let response = client
            .post(format!("{}/api/dev-login", self.base_url))
            .json(&serde_json::json!({ "email": TEST_EMAIL }))
            .send()
            .unwrap();
        let status = response.status();
        if status != reqwest::StatusCode::OK {
            panic!(
                "dev-login failed (HTTP {status}): {}",
                response.text().unwrap()
            );
        }

        response
            .headers()
            .get_all(reqwest::header::SET_COOKIE)
            .iter()
            .filter_map(|value| value.to_str().ok())
            .find_map(|value| {
                value
                    .strip_prefix("session_token=")
                    .map(|rest| rest.split(';').next().unwrap_or(rest).to_string())
            })
            .expect("session token cookie missing")
    }

    pub fn connect_manual(&self, session_token: &str, integration: &str, credential: &str) {
        let client = reqwest::blocking::Client::builder()
            .timeout(Duration::from_secs(5))
            .build()
            .unwrap();
        let response = client
            .post(format!("{}/api/v1/auth/connect-manual", self.base_url))
            .header(
                reqwest::header::AUTHORIZATION,
                format!("Bearer {session_token}"),
            )
            .json(&serde_json::json!({
                "integration": integration,
                "credential": credential,
            }))
            .send()
            .unwrap();
        let status = response.status();
        if status != reqwest::StatusCode::OK {
            panic!(
                "connect-manual failed (HTTP {status}): {}",
                response.text().unwrap()
            );
        }
    }

    pub fn run_cli(&self, home: &CliHome, args: &[&str]) -> CliOutput {
        self.run_cli_with(home, args, &[], None, None)
    }

    pub fn run_cli_with(
        &self,
        home: &CliHome,
        args: &[&str],
        envs: &[(&str, &str)],
        cwd: Option<&Path>,
        stdin: Option<&str>,
    ) -> CliOutput {
        let output = home.run(args, envs, cwd, stdin).unwrap();
        CliOutput::from(output)
    }
}

impl Drop for TestEnv {
    fn drop(&mut self) {
        if let Ok(None) = self.server.try_wait() {
            let _ = self.server.kill();
        }
        let _ = self.server.wait();
    }
}

impl CliHome {
    fn new() -> Result<Self> {
        Ok(Self {
            tempdir: tempfile::tempdir().context("create cli home")?,
        })
    }

    pub fn root(&self) -> &Path {
        self.tempdir.path()
    }

    pub fn config_home(&self) -> PathBuf {
        if cfg!(target_os = "macos") {
            self.root().join("Library/Application Support")
        } else if cfg!(windows) {
            self.root().join("AppData/Roaming")
        } else {
            self.root().join(".config")
        }
    }

    pub fn config_path(&self) -> PathBuf {
        self.config_home().join("gestalt/config.json")
    }

    pub fn credentials_path(&self) -> PathBuf {
        self.config_home().join("gestalt/credentials.json")
    }

    fn run(
        &self,
        args: &[&str],
        envs: &[(&str, &str)],
        cwd: Option<&Path>,
        stdin: Option<&str>,
    ) -> Result<Output> {
        let mut child = self.command(args, envs, cwd, stdin)?;
        if let Some(input) = stdin {
            let mut handle = child.stdin.take().context("take stdin")?;
            handle.write_all(input.as_bytes()).context("write stdin")?;
        }

        child.wait_with_output().context("wait for gestalt")
    }

    fn command(
        &self,
        args: &[&str],
        envs: &[(&str, &str)],
        cwd: Option<&Path>,
        stdin: Option<&str>,
    ) -> Result<Child> {
        let mut cmd = Command::new(env!("CARGO_BIN_EXE_gestalt"));
        cmd.args(args)
            .env("HOME", self.root())
            .env("XDG_CONFIG_HOME", self.config_home())
            .env("NO_COLOR", "1")
            .env("PATH", self.path_with_test_bin())
            .current_dir(cwd.unwrap_or_else(|| self.root()))
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());

        for (key, value) in envs {
            cmd.env(key, value);
        }

        if stdin.is_some() {
            cmd.stdin(Stdio::piped());
        } else {
            cmd.stdin(Stdio::null());
        }

        cmd.spawn().context("spawn gestalt")
    }

    fn path_with_test_bin(&self) -> String {
        let bin_dir = self.root().join("bin");
        let current = std::env::var_os("PATH").unwrap_or_default();
        let mut paths = vec![bin_dir];
        paths.extend(std::env::split_paths(&current));
        std::env::join_paths(paths)
            .unwrap()
            .to_string_lossy()
            .into_owned()
    }
}

impl CliOutput {
    fn from(output: Output) -> Self {
        Self {
            status: output.status.code().unwrap_or(-1),
            stdout: String::from_utf8_lossy(&output.stdout).into_owned(),
            stderr: String::from_utf8_lossy(&output.stderr).into_owned(),
        }
    }

    pub fn assert_success(&self) {
        assert_eq!(
            self.status, 0,
            "expected success\nstdout:\n{}\nstderr:\n{}",
            self.stdout, self.stderr
        );
    }

    pub fn assert_failure(&self) {
        assert_ne!(
            self.status, 0,
            "expected failure\nstdout:\n{}\nstderr:\n{}",
            self.stdout, self.stderr
        );
    }

    pub fn json_stdout(&self) -> serde_json::Value {
        serde_json::from_str(self.stdout.trim()).unwrap_or_else(|err| {
            panic!("stdout is not JSON ({err}): {}", self.stdout);
        })
    }
}

impl HttpServer {
    fn start<F, G>(builder: F) -> Result<Self>
    where
        F: FnOnce(String) -> G,
        G: Fn(TestRequest) -> TestResponse + Send + Sync + 'static,
    {
        let listener = TcpListener::bind("127.0.0.1:0").context("bind fixture server")?;
        listener
            .set_nonblocking(true)
            .context("set fixture server nonblocking")?;
        let addr = listener.local_addr().context("fixture local_addr")?;
        let base_url = format!("http://{}", addr);
        let stop = Arc::new(AtomicBool::new(false));
        let handler = Arc::new(builder(base_url.clone()));
        let stop_clone = Arc::clone(&stop);
        let handle = thread::spawn(move || {
            while !stop_clone.load(Ordering::SeqCst) {
                match listener.accept() {
                    Ok((mut stream, _)) => {
                        if let Err(err) = handle_connection(&mut stream, handler.as_ref()) {
                            if let Some(io_err) = err.downcast_ref::<std::io::Error>() {
                                if io_err.kind() == std::io::ErrorKind::WouldBlock {
                                    continue;
                                }
                            }
                            panic!("fixture server error: {err:#}");
                        }
                    }
                    Err(err) if err.kind() == std::io::ErrorKind::WouldBlock => {
                        thread::sleep(Duration::from_millis(10));
                    }
                    Err(err) => panic!("fixture accept failed: {err}"),
                }
            }
        });

        Ok(Self {
            base_url,
            stop,
            handle: Some(handle),
        })
    }
}

impl Drop for HttpServer {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::SeqCst);
        let _ = TcpStream::connect(
            self.base_url
                .strip_prefix("http://")
                .unwrap_or(&self.base_url),
        );
        if let Some(handle) = self.handle.take() {
            let _ = handle.join();
        }
    }
}

impl TestRequest {
    fn query(&self, key: &str) -> Option<&str> {
        self.query.get(key).map(String::as_str)
    }
}

impl TestResponse {
    fn json(status: u16, value: serde_json::Value) -> Self {
        Self {
            status,
            headers: vec![("Content-Type".into(), "application/json".into())],
            body: serde_json::to_vec(&value).unwrap(),
        }
    }

    fn text(status: u16, body: &str) -> Self {
        Self {
            status,
            headers: vec![("Content-Type".into(), "text/plain; charset=utf-8".into())],
            body: body.as_bytes().to_vec(),
        }
    }

    fn redirect(location: String) -> Self {
        Self {
            status: 302,
            headers: vec![("Location".into(), location)],
            body: Vec::new(),
        }
    }
}

fn start_oidc_server(redirect_url: &str) -> HttpServer {
    let redirect_url = redirect_url.to_string();
    HttpServer::start(move |base_url| {
        let redirect_url = redirect_url.clone();
        move |request| match (request.method.as_str(), request.path.as_str()) {
            ("GET", "/.well-known/openid-configuration") => TestResponse::json(
                200,
                serde_json::json!({
                    "issuer": base_url,
                    "authorization_endpoint": format!("{base_url}/authorize"),
                    "token_endpoint": format!("{base_url}/token"),
                    "userinfo_endpoint": format!("{base_url}/userinfo"),
                    "jwks_uri": format!("{base_url}/jwks"),
                    "scopes_supported": ["openid", "email", "profile"]
                }),
            ),
            ("GET", "/authorize") => {
                let state = request.query("state").unwrap_or_default();
                TestResponse::redirect(format!("{redirect_url}?code=valid-code&state={state}"))
            }
            ("POST", "/token") => {
                let form = parse_form_body(&request.body);
                if form.get("code").map(String::as_str) != Some("valid-code") {
                    return TestResponse::json(
                        400,
                        serde_json::json!({
                            "error": "invalid_grant",
                            "error_description": "invalid authorization code"
                        }),
                    );
                }
                TestResponse::json(
                    200,
                    serde_json::json!({
                        "access_token": "oidc-access-token",
                        "token_type": "Bearer",
                        "expires_in": 3600,
                        "refresh_token": "oidc-refresh-token"
                    }),
                )
            }
            ("GET", "/userinfo") => {
                if request.headers.get("authorization").map(String::as_str)
                    != Some("Bearer oidc-access-token")
                {
                    return TestResponse::text(401, "invalid token");
                }
                TestResponse::json(
                    200,
                    serde_json::json!({
                        "email": TEST_EMAIL,
                        "name": "CLI Test User",
                        "picture": "https://example.com/avatar.png",
                        "email_verified": true
                    }),
                )
            }
            _ => TestResponse::text(404, "not found"),
        }
    })
    .unwrap()
}

fn start_upstream_server() -> HttpServer {
    HttpServer::start(move |base_url| {
        move |request| match (request.method.as_str(), request.path.as_str()) {
            ("GET", "/openapi.json") => TestResponse::json(
                200,
                serde_json::json!({
                    "openapi": "3.0.0",
                    "info": { "title": "CLI Test API", "version": "1.0.0" },
                    "servers": [{ "url": base_url }],
                    "paths": {
                        "/items": {
                            "get": {
                                "operationId": "list_items",
                                "summary": "List items",
                                "responses": {
                                    "200": {
                                        "description": "ok",
                                        "content": {
                                            "application/json": {
                                                "schema": {
                                                    "type": "array",
                                                    "items": { "type": "object" }
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        },
                        "/echo": {
                            "post": {
                                "operationId": "echo_item",
                                "summary": "Echo item",
                                "requestBody": {
                                    "required": false,
                                    "content": {
                                        "application/json": {
                                            "schema": {
                                                "type": "object",
                                                "properties": {
                                                    "query": { "type": "string" }
                                                }
                                            }
                                        }
                                    }
                                },
                                "responses": {
                                    "200": {
                                        "description": "ok",
                                        "content": {
                                            "application/json": {
                                                "schema": { "type": "object" }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }),
            ),
            ("GET", "/items") => TestResponse::json(
                200,
                serde_json::json!([
                    { "id": 1, "name": "example" }
                ]),
            ),
            ("POST", "/echo") => {
                let payload: serde_json::Value =
                    serde_json::from_slice(&request.body).unwrap_or_else(|_| serde_json::json!({}));
                TestResponse::json(
                    200,
                    serde_json::json!({
                        "received": payload,
                        "target": request.target
                    }),
                )
            }
            _ => TestResponse::text(404, "not found"),
        }
    })
    .unwrap()
}

fn repo_root() -> Result<PathBuf> {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../..")
        .canonicalize()
        .context("resolve repo root")
}

fn gestaltd_binary() -> Result<PathBuf> {
    static BIN: OnceLock<PathBuf> = OnceLock::new();
    if let Some(path) = BIN.get() {
        return Ok(path.clone());
    }

    let root = repo_root()?;
    let bin_dir = root.join("client/target/gestaltd-integration");
    fs::create_dir_all(&bin_dir).context("create gestaltd bin dir")?;
    let binary = if cfg!(windows) {
        bin_dir.join("gestaltd.exe")
    } else {
        bin_dir.join("gestaltd")
    };
    let status = Command::new("go")
        .current_dir(&root)
        .arg("build")
        .arg("-o")
        .arg(&binary)
        .arg("./cmd/gestaltd")
        .status()
        .context("build gestaltd")?;
    if !status.success() {
        bail!("go build ./cmd/gestaltd failed with status {status}");
    }

    let _ = BIN.set(binary.clone());
    Ok(BIN.get().cloned().unwrap_or(binary))
}

fn wait_for_server(
    base_url: &str,
    server: &mut Child,
    stdout_log: &Path,
    stderr_log: &Path,
) -> Result<()> {
    let client = reqwest::blocking::Client::builder()
        .timeout(Duration::from_millis(300))
        .build()
        .context("build readiness client")?;
    let deadline = Instant::now() + Duration::from_secs(20);

    while Instant::now() < deadline {
        if let Some(status) = server.try_wait().context("check gestaltd status")? {
            bail!(
                "gestaltd exited early with status {status}\nstdout:\n{}\nstderr:\n{}",
                read_or_empty(stdout_log),
                read_or_empty(stderr_log)
            );
        }

        if let Ok(response) = client.get(format!("{base_url}/ready")).send() {
            if response.status().is_success() {
                return Ok(());
            }
        }
        thread::sleep(Duration::from_millis(100));
    }

    bail!(
        "gestaltd did not become ready\nstdout:\n{}\nstderr:\n{}",
        read_or_empty(stdout_log),
        read_or_empty(stderr_log)
    )
}

fn read_or_empty(path: &Path) -> String {
    fs::read_to_string(path).unwrap_or_default()
}

fn free_port() -> Result<u16> {
    let listener = TcpListener::bind("127.0.0.1:0").context("bind free port")?;
    let port = listener.local_addr().context("read free port")?.port();
    Ok(port)
}

fn handle_connection<F>(stream: &mut TcpStream, handler: &F) -> Result<()>
where
    F: Fn(TestRequest) -> TestResponse + Send + Sync + 'static,
{
    stream
        .set_read_timeout(Some(Duration::from_secs(2)))
        .context("set read timeout")?;
    let Some(request) = read_request(stream)? else {
        return Ok(());
    };
    let response = handler(request);
    write_response(stream, response)
}

fn read_request(stream: &mut TcpStream) -> Result<Option<TestRequest>> {
    let mut raw = Vec::new();
    let mut buf = [0_u8; 4096];
    let header_end;
    loop {
        let read = stream.read(&mut buf).context("read request")?;
        if read == 0 {
            if raw.is_empty() {
                return Ok(None);
            }
            bail!("unexpected EOF while reading request");
        }
        raw.extend_from_slice(&buf[..read]);
        if let Some(pos) = find_header_end(&raw) {
            header_end = pos;
            break;
        }
    }

    let headers = &raw[..header_end];
    let mut body = raw[header_end + 4..].to_vec();
    let headers_text =
        String::from_utf8(headers.to_vec()).context("request headers were not utf-8")?;
    let mut lines = headers_text.split("\r\n");
    let request_line = lines.next().context("missing request line")?;
    let mut parts = request_line.split_whitespace();
    let method = parts.next().context("missing method")?.to_string();
    let target = parts.next().context("missing target")?.to_string();

    let mut headers_map = BTreeMap::new();
    for line in lines {
        if line.is_empty() {
            continue;
        }
        let (name, value) = line
            .split_once(':')
            .with_context(|| format!("invalid header line: {line}"))?;
        headers_map.insert(name.trim().to_lowercase(), value.trim().to_string());
    }

    let content_length = headers_map
        .get("content-length")
        .and_then(|value| value.parse::<usize>().ok())
        .unwrap_or(0);
    while body.len() < content_length {
        let read = stream.read(&mut buf).context("read request body")?;
        if read == 0 {
            break;
        }
        body.extend_from_slice(&buf[..read]);
    }
    body.truncate(content_length);

    let url =
        url::Url::parse(&format!("http://fixture{target}")).context("parse request target")?;
    let query = url.query_pairs().into_owned().collect();

    Ok(Some(TestRequest {
        method,
        target,
        path: url.path().to_string(),
        query,
        headers: headers_map,
        body,
    }))
}

fn write_response(stream: &mut TcpStream, response: TestResponse) -> Result<()> {
    let reason = reason_phrase(response.status);
    write!(stream, "HTTP/1.1 {} {}\r\n", response.status, reason).context("write status line")?;

    let mut has_length = false;
    for (name, value) in &response.headers {
        if name.eq_ignore_ascii_case("content-length") {
            has_length = true;
        }
        write!(stream, "{name}: {value}\r\n").context("write header")?;
    }
    if !has_length {
        write!(stream, "Content-Length: {}\r\n", response.body.len())
            .context("write content-length")?;
    }
    write!(stream, "Connection: close\r\n\r\n").context("write response separator")?;
    stream
        .write_all(&response.body)
        .context("write response body")?;
    stream.flush().context("flush response")
}

fn find_header_end(bytes: &[u8]) -> Option<usize> {
    bytes.windows(4).position(|window| window == b"\r\n\r\n")
}

fn parse_form_body(body: &[u8]) -> BTreeMap<String, String> {
    url::form_urlencoded::parse(body).into_owned().collect()
}

fn simulate_browser(url: &str) -> Result<()> {
    let client = reqwest::blocking::Client::builder()
        .redirect(reqwest::redirect::Policy::none())
        .timeout(Duration::from_secs(5))
        .build()
        .context("build browser client")?;
    let response = client.get(url).send().context("visit auth url")?;
    let location = response
        .headers()
        .get(reqwest::header::LOCATION)
        .context("authorization response missing location header")?
        .to_str()
        .context("location header was not utf-8")?;
    let redirect = url::Url::parse(location).context("parse redirect location")?;
    let code = redirect
        .query_pairs()
        .find(|(key, _)| key == "code")
        .map(|(_, value)| value.into_owned())
        .context("redirect missing code")?;
    let state = redirect
        .query_pairs()
        .find(|(key, _)| key == "state")
        .map(|(_, value)| value.into_owned())
        .context("redirect missing state")?;

    if let Some(rest) = state.strip_prefix("cli:") {
        let (port, original_state) = rest.split_once(':').context("invalid cli callback state")?;
        client
            .get(format!(
                "http://127.0.0.1:{port}/?state={original_state}&code={code}"
            ))
            .send()
            .context("deliver cli callback")?;
        return Ok(());
    }

    client.get(location).send().context("follow redirect")?;
    Ok(())
}

fn reason_phrase(status: u16) -> &'static str {
    match status {
        200 => "OK",
        201 => "Created",
        302 => "Found",
        400 => "Bad Request",
        401 => "Unauthorized",
        404 => "Not Found",
        _ => "OK",
    }
}
