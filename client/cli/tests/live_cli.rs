mod support;

use std::fs;

#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;

use support::TestEnv;

#[test]
fn auth_login_status_and_logout_round_trip() {
    let env = TestEnv::new();
    let home = env.new_home();

    let before = env.run_cli(&home, &["--format", "json", "auth", "status"]);
    before.assert_success();
    let before_json = before.json_stdout();
    assert_eq!(before_json["authenticated"], false);
    assert_eq!(before_json["source"], "none");

    let login = env.login(&home);
    login.assert_success();
    assert!(login.stderr.contains("Logged in successfully."));

    let credentials_path = home.credentials_path();
    assert!(credentials_path.exists(), "credentials file should exist");

    let credentials: serde_json::Value =
        serde_json::from_str(&fs::read_to_string(&credentials_path).unwrap()).unwrap();
    assert_eq!(credentials["api_url"], env.base_url());
    assert!(!credentials["session_token"]
        .as_str()
        .unwrap_or_default()
        .is_empty());

    #[cfg(unix)]
    {
        let mode = fs::metadata(&credentials_path)
            .unwrap()
            .permissions()
            .mode()
            & 0o777;
        assert_eq!(mode, 0o600);
    }

    let status = env.run_cli(&home, &["--format", "json", "auth", "status"]);
    status.assert_success();
    let status_json = status.json_stdout();
    assert_eq!(status_json["authenticated"], true);
    assert_eq!(status_json["source"], "session");
    assert_eq!(status_json["session_stored"], true);

    let env_status = env.run_cli_with(
        &home,
        &["--format", "json", "auth", "status"],
        &[("GESTALT_API_KEY", "env-token")],
        None,
        None,
    );
    env_status.assert_success();
    let env_status_json = env_status.json_stdout();
    assert_eq!(env_status_json["authenticated"], true);
    assert_eq!(env_status_json["source"], "env");
    assert_eq!(env_status_json["env_var_set"], true);
    assert_eq!(env_status_json["session_stored"], true);

    let logout = env.run_cli(&home, &["auth", "logout"]);
    logout.assert_success();
    assert!(logout.stderr.contains("Logged out. Credentials removed."));
    assert!(
        !credentials_path.exists(),
        "credentials file should be deleted"
    );

    let after = env.run_cli(&home, &["--format", "json", "auth", "status"]);
    after.assert_success();
    let after_json = after.json_stdout();
    assert_eq!(after_json["authenticated"], false);
    assert_eq!(after_json["source"], "none");
}

#[test]
fn config_commands_and_url_precedence_use_real_resolution_rules() {
    let env = TestEnv::new();
    let home = env.new_home();
    let session_token = env.dev_session_token();

    let set = env.run_cli(&home, &["config", "set", "url", "api.example.com"]);
    set.assert_success();
    assert!(set.stderr.contains("url = https://api.example.com"));

    let get = env.run_cli(&home, &["--format", "json", "config", "get", "url"]);
    get.assert_success();
    assert_eq!(get.json_stdout()["url"], "https://api.example.com");

    let list = env.run_cli(&home, &["--format", "json", "config", "list"]);
    list.assert_success();
    assert_eq!(list.json_stdout()["url"], "https://api.example.com");

    let bad_url = "http://127.0.0.1:9";
    let set_bad = env.run_cli(&home, &["config", "set", "url", bad_url]);
    set_bad.assert_success();

    let project_root = home.root().join("project");
    let nested = project_root.join("nested");
    fs::create_dir_all(&nested).unwrap();
    fs::write(
        project_root.join(".gestalt.json"),
        format!("{{\n  \"url\": \"{}\"\n}}\n", env.base_url()),
    )
    .unwrap();

    let project_config = env.run_cli_with(
        &home,
        &["--format", "json", "integrations", "list"],
        &[("GESTALT_API_KEY", &session_token)],
        Some(&nested),
        None,
    );
    project_config.assert_success();
    let integrations = project_config.json_stdout();
    assert!(integrations
        .as_array()
        .unwrap()
        .iter()
        .any(|item| item["name"] == "restapi"));

    let url_override = env.run_cli_with(
        &home,
        &[
            "--url",
            env.base_url(),
            "--format",
            "json",
            "integrations",
            "list",
        ],
        &[
            ("GESTALT_API_KEY", &session_token),
            ("GESTALT_URL", "http://127.0.0.1:8"),
        ],
        None,
        None,
    );
    url_override.assert_success();
    assert!(url_override
        .json_stdout()
        .as_array()
        .unwrap()
        .iter()
        .any(|item| item["name"] == "restapi"));

    let unset = env.run_cli(&home, &["config", "unset", "url"]);
    unset.assert_success();
    assert!(unset.stderr.contains("url removed"));
}

