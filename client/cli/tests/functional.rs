use std::path::Path;
use std::process::Command;

use mockito::{Matcher, Server};
use tempfile::TempDir;

fn cli(temp_home: &Path) -> Command {
    let mut cmd = Command::new(env!("CARGO_BIN_EXE_gestalt"));
    cmd.env("HOME", temp_home);
    cmd.env("XDG_CONFIG_HOME", temp_home);
    cmd
}

#[test]
fn config_commands_round_trip() {
    let home = TempDir::new().unwrap();

    let output = cli(home.path())
        .args(["config", "set", "url", "gestalt.example.com"])
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");

    let output = cli(home.path())
        .args(["--format", "json", "config", "get", "url"])
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");
    let json: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(json["url"], "https://gestalt.example.com");

    let output = cli(home.path())
        .args(["--format", "json", "config", "list"])
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");
    let json: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(json["url"], "https://gestalt.example.com");

    let output = cli(home.path())
        .args(["config", "unset", "url"])
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");

    let output = cli(home.path())
        .args(["--format", "json", "config", "get", "url"])
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");
    let json: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert!(json["url"].is_null());
}

#[test]
fn integrations_and_invoke_commands_use_the_cli_binary() {
    let home = TempDir::new().unwrap();
    let mut server = Server::new();

    let integrations = server
        .mock("GET", "/api/v1/integrations")
        .match_header("authorization", "Bearer test-token")
        .with_status(200)
        .with_header("content-type", "application/json")
        .with_body(
            r#"[{"name":"github","display_name":"GitHub","description":"GitHub integration","connected":true}]"#,
        )
        .create();

    let output = cli(home.path())
        .env("GESTALT_API_KEY", "test-token")
        .args(["--format", "json", "integrations", "list"])
        .args(["--url", &server.url()])
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");
    integrations.assert();
    let json: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(json[0]["name"], "github");

    let list_ops = server
        .mock("GET", "/api/v1/integrations/github/operations")
        .match_header("authorization", "Bearer test-token")
        .with_status(200)
        .with_header("content-type", "application/json")
        .with_body(
            r#"[{"Name":"search_code","Description":"Search code","Method":"POST","Parameters":[{"Name":"query","Type":"string","Required":true}]}]"#,
        )
        .create();

    let output = cli(home.path())
        .env("GESTALT_API_KEY", "test-token")
        .args(["--format", "json", "invoke", "github"])
        .args(["--url", &server.url()])
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");
    list_ops.assert();
    let json: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(json[0]["Name"], "search_code");

    let invoke = server
        .mock("POST", "/api/v1/github/search_code")
        .match_header("authorization", "Bearer test-token")
        .match_header("content-type", "application/json")
        .match_body(Matcher::JsonString(r#"{"query":"hello"}"#.to_string()))
        .with_status(200)
        .with_header("content-type", "application/json")
        .with_body(r#"{"results":[{"path":"src/main.go"}]}"#)
        .create();

    let output = cli(home.path())
        .env("GESTALT_API_KEY", "test-token")
        .args([
            "--format",
            "json",
            "invoke",
            "github",
            "search_code",
            "--param",
            "query=hello",
            "--url",
            &server.url(),
        ])
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");
    invoke.assert();
    let json: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(json["results"][0]["path"], "src/main.go");
}

#[test]
fn token_commands_cover_create_list_and_revoke() {
    let home = TempDir::new().unwrap();
    let mut server = Server::new();

    let create = server
        .mock("POST", "/api/v1/tokens")
        .match_header("authorization", "Bearer test-token")
        .match_header("content-type", "application/json")
        .match_body(Matcher::JsonString(r#"{"name":"cli-token"}"#.to_string()))
        .with_status(201)
        .with_header("content-type", "application/json")
        .with_body(r#"{"id":"tok-1","name":"cli-token","token":"plaintext-secret"}"#)
        .create();

    let output = cli(home.path())
        .env("GESTALT_API_KEY", "test-token")
        .args([
            "--format",
            "json",
            "tokens",
            "create",
            "--name",
            "cli-token",
        ])
        .args(["--url", &server.url()])
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");
    create.assert();
    let json: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(json["token"], "plaintext-secret");

    let list = server
        .mock("GET", "/api/v1/tokens")
        .match_header("authorization", "Bearer test-token")
        .with_status(200)
        .with_header("content-type", "application/json")
        .with_body(
            r#"[{"id":"tok-1","name":"cli-token","scopes":"","created_at":"2026-01-01T00:00:00Z"}]"#,
        )
        .create();

    let output = cli(home.path())
        .env("GESTALT_API_KEY", "test-token")
        .args(["--format", "json", "tokens", "list"])
        .args(["--url", &server.url()])
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");
    list.assert();
    let json: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(json[0]["id"], "tok-1");

    let revoke = server
        .mock("DELETE", "/api/v1/tokens/tok-1")
        .match_header("authorization", "Bearer test-token")
        .with_status(200)
        .with_header("content-type", "application/json")
        .with_body(r#"{"status":"revoked"}"#)
        .create();

    let output = cli(home.path())
        .env("GESTALT_API_KEY", "test-token")
        .args(["--format", "json", "tokens", "revoke", "tok-1"])
        .args(["--url", &server.url()])
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");
    revoke.assert();
    let json: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(json["status"], "revoked");
}
