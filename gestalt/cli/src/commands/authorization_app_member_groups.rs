use anyhow::{Context, Result, bail};
use serde_json::Value;

use crate::api::ApiClient;
use crate::cli::AuthorizationRelationshipMutationArgs;
use crate::commands::authorization::{
    build_relationship_from_args, relationship_tuple_from_parts,
};
use crate::output::{self, Format};

use gestalt_sdk::authorization::{AddRelationshipRequest, DeleteRelationshipRequest};
use gestalt_sdk::public::generated::app_client::AuthorizationClient;
use gestalt_sdk::public::rest_transport::SyncRestTransport;

pub(crate) fn set_group_member(
    authz: &AuthorizationClient<SyncRestTransport>,
    app: &str,
    group_id: &str,
    role: &str,
    format: Format,
) -> Result<()> {
    let subject_set = group_member_subject_set(group_id);
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
    match format {
        Format::Json => output::print_json(&serde_json::json!({
            "changed": true,
            "groupId": group_id,
            "role": role,
            "subjectSet": subject_set,
        })),
        Format::Table => output::print_success(&format!(
            "Granted {role} on {app} to group {group_id}."
        )),
    }
    Ok(())
}

pub(crate) fn remove_group_member(
    api: &ApiClient,
    authz: &AuthorizationClient<SyncRestTransport>,
    members_path: &str,
    app: &str,
    group_id: &str,
    role: Option<&str>,
    format: Format,
) -> Result<()> {
    let subject_set = group_member_subject_set(group_id);
    let roles = match role {
        Some(role) => vec![role.to_string()],
        None => mutable_group_roles(api, members_path, &subject_set)?,
    };
    if roles.is_empty() {
        bail!("no mutable group grants found for {group_id} on {app}");
    }
    for role in &roles {
        let tuple =
            relationship_tuple_from_parts("app", app, role, None, Some(&subject_set))?;
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
        if let Some(role) = row.get("role").and_then(Value::as_str).map(str::trim) {
            if !role.is_empty() {
                roles.push(role.to_string());
            }
        }
    }
    Ok(roles)
}

#[cfg(test)]
mod tests {
    use super::group_member_subject_set;

    #[test]
    fn group_member_subject_set_formats_selector() {
        assert_eq!(
            group_member_subject_set("valon-employees"),
            "group:valon-employees#member"
        );
    }
}
