mod support;

use support::*;

#[test]
fn test_cli_ignores_legacy_stored_credentials_api_url() {
    let mut server = Server::new();
    let tokens = authed_json_mock!(server, Method::GET, "/api/v1/tokens", StatusCode::OK)
        .expect(0)
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
        .args(["auth", "token", "list"])
        .assert()
        .failure()
        .stderr(predicate::str::contains("no URL configured"));
    tokens.assert();
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
fn test_resolve_url_ignores_project_config_file() {
    let _lock = env_lock();
    let config_root = TempDir::new().unwrap();
    let _env = EnvGuard::new(config_root.path());
    let workspace = TempDir::new().unwrap();
    let repo_root = workspace.path().join("repo");
    let nested = repo_root.join("nested");

    std::fs::create_dir_all(repo_root.join(".gestalt")).unwrap();
    std::fs::create_dir_all(&nested).unwrap();
    std::fs::write(
        repo_root.join(".gestalt.json"),
        "{\n  \"url\": \"https://unsupported.example.com\"\n}\n",
    )
    .unwrap();
    std::fs::write(
        repo_root.join(".gestalt/config.json"),
        "{\n  \"url\": \"https://project.example.com\"\n}\n",
    )
    .unwrap();
    gestalt::config::ConfigStore::new()
        .unwrap()
        .set("url", "https://global.example.com")
        .unwrap();

    let _cwd = CurrentDirGuard::new(&nested);
    let resolved = gestalt::api::resolve_url(Some("localhost:9999")).unwrap();
    assert_eq!(resolved, "http://localhost:9999");

    let resolved = gestalt::api::resolve_url(None).unwrap();
    assert_eq!(resolved, "https://global.example.com");

    std::fs::write(repo_root.join(".gestalt/config.json"), "{\n  invalid\n}\n").unwrap();

    let resolved = gestalt::api::resolve_url(None).unwrap();
    assert_eq!(resolved, "https://global.example.com");
}
