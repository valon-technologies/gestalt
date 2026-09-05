use anyhow::{Context, Result, bail};
use serde::Deserialize;
use serde::Serialize;
use serde_json::Value;

use crate::api::{ApiClient, encode_path_segment};
use crate::cli::{
    AuthorizationAppsAllowedOperationsCommands, AuthorizationAppsAllowedOperationsListArgs,
    AuthorizationAppsAllowedOperationsSetArgs, AuthorizationAppsCommands,
    AuthorizationAppsMembersCommands, AuthorizationAppsMembersListArgs,
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
    authz: &AuthorizationClient<SyncRestTransport>,
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

    let existing = mutable_roles_for_subject(authz, api, &app, &subject_id)?;
    let plan = plan_role_set(&existing, &role);
    if plan.roles_to_remove.is_empty() && !plan.add_role {
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

    for existing_role in &plan.roles_to_remove {
        delete_member_tuple(authz, &app, existing_role, &subject_id)?;
    }

    if plan.add_role {
        let relationship = build_app_member_relationship(&app, &role, &subject_id)?;
        authz
            .add_relationship_sync(AddRelationshipRequest {
                relationship: Some(relationship),
            })
            .context("failed to grant app member access")?;
    }

    match format {
        Format::Json => output::print_json(&serde_json::json!({
            "app": app,
            "subjectId": subject_id,
            "role": role,
            "changed": true,
        })),
        Format::Table => {
            if plan.add_role {
                output::print_success(&format!("Granted {role} on {app} to {subject_id}."))
            } else {
                output::print_success(&format!("Updated {subject_id} on {app} to only {role}."))
            }
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
    let subject = resolve_canonical_member_subject_id(
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

fn list_allowed_operations(
    api: &ApiClient,
    args: &AuthorizationAppsAllowedOperationsListArgs,
    format: Format,
) -> Result<()> {
    let app = require_app_name(&args.app)?;
    let path = format!(
        "/api/v1/apps/{}/admin/allowed-operations",
        encode_path_segment(&app)
    );
    let resp = api
        .get(&path)
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
    let path = format!(
        "/api/v1/apps/{}/admin/allowed-operations",
        encode_path_segment(&app)
    );
    let resp = api
        .put(&path, &body)
        .with_context(|| format!("failed to update allowed operations for app {app}"))?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            output::print_success(&format!("Updated allowed operations for {app}."))
        }
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

    let mut operations = std::collections::HashMap::new();
    for entry in &args.set {
        let (operation_id, roles) = parse_operation_roles_assignment(entry)?;
        operations.insert(
            operation_id,
            OperationOverrideBody {
                allowed_roles: roles,
            },
        );
    }

    let removed: Vec<String> = args
        .remove
        .iter()
        .map(|id| {
            let trimmed = id.trim();
            if trimmed.is_empty() {
                bail!("operation id is required");
            }
            Ok(trimmed.to_string())
        })
        .collect::<Result<_>>()?;

    Ok(AllowedOperationsUpdateRequest {
        operations,
        removed,
    })
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
    Ok(trimmed.to_string())
}

#[derive(Debug, PartialEq, Eq)]
struct RoleSetPlan {
    roles_to_remove: Vec<String>,
    add_role: bool,
}

fn plan_role_set(existing_roles: &[String], role: &str) -> RoleSetPlan {
    let already_has_role = existing_roles.iter().any(|existing| existing == role);
    let roles_to_remove = existing_roles
        .iter()
        .filter(|existing| existing.as_str() != role)
        .cloned()
        .collect();
    RoleSetPlan {
        roles_to_remove,
        add_role: !already_has_role,
    }
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct AppAdminMember {
    role: String,
    #[serde(default)]
    mutable: bool,
    #[serde(default)]
    subject_id: Option<String>,
    #[serde(default)]
    email: Option<String>,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct AllowedOperationsUpdateRequest {
    operations: std::collections::HashMap<String, OperationOverrideBody>,
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
        AppAdminMember, AuthorizationAppsAllowedOperationsSetArgs, RoleSetPlan,
        canonical_subject_id_from_members, is_service_account_subject, normalize_subject_id,
        parse_operation_roles_assignment, plan_role_set, subject_id_for_email_in_members,
        subject_matches_member,
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
            email: Some("alice@example.com".to_string()),
        };
        assert!(subject_matches_member("user:abc", &member));
        assert!(subject_matches_member("user:alice@example.com", &member));
        assert!(!subject_matches_member("user:def", &member));
    }

    #[test]
    fn subject_matches_member_ignores_subject_set_selectors() {
        let member = AppAdminMember {
            role: "viewer".to_string(),
            mutable: true,
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
            role: "viewer".to_string(),
            mutable: true,
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
            role: "viewer".to_string(),
            mutable: true,
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
    fn plan_role_set_noop_when_only_requested_role_present() {
        assert_eq!(
            plan_role_set(&["viewer".to_string()], "viewer"),
            RoleSetPlan {
                roles_to_remove: vec![],
                add_role: false,
            }
        );
    }

    #[test]
    fn plan_role_set_removes_extra_roles_without_readding() {
        assert_eq!(
            plan_role_set(&["viewer".to_string(), "editor".to_string()], "viewer"),
            RoleSetPlan {
                roles_to_remove: vec!["editor".to_string()],
                add_role: false,
            }
        );
    }

    #[test]
    fn plan_role_set_replaces_existing_role() {
        assert_eq!(
            plan_role_set(&["editor".to_string()], "viewer"),
            RoleSetPlan {
                roles_to_remove: vec!["editor".to_string()],
                add_role: true,
            }
        );
    }

    #[test]
    fn parse_operation_roles_assignment_splits_roles() {
        assert_eq!(
            parse_operation_roles_assignment("get_item=viewer,editor").unwrap(),
            ("get_item".to_string(), vec!["viewer".to_string(), "editor".to_string()])
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
