mod support;

use std::sync::{Arc, Mutex};

use support::*;

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
    let body = serde_json::json!({"integration": "acme_crm"});
    let resp = client.post("/api/v1/auth/start-oauth", &body).unwrap();

    mock.assert();
    assert_eq!(resp["url"], "https://example.com/oauth");
    assert_eq!(resp["state"], "abc123");
}

#[test]
fn test_auth_login_stores_credentials_and_serves_browser_callback_page() {
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
    assert!(!html.contains("Gestalt CLI"));
    assert!(!html.contains("CLI login complete"));

    let credentials_path = gestalt::paths::gestalt_config_dir()
        .unwrap()
        .join("credentials.json");
    let credentials: serde_json::Value =
        serde_json::from_str(&std::fs::read_to_string(credentials_path).unwrap()).unwrap();
    assert_eq!(credentials["api_url"], base_url);
    assert_eq!(credentials["api_token"], "cli-secret");
    assert_eq!(credentials["api_token_id"], "tok-123");
}

#[test]
fn test_auth_login_fails_when_server_auth_is_disabled() {
    let _lock = env_lock();
    let tempdir = tempfile::tempdir().unwrap();
    let _env = EnvGuard::new(tempdir.path());
    let mut server = Server::new();
    let _auth_info = json_mock!(server, Method::GET, "/api/v1/auth/info", StatusCode::OK)
        .with_body(r#"{"provider":"none","displayName":"none","loginSupported":false}"#)
        .create();
    let login = json_mock!(server, Method::POST, "/api/v1/auth/login", StatusCode::OK)
        .expect(0)
        .create();

    let err = gestalt::commands::auth::login_with_browser_opener(Some(&server.url()), |_| {
        panic!("browser should not open when auth is disabled");
    })
    .unwrap_err();

    assert_eq!(err.to_string(), "authentication is disabled on this server");
    login.assert();
}

#[test]
fn test_auth_logout_revokes_token_using_credential_url_even_when_configured_url_differs() {
    let mut token_server = Server::new();
    let revoke = authed_json_mock!(
        token_server,
        Method::DELETE,
        "/api/v1/tokens/tok-123",
        StatusCode::OK
    )
    .with_body(r#"{"status":"ok"}"#)
    .create();
    let wrong_server = Server::new();

    let home = tempfile::tempdir().unwrap();
    write_credentials(
        home.path(),
        serde_json::json!({
            "api_url": token_server.url(),
            "api_token": TEST_TOKEN,
            "api_token_id": "tok-123",
        }),
    );

    cli_command(home.path())
        .args(["config", "set", "url", &wrong_server.url()])
        .assert()
        .success();

    cli_command(home.path())
        .args(["auth", "logout"])
        .assert()
        .success()
        .stderr(predicate::str::contains("Logged out. Credentials removed."))
        .stderr(predicate::str::contains("Failed to revoke stored CLI token").not());

    revoke.assert();
    assert!(
        !home
            .path()
            .join("xdg-config")
            .join("gestalt")
            .join("credentials.json")
            .exists()
    );
}

#[test]
fn test_auth_logout_uses_configured_url_when_legacy_credentials_omit_api_url() {
    let mut server = Server::new();
    let revoke = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/tokens/tok-123",
        StatusCode::OK
    )
    .with_body(r#"{"status":"ok"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    write_credentials(
        home.path(),
        serde_json::json!({
            "api_token": TEST_TOKEN,
            "api_token_id": "tok-123",
        }),
    );

    cli_command(home.path())
        .args(["config", "set", "url", &server.url()])
        .assert()
        .success();

    cli_command(home.path())
        .args(["auth", "logout"])
        .assert()
        .success()
        .stderr(predicate::str::contains("Logged out. Credentials removed."))
        .stderr(predicate::str::contains("Failed to revoke stored CLI token").not());

    revoke.assert();
}

#[test]
fn test_init_skips_login_when_server_auth_is_disabled() {
    let mut server = Server::new();
    let _auth_info = json_mock!(server, Method::GET, "/api/v1/auth/info", StatusCode::OK)
        .with_body(r#"{"provider":"none","displayName":"none","loginSupported":false}"#)
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command(home.path())
        .arg("--url")
        .arg(server.url())
        .arg("init")
        .write_stdin("\n\n")
        .assert()
        .success()
        .stderr(predicate::str::contains(
            "Authentication is disabled on this server; skipping login.",
        ))
        .stderr(predicate::str::contains("Log in now?").not());
}

#[test]
fn test_auth_status_reports_auth_disabled_before_stored_credentials() {
    let mut server = Server::new();
    let _auth_info = json_mock!(server, Method::GET, "/api/v1/auth/info", StatusCode::OK)
        .with_body(r#"{"loginSupported":false}"#)
        .create();

    let home = tempfile::tempdir().unwrap();
    write_credentials(
        home.path(),
        serde_json::json!({
            "api_url": server.url(),
            "api_token": TEST_TOKEN,
            "api_token_id": "tok-123",
        }),
    );

    cli_command(home.path())
        .arg("--url")
        .arg(server.url())
        .args(["auth", "status"])
        .assert()
        .success()
        .stderr(predicate::str::contains("Auth:        disabled"))
        .stderr(predicate::str::contains("Credentials: stored CLI token"))
        .stderr(predicate::str::contains("Reachable:   yes"))
        .stderr(predicate::str::contains("URL source:  --url flag"));
}

#[test]
fn test_auth_status_prefers_gestalt_url_over_project_and_global_config() {
    let _lock = env_lock();
    let config_root = TempDir::new().unwrap();
    let _env = EnvGuard::new(config_root.path());
    let workspace = TempDir::new().unwrap();
    let repo_root = workspace.path().join("repo");
    let nested = repo_root.join("nested");
    let mut server = Server::new();
    let _auth_info = json_mock!(server, Method::GET, "/api/v1/auth/info", StatusCode::OK)
        .with_body(r#"{"loginSupported":true}"#)
        .create();

    std::fs::create_dir_all(repo_root.join(".gestalt")).unwrap();
    std::fs::create_dir_all(&nested).unwrap();
    std::fs::write(
        repo_root.join(".gestalt/config.json"),
        "{\n  \"url\": \"https://project.example.com\"\n}\n",
    )
    .unwrap();

    cli_command(config_root.path())
        .args(["config", "set", "url", "https://global.example.com"])
        .assert()
        .success();

    let _cwd = CurrentDirGuard::new(&nested);
    cli_command(config_root.path())
        .env("GESTALT_URL", server.url())
        .args(["auth", "status"])
        .assert()
        .success()
        .stderr(predicate::str::contains(format!(
            "Server:      {}",
            server.url()
        )))
        .stderr(predicate::str::contains(
            "URL source:  GESTALT_URL environment variable",
        ))
        .stderr(predicate::str::contains("Reachable:   yes"));
}

#[test]
fn test_auth_status_reports_unreachable_server() {
    let home = tempfile::tempdir().unwrap();
    write_credentials(
        home.path(),
        serde_json::json!({
            "api_url": "http://127.0.0.1:1",
            "api_token": TEST_TOKEN,
            "api_token_id": "tok-123",
        }),
    );

    cli_command(home.path())
        .arg("--url")
        .arg("http://127.0.0.1:1")
        .args(["auth", "status"])
        .assert()
        .success()
        .stderr(predicate::str::contains("Server:      http://127.0.0.1:1"))
        .stderr(predicate::str::contains("Reachable:   no"))
        .stderr(predicate::str::contains("Credentials: stored CLI token"));
}

#[test]
fn test_auth_status_prefers_env_api_key_over_stored_credentials() {
    let mut server = Server::new();
    let _auth_info = json_mock!(server, Method::GET, "/api/v1/auth/info", StatusCode::OK)
        .with_body(r#"{"loginSupported":true}"#)
        .create();

    let home = tempfile::tempdir().unwrap();
    write_credentials(
        home.path(),
        serde_json::json!({
            "api_url": server.url(),
            "api_token": TEST_TOKEN,
            "api_token_id": "tok-123",
        }),
    );

    cli_command(home.path())
        .env("GESTALT_API_KEY", "env-token")
        .arg("--url")
        .arg(server.url())
        .args(["auth", "status"])
        .assert()
        .success()
        .stderr(predicate::str::contains("Credentials: GESTALT_API_KEY set"))
        .stderr(predicate::str::contains(
            "Stored CLI credentials also exist but are being ignored.",
        ));

    let output = cli_command(home.path())
        .env("GESTALT_API_KEY", "env-token")
        .arg("--url")
        .arg(server.url())
        .args(["auth", "status", "--format", "json"])
        .output()
        .unwrap();

    let json: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(json["source"], serde_json::json!("env"));
    assert_eq!(json["envVarSet"], serde_json::json!(true));
    assert_eq!(json["storedCredentials"], serde_json::json!(true));
}

#[test]
fn test_auth_status_does_not_report_stored_credentials_api_url_as_configured_server() {
    let home = tempfile::tempdir().unwrap();
    write_credentials(
        home.path(),
        serde_json::json!({
            "api_url": "https://stored.example.com",
            "api_token": TEST_TOKEN,
            "api_token_id": "tok-123",
        }),
    );

    cli_command(home.path())
        .args(["auth", "status"])
        .assert()
        .success()
        .stderr(predicate::str::contains("Server:      not configured"))
        .stderr(predicate::str::contains("Credentials: stored CLI token"))
        .stderr(predicate::str::contains("stored.example.com").not());

    let output = cli_command(home.path())
        .args(["auth", "status", "--format", "json"])
        .output()
        .unwrap();

    let json: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(json["serverUrl"], serde_json::json!(null));
    assert_eq!(json["urlSource"], serde_json::json!(null));
    assert_eq!(json["storedCredentials"], serde_json::json!(true));
}

#[test]
fn test_auth_status_no_url_configured() {
    let home = tempfile::tempdir().unwrap();

    cli_command(home.path())
        .args(["auth", "status"])
        .assert()
        .success()
        .stderr(predicate::str::contains("Server:      not configured"))
        .stderr(predicate::str::contains("Credentials: none"))
        .stderr(predicate::str::contains("gestalt init"));
}

#[test]
fn test_auth_status_json_includes_server_fields() {
    let mut server = Server::new();
    let _auth_info = json_mock!(server, Method::GET, "/api/v1/auth/info", StatusCode::OK)
        .with_body(r#"{"loginSupported":true}"#)
        .create();

    let home = tempfile::tempdir().unwrap();

    let output = cli_command(home.path())
        .arg("--url")
        .arg(server.url())
        .args(["auth", "status", "--format", "json"])
        .output()
        .unwrap();

    let json: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(json["serverReachable"], serde_json::json!(true));
    assert_eq!(json["loginSupported"], serde_json::json!(true));
    assert_eq!(json["serverUrl"], serde_json::json!(server.url()));
    assert_eq!(json["urlSource"], serde_json::json!("--url flag"));
    assert_eq!(json["authenticated"], serde_json::json!(false));
}
