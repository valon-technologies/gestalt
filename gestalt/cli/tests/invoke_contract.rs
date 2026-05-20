mod support;

use support::*;

#[test]
fn test_execute_operation() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/github/search_code",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .with_body(r#"{"results":[]}"#)
    .create();

    let client = create_client(&server);
    let body = serde_json::json!({"query": "hello"});
    let resp = client.post("/api/v1/github/search_code", &body).unwrap();

    mock.assert();
    assert_eq!(resp["results"], serde_json::json!([]));
}

#[test]
fn test_invoke_precondition_error_suggests_connect_command() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();

    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/auth_svc/operations",
        StatusCode::OK
    )
    .with_body(single_operation_catalog("list_items"))
    .create();

    let _invoke_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/auth_svc/list_items",
        StatusCode::PRECONDITION_FAILED
    )
    .with_body(r#"{"error":"no token stored for integration \"auth_svc\"; connect via OAuth first","code":"not_connected","integration":"auth_svc"}"#)
    .create();

    cli_command_for_server(home.path(), &server)
        .args(["plugin", "invoke", "auth_svc", "list_items"])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "failed to invoke auth_svc.list_items: plugin \"auth_svc\" is not connected. Connect it first with `gestalt plugin connect auth_svc`",
        ))
        .stderr(predicate::str::contains("OAuth first").not());
}

#[test]
fn test_invoke_reconnect_error_suggests_reconnect_command() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();

    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/clickhouse/operations",
        StatusCode::OK
    )
    .with_body(single_operation_catalog("run_query"))
    .create();

    let _invoke_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/clickhouse/run_query",
        StatusCode::PRECONDITION_FAILED
    )
    .with_body(r#"{"error":"OAuth token for integration \"clickhouse\" expired or was revoked; reconnect it","code":"reconnect_required","integration":"clickhouse"}"#)
    .create();

    cli_command_for_server(home.path(), &server)
        .args(["plugin", "invoke", "clickhouse", "run_query"])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "failed to invoke clickhouse.run_query: token for plugin \"clickhouse\" expired or was revoked. Reconnect it with `gestalt plugin connect clickhouse`",
        ))
        .stderr(predicate::str::contains("OAuth token for integration").not());
}

#[test]
fn test_invoke_instance_selection_error_suggests_instance_flag() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();

    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/slack/operations",
        StatusCode::OK
    )
    .with_body(single_operation_catalog("channels"))
    .create();

    let _invoke_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/slack/channels",
        StatusCode::CONFLICT
    )
    .with_body(r#"{"error":"ambiguous instance: integration \"slack\" has 2 connections ([team-a team-b]); specify which instance to use with the \"_instance\" parameter","code":"instance_selection_required","integration":"slack"}"#)
    .create();

    let _integrations_mock =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(
                r#"[{
            "name":"slack",
            "connections":[
                {"name":"workspace","credentialState":"connected","instances":[{"name":"default"}]},
                {"name":"--sandbox","credentialState":"connected","instances":[{"name":"-team"}]}
            ]
        }]"#,
            )
            .create();

    cli_command_for_server(home.path(), &server)
        .args(["plugin", "invoke", "slack", "channels"])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "plugin \"slack\" has multiple connected instances.",
        ))
        .stderr(predicate::str::contains(
            "Choose a connection and instance.",
        ))
        .stderr(predicate::str::contains("Connection"))
        .stderr(predicate::str::contains("Instance"))
        .stderr(predicate::str::contains("Example"))
        .stderr(predicate::str::contains("workspace"))
        .stderr(predicate::str::contains("default"))
        .stderr(predicate::str::contains("--sandbox"))
        .stderr(predicate::str::contains("-team"))
        .stderr(predicate::str::contains(
            "gestalt plugin invoke slack channels",
        ))
        .stderr(predicate::str::contains("'--sandbox'"))
        .stderr(predicate::str::contains("'-team'"))
        .stderr(predicate::str::contains("gestalt plugin connect").not());
}

