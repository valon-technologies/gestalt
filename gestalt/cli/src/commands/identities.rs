use anyhow::{Context, Result};
use serde::de::DeserializeOwned;

use crate::api::{self, ApiClient, TokenPermission};
use crate::cli::IdentityPermissionArg;
use crate::output::{self, Format};

pub fn list(client: &ApiClient, format: Format) -> Result<()> {
    let resp = client
        .list_identities()
        .context("failed to list identities")?;
    print_identities(&resp, format);
    Ok(())
}

pub fn get(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let raw = client
        .get(&format!("/api/v1/identities/{id}"))
        .with_context(|| format!("failed to get identity {id}"))?;
    if format == Format::Json {
        output::print_json(&raw);
        return Ok(());
    }
    match parse_raw::<api::IdentityRecord>(&raw) {
        Ok(resp) => print_identity(&resp, format),
        Err(_) => output::print_json_table(&raw),
    }
    Ok(())
}

pub fn create(client: &ApiClient, display_name: &str, format: Format) -> Result<()> {
    let resp = client
        .create_identity(&api::IdentityDisplayNameRequest {
            display_name: display_name.to_string(),
        })
        .context("failed to create identity")?;
    print_identity(&resp, format);
    Ok(())
}

pub fn update(client: &ApiClient, id: &str, display_name: &str, format: Format) -> Result<()> {
    let raw = client
        .patch(
            &format!("/api/v1/identities/{id}"),
            &api::IdentityDisplayNameRequest {
                display_name: display_name.to_string(),
            },
        )
        .with_context(|| format!("failed to update identity {id}"))?;
    if format == Format::Json {
        output::print_json(&raw);
        return Ok(());
    }
    match parse_raw::<api::IdentityRecord>(&raw) {
        Ok(resp) => print_identity(&resp, format),
        Err(_) => output::print_json_table(&raw),
    }
    Ok(())
}

pub fn delete(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let raw = client
        .delete(&format!("/api/v1/identities/{id}"))
        .with_context(|| format!("failed to delete identity {id}"))?;
    if format == Format::Json {
        output::print_json(&raw);
        return Ok(());
    }
    match parse_raw::<api::StatusResponse>(&raw) {
        Ok(resp) => print_status(&resp, format, &format!("Identity {id} deleted.")),
        Err(_) => output::print_json_table(&raw),
    }
    Ok(())
}

pub fn list_members(client: &ApiClient, identity: &str, format: Format) -> Result<()> {
    let resp = client
        .list_identity_members(identity)
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
        .put_identity_member(
            identity,
            &api::IdentityMemberRequest {
                email: email.to_string(),
                role: role.to_string(),
            },
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
    let resp = client
        .delete_identity_member(identity, email)
        .with_context(|| format!("failed to remove member {email} from identity {identity}"))?;
    print_status(
        &resp,
        format,
        &format!("Removed member {email} from identity {identity}."),
    );
    Ok(())
}

pub fn list_grants(client: &ApiClient, identity: &str, format: Format) -> Result<()> {
    let resp = client
        .list_identity_grants(identity)
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
        .put_identity_grant(
            identity,
            plugin,
            &api::IdentityGrantRequest {
                operations: operations.to_vec(),
            },
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
        .delete_identity_grant(identity, plugin)
        .with_context(|| {
            format!("failed to revoke grant for plugin {plugin} on identity {identity}")
        })?;
    print_status(
        &resp,
        format,
        &format!("Revoked plugin {plugin} from identity {identity}."),
    );
    Ok(())
}

pub fn list_tokens(client: &ApiClient, identity: &str, format: Format) -> Result<()> {
    let raw = client
        .get(&format!("/api/v1/identities/{identity}/tokens"))
        .with_context(|| format!("failed to list tokens for identity {identity}"))?;
    if format == Format::Json {
        output::print_json(&raw);
        return Ok(());
    }
    match parse_raw::<Vec<api::IdentityTokenRecord>>(&raw) {
        Ok(resp) => print_tokens(&resp, format),
        Err(_) => output::print_json_table(&raw),
    }
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
        .create_identity_api_token(
            identity,
            &api::IdentityTokenCreateRequest {
                name: token_name.to_string(),
                permissions,
            },
        )
        .with_context(|| format!("failed to create token for identity {identity}"))?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            if let Some(token) = resp.token.as_deref() {
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
    print_status(
        &resp,
        format,
        &format!("Token {id} for identity {identity} revoked."),
    );
    Ok(())
}

fn print_status(value: &api::StatusResponse, format: Format, table_message: &str) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => output::print_success(table_message),
    }
}

fn print_identity(value: &api::IdentityRecord, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let row = vec![vec![
                value.id.clone(),
                value.display_name.clone(),
                value.role.clone(),
                value.created_at.clone(),
                value.updated_at.clone(),
            ]];
            output::print_table(&["ID", "Name", "Role", "Created", "Updated"], &row);
        }
    }
}

