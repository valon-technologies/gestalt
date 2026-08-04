mod support;

use std::sync::{Arc, Mutex};

use support::*;

#[test]
fn test_list_apps() {
    let mut server = Server::new();
    let mock = authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
        .with_body(r#"[{"name":"acme_crm","displayName":"Acme CRM","description":"Acme CRM app"}]"#)
        .create();

    let client = create_client(&server);
    let resp = client.get("/api/v1/apps").unwrap();

    mock.assert();
    let items = resp.as_array().unwrap();
    assert_eq!(items.len(), 1);
    assert_eq!(items[0]["name"], "acme_crm");
}

#[test]
fn test_connect_includes_connection_and_instance() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
        .with_body(
            r#"[{"name":"acme_crm","connections":[{"name":"workspace","authTypes":["oauth"]}]}]"#,
        )
        .create();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/start-oauth",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"workspace","instance":"team-a","integration":"acme_crm"}"#.to_string(),
    ))
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::apps::connect_with_browser_opener(
        &client,
        "acme_crm",
        Some("workspace"),
        Some("team-a"),
        None,
        |_| Ok(()),
    );

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_connect_oauth_includes_service_account_id() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
        .with_body(
            r#"[{"name":"acme_crm","connections":[{"name":"workspace","authTypes":["oauth"]}]}]"#,
        )
        .create();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/start-oauth",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"workspace","integration":"acme_crm","serviceAccountId":"service_account:nightly-sync"}"#.to_string(),
    ))
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::apps::connect_with_browser_opener(
        &client,
        "acme_crm",
        Some("workspace"),
        None,
        Some("service_account:nightly-sync"),
        |_| Ok(()),
    );

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_connect_prefers_oauth_when_manual_also_exists_and_omits_null_instance() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/apps",
        StatusCode::OK
    )
    .with_body(
        r#"[{"name":"acme_crm","connections":[{"name":"app","authTypes":["oauth","manual"]}]}]"#,
    )
    .create();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/start-oauth",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"app","integration":"acme_crm"}"#.to_string(),
    ))
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let client = create_client(&server);
    let opened_url = Arc::new(Mutex::new(None));
    let opened_url_handle = Arc::clone(&opened_url);
    let result = gestalt::commands::apps::connect_with_browser_opener(
        &client,
        "acme_crm",
        None,
        None,
        None,
        move |url| {
            *opened_url_handle.lock().unwrap() = Some(url.to_string());
            Ok(())
        },
    );

    mock.assert();
    assert!(result.is_ok());
    assert_eq!(
        opened_url.lock().unwrap().as_deref(),
        Some("https://example.com/oauth")
    );
}

#[test]
fn test_connect_uses_user_facing_app_connection_name_on_the_wire() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
        .with_body(
            r#"[{
                "name":"acme_crm",
                "connections":[{"name":"app","displayName":"App OAuth","authTypes":["oauth"]}]
            }]"#,
        )
        .create();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/start-oauth",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"app","integration":"acme_crm"}"#.to_string(),
    ))
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::apps::connect_with_browser_opener(
        &client,
        "acme_crm",
        Some("app"),
        None,
        None,
        |_| Ok(()),
    );

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_disconnect_sends_delete_with_connection_and_instance() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/apps/widget_metrics?_connection=oauth&_instance=prod",
        StatusCode::OK
    )
    .with_body(r#"{"status":"disconnected"}"#)
    .create();

    let client = create_client(&server);
    let result =
        gestalt::commands::apps::disconnect(&client, "widget_metrics", Some("oauth"), Some("prod"));

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_disconnect_normalizes_app_connection_name() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/apps/acme_crm?_connection=_app",
        StatusCode::OK
    )
    .with_body(r#"{"status":"disconnected"}"#)
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::apps::disconnect(&client, "acme_crm", Some("app"), None);

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_disconnect_without_optional_params() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/apps/buzz_chat",
        StatusCode::OK
    )
    .with_body(r#"{"status":"disconnected"}"#)
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::apps::disconnect(&client, "buzz_chat", None, None);

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_manual_connect_uses_prompted_credentials_and_connection_params() {
    let mut server = Server::new();
    let _integrations =
		authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
			.with_body(
				r#"[{
                "name":"widget_metrics",
                "displayName":"Widget Metrics",
                "description":"Metrics and logs",
                "connections":[{
                    "name":"app",
                    "authTypes":["manual"],
                    "connectionParams":{"region":{"description":"API region","default":"us-east","required":true}},
                    "credentialFields":[{"name":"api_key","label":"API key","description":"Use a personal API key"}]
                }]
            }]"#,
			)
			.create();
    let _connect = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/connect-manual",
        StatusCode::OK
    )
		.match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
		.match_body(Matcher::JsonString(
			r#"{"connection":"app","connectionParams":{"region":"eu-west"},"credential":"wm-key","integration":"widget_metrics"}"#.to_string(),
		))
		.with_body(r#"{"status":"connected","integration":"widget_metrics"}"#)
		.create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["app", "connect", "widget_metrics"])
        .write_stdin("eu-west\nwm-key\n")
        .assert()
        .success()
        .stderr(predicate::str::contains("API region"))
        .stderr(predicate::str::contains("API key"))
        .stderr(predicate::str::contains("Connected widget_metrics."));
}