#[test]
fn test_invoke_requires_selector_before_catalog_resolution_when_multiple_connections_exist() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();

    let integrations_mock =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(
                r#"[{
            "name":"multi_svc",
            "connections":[
                {"name":"dev","credentialState":"connected","instances":[{"name":"default"}]},
                {"name":"stage","credentialState":"connected","instances":[{"name":"default"}]}
            ]
        }]"#,
            )
            .create();

    cli_command_for_server(home.path(), &server)
        .args(["plugin", "invoke", "multi_svc", "healthcheck"])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "Choose a connection and instance.",
        ))
        .stderr(predicate::str::contains("Connection"))
        .stderr(predicate::str::contains("Instance"))
        .stderr(predicate::str::contains("Example"))
        .stderr(predicate::str::contains("dev"))
        .stderr(predicate::str::contains("stage"))
        .stderr(predicate::str::contains("default"))
        .stderr(predicate::str::contains(
            "gestalt plugin invoke multi_svc healthcheck",
        ))
        .stderr(predicate::str::contains("--connection dev"))
        .stderr(predicate::str::contains("--instance default"))
        .stderr(predicate::str::contains("no operation matching").not());

    integrations_mock.assert();
}

#[test]
fn test_list_operations_infers_only_connected_instance_before_catalog_resolution() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();

    let integrations_mock =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(
                r#"[{
            "name":"frontPorch",
            "connections":[
                {"name":"dev","credentialState":"missing","instances":[]},
                {"name":"prod","credentialState":"connected","status":"ready","instances":[{"connection":"prod","name":"default"}]},
                {"name":"stage","credentialState":"missing","instances":[]}
            ]
        }]"#,
            )
            .create();

    let catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/frontPorch/operations?_connection=prod&_instance=default",
        StatusCode::OK
    )
    .with_body(single_operation_catalog("healthcheck"))
    .create();

    cli_command_for_server(home.path(), &server)
        .args(["plugin", "invoke", "frontPorch"])
        .assert()
        .success()
        .stdout(predicate::str::contains("Connection: prod"))
        .stdout(predicate::str::contains("Instance:   default"))
        .stdout(predicate::str::contains("healthcheck"))
        .stderr(predicate::str::contains("plugin \"frontPorch\" is not connected").not());

    integrations_mock.assert();
    catalog_mock.assert();
}

#[test]
fn test_invoke_with_connection_infers_only_matching_instance() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();

    let integrations_mock =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(
                r#"[{
            "name":"multi_svc",
            "connections":[
                {"name":"dev","credentialState":"connected","instances":[{"name":"default"}]},
                {"name":"stage","credentialState":"connected","instances":[{"name":"default"}]}
            ]
        }]"#,
            )
            .create();

    let catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/multi_svc/operations?_connection=dev&_instance=default",
        StatusCode::OK
    )
    .with_body(single_operation_catalog("healthcheck"))
    .create();

    let invoke_mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/multi_svc/healthcheck",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"_connection":"dev","_instance":"default"}"#.to_string(),
    ))
    .with_body(r#"{"healthcheck":"OK"}"#)
    .create();

    cli_command_for_server(home.path(), &server)
        .args([
            "plugin",
            "invoke",
            "multi_svc",
            "healthcheck",
            "--connection",
            "dev",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("healthcheck"))
        .stdout(predicate::str::contains("OK"));

    integrations_mock.assert();
    catalog_mock.assert();
    invoke_mock.assert();
}

#[test]
fn test_invoke_with_unknown_connection_does_not_infer_unrelated_instance() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();

    let integrations_mock =
        authed_json_mock!(server, Method::GET, "/api/v1/integrations", StatusCode::OK)
            .with_body(
                r#"[{
            "name":"multi_svc",
            "connections":[
                {"name":"dev","credentialState":"connected","instances":[{"name":"default"}]}
            ]
        }]"#,
            )
            .create();

    let catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/multi_svc/operations?_connection=stage",
        StatusCode::OK
    )
    .with_body(single_operation_catalog("healthcheck"))
    .create();

    let invoke_mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/multi_svc/healthcheck",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"_connection":"stage"}"#.to_string(),
    ))
    .with_body(r#"{"healthcheck":"OK"}"#)
    .create();

    cli_command_for_server(home.path(), &server)
        .args([
            "plugin",
            "invoke",
            "multi_svc",
            "healthcheck",
            "--connection",
            "stage",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("OK"));

    integrations_mock.assert();
    catalog_mock.assert();
    invoke_mock.assert();
}

#[test]
fn test_invoke_error_with_instance_action_name_keeps_original_error() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();

    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/slack/operations",
        StatusCode::OK
    )
    .with_body(single_operation_catalog("status"))
    .create();

    let _invoke_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/slack/status",
        StatusCode::PRECONDITION_FAILED
    )
    .with_body(r#"{"error":"action select_instance is unavailable for this connection"}"#)
    .create();

    cli_command_for_server(home.path(), &server)
        .args(["plugin", "invoke", "slack", "status"])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "action select_instance is unavailable for this connection",
        ))
        .stderr(predicate::str::contains("multiple connected instances").not());
}

