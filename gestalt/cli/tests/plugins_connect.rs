mod support;

use std::sync::{Arc, Mutex};

use support::*;

#[test]
fn test_list_plugins() {
    let mut server = Server::new();
    let mock = authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
        .with_body(
            r#"[{"name":"acme_crm","displayName":"Acme CRM","description":"Acme CRM plugin"}]"#,
        )
        .create();

    let client = create_client(&server);
    let resp = client.get("/api/v1/integrations").unwrap();

    mock.assert();
    let items = resp.as_array().unwrap();
    assert_eq!(items.len(), 1);
    assert_eq!(items[0]["name"], "acme_crm");
}

#[test]
fn test_connect_includes_connection_and_instance() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(r#"[{"name":"acme_crm","authTypes":["oauth"],"connected":false}]"#)
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
    let result = gestalt::commands::plugins::connect_with_browser_opener(
        &client,
        "acme_crm",
        Some("workspace"),
        Some("team-a"),
        |_| Ok(()),
    );

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_connect_prefers_oauth_when_manual_also_exists_and_omits_null_connection_and_instance() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(r#"[{"name":"acme_crm","authTypes":["oauth","manual"],"connected":false}]"#)
            .create();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/start-oauth",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"integration":"acme_crm"}"#.to_string(),
    ))
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let client = create_client(&server);
    let opened_url = Arc::new(Mutex::new(None));
    let opened_url_handle = Arc::clone(&opened_url);
    let result = gestalt::commands::plugins::connect_with_browser_opener(
        &client,
        "acme_crm",
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
fn test_connect_uses_defined_plugin_connection_name_on_the_wire() {
    let mut server = Server::new();
    let _integrations = authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
        .with_body(
            r#"[{
                "name":"acme_crm",
                "authTypes":["oauth"],
                "connections":[{"name":"_plugin","displayName":"Plugin OAuth","authTypes":["oauth"]}]
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
        r#"{"connection":"_plugin","integration":"acme_crm"}"#.to_string(),
    ))
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::plugins::connect_with_browser_opener(
        &client,
        "acme_crm",
        Some("plugin"),
        None,
        |_| Ok(()),
    );

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_connect_preserves_requested_plugin_connection_when_no_definitions_exist() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(r#"[{"name":"acme_crm","authTypes":["oauth"],"connected":false}]"#)
            .create();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/start-oauth",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"plugin","integration":"acme_crm"}"#.to_string(),
    ))
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::plugins::connect_with_browser_opener(
        &client,
        "acme_crm",
        Some("plugin"),
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
        "/api/v1/integrations/widget_metrics?_connection=oauth&_instance=prod",
        StatusCode::OK
    )
    .with_body(r#"{"status":"disconnected"}"#)
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::plugins::disconnect(
        &client,
        "widget_metrics",
        Some("oauth"),
        Some("prod"),
    );

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_disconnect_normalizes_plugin_connection_name() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/integrations/acme_crm?_connection=_plugin",
        StatusCode::OK
    )
    .with_body(r#"{"status":"disconnected"}"#)
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::plugins::disconnect(&client, "acme_crm", Some("plugin"), None);

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_disconnect_without_optional_params() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::DELETE,
        "/api/v1/integrations/buzz_chat",
        StatusCode::OK
    )
    .with_body(r#"{"status":"disconnected"}"#)
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::plugins::disconnect(&client, "buzz_chat", None, None);

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_manual_connect_uses_prompted_credentials_and_connection_params() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(
                r#"[{
                "name":"widget_metrics",
                "displayName":"Widget Metrics",
                "description":"Metrics and logs",
                "authTypes":["manual"],
                "connectionParams":{"region":{"description":"API region","default":"us-east","required":true}},
                "credentialFields":[{"name":"api_key","label":"API key","description":"Use a personal API key"}]
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
        r#"{"connectionParams":{"region":"eu-west"},"credential":"wm-key","integration":"widget_metrics"}"#.to_string(),
    ))
    .with_body(r#"{"status":"connected","integration":"widget_metrics"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["plugin", "connect", "widget_metrics"])
        .write_stdin("eu-west\nwm-key\n")
        .assert()
        .success()
        .stderr(predicate::str::contains("API region"))
        .stderr(predicate::str::contains("API key"))
        .stderr(predicate::str::contains("Connected widget_metrics."));
}

#[test]
fn test_manual_connect_prompts_for_connection_and_finishes_candidate_selection() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(
                r#"[{
                    "name":"manual-svc",
                    "displayName":"Manual Service",
                    "authTypes":["manual"],
                    "connections":[
                        {"name":"workspace","displayName":"Workspace OAuth","credentialFields":[{"name":"token","label":"Workspace token"}]},
                        {"name":"plugin","displayName":"Plugin OAuth","authTypes":["oauth"]}
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
        .args(["plugin", "connect", "manual-svc"])
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
fn test_connection_selection_uses_selected_connection_auth_type() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(
                r#"[{
                    "name":"manual-svc",
                    "displayName":"Manual Service",
                    "authTypes":["manual"],
                    "connections":[
                        {"name":"workspace","displayName":"Workspace OAuth","authTypes":["oauth"]},
                        {"name":"apikey","displayName":"API Key","authTypes":["manual"],"credentialFields":[{"name":"token","label":"Token"}]}
                    ]
                }]"#,
            )
            .create();
    let _oauth = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/start-oauth",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"connection":"workspace","integration":"manual-svc"}"#.to_string(),
    ))
    .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["plugin", "connect", "manual-svc"])
        .write_stdin("1\n")
        .assert()
        .success()
        .stderr(predicate::str::contains(
            "Select a Manual Service connection:",
        ))
        .stderr(predicate::str::contains("Workspace OAuth"))
        .stderr(predicate::str::contains(
            "Opening browser to connect manual-svc...",
        ));
}

