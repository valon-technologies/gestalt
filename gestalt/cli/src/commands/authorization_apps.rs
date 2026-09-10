use anyhow::{Context, Result, bail};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::HashMap;

use crate::api::{ApiClient, encode_path_segment};
use crate::cli::{
    AuthorizationAppsAllowedOperationsCommands, AuthorizationAppsAllowedOperationsListArgs,
    AuthorizationAppsAllowedOperationsSetArgs, AuthorizationAppsCommands,
    AuthorizationAppsMembersCommands, AuthorizationAppsMembersListArgs,
    AuthorizationAppsMembersRemoveArgs, AuthorizationAppsMembersSetArgs,
};
use crate::output::{self, Format};

use gestalt_sdk::public::generated::app_client::AuthorizationClient;
use gestalt_sdk::public::rest_transport::SyncRestTransport;

use crate::commands::authorization_app_member_groups::{remove_group_member, set_group_member};
use crate::commands::authorization_app_member_people::{
    resolve_canonical_member_subject_id, trimmed_option,
};

pub fn dispatch(
    api: &ApiClient,
    authz: &AuthorizationClient<SyncRestTransport>,
    command: AuthorizationAppsCommands,
    format: Format,
) -> Result<()> {
    match command {
        AuthorizationAppsCommands::List => list_apps(api, format),
        AuthorizationAppsCommands::Members { command } => match command {
            AuthorizationAppsMembersCommands::List(args) => list_members(api, &args, format),
            AuthorizationAppsMembersCommands::Set(args) => set_member(api, authz, &args, format),
            AuthorizationAppsMembersCommands::Remove(args) => {
                remove_member(api, authz, &args, format)
            }
        },
        AuthorizationAppsCommands::AllowedOperations { command } => match command {
            AuthorizationAppsAllowedOperationsCommands::List(args) => {
                list_allowed_operations(api, &args, format)
            }
            AuthorizationAppsAllowedOperationsCommands::Set(args) => {
                set_allowed_operations(api, &args, format)
            }
        },
    }
}

fn list_apps(api: &ApiClient, format: Format) -> Result<()> {
    let resp = api.get("/api/v1/apps").context("failed to list apps")?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            let rows: Vec<Vec<String>> = resp
                .as_array()
                .unwrap_or(&Vec::new())
                .iter()
                .map(|item| {
                    vec![
                        item.get("name")
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        item.get("displayName")
                            .or_else(|| item.get("display_name"))
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                    ]
                })
                .collect();
            println!("{}", output::render_table(&["Name", "Display Name"], &rows));
        }
    }
    Ok(())
}

fn list_members(
    api: &ApiClient,
    args: &AuthorizationAppsMembersListArgs,
    format: Format,
) -> Result<()> {
    let app = require_app_name(&args.app)?;
    let path = format!("/api/v1/apps/{}/admin/members", encode_path_segment(&app));
    let resp = api
        .get(&path)
        .with_context(|| format!("failed to list members for app {app}"))?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            let rows: Vec<Vec<String>> = resp
                .as_array()
                .unwrap_or(&Vec::new())
                .iter()
                .map(member_row)
                .collect();
            println!(
                "{}",
                output::render_table(&["Role", "Source", "Mutable", "Subject", "Email"], &rows)
            );
        }
    }
    Ok(())
}

fn set_member(
    api: &ApiClient,
    authz: &AuthorizationClient<SyncRestTransport>,
    args: &AuthorizationAppsMembersSetArgs,
    format: Format,
) -> Result<()> {
    let app = require_app_name(&args.app)?;
    let role = require_role(&args.role)?;
    if let Some(group_id) = trimmed_option(args.group_id.as_deref()) {
        return set_group_member(authz, &app, group_id, &role, format);
    }
    let subject_id = resolve_canonical_member_subject_id(
        api,
        &app_admin_members_path(&app),
        args.email.as_deref(),
        args.subject_id.as_deref(),
    )?;

    let resp = api
        .post(
            &app_admin_members_path(&app),
            &AppAdminMemberSetRequest {
                subject_id: subject_id.clone(),
                role: role.clone(),
            },
        )
        .with_context(|| format!("failed to grant app member access for {subject_id} on {app}"))?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            if resp.get("changed").and_then(Value::as_bool) == Some(false) {
                output::print_success(&format!("{subject_id} already has {role} on {app}."))
            } else {
                output::print_success(&format!("Granted {role} on {app} to {subject_id}."))
            }
        }
    }
    Ok(())
}