#[test]
fn tokens_and_invocations_run_against_live_gestaltd() {
    let env = TestEnv::new();
    let home = env.new_home();
    let session_token = env.dev_session_token();

    let login = env.login(&home);
    login.assert_success();

    env.connect_manual(&session_token, "restapi", "manual-token");

    let invalid_env = env.run_cli_with(
        &home,
        &["--format", "json", "tokens", "list"],
        &[("GESTALT_API_KEY", "not-a-real-token")],
        None,
        None,
    );
    invalid_env.assert_failure();
    assert!(invalid_env
        .stderr
        .contains("unset it to use your session token from 'gestalt auth login'"));

    let create = env.run_cli(
        &home,
        &["--format", "json", "tokens", "create", "--name", "smoke"],
    );
    create.assert_success();
    let created = create.json_stdout();
    let token_id = created["id"].as_str().unwrap().to_string();
    assert_eq!(created["name"], "smoke");
    assert!(!created["token"].as_str().unwrap_or_default().is_empty());

    let list = env.run_cli(&home, &["--format", "json", "tokens", "list"]);
    list.assert_success();
    let listed = list.json_stdout();
    assert!(listed
        .as_array()
        .unwrap()
        .iter()
        .any(|item| item["id"] == token_id && item["name"] == "smoke"));

    let ops = env.run_cli(&home, &["--format", "json", "invoke", "restapi"]);
    ops.assert_success();
    let operations = ops.json_stdout();
    assert!(operations
        .as_array()
        .unwrap()
        .iter()
        .any(|item| item["Name"] == "list_items"));
    assert!(operations
        .as_array()
        .unwrap()
        .iter()
        .any(|item| item["Name"] == "echo_item"));

    let invoke_list = env.run_cli(
        &home,
        &["--format", "json", "invoke", "restapi", "list_items"],
    );
    invoke_list.assert_success();
    let items = invoke_list.json_stdout();
    assert_eq!(items[0]["name"], "example");

    let invoke_post = env.run_cli(
        &home,
        &[
            "--format",
            "json",
            "invoke",
            "restapi",
            "echo_item",
            "-p",
            "query=hello",
        ],
    );
    invoke_post.assert_success();
    let echoed = invoke_post.json_stdout();
    assert_eq!(echoed["received"]["query"], "hello");

    let revoke = env.run_cli(&home, &["--format", "json", "tokens", "revoke", &token_id]);
    revoke.assert_success();
    assert_eq!(revoke.json_stdout()["status"], "revoked");

    let after = env.run_cli(&home, &["--format", "json", "tokens", "list"]);
    after.assert_success();
    assert!(!after
        .json_stdout()
        .as_array()
        .unwrap()
        .iter()
        .any(|item| item["id"] == token_id));
}

#[test]
fn init_writes_global_and_project_config_without_logging_in() {
    let env = TestEnv::new();
    let home = env.new_home();
    let project_dir = home.root().join("workspace");
    fs::create_dir_all(&project_dir).unwrap();

    let input = format!("{}\nn\ny\n", env.base_url());
    let init = env.run_cli_with(&home, &["init"], &[], Some(&project_dir), Some(&input));
    init.assert_success();
    assert!(init.stderr.contains("Saved to global config."));
    assert!(init.stderr.contains("Created .gestalt.json"));
    assert!(init
        .stderr
        .contains("You're all set! Run 'gestalt --help' to see available commands."));

    let global_config: serde_json::Value =
        serde_json::from_str(&fs::read_to_string(home.config_path()).unwrap()).unwrap();
    assert_eq!(global_config["url"], env.base_url());

    let project_config: serde_json::Value =
        serde_json::from_str(&fs::read_to_string(project_dir.join(".gestalt.json")).unwrap())
            .unwrap();
    assert_eq!(project_config["url"], env.base_url());
}
