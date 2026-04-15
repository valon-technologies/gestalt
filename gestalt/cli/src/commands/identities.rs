use anyhow::{Context, Result};
use serde_json::json;

use crate::api::{ApiClient, TokenPermission};
use crate::cli::IdentityPermissionArg;
use crate::output::{self, Format};

pub fn list(client: &ApiClient, format: Format) -> Result<()> {
    let resp = client
        .get("/api/v1/identities")
        .context("failed to list identities")?;
    print_identities(&resp, format);
    Ok(())
}

pub fn get(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("/api/v1/identities/{id}"))
        .with_context(|| format!("failed to get identity {id}"))?;
    print_identity(&resp, format);
    Ok(())
}

pub fn create(client: &ApiClient, display_name: &str, format: Format) -> Result<()> {
    let resp = client
        .post(
            "/api/v1/identities",
            &json!({
                "displayName": display_name,
            }),
        )
        .context("failed to create identity")?;
    print_identity(&resp, format);
    Ok(())
}

pub fn update(client: &ApiClient, id: &str, display_name: &str, format: Format) -> Result<()> {
    let resp = client
        .patch(
            &format!("/api/v1/identities/{id}"),
            &json!({
                "displayName": display_name,
            }),
        )
        .with_context(|| format!("failed to update identity {id}"))?;
    print_identity(&resp, format);
    Ok(())
}

pub fn delete(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .delete(&format!("/api/v1/identities/{id}"))
        .with_context(|| format!("failed to delete identity {id}"))?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => output::print_success(&format!("Identity {id} deleted.")),
    }
    Ok(())
}

pub fn list_members(client: &ApiClient, identity: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("/api/v1/identities/{identity}/members"))
        .with_context(|| format!("failed to list members for identity {identity}"))?;
    print_members(&resp, format);
    Ok(())
}

pub fn upsert_member(
    client: &ApiClient,
    identity: &str,
    email: &str,
    role: &str,
    format: Format,
) -> Result<()> {
    let resp = client
        .put(
            &format!("/api/v1/identities/{identity}/members"),
            &json!({
                "email": email,
                "role": role,
            }),
        )
        .with_context(|| format!("failed to update member {email} on identity {identity}"))?;
    print_member(&resp, format);
    Ok(())
}

pub fn remove_member(
    client: &ApiClient,
    identity: &str,
    email: &str,
    format: Format,
) -> Result<()> {
    let encoded_email = encode_path_segment(email);
    let resp = client
        .delete(&format!(
            "/api/v1/identities/{identity}/members/{encoded_email}"
        ))
        .with_context(|| format!("failed to remove member {email} from identity {identity}"))?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            output::print_success(&format!("Removed member {email} from identity {identity}."))
        }
    }
    Ok(())
}

pub fn list_grants(client: &ApiClient, identity: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("/api/v1/identities/{identity}/grants"))
        .with_context(|| format!("failed to list grants for identity {identity}"))?;
    print_grants(&resp, format);
    Ok(())
}

pub fn set_grant(
    client: &ApiClient,
    identity: &str,
    plugin: &str,
    operations: &[String],
    format: Format,
) -> Result<()> {
    let resp = client
        .put(
            &format!("/api/v1/identities/{identity}/grants/{plugin}"),
            &json!({
                "operations": operations,
            }),
        )
        .with_context(|| {
            format!("failed to set grant for plugin {plugin} on identity {identity}")
        })?;
    print_grant(&resp, format);
    Ok(())
}

pub fn revoke_grant(
    client: &ApiClient,
    identity: &str,
    plugin: &str,
    format: Format,
) -> Result<()> {
    let resp = client
        .delete(&format!("/api/v1/identities/{identity}/grants/{plugin}"))
        .with_context(|| {
            format!("failed to revoke grant for plugin {plugin} on identity {identity}")
        })?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => output::print_success(&format!(
            "Revoked plugin {plugin} from identity {identity}."
        )),
    }
    Ok(())
}

pub fn list_tokens(client: &ApiClient, identity: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("/api/v1/identities/{identity}/tokens"))
        .with_context(|| format!("failed to list tokens for identity {identity}"))?;
    print_tokens(&resp, format);
    Ok(())
}

pub fn create_token(
    client: &ApiClient,
    identity: &str,
    name: Option<&str>,
    permissions: &[IdentityPermissionArg],
    format: Format,
) -> Result<()> {
    let token_name = name.unwrap_or("cli-token");
    let permissions: Vec<TokenPermission> = permissions
        .iter()
        .map(|permission| TokenPermission {
            plugin: permission.plugin.clone(),
            operations: permission.operations.clone(),
        })
        .collect();
    let resp = client
        .create_identity_api_token(identity, token_name, &permissions)
        .with_context(|| format!("failed to create token for identity {identity}"))?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            if let Some(token) = resp["token"].as_str() {
                output::print_success("Token created. Save it now; it won't be shown again.");
                println!("{token}");
            } else {
                output::print_json(&resp);
            }
        }
    }

    Ok(())
}

pub fn revoke_token(client: &ApiClient, identity: &str, id: &str, format: Format) -> Result<()> {
    let resp = client
        .revoke_identity_api_token(identity, id)
        .with_context(|| format!("failed to revoke token {id} for identity {identity}"))?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            output::print_success(&format!("Token {id} for identity {identity} revoked."))
        }
    }

    Ok(())
}

fn print_identity(value: &serde_json::Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let row = vec![vec![
                value["id"].as_str().unwrap_or("-").to_string(),
                value["displayName"].as_str().unwrap_or("-").to_string(),
                value["role"].as_str().unwrap_or("-").to_string(),
                value["createdAt"].as_str().unwrap_or("-").to_string(),
                value["updatedAt"].as_str().unwrap_or("-").to_string(),
            ]];
            output::print_table(&["ID", "Name", "Role", "Created", "Updated"], &row);
        }
    }
}