fn remove_member(
    api: &ApiClient,
    authz: &AuthorizationClient<SyncRestTransport>,
    args: &AuthorizationAppsMembersRemoveArgs,
    format: Format,
) -> Result<()> {
    let app = require_app_name(&args.app)?;
    if let Some(group_id) = trimmed_option(args.group_id.as_deref()) {
        let role = trimmed_option(args.role.as_deref())
            .map(require_role)
            .transpose()?;
        return remove_group_member(
            api,
            authz,
            &app,
            group_id,
            role.as_deref(),
            format,
        );
    }
    let subject = resolve_canonical_member_subject_id(
        api,
        &app_admin_members_path(&app),
        args.email.as_deref(),
        args.subject_id.as_deref().or(args.subject.as_deref()),
    )?;
    let role = trimmed_option(args.role.as_deref())
        .map(require_role)
        .transpose()?;
    let resp = api
        .delete_json(
            &app_admin_members_path(&app),
            &AppAdminMemberRemoveRequest {
                subject_id: subject.clone(),
                role,
            },
        )
        .with_context(|| format!("failed to remove app member access for {subject} on {app}"))?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => output::print_success(&format!(
            "Removed {} grant(s) for {subject} on {app}.",
            resp.get("removedRoles")
                .and_then(Value::as_array)
                .map(Vec::len)
                .unwrap_or_default()
        )),
    }
    Ok(())
}

fn list_allowed_operations(
    api: &ApiClient,
    args: &AuthorizationAppsAllowedOperationsListArgs,
    format: Format,
) -> Result<()> {
    let app = require_app_name(&args.app)?;
    let resp = api
        .get(&app_admin_allowed_operations_path(&app))
        .with_context(|| format!("failed to list allowed operations for app {app}"))?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            let rows: Vec<Vec<String>> = resp
                .get("operations")
                .and_then(Value::as_array)
                .unwrap_or(&Vec::new())
                .iter()
                .map(allowed_operation_row)
                .collect();
            println!(
                "{}",
                output::render_table(&["ID", "Source", "Allowed Roles"], &rows)
            );
        }
    }
    Ok(())
}

fn set_allowed_operations(
    api: &ApiClient,
    args: &AuthorizationAppsAllowedOperationsSetArgs,
    format: Format,
) -> Result<()> {
    let app = require_app_name(&args.app)?;
    let body = build_allowed_operations_update_request(args)?;
    let resp = api
        .put(&app_admin_allowed_operations_path(&app), &body)
        .with_context(|| format!("failed to update allowed operations for app {app}"))?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => output::print_success(&format!("Updated allowed operations for {app}.")),
    }
    Ok(())
}

fn build_allowed_operations_update_request(
    args: &AuthorizationAppsAllowedOperationsSetArgs,
) -> Result<AllowedOperationsUpdateRequest> {
    if let Some(path) = args
        .input_file
        .as_deref()
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        let raw = std::fs::read_to_string(path)
            .with_context(|| format!("failed to read allowed operations file {path}"))?;
        return serde_json::from_str(&raw)
            .with_context(|| format!("failed to parse allowed operations file {path}"));
    }

    if args.set.is_empty() && args.remove.is_empty() {
        bail!("pass --set id=viewer,editor and/or --remove id, or use --input-file");
    }

    let mut operations = HashMap::new();
    for entry in &args.set {
        let (operation_id, roles) = parse_operation_roles_assignment(entry)?;
        operations.insert(
            operation_id,
            OperationOverrideBody {
                allowed_roles: roles,
            },
        );
    }

    let mut removed = Vec::with_capacity(args.remove.len());
    for id in &args.remove {
        let trimmed = id.trim();
        if trimmed.is_empty() {
            bail!("operation id is required");
        }
        removed.push(trimmed.to_string());
    }

    Ok(AllowedOperationsUpdateRequest {
        operations,
        removed,
    })
}

fn app_admin_allowed_operations_path(app: &str) -> String {
    format!(
        "/api/v1/apps/{}/admin/allowed-operations",
        encode_path_segment(app)
    )
}

fn app_admin_members_path(app: &str) -> String {
    format!("/api/v1/apps/{}/admin/members", encode_path_segment(app))
}

