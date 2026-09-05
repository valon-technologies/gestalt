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

pub fn dispatch(
    api: &ApiClient,
    _authz: &AuthorizationClient<SyncRestTransport>,
    command: AuthorizationAppsCommands,
    format: Format,
) -> Result<()> {
    match command {
        AuthorizationAppsCommands::List => list_apps(api, format),
        AuthorizationAppsCommands::Members { command } => match command {
            AuthorizationAppsMembersCommands::List(args) => list_members(api, &args, format),
            AuthorizationAppsMembersCommands::Set(args) => set_member(api, &args, format),
            AuthorizationAppsMembersCommands::Remove(args) => remove_member(api, &args, format),
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
    args: &AuthorizationAppsMembersSetArgs,
    format: Format,
) -> Result<()> {
    let app = require_app_name(&args.app)?;
    let role = require_role(&args.role)?;
    let subject_id = resolve_canonical_member_subject_id(
        api,
        &app,
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
    args: &AuthorizationAppsMembersRemoveArgs,
    format: Format,
) -> Result<()> {
    let app = require_app_name(&args.app)?;
    let subject = resolve_canonical_member_subject_id(
        api,
        &app,
        args.email.as_deref(),
        args.subject_id.as_deref().or(args.subject.as_deref()),
    )?;
    let role = args
        .role
        .as_deref()
        .map(str::trim)
        .filter(|value| !value.is_empty())
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

fn load_app_admin_members(api: &ApiClient, app: &str) -> Result<Vec<AppAdminMember>> {
    let resp = api
        .get(&app_admin_members_path(app))
        .with_context(|| format!("failed to list members for app {app}"))?;
    serde_json::from_value(resp).context("failed to parse app admin members response")
}

fn resolve_canonical_member_subject_id(
    api: &ApiClient,
    app: &str,
    email: Option<&str>,
    subject_id: Option<&str>,
) -> Result<String> {
    if let Some(subject_id) = subject_id.map(str::trim).filter(|value| !value.is_empty()) {
        let normalized = normalize_subject_id(subject_id)?;
        if is_service_account_subject(&normalized) {
            return Ok(normalized);
        }
        return canonical_subject_id_from_members(&load_app_admin_members(api, app)?, &normalized);
    }

    let email = email
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .context("either --email or --subject-id is required")?;
    let members = load_app_admin_members(api, app)?;
    if let Some(subject_id) = subject_id_for_email_in_members(&members, email) {
        return Ok(subject_id);
    }
    bail!(
        "could not resolve {email} from the app member roster; pass --subject-id user:<uuid> from `members list`"
    )
}

fn canonical_subject_id_from_members(
    members: &[AppAdminMember],
    subject_id: &str,
) -> Result<String> {
    let normalized = normalize_subject_id(subject_id)?;
    if is_service_account_subject(&normalized) {
        return Ok(normalized);
    }
    for member in members {
        if !subject_matches_member(&normalized, member) {
            continue;
        }
        if let Some(canonical) = member
            .subject_id
            .as_deref()
            .map(str::trim)
            .filter(|value| !value.is_empty())
        {
            return Ok(canonical.to_string());
        }
    }
    Ok(normalized)
}

fn subject_id_for_email_in_members(members: &[AppAdminMember], email: &str) -> Option<String> {
    let normalized_email = email.trim().to_lowercase();
    for member in members {
        let Some(member_email) = member.email.as_deref() else {
            continue;
        };
        if member_email.trim().eq_ignore_ascii_case(&normalized_email)
            && let Some(subject_id) = member
                .subject_id
                .as_deref()
                .map(str::trim)
                .filter(|value| !value.is_empty())
        {
            return Some(subject_id.to_string());
        }
    }
    None
}

fn subject_matches_member(subject_id: &str, member: &AppAdminMember) -> bool {
    let normalized = normalize_subject_id(subject_id).unwrap_or_default();
    if let Some(subject) = member.subject_id.as_deref()
        && normalize_subject_id(subject).ok().as_deref() == Some(normalized.as_str())
    {
        return true;
    }
    if let Some(email) = member.email.as_deref() {
        let email_subject = format!("user:{}", email.trim().to_lowercase());
        if normalize_subject_id(&email_subject).ok().as_deref() == Some(normalized.as_str()) {
            return true;
        }
    }
    false
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
        value
            .get("subjectId")
            .or_else(|| value.get("selectorValue"))
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string(),
        value
            .get("email")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string(),
    ]
}

fn is_service_account_subject(subject_id: &str) -> bool {
    subject_id
        .trim()
        .to_ascii_lowercase()
        .starts_with("service_account:")
}

fn normalize_subject_id(raw: &str) -> Result<String> {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        bail!("subject id is required");
    }
    if trimmed.contains('#') {
        bail!(
            "subject id must be a direct subject, not a subject-set selector; use `authorization relationships` for group grants"
        );
    }
    if trimmed.contains(':') {
        Ok(trimmed.to_string())
    } else {
        Ok(format!("user:{trimmed}"))
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

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct AppAdminMember {
    #[serde(default)]
    subject_id: Option<String>,
    #[serde(default)]
    email: Option<String>,
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
        AppAdminMember, AuthorizationAppsAllowedOperationsSetArgs,
        canonical_subject_id_from_members, is_service_account_subject, normalize_subject_id,
        parse_operation_roles_assignment, subject_id_for_email_in_members, subject_matches_member,
    };

    #[test]
    fn normalize_subject_id_prefixes_bare_ids() {
        assert_eq!(normalize_subject_id("user_123").unwrap(), "user:user_123");
    }

    #[test]
    fn subject_matches_member_compares_subject_id_and_email() {
        let member = AppAdminMember {
            subject_id: Some("user:abc".to_string()),
            email: Some("alice@example.com".to_string()),
        };
        assert!(subject_matches_member("user:abc", &member));
        assert!(subject_matches_member("user:alice@example.com", &member));
        assert!(!subject_matches_member("user:def", &member));
    }

    #[test]
    fn subject_matches_member_ignores_subject_set_selectors() {
        let member = AppAdminMember {
            subject_id: None,
            email: None,
        };
        assert!(!subject_matches_member(
            "group:valon-employees#member",
            &member
        ));
    }

    #[test]
    fn normalize_subject_id_rejects_subject_set_selectors() {
        let err = normalize_subject_id("group:valon-employees#member").unwrap_err();
        assert!(
            err.to_string()
                .contains("subject id must be a direct subject")
        );
    }

    #[test]
    fn service_account_subject_detection() {
        assert!(is_service_account_subject("service_account:bot"));
        assert!(!is_service_account_subject("user:abc"));
    }

    #[test]
    fn canonical_subject_id_prefers_roster_subject_id() {
        let members = [AppAdminMember {
            subject_id: Some("user:canonical-id".to_string()),
            email: Some("alice@example.com".to_string()),
        }];
        assert_eq!(
            canonical_subject_id_from_members(&members, "user:alice@example.com").unwrap(),
            "user:canonical-id"
        );
    }

    #[test]
    fn subject_id_for_email_uses_roster_only() {
        let members = [AppAdminMember {
            subject_id: Some("user:canonical-id".to_string()),
            email: Some("Alice@Example.com".to_string()),
        }];
        assert_eq!(
            subject_id_for_email_in_members(&members, "alice@example.com"),
            Some("user:canonical-id".to_string())
        );
        assert_eq!(
            subject_id_for_email_in_members(&members, "bob@example.com"),
            None
        );
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