#[test]
fn test_manual_connect_includes_service_account_id_flag() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
        .with_body(
            r#"[{
                "name":"widget_metrics",
                "displayName":"Widget Metrics",
                "connections":[{
                    "name":"app",
                    "authTypes":["manual"],
                    "credentialFields":[{"name":"api_key","label":"API key"}]
                }]
            }]"#,
        )
        .create();
    let _connect = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/connect-manual",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"app","credential":"wm-key","integration":"widget_metrics","serviceAccountId":"nightly-sync"}"#.to_string(),
    ))
    .with_body(r#"{"status":"connected","integration":"widget_metrics"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args([
            "app",
            "connect",
            "widget_metrics",
            "--service-account-id",
            "nightly-sync",
        ])
        .write_stdin("wm-key\n")
        .assert()
        .success()
        .stderr(predicate::str::contains("Connected widget_metrics."));
}

#[test]
fn test_manual_connect_prompts_for_connection_and_finishes_candidate_selection() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
            .with_body(
                r#"[{
                    "name":"manual-svc",
                    "displayName":"Manual Service",
                    "connections":[
                        {"name":"workspace","displayName":"Workspace OAuth","authTypes":["manual"],"credentialFields":[{"name":"token","label":"Workspace token"}]},
                        {"name":"app","displayName":"App OAuth","authTypes":["oauth"]}
                    ]
                }]"#,
            )
            .create();
    let _connect = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/connect-manual",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"workspace","credential":"abc123","integration":"manual-svc"}"#
            .to_string(),
    ))
    .with_body(
        r#"{
                "status":"selection_required",
                "integration":"manual-svc",
                "selectionUrl":"/api/v1/auth/pending-connection",
                "pendingToken":"pending-123",
                "candidates":[
                    {"id":"site-a","name":"Site A"},
                    {"id":"site-b","name":"Site B"}
                ]
            }"#,
    )
    .create();
    let _select = server
        .mock(Method::POST.as_str(), "/api/v1/auth/pending-connection")
        .match_header(
            header::AUTHORIZATION.as_str(),
            Matcher::Exact(test_bearer()),
        )
        .match_header(
            header::CONTENT_TYPE.as_str(),
            Matcher::Regex(format!("{}.*", http::APPLICATION_X_WWW_FORM_URLENCODED)),
        )
        .match_body(Matcher::Exact(
            "pending_token=pending-123&candidate_index=1".to_string(),
        ))
        .with_status(usize::from(StatusCode::OK.as_u16()))
        .with_header(header::CONTENT_TYPE.as_str(), http::TEXT_HTML)
        .with_body("<html>ok</html>")
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["app", "connect", "manual-svc"])
        .write_stdin("1\nabc123\n2\n")
        .assert()
        .success()
        .stderr(predicate::str::contains(
            "Select a Manual Service connection:",
        ))
        .stderr(predicate::str::contains("Workspace OAuth"))
        .stderr(predicate::str::contains("Connection: workspace"))
        .stderr(predicate::str::contains("Workspace token"))
        .stderr(predicate::str::contains(
            "Gestalt found more than one manual-svc connection. Choose one to save:",
        ))
        .stderr(predicate::str::contains("Connected manual-svc (Site B)"));
}

#[test]
fn test_connect_auto_selects_single_oauth_connection() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
        .with_body(
            r#"[{"name":"single-svc","connections":[{"name":"workspace","authTypes":["oauth"]}]}]"#,
        )
        .create();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/start-oauth",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"workspace","integration":"single-svc"}"#.to_string(),
    ))
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::apps::connect_with_browser_opener(
        &client,
        "single-svc",
        None,
        None,
        None,
        |_| Ok(()),
    );

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_connect_unknown_connection_lists_normalized_available_names() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
        .with_body(
            r#"[{
                    "name":"manual-svc",
                    "connections":[
                        {"name":"_app","displayName":"App OAuth","authTypes":["oauth"]},
                        {"name":"workspace","displayName":"Workspace OAuth","authTypes":["manual"]}
                    ]
                }]"#,
        )
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["app", "connect", "manual-svc", "--connection", "bogus"])
        .assert()
        .failure()
        .stderr(predicate::str::contains("unknown connection 'bogus'"))
        .stderr(predicate::str::contains(
            "available connections: app, workspace",
        ));
}