#[test]
fn test_catalog_reconnect_error_suggests_reconnect_command() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();

    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/clickhouse/operations",
        StatusCode::PRECONDITION_FAILED
    )
    .with_body(r#"{"error":"OAuth token for integration \"clickhouse\" expired or was revoked; reconnect it","code":"reconnect_required","integration":"clickhouse"}"#)
    .create();

    cli_command_for_server(home.path(), &server)
        .args(["plugin", "invoke", "clickhouse"])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "failed to invoke clickhouse: token for plugin \"clickhouse\" expired or was revoked. Reconnect it with `gestalt plugin connect clickhouse`",
        ))
        .stderr(predicate::str::contains("OAuth token for integration").not());
}

#[test]
fn test_list_operations_formats_parameters() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(
        r#"[{
                "id": "do_thing",
                "description": "Does a thing",
                "method": "POST",
                "parameters": [
                    {"name": "id", "type": "string", "location": "path", "required": true, "description": "The ID"},
                    {"name": "mode", "type": "string", "location": "query", "required": false, "description": "Mode"}
                ]
            },{
                "id": "workflowStateCreate",
                "description": "Creates a workflow state",
                "method": "POST",
                "parameters": [
                    {"name": "input", "type": "object{name!, position, teamId!}", "required": true}
                ]
            },{
                "id": "save_comment",
                "description": "Create or update a comment",
                "method": "POST",
                "parameters": [
                    {"name": "body", "type": "string", "required": true},
                    {"name": "issueId", "type": "string", "required": true}
                ]
            }]"#,
    )
    .create();

    let output = run_cli(&server, &["plugin", "invoke", "test_svc"]);
    mock.assert();
    assert!(
        output.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(stdout.contains("id=<string> [path]"), "stdout: {stdout}");
    assert!(stdout.contains("mode=<string> [query]"), "stdout: {stdout}");
    assert!(stdout.contains("workflowStateCreate"), "stdout: {stdout}");
    assert!(stdout.contains("object{name!,"), "stdout: {stdout}");
    assert!(stdout.contains("position, teamId!}>"), "stdout: {stdout}");
    assert!(stdout.contains("-p body=<string>"), "stdout: {stdout}");
    assert!(stdout.contains("issueId=<string>"), "stdout: {stdout}");
    assert!(
        stdout.matches("(required)").count() >= 3,
        "stdout: {stdout}"
    );
}

#[test]
fn test_list_operations_json_format() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(
        r#"[{
                "id": "do_thing",
                "description": "Does a thing",
                "method": "POST",
                "parameters": [
                    {"name": "id", "type": "string", "location": "path", "required": true, "description": "The ID"}
                ]
            }]"#,
    )
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::invoke::list_operations(&client, "test_svc", Format::Json);

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_list_operations_empty_parameters() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(
        r#"[{"id": "list_items", "description": "Lists items", "method": "GET", "parameters": []}]"#,
    )
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::invoke::list_operations(&client, "test_svc", Format::Table);

    mock.assert();
    assert!(result.is_ok());
}

