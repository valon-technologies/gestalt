mod support;

use std::fs;

use support::{CliEnv, TestServer};

#[test]
fn config_commands_use_real_filesystem_state() {
    let cli = CliEnv::new();

    let set = cli.run(&["config", "set", "url", "gestalt.example.test"]);
    set.assert_success();

    let get = cli.run(&["--format", "json", "config", "get", "url"]);
    get.assert_success();
    assert_eq!(get.stdout_json()["url"], "https://gestalt.example.test");

    let list = cli.run(&["--format", "json", "config", "list"]);
    list.assert_success();
    assert_eq!(list.stdout_json()["url"], "https://gestalt.example.test");

    let unset = cli.run(&["config", "unset", "url"]);
    unset.assert_success();

    let get_missing = cli.run(&["--format", "json", "config", "get", "url"]);
    get_missing.assert_success();
    assert!(get_missing.stdout_json()["url"].is_null());
}

#[test]
fn auth_status_and_logout_use_real_credential_store() {
    let server = TestServer::start();
    let cli = CliEnv::new();
    let session = server.dev_login("status@gestalt.dev");
    cli.write_credentials(&server.base_url, &session);

    let status = cli.run(&["--format", "json", "auth", "status"]);
    status.assert_success();
    let body = status.stdout_json();
    assert_eq!(body["authenticated"], true);
    assert_eq!(body["source"], "session");
    assert_eq!(body["session_stored"], true);

    let logout = cli.run(&["auth", "logout"]);
    logout.assert_success();
    assert!(
        !cli.credentials_path().exists(),
        "credentials file should be removed on logout"
    );

    let status_after = cli.run(&["--format", "json", "auth", "status"]);
    status_after.assert_success();
    let body = status_after.stdout_json();
    assert_eq!(body["authenticated"], false);
    assert_eq!(body["source"], "none");
    assert_eq!(body["session_stored"], false);
}

#[test]
fn integrations_list_and_invoke_use_project_url_precedence() {
    let server = TestServer::start();
    let cli = CliEnv::new();
    let session = server.dev_login("invoke@gestalt.dev");
    cli.write_credentials("http://127.0.0.1:9", &session);

    let project_root = cli.project_dir.join("workspace");
    let nested = project_root.join("nested");
    fs::create_dir_all(&nested).expect("create nested project dir");
    cli.write_project_url(&project_root, &server.base_url);

    let list = cli.run_in(
        &nested,
        &["--format", "json", "integrations", "list"],
        None,
        &[],
    );
    list.assert_success();
    assert!(
        list.stdout_json()
            .as_array()
            .expect("integrations array")
            .iter()
            .any(|item| item["name"] == "echo"),
        "expected built-in echo integration in list output"
    );

    let invoke = cli.run_in(
        &nested,
        &[
            "--format",
            "json",
            "invoke",
            "echo",
            "echo",
            "-p",
            "message=hello",
        ],
        None,
        &[],
    );
    invoke.assert_success();
    let body = invoke.stdout_json();
    assert_eq!(body["message"], "hello");
}

#[test]
fn tokens_create_list_and_revoke_use_live_server() {
    let server = TestServer::start();
    let cli = CliEnv::new();
    let session = server.dev_login("tokens@gestalt.dev");
    cli.write_credentials(&server.base_url, &session);

    let create = cli.run(&[
        "--format",
        "json",
        "tokens",
        "create",
        "--name",
        "cli-token",
    ]);
    create.assert_success();
    let created = create.stdout_json();
    let token_id = created["id"].as_str().expect("token id").to_string();
    assert_eq!(created["name"], "cli-token");
    assert!(
        created["token"]
            .as_str()
            .is_some_and(|token| !token.is_empty()),
        "plaintext token should be returned once on create"
    );

    let list = cli.run(&["--format", "json", "tokens", "list"]);
    list.assert_success();
    assert!(
        list.stdout_json()
            .as_array()
            .expect("tokens array")
            .iter()
            .any(|item| item["id"] == token_id && item["name"] == "cli-token"),
        "expected created token in list output"
    );

    let revoke = cli.run(&["tokens", "revoke", &token_id]);
    revoke.assert_success();

    let list_after = cli.run(&["--format", "json", "tokens", "list"]);
    list_after.assert_success();
    assert!(
        list_after
            .stdout_json()
            .as_array()
            .expect("tokens array")
            .is_empty(),
        "expected token list to be empty after revoke"
    );
}

#[test]
fn env_api_key_overrides_session_and_invalid_env_key_shows_guidance() {
    let server = TestServer::start();
    let cli = CliEnv::new();
    let valid_session = server.dev_login("env@gestalt.dev");
    cli.write_credentials(&server.base_url, &valid_session);

    let api_token = server.create_api_token(&valid_session, "env-token");
    let token_value = api_token["token"].as_str().expect("api token value");

    cli.write_credentials(&server.base_url, "invalid-session-token");
    let env_success = cli.run_in(
        &cli.project_dir,
        &["--format", "json", "integrations", "list"],
        None,
        &[("GESTALT_API_KEY", token_value)],
    );
    env_success.assert_success();

    cli.write_credentials(&server.base_url, &valid_session);
    let env_failure = cli.run_in(
        &cli.project_dir,
        &["integrations", "list"],
        None,
        &[("GESTALT_API_KEY", "not-a-real-token")],
    );
    env_failure.assert_failure();
    assert!(
        env_failure
            .stderr
            .contains("using GESTALT_API_KEY from environment"),
        "stderr should explain env token precedence:\n{}",
        env_failure.stderr
    );
}

#[test]
fn init_writes_global_and_project_config_without_logging_in() {
    let server = TestServer::start();
    let cli = CliEnv::new();
    let project_dir = cli.project_dir.join("init-project");
    fs::create_dir_all(&project_dir).expect("create init project dir");

    let init = cli.run_in(
        &project_dir,
        &["init"],
        Some(&format!("{}\nn\ny\n", server.base_url)),
        &[],
    );
    init.assert_success();

    let global_config = fs::read_to_string(cli.config_path()).expect("read global config");
    assert!(
        global_config.contains(&server.base_url),
        "global config should contain configured url"
    );

    let project_config =
        fs::read_to_string(project_dir.join(".gestalt.json")).expect("read project config");
    assert!(
        project_config.contains(&server.base_url),
        "project config should contain configured url"
    );
}
