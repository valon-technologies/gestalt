use anyhow::{Context, Result, bail};
use serde::Deserialize;
use serde_json::Value;

use crate::api::ApiClient;

const WORKSPACE_USERS_LIST_PATH: &str = "/api/v1/home/users.list";

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AppAdminMember {
    #[serde(default)]
    subject_id: Option<String>,
    #[serde(default)]
    email: Option<String>,
}

pub(crate) fn trimmed_option(value: Option<&str>) -> Option<&str> {
    value.map(str::trim).filter(|value| !value.is_empty())
}

pub(crate) fn load_app_admin_members(api: &ApiClient, path: &str) -> Result<Vec<AppAdminMember>> {
    let resp = api
        .get(path)
        .context("failed to list app admin members")?;
    serde_json::from_value(resp).context("failed to parse app admin members response")
}

pub(crate) fn resolve_canonical_member_subject_id(
    api: &ApiClient,
    members_path: &str,
    email: Option<&str>,
    subject_id: Option<&str>,
) -> Result<String> {
    if let Some(subject_id) = trimmed_option(subject_id) {
        let normalized = normalize_subject_id(subject_id)?;
        if is_service_account_subject(&normalized) {
            return Ok(normalized);
        }
        let members = load_app_admin_members(api, members_path)?;
        return canonical_subject_id_from_members(&members, &normalized);
    }

    let email = trimmed_option(email)
        .context("either --email, --subject-id, or --group-id is required")?;
    resolve_email_subject_id(api, members_path, email)
}

fn resolve_email_subject_id(api: &ApiClient, members_path: &str, email: &str) -> Result<String> {
    let members = load_app_admin_members(api, members_path)?;
    if let Some(subject_id) = subject_id_for_email_in_members(&members, email) {
        return Ok(subject_id);
    }
    resolve_workspace_user_subject_id(api, email)
}

fn resolve_workspace_user_subject_id(api: &ApiClient, email: &str) -> Result<String> {
    let resp = api
        .get(WORKSPACE_USERS_LIST_PATH)
        .context("failed to load workspace user directory")?;
    let empty = Vec::new();
    let users = resp
        .get("users")
        .and_then(Value::as_array)
        .unwrap_or(&empty);
    workspace_user_subject_id_for_email(email, users)
}

fn workspace_user_subject_id_for_email(email: &str, users: &[Value]) -> Result<String> {
    let normalized = email.trim().to_lowercase();
    for user in users {
        let Some(user_email) = user.get("email").and_then(Value::as_str).map(str::trim) else {
            continue;
        };
        let Some(user_id) = user.get("id").and_then(Value::as_str).map(str::trim) else {
            continue;
        };
        if user_email.is_empty() || user_id.is_empty() {
            continue;
        }
        if user_email.eq_ignore_ascii_case(&normalized) {
            return Ok(format!("user:{user_id}"));
        }
    }
    bail!(
        "could not resolve {email} from the workspace user directory; use --email with a workspace address or pass --subject-id user:<uuid>"
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
    let Ok(normalized) = normalize_subject_id(subject_id) else {
        return false;
    };
    member.subject_id.as_deref().is_some_and(|subject| {
        normalize_subject_id(subject)
            .ok()
            .is_some_and(|candidate| candidate == normalized)
    })
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
            "subject id must be a direct subject, not a subject-set selector; use --group-id or `authorization relationships` for group grants"
        );
    }
    let subject_id = if trimmed.contains(':') {
        trimmed.to_string()
    } else if is_canonical_user_uuid(trimmed) {
        format!("user:{trimmed}")
    } else {
        bail!(
            "subject id must be user:<uuid>, service_account:<id>, or a bare user uuid; resolve people with --email"
        );
    };
    validate_person_subject_id(&subject_id)?;
    Ok(subject_id)
}

fn validate_person_subject_id(subject_id: &str) -> Result<()> {
    if is_service_account_subject(subject_id) {
        return Ok(());
    }
    let Some(local) = subject_id
        .strip_prefix("user:")
        .map(str::trim)
        .filter(|value| !value.is_empty())
    else {
        return Ok(());
    };
    if local.contains('@') || !is_canonical_user_uuid(local) {
        bail!(
            "subject id must be user:<uuid>; resolve people with --email from the workspace directory"
        );
    }
    Ok(())
}

