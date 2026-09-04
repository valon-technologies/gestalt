use anyhow::{Context, Result, bail};
use serde::Deserialize;
use serde_json::Value;

use crate::api::{ApiClient, encode_path_segment};
use crate::cli::{
    AuthorizationAppsCommands, AuthorizationAppsMembersCommands, AuthorizationAppsMembersListArgs,
    AuthorizationAppsMembersRemoveArgs, AuthorizationAppsMembersSetArgs,
};
use crate::commands::authorization::{
    build_app_member_relationship, relationship_tuple_from_parts,
};
use crate::output::{self, Format};

use gestalt_sdk::authorization::{AddRelationshipRequest, DeleteRelationshipRequest};
use gestalt_sdk::public::generated::app_client::AuthorizationClient;
use gestalt_sdk::public::rest_transport::SyncRestTransport;

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
            AuthorizationAppsMembersCommands::Set(args) => set_member(authz, api, &args, format),
            AuthorizationAppsMembersCommands::Remove(args) => {
                remove_member(authz, api, &args, format)
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
    authz: &AuthorizationClient<SyncRestTransport>,
    api: &ApiClient,
    args: &AuthorizationAppsMembersSetArgs,
    format: Format,
) -> Result<()> {
    let app = require_app_name(&args.app)?;
    let role = require_role(&args.role)?;
    let subject_id = member_subject_id(args.email.as_deref(), args.subject_id.as_deref())?;

    for existing in mutable_member_roles_for_subject(api, &app, &subject_id)? {
        if existing == role {
            match format {
                Format::Json => output::print_json(&serde_json::json!({
                    "app": app,
                    "subjectId": subject_id,
                    "role": role,
                    "changed": false,
                })),
                Format::Table => {
                    output::print_success(&format!("{subject_id} already has {role} on {app}."))
                }
            }
            return Ok(());
        }
        delete_member_tuple(authz, &app, &existing, &subject_id)?;
    }

    let relationship = build_app_member_relationship(&app, &role, &subject_id)?;
    authz
        .add_relationship_sync(AddRelationshipRequest {
            relationship: Some(relationship),
        })
        .context("failed to grant app member access")?;
    match format {
        Format::Json => output::print_json(&serde_json::json!({
            "app": app,
            "subjectId": subject_id,
            "role": role,
            "changed": true,
        })),
        Format::Table => {
            output::print_success(&format!("Granted {role} on {app} to {subject_id}."))
        }
    }
    Ok(())
}

fn remove_member(
    authz: &AuthorizationClient<SyncRestTransport>,
    api: &ApiClient,
    args: &AuthorizationAppsMembersRemoveArgs,
    format: Format,
) -> Result<()> {
    let app = require_app_name(&args.app)?;
    let subject = normalize_subject_id(&args.subject)?;
    let roles = match args
        .role
        .as_deref()
        .map(str::trim)
        .filter(|v| !v.is_empty())
    {
        Some(role) => vec![require_role(role)?],
        None => mutable_member_roles_for_subject(api, &app, &subject)?,
    };
    if roles.is_empty() {
        bail!("no mutable member grants found for {subject} on {app}");
    }
    for role in &roles {
        delete_member_tuple(authz, &app, role, &subject)?;
    }
    match format {
        Format::Json => output::print_json(&serde_json::json!({
            "app": app,
            "subjectId": subject,
            "removedRoles": roles,
        })),
        Format::Table => output::print_success(&format!(
            "Removed {} grant(s) for {subject} on {app}.",
            roles.len()
        )),
    }
    Ok(())
}

fn delete_member_tuple(
    authz: &AuthorizationClient<SyncRestTransport>,
    app: &str,
    role: &str,
    subject_id: &str,
) -> Result<()> {
    let tuple = relationship_tuple_from_parts("app", app, role, Some(subject_id), None)?;
    authz
        .delete_relationship_sync(DeleteRelationshipRequest {
            relationship_tuple: Some(tuple),
        })
        .with_context(|| format!("failed to remove {role} grant for {subject_id} on {app}"))?;
    Ok(())
}

fn mutable_member_roles_for_subject(
    api: &ApiClient,
    app: &str,
    subject_id: &str,
) -> Result<Vec<String>> {
    let path = format!("/api/v1/apps/{}/admin/members", encode_path_segment(app));
    let resp = api
        .get(&path)
        .with_context(|| format!("failed to list members for app {app}"))?;
    let members: Vec<AppAdminMember> =
        serde_json::from_value(resp).context("failed to parse app admin members response")?;
    Ok(members
        .into_iter()
        .filter(|member| member.mutable && subject_matches_member(subject_id, member))
        .map(|member| member.role)
        .collect())
}

fn subject_matches_member(subject_id: &str, member: &AppAdminMember) -> bool {
    let normalized = normalize_subject_id(subject_id).unwrap_or_default();
    if let Some(subject) = member.subject_id.as_deref() {
        if normalize_subject_id(subject).ok().as_deref() == Some(normalized.as_str()) {
            return true;
        }
    }
    member
        .selector_value
        .as_deref()
        .and_then(|value| normalize_subject_id(value).ok())
        .as_deref()
        == Some(normalized.as_str())
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

fn member_subject_id(email: Option<&str>, subject_id: Option<&str>) -> Result<String> {
    if let Some(email) = email.map(str::trim).filter(|value| !value.is_empty()) {
        return normalize_user_email_subject(email);
    }
    if let Some(subject_id) = subject_id.map(str::trim).filter(|value| !value.is_empty()) {
        return normalize_subject_id(subject_id);
    }
    bail!("either --email or --subject-id is required")
}

fn normalize_user_email_subject(email: &str) -> Result<String> {
    let trimmed = email.trim();
    if trimmed.is_empty() {
        bail!("email is required");
    }
    if trimmed.to_lowercase().starts_with("user:") {
        Ok(trimmed.to_string())
    } else {
        Ok(format!("user:{trimmed}"))
    }
}

fn normalize_subject_id(raw: &str) -> Result<String> {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        bail!("subject id is required");
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
    Ok(trimmed.to_string())
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct AppAdminMember {
    role: String,
    #[serde(default)]
    mutable: bool,
    #[serde(default)]
    subject_id: Option<String>,
    #[serde(default)]
    selector_value: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::{
        AppAdminMember, member_subject_id, normalize_subject_id, normalize_user_email_subject,
        subject_matches_member,
    };

    #[test]
    fn normalize_user_email_subject_prefixes_user() {
        assert_eq!(
            normalize_user_email_subject("alice@example.com").unwrap(),
            "user:alice@example.com"
        );
    }

    #[test]
    fn normalize_subject_id_prefixes_bare_ids() {
        assert_eq!(normalize_subject_id("user_123").unwrap(), "user:user_123");
    }

    #[test]
    fn member_subject_id_prefers_email() {
        assert_eq!(
            member_subject_id(Some("alice@example.com"), Some("user:other")).unwrap(),
            "user:alice@example.com"
        );
    }

    #[test]
    fn subject_matches_member_compares_subject_id() {
        let member = AppAdminMember {
            role: "viewer".to_string(),
            mutable: true,
            subject_id: Some("user:abc".to_string()),
            selector_value: None,
        };
        assert!(subject_matches_member("user:abc", &member));
        assert!(!subject_matches_member("user:def", &member));
    }
}