#[test]
fn test_manual_connect_uses_credentials_object_for_multi_field_auth() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
        .with_body(
            r#"[{
                "name":"widget_metrics",
                "displayName":"Widget Metrics",
                "connections":[{
                    "name":"app",
                    "authTypes":["manual"],
                    "credentialFields":[
                        {"name":"api_key","label":"API key"},
                        {"name":"workspace_id","label":"Workspace ID"}
                    ]
                }]
            }]"#,
        )
        .create();
    let _connect = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/connect-manual",
        StatusCode::OK
    )
		.match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
		.match_body(Matcher::JsonString(
			r#"{"connection":"app","credentials":{"api_key":"wm-key","workspace_id":"workspace-42"},"integration":"widget_metrics"}"#.to_string(),
		))
		.with_body(r#"{"status":"connected","integration":"widget_metrics"}"#)
		.create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["app", "connect", "widget_metrics"])
        .write_stdin("wm-key\nworkspace-42\n")
        .assert()
        .success()
        .stderr(predicate::str::contains("API key"))
        .stderr(predicate::str::contains("Workspace ID"))
        .stderr(predicate::str::contains("Connected widget_metrics."));
}

#[test]
fn test_manual_connect_falls_back_to_generic_credential_prompt() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
        .with_body(
            r#"[{"name":"manual-svc","connections":[{"name":"app","authTypes":["manual"]}]}]"#,
        )
        .create();
    let _connect = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/connect-manual",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"app","credential":"secret","integration":"manual-svc"}"#.to_string(),
    ))
    .with_body(r#"{"status":"connected","integration":"manual-svc"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["app", "connect", "manual-svc"])
        .write_stdin("secret\n")
        .assert()
        .success()
        .stderr(predicate::str::contains("\nCredential\n"))
        .stderr(predicate::str::contains("Connected manual-svc."));
}

#[test]
fn test_manual_connect_fails_when_stdin_closes_during_prompt() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
        .with_body(
            r#"[{"name":"manual-svc","connections":[{"name":"app","authTypes":["manual"]}]}]"#,
        )
        .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["app", "connect", "manual-svc"])
        .write_stdin("")
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "stdin closed while waiting for input",
        ));
}

#[test]
fn test_cli_apps_list_table_output() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();
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
    let mock = authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
		.with_body(
			r#"[
                {"name":"acme_crm","description":"Acme CRM app with a longer description","status":"ready","connections":[{"name":"workspace","status":"ready"}]},
                {"name":"legacy_svc","description":"Legacy service","status":"ready","connections":[{"name":"legacy","status":"ready","instances":[{"displayName":"Legacy"}]}]},
                {"name":"multi_svc","description":"Multi-instance service","status":"needs_instance_selection","connections":[{"name":"workspace","status":"needs_instance_selection","credentialState":"connected","connected":false,"instances":[{"name":"team-a"},{"name":"team-b"}]}]}
            ]"#,
		)
        .create();

    let mut cmd = cli_command(home.path());
    cmd.args(["app", "list"]);
    let output = cmd.output().unwrap();
    assert!(
        output.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(stdout.contains("acme_crm"), "stdout: {stdout}");
    assert!(stdout.contains("Acme CRM app"), "stdout: {stdout}");
    assert!(stdout.contains("Status"), "stdout: {stdout}");
    assert!(stdout.contains("Connection"), "stdout: {stdout}");
    assert!(stdout.contains("Instance"), "stdout: {stdout}");
    assert!(!stdout.contains("Connections"), "stdout: {stdout}");
    assert!(stdout.contains("ready"), "stdout: {stdout}");
    assert!(stdout.contains("workspace"), "stdout: {stdout}");
    assert!(stdout.contains("legacy_svc"), "stdout: {stdout}");
    assert!(stdout.contains("legacy"), "stdout: {stdout}");
    assert!(stdout.contains("team-a"), "stdout: {stdout}");
    assert!(stdout.contains("team-b"), "stdout: {stdout}");
    assert!(stdout.contains("choose account"), "stdout: {stdout}");
    assert!(stdout.contains("Multi-instance"), "stdout: {stdout}");
    assert_eq!(stdout.matches("multi_svc").count(), 1);

    mock.assert();

    let mock = authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
		.with_body(
			r#"[{"name":"acme_crm","description":"Acme CRM app with a longer description","status":"ready"}]"#,
		)
        .create();

    let mut cmd = cli_command(home.path());
    cmd.args(["app", "list"]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("acme_crm"));

    mock.assert();

    let mock = authed_json_mock!(server, Method::GET, "/api/v1/apps", StatusCode::OK)
		.with_body(
			r#"[{"name":"acme_crm","description":"Acme CRM app with a longer description","status":"ready"}]"#,
		)
        .create();

    let mut cmd = cli_command(home.path());
    cmd.args(["apps", "list"]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("acme_crm"));

    mock.assert();
}
