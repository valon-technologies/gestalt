mod support;

use gestalt::output::Format;
use support::*;

const FAILING_REPORT: &str = r#"{
  "app": "linear",
  "ok": false,
  "surfaces": [{
    "upstream": "linear",
    "tools": ["create_issue", "list_issues"],
    "allowedOperationsConfigured": true,
    "exposed": [{"tool": "list_issues", "exposedAs": "issues.list"}],
    "unknownAllowedOperations": ["list_isssues"],
    "unexposedTools": ["create_issue"]
  }]
}"#;

#[test]
fn test_app_check_fails_on_unknown_allowed_operations() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/apps/linear/admin/check?_connection=workspace",
        StatusCode::OK
    )
    .with_body(FAILING_REPORT)
    .create();

    let client = create_client(&server);
    let err =
        gestalt::commands::check::check(&client, "linear", Some("workspace"), None, Format::Json)
            .unwrap_err();

    mock.assert();
    assert!(
        err.to_string()
            .starts_with("1 allowedOperations key matches no MCP tool"),
        "{err}"
    );
}

#[test]
fn test_app_check_passes_when_report_ok() {
    let mut server = Server::new();
    let mock = authed_json_mock!(
        server,
        Method::GET,
        "/api/v1/apps/linear/admin/check",
        StatusCode::OK
    )
    .with_body(
        r#"{"app":"linear","ok":true,"surfaces":[{"upstream":"linear","tools":["list_issues"],"allowedOperationsConfigured":false,"exposed":[{"tool":"list_issues","exposedAs":"list_issues"}],"unknownAllowedOperations":[],"unexposedTools":[]}]}"#,
    )
    .create();

    let client = create_client(&server);
    gestalt::commands::check::check(&client, "linear", None, None, Format::Table).unwrap();
    mock.assert();
}