fn parse_operation_roles_assignment(raw: &str) -> Result<(String, Vec<String>)> {
    let (operation_id, roles_raw) = raw
        .split_once('=')
        .with_context(|| format!("expected operation override as id=viewer,editor, got {raw:?}"))?;
    let operation_id = operation_id.trim();
    if operation_id.is_empty() {
        bail!("operation id is required");
    }
    let roles: Vec<String> = roles_raw
        .split(',')
        .map(str::trim)
        .filter(|role| !role.is_empty())
        .map(str::to_string)
        .collect();
    if roles.is_empty() {
        bail!("allowed roles are required for operation {operation_id}");
    }
    Ok((operation_id.to_string(), roles))
}

fn allowed_operation_row(value: &Value) -> Vec<String> {
    let roles = value
        .get("allowedRoles")
        .and_then(Value::as_array)
        .map(|items| {
            items
                .iter()
                .filter_map(Value::as_str)
                .collect::<Vec<_>>()
                .join(", ")
        })
        .unwrap_or_default();
    vec![
        value
            .get("id")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string(),
        value
            .get("source")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string(),
        roles,
    ]
}

fn member_row(value: &Value) -> Vec<String> {
    vec![
        value
            .get("role")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string(),
        value
            .get("source")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string(),
        value
            .get("mutable")
            .and_then(Value::as_bool)
            .map(|mutable| mutable.to_string())
            .unwrap_or_default(),
        member_subject_label(value),
        value
            .get("email")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string(),
    ]
}

fn member_subject_label(value: &Value) -> String {
    let selector = value
        .get("subjectId")
        .or_else(|| value.get("selectorValue"))
        .and_then(Value::as_str)
        .unwrap_or("");
    let display_name = value
        .get("subjectSet")
        .and_then(|subject_set| subject_set.get("resource"))
        .and_then(|resource| resource.get("displayName"))
        .and_then(Value::as_str)
        .map(str::trim)
        .filter(|name| !name.is_empty());
    match display_name {
        Some(name) if !selector.is_empty() => format!("{name} ({selector})"),
        Some(name) => name.to_string(),
        None => selector.to_string(),
    }
}

fn require_app_name(app: &str) -> Result<String> {
    let trimmed = app.trim();
    if trimmed.is_empty() {
        bail!("app name is required");
    }
    Ok(trimmed.to_string())
}

fn require_role(role: &str) -> Result<String> {
    let trimmed = role.trim();
    if trimmed.is_empty() {
        bail!("role is required");
    }
    if !matches!(trimmed, "admin" | "viewer" | "editor") {
        bail!("role must be admin, viewer, or editor");
    }
    Ok(trimmed.to_string())
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AppAdminMemberSetRequest {
    subject_id: String,
    role: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AppAdminMemberRemoveRequest {
    subject_id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    role: Option<String>,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct AllowedOperationsUpdateRequest {
    operations: HashMap<String, OperationOverrideBody>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    removed: Vec<String>,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct OperationOverrideBody {
    allowed_roles: Vec<String>,
}

#[cfg(test)]
mod tests {
    use super::{
        AuthorizationAppsAllowedOperationsSetArgs, member_subject_label,
        parse_operation_roles_assignment,
    };

    #[test]
    fn member_subject_label_prefers_subject_set_display_name_and_keeps_selector() {
        let row = serde_json::json!({
            "selectorValue": "group:eng#member",
            "subjectSet": {"resource": {"displayName": "Engineering"}}
        });
        assert_eq!(member_subject_label(&row), "Engineering (group:eng#member)");
    }

    #[test]
    fn parse_operation_roles_assignment_splits_roles() {
        assert_eq!(
            parse_operation_roles_assignment("get_item=viewer,editor").unwrap(),
            (
                "get_item".to_string(),
                vec!["viewer".to_string(), "editor".to_string()]
            )
        );
    }

    #[test]
    fn parse_operation_roles_assignment_rejects_missing_roles() {
        assert!(parse_operation_roles_assignment("get_item=").is_err());
    }

    #[test]
    fn build_allowed_operations_update_request_supports_remove_only() {
        let body = super::build_allowed_operations_update_request(
            &AuthorizationAppsAllowedOperationsSetArgs {
                app: "home".to_string(),
                input_file: None,
                set: vec![],
                remove: vec!["legacy_op".to_string()],
            },
        )
        .unwrap();
        assert!(body.operations.is_empty());
        assert_eq!(body.removed, vec!["legacy_op".to_string()]);
    }
}