#[test]
fn test_invoke_with_connection_and_instance() {
    let mut server = Server::new();

    let _catalog_mock = json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations?_connection=workspace&_instance=team-a",
        StatusCode::OK
    )
    .with_body(catalog_body())
    .create();

    let invoke_mock = json_mock!(
        server,
        Method::POST,
        "/api/v1/test_svc/do_thing",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"_connection":"workspace","_instance":"team-a","name":"test"}"#.to_string(),
    ))
    .with_body(r#"{"ok": true}"#)
    .create();

    let params = vec![gestalt::params::ParamEntry {
        key: "name".to_string(),
        value: gestalt::params::ParamValue::StringVal("test".to_string()),
    }];

    let client = create_client(&server);
    let result = gestalt::commands::invoke::invoke(
        &client,
        "test_svc",
        "do_thing",
        &params,
        gestalt::commands::invoke::InvokeOptions {
            connection: Some("workspace"),
            instance: Some("team-a"),
            ..Default::default()
        },
        Format::Json,
    );

    invoke_mock.assert();
    assert!(result.is_ok());

    let _secondary_catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/other_svc/operations?_connection=workspace&_instance=team-a",
        StatusCode::OK
    )
    .with_body(single_operation_catalog("check_status"))
    .create();

    let secondary_invoke_mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/other_svc/check_status",
        StatusCode::PRECONDITION_FAILED
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"_connection":"workspace","_instance":"team-a"}"#.to_string(),
    ))
    .with_body(r#"{"error":"no token stored for integration \"other_svc\" instance \"team-a\""}"#)
    .create();

    let err = format!(
        "{:#}",
        gestalt::commands::invoke::invoke(
            &client,
            "other_svc",
            "check_status",
            &[],
            gestalt::commands::invoke::InvokeOptions {
                connection: Some("workspace"),
                instance: Some("team-a"),
                ..Default::default()
            },
            Format::Json,
        )
        .unwrap_err()
    );

    secondary_invoke_mock.assert();
    assert!(err.contains(
        "Connect it first with `gestalt plugin connect other_svc --connection workspace --instance team-a`"
    ));
}

#[test]
fn test_list_operations_uses_unselected_catalog_when_selected_catalog_is_empty() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();

    let selected_catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/sample_svc/operations?_connection=dev",
        StatusCode::OK
    )
    .with_body(r#"[]"#)
    .create();
    let fallback_catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/sample_svc/operations",
        StatusCode::OK
    )
    .with_body(single_operation_catalog("healthcheck"))
    .create();

    cli_command_for_server(home.path(), &server)
        .args(["plugin", "invoke", "sample_svc", "--connection", "dev"])
        .assert()
        .success()
        .stdout(predicate::str::contains("Connection: dev"))
        .stdout(predicate::str::contains("healthcheck"))
        .stdout(predicate::str::contains("No results.").not());

    selected_catalog_mock.assert();
    fallback_catalog_mock.assert();
}

#[test]
fn test_invoke_retries_without_catalog_when_preflight_masks_surface_error() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();

    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/sample_svc/operations?_connection=session-conn&_instance=default",
        StatusCode::PRECONDITION_FAILED
    )
    .with_body(r#"{"error":"no token stored for integration \"sample_svc\"; connect via OAuth first","code":"not_connected","integration":"sample_svc"}"#)
    .create();

    let _invoke_mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/sample_svc/api_get_resource",
        StatusCode::BAD_REQUEST
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"_connection":"session-conn","_instance":"default"}"#.to_string(),
    ))
    .with_body(
        r#"{"error":"operation \"api_get_resource\" on integration \"sample_svc\" uses connection \"api-conn\"; omit the connection override or use that connection instead of \"session-conn\""}"#,
    )
    .create();

    cli_command_for_server(home.path(), &server)
        .args([
            "plugin",
            "invoke",
            "--connection",
            "session-conn",
            "--instance",
            "default",
            "sample_svc",
            "api_get_resource",
        ])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "operation \"api_get_resource\" on integration \"sample_svc\" uses connection \"api-conn\"",
        ))
        .stderr(predicate::str::contains("plugin \"sample_svc\" is not connected").not());
}

#[test]
fn test_cli_lists_operations_with_connection_and_instance() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();

    let catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations?_connection=workspace&_instance=team-a",
        StatusCode::OK
    )
    .with_body(single_operation_catalog("do_thing"))
    .create();

    let mut cmd = cli_command(home.path());
    cmd.env("GESTALT_API_KEY", TEST_TOKEN).args([
        "--url",
        &server.url(),
        "plugin",
        "invoke",
        "--connection",
        "workspace",
        "--instance",
        "team-a",
        "test_svc",
    ]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("Connection: workspace"))
        .stdout(predicate::str::contains("Instance:   team-a"))
        .stdout(predicate::str::contains("Selector:").not())
        .stdout(predicate::str::contains("do_thing"));

    catalog_mock.assert();
}

