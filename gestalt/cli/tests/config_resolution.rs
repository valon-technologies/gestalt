mod support;

use support::*;

#[test]
fn test_cli_reuses_stored_credentials_api_url() {
    let mut server = Server::new();
    let _tokens = authed_json_mock!(server, Method::GET, "/api/v1/tokens", StatusCode::OK)
        .with_body("[]")
        .create();

    let home = tempfile::tempdir().unwrap();
    write_cli_credentials(
        home.path(),
        &format!(
            r#"{{"api_url":"{}","api_token":"{}","api_token_id":"tok-123"}}"#,
            server.url(),
            TEST_TOKEN
        ),
    );

    cli_command(home.path())
        .args(["tokens", "list"])
        .assert()
        .success();
}

#[test]
fn test_cli_refuses_stored_credentials_when_project_config_points_elsewhere() {
    let mut credential_server = Server::new();
    let credential_server_tokens = authed_json_mock!(
        credential_server,
        Method::GET,
        "/api/v1/tokens",
        StatusCode::OK
    )
    .expect(0)
    .with_body("[]")
    .create();
    let mut poisoned_server = Server::new();
    let poisoned_server_tokens = authed_json_mock!(
        poisoned_server,
        Method::GET,
        "/api/v1/tokens",
        StatusCode::OK
    )
    .expect(0)
    .with_body("[]")
    .create();

    let home = tempfile::tempdir().unwrap();
    write_credentials(
        home.path(),
        serde_json::json!({
            "api_url": credential_server.url(),
            "api_token": TEST_TOKEN,
            "api_token_id": "tok-123",
        }),
    );

    let workspace = TempDir::new().unwrap();
    let repo_root = workspace.path().join("repo");
    let nested = repo_root.join("nested");
    std::fs::create_dir_all(repo_root.join(".gestalt")).unwrap();
    std::fs::create_dir_all(&nested).unwrap();
    std::fs::write(
        repo_root.join(".gestalt/config.json"),
        format!("{{\n  \"url\": {:?}\n}}\n", poisoned_server.url()),
    )
    .unwrap();

    cli_command(home.path())
        .current_dir(&nested)
        .args(["tokens", "list"])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "refusing to send stored CLI token",
        ))
        .stderr(predicate::str::contains(credential_server.url()))
        .stderr(predicate::str::contains(poisoned_server.url()));

    credential_server_tokens.assert();
    poisoned_server_tokens.assert();
}

#[test]
fn test_cli_allows_explicit_env_token_for_project_config_url() {
    let mut credential_server = Server::new();
    let credential_server_tokens = authed_json_mock!(
        credential_server,
        Method::GET,
        "/api/v1/tokens",
        StatusCode::OK
    )
    .expect(0)
    .with_body("[]")
    .create();
    let mut project_server = Server::new();
    let project_server_tokens = authed_json_mock!(
        project_server,
        Method::GET,
        "/api/v1/tokens",
        StatusCode::OK
    )
    .with_body("[]")
    .create();

    let home = tempfile::tempdir().unwrap();
    write_credentials(
        home.path(),
        serde_json::json!({
            "api_url": credential_server.url(),
            "api_token": "stored-token",
            "api_token_id": "tok-123",
        }),
    );

    let workspace = TempDir::new().unwrap();
    std::fs::create_dir_all(workspace.path().join(".gestalt")).unwrap();
    std::fs::write(
        workspace.path().join(".gestalt/config.json"),
        format!("{{\n  \"url\": {:?}\n}}\n", project_server.url()),
    )
    .unwrap();

    cli_command(home.path())
        .current_dir(workspace.path())
        .env(gestalt::api::ENV_API_KEY, TEST_TOKEN)
        .args(["tokens", "list"])
        .assert()
        .success();

    credential_server_tokens.assert();
    project_server_tokens.assert();
}

#[test]
fn test_cli_ignores_blank_stored_credentials_api_url() {
    let home = tempfile::tempdir().unwrap();
    write_cli_credentials(
        home.path(),
        &format!(
            r#"{{"api_url":"   ","api_token":"{}","api_token_id":"tok-123"}}"#,
            TEST_TOKEN
        ),
    );

    cli_command(home.path())
        .args(["tokens", "list"])
        .assert()
        .failure()
        .stderr(predicate::str::contains("no URL configured"));
}

#[test]
fn test_cli_refuses_blank_stored_credentials_api_url_for_project_config() {
    let mut project_server = Server::new();
    let project_server_tokens = authed_json_mock!(
        project_server,
        Method::GET,
        "/api/v1/tokens",
        StatusCode::OK
    )
    .expect(0)
    .with_body("[]")
    .create();

    let home = tempfile::tempdir().unwrap();
    write_cli_credentials(
        home.path(),
        &format!(
            r#"{{"api_url":"   ","api_token":"{}","api_token_id":"tok-123"}}"#,
            TEST_TOKEN
        ),
    );

    let workspace = TempDir::new().unwrap();
    std::fs::create_dir_all(workspace.path().join(".gestalt")).unwrap();
    std::fs::write(
        workspace.path().join(".gestalt/config.json"),
        format!("{{\n  \"url\": {:?}\n}}\n", project_server.url()),
    )
    .unwrap();

    cli_command(home.path())
        .current_dir(workspace.path())
        .args(["tokens", "list"])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "refusing to send stored CLI token without a recorded server URL",
        ))
        .stderr(predicate::str::contains(project_server.url()));

    project_server_tokens.assert();
}

#[test]
fn test_bare_command_shows_server_footer() {
    let home = tempfile::tempdir().unwrap();

    cli_command(home.path())
        .arg("--url")
        .arg("http://localhost:9999")
        .assert()
        .success()
        .stderr(predicate::str::contains(
            "Target server: http://localhost:9999",
        ))
        .stderr(predicate::str::contains("Config source: --url flag"));
}

#[test]
fn test_bare_command_shows_not_configured_when_no_url() {
    let home = tempfile::tempdir().unwrap();

    cli_command(home.path())
        .assert()
        .success()
        .stderr(predicate::str::contains("Target server: not configured"));
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
fn test_resolve_url_prefers_project_config_file_and_propagates_project_config_errors() {
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

    std::fs::write(repo_root.join(".gestalt/config.json"), "{\n  invalid\n}\n").unwrap();

    let err = gestalt::api::resolve_url_with_fallback(None, Some("https://fallback.example.com"))
        .unwrap_err();
    assert!(
        err.to_string().contains("failed to parse project config"),
        "{err:#}"
    );
}

#[test]
fn test_resolve_url_does_not_read_unsupported_dot_gestalt_json_file() {
    let _lock = env_lock();
    let config_root = TempDir::new().unwrap();
    let _env = EnvGuard::new(config_root.path());
    let workspace = TempDir::new().unwrap();
    let repo_root = workspace.path().join("repo");
    let nested = repo_root.join("nested");

    std::fs::create_dir_all(&nested).unwrap();
    std::fs::write(
        repo_root.join(".gestalt.json"),
        "{\n  \"url\": \"https://unsupported.example.com\"\n}\n",
    )
    .unwrap();

    let mut set_cmd = cli_command(config_root.path());
    set_cmd.args(["config", "set", "url", "https://global.example.com"]);
    set_cmd.assert().success();

    let _cwd = CurrentDirGuard::new(&nested);
    let resolved = gestalt::api::resolve_url(None).unwrap();
    assert_eq!(resolved, "https://global.example.com");
}
