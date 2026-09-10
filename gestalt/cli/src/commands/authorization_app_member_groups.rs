use anyhow::{Context, Result, bail};
use serde_json::Value;

use crate::api::{ApiClient, encode_path_segment};
use crate::cli::AuthorizationRelationshipMutationArgs;
use crate::commands::authorization::{build_relationship_from_args, relationship_tuple_from_parts};
use crate::output::{self, Format};

use gestalt_sdk::authorization::{AddRelationshipRequest, DeleteRelationshipRequest};
use gestalt_sdk::public::generated::app_client::AuthorizationClient;
use gestalt_sdk::public::rest_transport::SyncRestTransport;

pub(crate) fn set_group_member(
    api: &ApiClient,
    authz: &AuthorizationClient<SyncRestTransport>,
    app: &str,
    group_id: &str,
    role: &str,
    format: Format,
) -> Result<()> {
    let members_path = format!("/api/v1/apps/{}/admin/members", encode_path_segment(app));
    let subject_set = group_member_subject_set(group_id);
    let existing = mutable_group_roles(api, &members_path, &subject_set)?;
    let (add_role, roles_to_remove) = group_roles_to_replace(&existing, role);
    for existing_role in &roles_to_remove {
        let tuple =
            relationship_tuple_from_parts("app", app, existing_role, None, Some(&subject_set))?;
        authz
            .delete_relationship_sync(DeleteRelationshipRequest {
                relationship_tuple: Some(tuple),
            })
            .with_context(|| {
                format!(
                    "failed to remove {existing_role} on {app} for group {group_id} ({subject_set})"
                )
            })?;
    }
    if add_role {
        let relationship = build_relationship_from_args(&AuthorizationRelationshipMutationArgs {
            resource_type: "app".to_string(),
            resource_id: app.to_string(),
            relation: role.to_string(),
            subject_id: None,
            subject_set: Some(subject_set.clone()),
        })?;
        authz
            .add_relationship_sync(AddRelationshipRequest {
                relationship: Some(relationship),
            })
            .with_context(|| {
                format!("failed to grant {role} on {app} to group {group_id} ({subject_set})")
            })?;
    }
    let changed = add_role || !roles_to_remove.is_empty();
    match format {
        Format::Json => output::print_json(&serde_json::json!({
            "changed": changed,
            "groupId": group_id,
            "role": role,
            "subjectSet": subject_set,
        })),
        Format::Table => {
            if changed {
                output::print_success(&format!("Granted {role} on {app} to group {group_id}."))
            } else {
                output::print_success(&format!("Group {group_id} already has {role} on {app}."))
            }
        }
    }
    Ok(())
}

pub(crate) fn remove_group_member(
    api: &ApiClient,
    authz: &AuthorizationClient<SyncRestTransport>,
    app: &str,
    group_id: &str,
    role: Option<&str>,
    format: Format,
) -> Result<()> {
    let members_path = format!("/api/v1/apps/{}/admin/members", encode_path_segment(app));
    let subject_set = group_member_subject_set(group_id);
    let mutable = mutable_group_roles(api, &members_path, &subject_set)?;
    let roles = match role {
        Some(role) => {
            require_mutable_group_role(&mutable, role, group_id, app)?;
            vec![role.to_string()]
        }
        None => mutable,
    };
    if roles.is_empty() {
        bail!("no mutable group grants found for {group_id} on {app}");
    }
    for role in &roles {
        let tuple = relationship_tuple_from_parts("app", app, role, None, Some(&subject_set))?;
        authz
            .delete_relationship_sync(DeleteRelationshipRequest {
                relationship_tuple: Some(tuple),
            })
            .with_context(|| {
                format!("failed to remove {role} on {app} for group {group_id} ({subject_set})")
            })?;
    }
    match format {
        Format::Json => output::print_json(&serde_json::json!({
            "removedRoles": roles,
            "groupId": group_id,
            "subjectSet": subject_set,
        })),
        Format::Table => output::print_success(&format!(
            "Removed {} grant(s) for group {group_id} on {app}.",
            roles.len()
        )),
    }
    Ok(())
}

pub(crate) fn group_member_subject_set(group_id: &str) -> String {
    format!("group:{}#member", group_id.trim())
}

fn mutable_group_roles(
    api: &ApiClient,
    members_path: &str,
    subject_set: &str,
) -> Result<Vec<String>> {
    let resp = api
        .get(members_path)
        .context("failed to list app members for group removal")?;
    let empty = Vec::new();
    let rows = resp.as_array().unwrap_or(&empty);
    let mut roles = Vec::new();
    for row in rows {
        if row.get("selectorValue").and_then(Value::as_str) != Some(subject_set) {
            continue;
        }
        if row.get("mutable").and_then(Value::as_bool) != Some(true) {
            continue;
        }
        if let Some(role) = row.get("role").and_then(Value::as_str).map(str::trim)
            && !role.is_empty()
        {
            roles.push(role.to_string());
        }
    }
    Ok(roles)
}

fn group_roles_to_replace(existing: &[String], target: &str) -> (bool, Vec<String>) {
    let mut add_target = true;
    let mut roles_to_remove = Vec::new();
    for role in existing {
        if role == target {
            add_target = false;
        } else {
            roles_to_remove.push(role.clone());
        }
    }
    (add_target, roles_to_remove)
}

fn require_mutable_group_role(
    mutable: &[String],
    role: &str,
    group_id: &str,
    app: &str,
) -> Result<()> {
    if mutable.iter().any(|existing| existing == role) {
        return Ok(());
    }
    bail!("no mutable {role} grant found for group {group_id} on {app}");
}

#[cfg(test)]
mod tests {
    use super::{group_member_subject_set, group_roles_to_replace, require_mutable_group_role};

    #[test]
    fn group_member_subject_set_formats_selector() {
        assert_eq!(
            group_member_subject_set("valon-employees"),
            "group:valon-employees#member"
        );
    }

    #[test]
    fn group_roles_to_replace_removes_other_mutable_roles() {
        let (add, remove) =
            group_roles_to_replace(&["admin".to_string(), "viewer".to_string()], "viewer");
        assert!(!add);
        assert_eq!(remove, vec!["admin".to_string()]);
    }

    #[test]
    fn group_roles_to_replace_adds_when_role_missing() {
        let (add, remove) = group_roles_to_replace(&["admin".to_string()], "viewer");
        assert!(add);
        assert_eq!(remove, vec!["admin".to_string()]);
    }

    #[test]
    fn require_mutable_group_role_rejects_unknown_role() {
        let err = require_mutable_group_role(&["viewer".to_string()], "admin", "eng", "demo")
            .unwrap_err();
        assert!(err.to_string().contains("no mutable admin grant found"));
    }
}
