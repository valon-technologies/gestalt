use anyhow::{Context, Result, bail};

use crate::api::{ApiClient, encode_path_segment};
use crate::cli::{
    AuthorizationSubjectCommands, AuthorizationSubjectCreateArgs,
    AuthorizationSubjectGrantCommands, AuthorizationSubjectGrantSetArgs,
    AuthorizationSubjectTokenCommands, AuthorizationSubjectTokenCreateArgs,
};
use crate::output::{self, Format};

pub fn dispatch(
    api: &ApiClient,
    command: AuthorizationSubjectCommands,
    format: Format,
) -> Result<()> {
    match command {
        AuthorizationSubjectCommands::Create(args) => create(api, &args, format),
        AuthorizationSubjectCommands::Grants { command } => match command {
            AuthorizationSubjectGrantCommands::Set(args) => set_grant(api, &args, format),
        },
        AuthorizationSubjectCommands::Tokens { command } => match command {
            AuthorizationSubjectTokenCommands::Create(args) => create_token(api, &args, format),
        },
    }
}

fn create(api: &ApiClient, args: &AuthorizationSubjectCreateArgs, format: Format) -> Result<()> {
    let body = serde_json::json!({
        "id": args.id.trim(),
        "displayName": args.display_name.trim(),
        "description": args.description.as_deref().unwrap_or("").trim(),
    });
    let resp = api
        .post("/api/v1/authorization/subjects", &body)
        .context("failed to create authorization subject")?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => output::print_success(&format!(
            "Created service account {}.",
            resp.get("subjectId")
                .and_then(|value| value.as_str())
                .unwrap_or(args.id.trim())
        )),
    }
    Ok(())
}

fn create_token(
    api: &ApiClient,
    args: &AuthorizationSubjectTokenCreateArgs,
    format: Format,
) -> Result<()> {
    if args.name.trim().is_empty() {
        bail!("--name is required");
    }
    if args.permission.is_empty() {
        bail!("at least one --permission is required");
    }
    let permissions: Vec<String> = args
        .permission
        .iter()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
        .collect();
    if permissions.is_empty() {
        bail!("at least one non-empty --permission is required");
    }
    let mut body = serde_json::json!({
        "name": args.name.trim(),
        "permissions": permissions,
    });
    if let Some(expires_in) = args.expires_in {
        body["expiresIn"] = expires_in.into();
    }
    let path = format!(
        "/api/v1/authorization/subjects/{}/tokens",
        encode_path_segment(args.subject_id.trim())
    );
    let resp = api
        .post(&path, &body)
        .context("failed to create service account token")?;
    let token = resp
        .get("token")
        .and_then(|value| value.as_str())
        .unwrap_or("")
        .trim()
        .to_string();
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            if token.is_empty() {
                output::print_json(&resp);
            } else {
                output::print_success("Token created. Save it now; it won't be shown again.");
                println!("{token}");
            }
        }
    }
    Ok(())
}

fn set_grant(
    api: &ApiClient,
    args: &AuthorizationSubjectGrantSetArgs,
    format: Format,
) -> Result<()> {
    let subject_id = normalize_service_account_subject_id(&args.subject_id)?;
    let body = serde_json::json!({
        "relation": args.relation.trim(),
        "resourceType": args.resource_type.trim(),
        "resourceId": args.resource_id.trim(),
    });
    let path = format!(
        "/api/v1/authorization/subjects/{}/grants",
        encode_path_segment(&subject_id)
    );
    let resp = api
        .put(&path, &body)
        .context("failed to set authorization grant")?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => output::print_success("Authorization grant created."),
    }
    Ok(())
}

fn normalize_service_account_subject_id(raw: &str) -> Result<String> {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        bail!("subject id is required");
    }
    if trimmed.contains(':') {
        Ok(trimmed.to_string())
    } else {
        Ok(format!("service_account:{trimmed}"))
    }
}