fn is_canonical_user_uuid(value: &str) -> bool {
    let value = value.trim();
    if value.len() != 36 {
        return false;
    }
    let bytes = value.as_bytes();
    if bytes[8] != b'-' || bytes[13] != b'-' || bytes[18] != b'-' || bytes[23] != b'-' {
        return false;
    }
    value
        .chars()
        .all(|ch| ch.is_ascii_hexdigit() || ch == '-')
}

#[cfg(test)]
mod tests {
    use super::{
        AppAdminMember, canonical_subject_id_from_members, is_service_account_subject,
        normalize_subject_id, subject_id_for_email_in_members, subject_matches_member,
        trimmed_option, workspace_user_subject_id_for_email,
    };

    #[test]
    fn normalize_subject_id_accepts_bare_user_uuid() {
        assert_eq!(
            normalize_subject_id("11111111-1111-1111-1111-111111111111").unwrap(),
            "user:11111111-1111-1111-1111-111111111111"
        );
    }

    #[test]
    fn normalize_subject_id_rejects_opaque_user_ids() {
        assert!(normalize_subject_id("user_123").is_err());
    }

    #[test]
    fn subject_matches_member_compares_subject_id_only() {
        let member = AppAdminMember {
            subject_id: Some("user:11111111-1111-1111-1111-111111111111".to_string()),
            email: Some("alice@example.com".to_string()),
        };
        assert!(subject_matches_member(
            "user:11111111-1111-1111-1111-111111111111",
            &member
        ));
        assert!(!subject_matches_member(
            "user:22222222-2222-2222-2222-222222222222",
            &member
        ));
    }

    #[test]
    fn normalize_subject_id_rejects_user_email_subject() {
        let err = normalize_subject_id("user:alice@example.com").unwrap_err();
        assert!(err.to_string().contains("user:<uuid>"));
        let err = normalize_subject_id("alice@example.com").unwrap_err();
        assert!(err.to_string().contains("user:<uuid>"));
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
        assert!(!is_service_account_subject("user:11111111-1111-1111-1111-111111111111"));
    }

    #[test]
    fn canonical_subject_id_prefers_roster_subject_id() {
        let members = [AppAdminMember {
            subject_id: Some("user:11111111-1111-1111-1111-111111111111".to_string()),
            email: Some("alice@example.com".to_string()),
        }];
        assert_eq!(
            canonical_subject_id_from_members(
                &members,
                "user:11111111-1111-1111-1111-111111111111"
            )
            .unwrap(),
            "user:11111111-1111-1111-1111-111111111111"
        );
    }

    #[test]
    fn subject_id_for_email_uses_roster_only() {
        let members = [AppAdminMember {
            subject_id: Some("user:11111111-1111-1111-1111-111111111111".to_string()),
            email: Some("Alice@Example.com".to_string()),
        }];
        assert_eq!(
            subject_id_for_email_in_members(&members, "alice@example.com"),
            Some("user:11111111-1111-1111-1111-111111111111".to_string())
        );
        assert_eq!(
            subject_id_for_email_in_members(&members, "bob@example.com"),
            None
        );
    }

    #[test]
    fn workspace_user_subject_id_for_email_resolves_directory_match() {
        let users = [serde_json::json!({
            "id": "11111111-1111-1111-1111-111111111111",
            "email": "Alice@Example.com",
        })];
        assert_eq!(
            workspace_user_subject_id_for_email("alice@example.com", &users).unwrap(),
            "user:11111111-1111-1111-1111-111111111111"
        );
    }

    #[test]
    fn workspace_user_subject_id_for_email_rejects_unknown_directory_user() {
        let users = [serde_json::json!({
            "id": "11111111-1111-1111-1111-111111111111",
            "email": "alice@example.com",
        })];
        let err = workspace_user_subject_id_for_email("bob@example.com", &users).unwrap_err();
        assert!(
            err.to_string()
                .contains("could not resolve bob@example.com from the workspace user directory")
        );
    }

    #[test]
    fn trimmed_option_rejects_blank_values() {
        assert_eq!(trimmed_option(Some("  alice  ")), Some("alice"));
        assert_eq!(trimmed_option(Some("   ")), None);
        assert_eq!(trimmed_option(None), None);
    }
}