#[test]
fn test_describe_operation() {
    let mut server = Server::new();

    let mock = json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(catalog_body())
    .create();

    let client = create_client(&server);
    let result = gestalt::commands::describe::describe(
        &client,
        "test_svc",
        "do_thing",
        None,
        None,
        Format::Table,
    );

    mock.assert();
    assert!(result.is_ok());

    let mock = json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(catalog_body())
    .create();

    let output = run_cli(&server, &["plugin", "describe", "test_svc", "do_thing"]);
    assert!(
        output.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    mock.assert();

    let mock = json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(catalog_body())
    .create();

    let output = run_cli(&server, &["describe", "test_svc", "do_thing"]);
    assert!(
        output.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    mock.assert();
}

#[test]
fn test_cli_describe_operation_with_connection_and_instance() {
    let mut server = Server::new();

    let mock = json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations?_connection=workspace&_instance=team-a",
        StatusCode::OK
    )
    .with_body(catalog_body())
    .create();

    let output = run_cli(
        &server,
        &[
            "plugin",
            "describe",
            "test_svc",
            "do_thing",
            "--connection",
            "workspace",
            "--instance",
            "team-a",
        ],
    );
    assert!(
        output.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(
        stdout.contains("Connection:  workspace"),
        "stdout: {stdout}"
    );
    assert!(stdout.contains("Instance:    team-a"), "stdout: {stdout}");
    assert!(!stdout.contains("Selector:"), "stdout: {stdout}");
    assert!(stdout.contains("Transport:   rest"), "stdout: {stdout}");
    mock.assert();
}

#[test]
fn test_cli_describe_fallback_error_lists_unselected_catalog_operations() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();

    let selected_catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/sample_svc/operations?_connection=dev",
        StatusCode::OK
    )
    .with_body(r#"[]"#)
    .create();

    let fallback_catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/sample_svc/operations",
        StatusCode::OK
    )
    .with_body(single_operation_catalog("healthcheck"))
    .create();

    cli_command_for_server(home.path(), &server)
        .args([
            "plugin",
            "describe",
            "sample_svc",
            "missing",
            "--connection",
            "dev",
        ])
        .assert()
        .failure()
        .stderr(predicate::str::contains(
            "operation 'missing' not found; available operations: healthcheck",
        ));

    selected_catalog_mock.assert();
    fallback_catalog_mock.assert();
}

#[test]
fn test_cli_invoke_merges_file_params_and_selects_output() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();
    let input_file = home.path().join("input.json");
    std::fs::write(&input_file, r#"{"count":1,"name":"from-file"}"#).unwrap();

    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(catalog_body())
    .create();

    let invoke_mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/test_svc/do_thing",
        StatusCode::OK
    )
    .match_header(header::CONTENT_TYPE.as_str(), http::APPLICATION_JSON)
    .match_body(Matcher::JsonString(
        r#"{"count":42,"name":"override","tags":["one","two"]}"#.to_string(),
    ))
    .with_body(r#"{"result":{"id":"1"}}"#)
    .create();

    let mut cmd = cli_command(home.path());
    cmd.env("GESTALT_API_KEY", TEST_TOKEN).args([
        "--url",
        &server.url(),
        "--format",
        "json",
        "plugin",
        "invoke",
        "test_svc",
        "do_thing",
        "-p",
        "name=override",
        "-p",
        "count:=42",
        "-p",
        "tags=one",
        "-p",
        "tags=two",
        "--input-file",
        input_file.to_str().unwrap(),
        "--select",
        "result.id",
    ]);
    cmd.assert().success().stdout("\"1\"\n");

    invoke_mock.assert();
}