fn print_identities(value: &serde_json::Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().unwrap_or(&Vec::new()).clone();
            let rows: Vec<Vec<String>> = items
                .iter()
                .map(|item| {
                    vec![
                        item["id"].as_str().unwrap_or("-").to_string(),
                        item["displayName"].as_str().unwrap_or("-").to_string(),
                        item["role"].as_str().unwrap_or("-").to_string(),
                        item["createdAt"].as_str().unwrap_or("-").to_string(),
                        item["updatedAt"].as_str().unwrap_or("-").to_string(),
                    ]
                })
                .collect();
            output::print_table(&["ID", "Name", "Role", "Created", "Updated"], &rows);
        }
    }
}

fn print_member(value: &serde_json::Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let row = vec![vec![
                value["userId"].as_str().unwrap_or("-").to_string(),
                value["email"].as_str().unwrap_or("-").to_string(),
                value["role"].as_str().unwrap_or("-").to_string(),
                value["createdAt"].as_str().unwrap_or("-").to_string(),
                value["updatedAt"].as_str().unwrap_or("-").to_string(),
            ]];
            output::print_table(&["User ID", "Email", "Role", "Created", "Updated"], &row);
        }
    }
}

fn print_members(value: &serde_json::Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().unwrap_or(&Vec::new()).clone();
            let rows: Vec<Vec<String>> = items
                .iter()
                .map(|item| {
                    vec![
                        item["userId"].as_str().unwrap_or("-").to_string(),
                        item["email"].as_str().unwrap_or("-").to_string(),
                        item["role"].as_str().unwrap_or("-").to_string(),
                        item["createdAt"].as_str().unwrap_or("-").to_string(),
                        item["updatedAt"].as_str().unwrap_or("-").to_string(),
                    ]
                })
                .collect();
            output::print_table(&["User ID", "Email", "Role", "Created", "Updated"], &rows);
        }
    }
}

fn print_grant(value: &serde_json::Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let operations = render_operations_cell(value.get("operations"));
            let row = vec![vec![
                value["plugin"].as_str().unwrap_or("-").to_string(),
                operations,
                value["createdAt"].as_str().unwrap_or("-").to_string(),
                value["updatedAt"].as_str().unwrap_or("-").to_string(),
            ]];
            output::print_table(&["Plugin", "Operations", "Created", "Updated"], &row);
        }
    }
}

fn print_grants(value: &serde_json::Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().unwrap_or(&Vec::new()).clone();
            let rows: Vec<Vec<String>> = items
                .iter()
                .map(|item| {
                    let operations = render_operations_cell(item.get("operations"));
                    vec![
                        item["plugin"].as_str().unwrap_or("-").to_string(),
                        operations,
                        item["createdAt"].as_str().unwrap_or("-").to_string(),
                        item["updatedAt"].as_str().unwrap_or("-").to_string(),
                    ]
                })
                .collect();
            output::print_table(&["Plugin", "Operations", "Created", "Updated"], &rows);
        }
    }
}

fn render_operations_cell(operations: Option<&serde_json::Value>) -> String {
    match operations.and_then(|value| value.as_array()) {
        None => "all".to_string(),
        Some(ops) if ops.is_empty() => "all".to_string(),
        Some(ops) => {
            let joined = ops
                .iter()
                .filter_map(|op| op.as_str())
                .collect::<Vec<_>>()
                .join(",");
            if joined.is_empty() {
                "-".to_string()
            } else {
                joined
            }
        }
    }
}

fn print_tokens(value: &serde_json::Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().unwrap_or(&Vec::new()).clone();
            let rows: Vec<Vec<String>> = items
                .iter()
                .map(|item| {
                    vec![
                        item["id"].as_str().unwrap_or("-").to_string(),
                        item["name"].as_str().unwrap_or("-").to_string(),
                        format_permissions(item),
                        string_field(item, &["createdAt", "created_at"])
                            .unwrap_or("-")
                            .to_string(),
                        string_field(item, &["expiresAt", "expires_at"])
                            .unwrap_or("never")
                            .to_string(),
                    ]
                })
                .collect();
            output::print_table(&["ID", "Name", "Permissions", "Created", "Expires"], &rows);
        }
    }
}

fn string_field<'a>(value: &'a serde_json::Value, keys: &[&str]) -> Option<&'a str> {
    keys.iter().find_map(|key| value[*key].as_str())
}

fn format_permissions(value: &serde_json::Value) -> String {
    let Some(permissions) = value["permissions"].as_array() else {
        return "-".to_string();
    };
    if permissions.is_empty() {
        return "-".to_string();
    }
    permissions
        .iter()
        .filter_map(|permission| {
            let plugin = permission["plugin"].as_str()?;
            let operations: Vec<&str> = permission["operations"]
                .as_array()
                .map(|items| items.iter().filter_map(|item| item.as_str()).collect())
                .unwrap_or_default();
            if operations.is_empty() {
                Some(plugin.to_string())
            } else {
                Some(format!("{plugin}:{}", operations.join(",")))
            }
        })
        .collect::<Vec<_>>()
        .join("; ")
}
fn encode_path_segment(value: &str) -> String {
    let mut url = url::Url::parse("https://managed-identities.invalid/").expect("static URL");
    {
        let mut segments = url
            .path_segments_mut()
            .expect("static URL should support path segments");
        segments.pop_if_empty();
        segments.push(value);
    }
    url.path().trim_start_matches('/').to_string()
}
