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

use gestalt_sdk::authorization::source_layer::SOURCE_LAYER_RUNTIME;
use gestalt_sdk::authorization::{
    AddRelationshipRequest, DeleteRelationshipRequest, ListRelationshipsRequest, Relationship,
    RelationshipFilter, RelationshipTargetKind, Resource,
};
use gestalt_sdk::public::generated::app::AppInvokeRequest;
use gestalt_sdk::public::generated::app_client::{AppClient, AuthorizationClient};
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
    let subject_id =
        resolve_member_subject_id(api, &app, args.email.as_deref(), args.subject_id.as_deref())?;

    let existing = mutable_roles_for_subject(authz, api, &app, &subject_id)?;
    let other_roles: Vec<String> = existing
        .iter()
        .filter(|existing_role| existing_role.as_str() != role)
        .cloned()
        .collect();
    for existing_role in &other_roles {
        delete_member_tuple(authz, &app, existing_role, &subject_id)?;
    }

    if existing.iter().any(|existing_role| existing_role == &role) {
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
    let subject = resolve_member_subject_id(
        api,
        &app,
        args.email.as_deref(),
        args.subject_id.as_deref().or(args.subject.as_deref()),
    )?;
    let mutable_roles = mutable_roles_for_subject(authz, api, &app, &subject)?;
    let roles = match args
        .role
        .as_deref()
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        Some(role) => {
            let role = require_role(role)?;
            if !mutable_roles
                .iter()
                .any(|mutable_role| mutable_role == &role)
            {
                bail!("no mutable {role} grant found for {subject} on {app}");
            }
            vec![role]
        }
        None => mutable_roles,
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

fn mutable_roles_for_subject(
    authz: &AuthorizationClient<SyncRestTransport>,
    api: &ApiClient,
    app: &str,
    subject_id: &str,
) -> Result<Vec<String>> {
    if is_service_account_subject(subject_id) {
        return runtime_roles_for_subject(authz, app, subject_id);
    }

    let members = load_app_admin_members(api, app)?;
    let roster_roles: Vec<String> = members
        .iter()
        .filter(|member| member.mutable && subject_matches_member(subject_id, member))
        .map(|member| member.role.clone())
        .collect();
    if !roster_roles.is_empty()
        || members
            .iter()
            .any(|member| subject_matches_member(subject_id, member))
    {
        return Ok(roster_roles);
    }

    runtime_roles_for_subject(authz, app, subject_id)
}

fn runtime_roles_for_subject(
    authz: &AuthorizationClient<SyncRestTransport>,
    app: &str,
    subject_id: &str,
) -> Result<Vec<String>> {
    let normalized = normalize_subject_id(subject_id)?;
    let mut roles = Vec::new();
    let mut page_token = String::new();
    loop {
        let resp = authz
            .list_relationships_sync(ListRelationshipsRequest {
                filter: Some(RelationshipFilter {
                    resource: Some(Resource {
                        r#type: "app".to_string(),
                        id: app.to_string(),
                        properties: None,
                    }),
                    ..Default::default()
                }),
                page_size: 500,
                page_token: page_token.clone(),
            })
            .context("failed to list app relationships")?;
        for relationship in resp.relationships {
            if relationship.source_layer != SOURCE_LAYER_RUNTIME {
                continue;
            }
            let Some(role) = runtime_role_for_subject(&relationship, &normalized) else {
                continue;
            };
            roles.push(role);
        }
        page_token = resp.next_page_token.trim().to_string();
        if page_token.is_empty() {
            return Ok(roles);
        }
    }
}

fn runtime_role_for_subject(relationship: &Relationship, subject_id: &str) -> Option<String> {
    let tuple = relationship.tuple.as_ref()?;
    let target = tuple.target.as_ref()?.kind.as_ref()?;
    let target_subject = match target {
        RelationshipTargetKind::Subject(subject) => subject,
        _ => return None,
    };
    if normalize_subject_id(&target_subject.id).ok().as_deref() != Some(subject_id) {
        return None;
    }
    let role = tuple.relation.trim();
    if role.is_empty() {
        return None;
    }
    Some(role.to_string())
}

fn load_app_admin_members(api: &ApiClient, app: &str) -> Result<Vec<AppAdminMember>> {
    let path = format!("/api/v1/apps/{}/admin/members", encode_path_segment(app));
    let resp = api
        .get(&path)
        .with_context(|| format!("failed to list members for app {app}"))?;
    serde_json::from_value(resp).context("failed to parse app admin members response")
}

fn resolve_member_subject_id(
    api: &ApiClient,
    app: &str,
    email: Option<&str>,
    subject_id: Option<&str>,
) -> Result<String> {
    if let Some(subject_id) = subject_id.map(str::trim).filter(|value| !value.is_empty()) {
        return normalize_subject_id(subject_id);
    }
    let email = email
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .context("either --email or --subject-id is required")?;

    if let Some(subject_id) = subject_id_for_email_in_members(api, app, email)? {
        return Ok(subject_id);
    }
    if let Some(user_id) = lookup_user_id_by_email(api, email)? {
        return Ok(format!("user:{user_id}"));
    }
    bail!(
        "could not resolve {email} to a canonical user id; pass --subject-id user:<uuid> from `members list`"
    )
}

fn subject_id_for_email_in_members(
    api: &ApiClient,
    app: &str,
    email: &str,
) -> Result<Option<String>> {
    let normalized_email = email.trim().to_lowercase();
    for member in load_app_admin_members(api, app)? {
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
            return Ok(Some(subject_id.to_string()));
        }
    }
    Ok(None)
}

fn lookup_user_id_by_email(api: &ApiClient, email: &str) -> Result<Option<String>> {
    let transport = api.sync_rest_transport(std::time::Duration::from_secs(30));
    let app_client = AppClient::new(transport);
    let resp = match app_client.invoke_sync(AppInvokeRequest {
        app: "home".to_string(),
        operation: "users.list".to_string(),
        ..Default::default()
    }) {
        Ok(resp) => resp,
        Err(_) => return Ok(None),
    };
    let users = resp
        .get("users")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    let normalized_email = email.trim().to_lowercase();
    for user in users {
        let user_email = user
            .get("email")
            .and_then(Value::as_str)
            .unwrap_or("")
            .trim()
            .to_lowercase();
        if user_email != normalized_email {
            continue;
        }
        if let Some(id) = user
            .get("id")
            .and_then(Value::as_str)
            .map(str::trim)
            .filter(|value| !value.is_empty())
        {
            return Ok(Some(id.to_string()));
        }
    }
    Ok(None)
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
    #[serde(default)]
    email: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::{
        AppAdminMember, is_service_account_subject, normalize_subject_id, subject_matches_member,
    };

    #[test]
    fn normalize_subject_id_prefixes_bare_ids() {
        assert_eq!(normalize_subject_id("user_123").unwrap(), "user:user_123");
    }

    #[test]
    fn subject_matches_member_compares_subject_id_and_email() {
        let member = AppAdminMember {
            role: "viewer".to_string(),
            mutable: true,
            subject_id: Some("user:abc".to_string()),
            selector_value: None,
            email: Some("alice@example.com".to_string()),
        };
        assert!(subject_matches_member("user:abc", &member));
        assert!(subject_matches_member("user:alice@example.com", &member));
        assert!(!subject_matches_member("user:def", &member));
    }

    #[test]
    fn service_account_subject_detection() {
        assert!(is_service_account_subject("service_account:bot"));
        assert!(!is_service_account_subject("user:abc"));
    }
}