#[test]
fn test_cli_invoke_table_keeps_nested_json_inline() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();
    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(catalog_body())
    .create();

    let invoke_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/test_svc/do_thing",
        StatusCode::OK
    )
    .with_body(
        r#"{
                "id":"abc123",
                "status":"ok",
                "user":{"name":"Amy","email":"amy@example.com"},
                "labels":["prod","urgent"],
                "jobs":[{"id":"j1","state":"done"},{"id":"j2","state":"running"}]
            }"#,
    )
    .create();

    let mut cmd = cli_command(home.path());
    cmd.env("GESTALT_API_KEY", TEST_TOKEN).args([
        "--url",
        &server.url(),
        "plugin",
        "invoke",
        "test_svc",
        "do_thing",
    ]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("user"))
        .stdout(predicate::str::contains(
            r#"{"email":"amy@example.com","name":"Amy"}"#,
        ))
        .stdout(predicate::str::contains("labels"))
        .stdout(predicate::str::contains(r#"["prod","urgent"]"#))
        .stdout(predicate::str::contains("jobs"))
        .stdout(predicate::str::contains(
            r#"[{"id":"j1","state":"done"},{"id":"j2","state":"running"}]"#,
        ));

    invoke_mock.assert();
}

#[test]
fn test_cli_invoke_rejects_duplicate_scalar_params() {
    let mut server = Server::new();
    let home = TempDir::new().unwrap();
    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(catalog_body())
    .create();

    let mut cmd = cli_command(home.path());
    cmd.env("GESTALT_API_KEY", TEST_TOKEN).args([
        "--url",
        &server.url(),
        "plugin",
        "invoke",
        "test_svc",
        "do_thing",
        "-p",
        "name=first",
        "-p",
        "name=second",
    ]);
    cmd.assert().failure().stderr(predicate::str::contains(
        "parameter 'name' is not an array type but was specified multiple times",
    ));
}

#[test]
fn test_prefix_match_shows_filtered_table() {
    let mut server = Server::new();
    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(multi_operation_catalog())
    .create();

    let output = run_cli(&server, &["plugin", "invoke", "test_svc", "widgets"]);
    assert!(
        output.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(stdout.contains("widgets.create"), "stdout: {stdout}");
    assert!(stdout.contains("widgets.delete"), "stdout: {stdout}");
    assert!(stdout.contains("widgets.list"), "stdout: {stdout}");
    assert!(
        !stdout.contains("gadgets.fetch"),
        "should not contain non-matching ops: {stdout}"
    );
    assert!(
        !stdout.contains("status.check"),
        "should not contain non-matching ops: {stdout}"
    );

    let output = run_cli(
        &server,
        &["plugin", "invoke", "test_svc", "widgets", "bulk"],
    );
    assert!(
        output.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(stdout.contains("widgets.bulk.create"), "stdout: {stdout}");
    assert!(stdout.contains("widgets.bulk.delete"), "stdout: {stdout}");
    assert!(
        !stdout.contains("widgets.create"),
        "should not contain shallower ops: {stdout}"
    );
    assert!(
        !stdout.contains("widgets.list"),
        "should not contain shallower ops: {stdout}"
    );
}

#[test]
fn test_space_separated_segments_invoke() {
    let mut server = Server::new();
    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(multi_operation_catalog())
    .create();

    let invoke_mock = authed_json_mock!(
        server,
        Method::POST,
        "/api/v1/test_svc/widgets.create",
        StatusCode::OK
    )
    .with_body(r#"{"ok": true}"#)
    .create();

    let output = run_cli(
        &server,
        &[
            "invoke",
            "test_svc",
            "widgets",
            "create",
            "-p",
            "name=foo",
            "-p",
            "color=red",
        ],
    );
    assert!(
        output.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    invoke_mock.assert();

    let deep_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/test_svc/widgets.bulk.create",
        StatusCode::OK
    )
    .with_body(r#"{"ok": true}"#)
    .create();

    let output = run_cli(
        &server,
        &["plugin", "invoke", "test_svc", "widgets", "bulk", "create"],
    );
    assert!(
        output.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    deep_mock.assert();

    let subcommand_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/test_svc/widgets.delete",
        StatusCode::OK
    )
    .with_body(r#"{"ok": true}"#)
    .create();

    let output = run_cli(
        &server,
        &["plugin", "invoke", "test_svc", "widgets", "delete"],
    );
    assert!(
        output.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    subcommand_mock.assert();
}

#[test]
fn test_no_match_returns_error() {
    let mut server = Server::new();
    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(multi_operation_catalog())
    .create();

    let output = run_cli(&server, &["plugin", "invoke", "test_svc", "nonexistent"]);
    assert!(!output.status.success());
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(stderr.contains("no operation matching"), "stderr: {stderr}");
}

#[test]
fn test_prefix_match_with_params_warns() {
    let mut server = Server::new();
    let _catalog_mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/integrations/test_svc/operations",
        StatusCode::OK
    )
    .with_body(multi_operation_catalog())
    .create();

    let output = run_cli(
        &server,
        &["plugin", "invoke", "test_svc", "widgets", "-p", "name=foo"],
    );
    assert!(
        output.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(stderr.contains("parameters ignored"), "stderr: {stderr}");
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(
        stdout.contains("widgets.create"),
        "should still show filtered table: {stdout}"
    );
}