#[test]
fn test_connect_auto_selects_single_connection_and_uses_its_auth_type() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(
                r#"[{
                    "name":"single-svc",
                    "displayName":"Single Service",
                    "authTypes":["manual"],
                    "connections":[
                        {"name":"workspace","displayName":"Workspace OAuth","authTypes":["oauth"]}
                    ]
                }]"#,
            )
            .create();
    let _oauth = authed_json_mock!(
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

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["plugin", "connect", "single-svc"])
        .assert()
        .success()
        .stderr(predicate::str::contains("Select a Single Service connection:").not())
        .stderr(predicate::str::contains(
            "Opening browser to connect single-svc...",
        ));
}

#[test]
fn test_connect_unknown_connection_lists_normalized_available_names() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(
                r#"[{
                    "name":"manual-svc",
                    "connections":[
                        {"name":"_plugin","displayName":"Plugin OAuth","authTypes":["oauth"]},
                        {"name":"workspace","displayName":"Workspace OAuth","authTypes":["manual"]}
                    ]
                }]"#,
            )
            .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["plugin", "connect", "manual-svc", "--connection", "bogus"])
        .assert()
        .failure()
        .stderr(predicate::str::contains("unknown connection 'bogus'"))
        .stderr(predicate::str::contains(
            "available connections: plugin, workspace",
        ));
}

#[test]
fn test_manual_connect_uses_credentials_object_for_multi_field_auth() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(
                r#"[{
                "name":"widget_metrics",
                "displayName":"Widget Metrics",
                "authTypes":["manual"],
                "credentialFields":[
                    {"name":"api_key","label":"API key"},
                    {"name":"workspace_id","label":"Workspace ID"}
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
        r#"{"credentials":{"api_key":"wm-key","workspace_id":"workspace-42"},"integration":"widget_metrics"}"#.to_string(),
    ))
    .with_body(r#"{"status":"connected","integration":"widget_metrics"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["plugin", "connect", "widget_metrics"])
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
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(r#"[{"name":"manual-svc","authTypes":["manual"]}]"#)
            .create();
    let _connect = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/auth/connect-manual",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"credential":"secret","integration":"manual-svc"}"#.to_string(),
    ))
    .with_body(r#"{"status":"connected","integration":"manual-svc"}"#)
    .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["plugin", "connect", "manual-svc"])
        .write_stdin("secret\n")
        .assert()
        .success()
        .stderr(predicate::str::contains("\nCredential\n"))
        .stderr(predicate::str::contains("Connected manual-svc."));
}

#[test]
fn test_manual_connect_fails_when_stdin_closes_during_prompt() {
    let mut server = Server::new();
    let _integrations =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(r#"[{"name":"manual-svc","authTypes":["manual"]}]"#)
            .create();

    let home = tempfile::tempdir().unwrap();
    cli_command_for_server(home.path(), &server)
        .args(["plugin", "connect", "manual-svc"])
        .write_stdin("")
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "stdin closed while waiting for input",
        ));
}

#[test]
fn test_cli_plugins_list_table_output() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();
    write_credentials(
        home.path(),
        serde_json::json!({
            "api_url": server.url(),
            "api_token": TEST_TOKEN,
            "api_token_id": "tok-123",
        }),
    );
    let mock = authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
        .with_body(
            r#"[{"name":"acme_crm","description":"Acme CRM plugin with a longer description","connected":true}]"#,
        )
        .create();

    let mut cmd = cli_command(home.path());
    cmd.args(["plugin", "list"]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("ACME_CRM").or(predicate::str::contains("acme_crm")))
        .stdout(predicate::str::contains("Acme CRM plugin"))
        .stdout(
            predicate::str::contains("Connected")
                .or(predicate::str::contains("CONNECTED"))
                .or(predicate::str::contains("connected")),
        );

    mock.assert();

    let mock = authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
        .with_body(
            r#"[{"name":"acme_crm","description":"Acme CRM plugin with a longer description","connected":true}]"#,
        )
        .create();

    let mut cmd = cli_command(home.path());
    cmd.args(["integrations", "list"]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("acme_crm"));

    mock.assert();

    let mock = authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
        .with_body(
            r#"[{"name":"acme_crm","description":"Acme CRM plugin with a longer description","connected":true}]"#,
        )
        .create();

    let mut cmd = cli_command(home.path());
    cmd.args(["plugins", "list"]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("acme_crm"));

    mock.assert();
}