fn print_identities(value: &[api::IdentityRecord], format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows: Vec<Vec<String>> = value
                .iter()
                .map(|item| {
                    vec![
                        item.id.clone(),
                        item.display_name.clone(),
                        item.role.clone(),
                        item.created_at.clone(),
                        item.updated_at.clone(),
                    ]
                })
                .collect();
            output::print_table(&["ID", "Name", "Role", "Created", "Updated"], &rows);
        }
    }
}

fn print_member(value: &api::IdentityMemberRecord, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let row = vec![vec![
                value.subject_id.clone(),
                value.email.clone(),
                value.role.clone(),
                value.created_at.clone(),
                value.updated_at.clone(),
            ]];
            output::print_table(&["Subject ID", "Email", "Role", "Created", "Updated"], &row);
        }
    }
}

fn print_members(value: &[api::IdentityMemberRecord], format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows: Vec<Vec<String>> = value
                .iter()
                .map(|item| {
                    vec![
                        item.subject_id.clone(),
                        item.email.clone(),
                        item.role.clone(),
                        item.created_at.clone(),
                        item.updated_at.clone(),
                    ]
                })
                .collect();
            output::print_table(
                &["Subject ID", "Email", "Role", "Created", "Updated"],
                &rows,
            );
        }
    }
}

fn print_grant(value: &api::IdentityGrantRecord, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let row = vec![vec![
                value.plugin.clone(),
                render_operations_cell(Some(value.operations.as_slice())),
                value.created_at.clone(),
                value.updated_at.clone(),
            ]];
            output::print_table(&["Plugin", "Operations", "Created", "Updated"], &row);
        }
    }
}

fn print_grants(value: &[api::IdentityGrantRecord], format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows: Vec<Vec<String>> = value
                .iter()
                .map(|item| {
                    vec![
                        item.plugin.clone(),
                        render_operations_cell(Some(item.operations.as_slice())),
                        item.created_at.clone(),
                        item.updated_at.clone(),
                    ]
                })
                .collect();
            output::print_table(&["Plugin", "Operations", "Created", "Updated"], &rows);
        }
    }
}

fn render_operations_cell(operations: Option<&[String]>) -> String {
    match operations {
        None => "all".to_string(),
        Some([]) => "all".to_string(),
        Some(ops) => {
            let joined = ops
                .iter()
                .map(String::as_str)
                .map(str::trim)
                .filter(|op| !op.is_empty())
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

fn print_tokens(value: &[api::IdentityTokenRecord], format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows: Vec<Vec<String>> = value
                .iter()
                .map(|item| {
                    vec![
                        item.id.clone(),
                        item.name.clone(),
                        format_permissions(Some(item.permissions.as_slice())),
                        item.created_at.clone(),
                        display_cell(item.expires_at.as_deref(), "never"),
                    ]
                })
                .collect();
            output::print_table(&["ID", "Name", "Permissions", "Created", "Expires"], &rows);
        }
    }
}

fn format_permissions(permissions: Option<&[TokenPermission]>) -> String {
    let Some(permissions) = permissions else {
        return "-".to_string();
    };
    if permissions.is_empty() {
        return "-".to_string();
    }

    let rendered = permissions
        .iter()
        .filter_map(|permission| {
            let plugin = permission.plugin.trim();
            if plugin.is_empty() {
                return None;
            }
            let operations = permission
                .operations
                .iter()
                .map(String::as_str)
                .map(str::trim)
                .filter(|operation| !operation.is_empty())
                .collect::<Vec<_>>();
            if operations.is_empty() {
                Some(plugin.to_string())
            } else {
                Some(format!("{plugin}:{}", operations.join(",")))
            }
        })
        .collect::<Vec<_>>();

    if rendered.is_empty() {
        "-".to_string()
    } else {
        rendered.join("; ")
    }
}

fn parse_raw<T>(value: &serde_json::Value) -> Result<T, serde_json::Error>
where
    T: DeserializeOwned,
{
    serde_json::from_value(value.clone())
}

fn display_cell(value: Option<&str>, fallback: &str) -> String {
    value.unwrap_or(fallback).to_string()
}
