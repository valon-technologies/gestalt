use std::collections::BTreeMap;
use std::io::Read;

use anyhow::{Context, Result, bail};
use serde_json::{Map, Value, json};

use crate::api::{ApiClient, encode_path_segment};
use crate::cli::{
    AuthorizationAdminCommands, AuthorizationAdminMemberCommands, AuthorizationAppCommands,
    AuthorizationCommands, AuthorizationManagedSubjectRole, AuthorizationModelCommands,
    AuthorizationPageArgs, AuthorizationPluginMemberCommands, AuthorizationProviderCommands,
    AuthorizationRelationshipCommands, AuthorizationRelationshipListArgs,
    AuthorizationSubjectCommands, AuthorizationSubjectCreateArgs,
    AuthorizationSubjectExternalIdentityCommands, AuthorizationSubjectGrantCommands,
    AuthorizationSubjectIntegrationCommands, AuthorizationSubjectMemberCommands,
    AuthorizationSubjectTokenCommands, AuthorizationSubjectTokenCreateArgs,
    AuthorizationSubjectUpdateArgs,
};
use crate::commands::apps;
use crate::output::{self, Format};

const SUBJECT_PREFIX: &str = "service_account:";

pub fn dispatch(client: &ApiClient, command: AuthorizationCommands, format: Format) -> Result<()> {
    match command {
        AuthorizationCommands::Subjects { command } => match command {
            AuthorizationSubjectCommands::List => list_subjects(client, format),
            AuthorizationSubjectCommands::Create(args) => create_subject(client, &args, format),
            AuthorizationSubjectCommands::Get { subject } => get_subject(client, &subject, format),
            AuthorizationSubjectCommands::Update(args) => update_subject(client, &args, format),
            AuthorizationSubjectCommands::Delete { subject } => {
                delete_subject(client, &subject, format)
            }
            AuthorizationSubjectCommands::Members { command } => match command {
                AuthorizationSubjectMemberCommands::List { subject } => {
                    list_subject_members(client, &subject, format)
                }
                AuthorizationSubjectMemberCommands::Set(args) => set_subject_member(
                    client,
                    &args.subject,
                    args.subject_id.as_deref(),
                    args.email.as_deref(),
                    args.role,
                    format,
                ),
                AuthorizationSubjectMemberCommands::Remove {
                    subject,
                    member_subject_id,
                } => remove_subject_member(client, &subject, &member_subject_id, format),
            },
            AuthorizationSubjectCommands::Grants { command } => match command {
                AuthorizationSubjectGrantCommands::List { subject } => {
                    list_subject_grants(client, &subject, format)
                }
                AuthorizationSubjectGrantCommands::Set { subject, app, role } => {
                    set_subject_grant(client, &subject, &app, &role, format)
                }
                AuthorizationSubjectGrantCommands::Remove { subject, app } => {
                    remove_subject_grant(client, &subject, &app, format)
                }
            },
            AuthorizationSubjectCommands::ExternalIdentities { command } => match command {
                AuthorizationSubjectExternalIdentityCommands::List { subject } => {
                    list_subject_external_identities(client, &subject, format)
                }
                AuthorizationSubjectExternalIdentityCommands::Add(args) => {
                    add_subject_external_identity(
                        client,
                        &args.subject,
                        &args.identity_type,
                        &args.id,
                        format,
                    )
                }
                AuthorizationSubjectExternalIdentityCommands::Remove(args) => {
                    remove_subject_external_identity(
                        client,
                        &args.subject,
                        &args.identity_type,
                        &args.id,
                        format,
                    )
                }
            },
            AuthorizationSubjectCommands::Integrations { command } => match command {
                AuthorizationSubjectIntegrationCommands::List { subject } => {
                    list_subject_integrations(client, &subject, format)
                }
                AuthorizationSubjectIntegrationCommands::Connect {
                    subject,
                    name,
                    connection,
                    instance,
                } => connect_subject_integration(
                    client,
                    &subject,
                    &name,
                    connection.as_deref(),
                    instance.as_deref(),
                ),
                AuthorizationSubjectIntegrationCommands::Disconnect {
                    subject,
                    name,
                    connection,
                    instance,
                } => disconnect_subject_integration(
                    client,
                    &subject,
                    &name,
                    connection.as_deref(),
                    instance.as_deref(),
                    format,
                ),
            },
            AuthorizationSubjectCommands::Tokens { command } => match command {
                AuthorizationSubjectTokenCommands::List { subject } => {
                    list_subject_tokens(client, &subject, format)
                }
                AuthorizationSubjectTokenCommands::Create(args) => {
                    create_subject_token(client, &args, format)
                }
                AuthorizationSubjectTokenCommands::Revoke { subject, id } => {
                    revoke_subject_token(client, &subject, &id, format)
                }
                AuthorizationSubjectTokenCommands::RevokeAll { subject } => {
                    revoke_all_subject_tokens(client, &subject, format)
                }
            },
        },
        AuthorizationCommands::Apps { command } => match command {
            AuthorizationAppCommands::List => list_plugins(client, format),
            AuthorizationAppCommands::Members { command } => match command {
                AuthorizationPluginMemberCommands::List { app } => {
                    list_plugin_members(client, &app, format)
                }
                AuthorizationPluginMemberCommands::Set(args) => set_plugin_member(
                    client,
                    &args.app,
                    args.subject_id.as_deref(),
                    args.email.as_deref(),
                    &args.role,
                    format,
                ),
                AuthorizationPluginMemberCommands::Remove { app, subject_id } => {
                    remove_plugin_member(client, &app, &subject_id, format)
                }
            },
        },
        AuthorizationCommands::Admins { command } => match command {
            AuthorizationAdminCommands::Members { command } => match command {
                AuthorizationAdminMemberCommands::List => list_admin_members(client, format),
                AuthorizationAdminMemberCommands::Set(args) => set_admin_member(
                    client,
                    args.subject_id.as_deref(),
                    args.email.as_deref(),
                    &args.role,
                    format,
                ),
                AuthorizationAdminMemberCommands::Remove { subject_id } => {
                    remove_admin_member(client, &subject_id, format)
                }
            },
        },
        AuthorizationCommands::Provider { command } => match command {
            AuthorizationProviderCommands::Get => get_provider(client, format),
        },
        AuthorizationCommands::Models { command } => match command {
            AuthorizationModelCommands::List(args) => list_models(client, &args, format),
        },
        AuthorizationCommands::Relationships { command } => match command {
            AuthorizationRelationshipCommands::List(args) => {
                list_relationships(client, &args, format)
            }
        },
    }
}

pub fn list_subjects(client: &ApiClient, format: Format) -> Result<()> {
    let resp = client
        .get("/api/v1/authorization/subjects")
        .context("failed to list service accounts")?;
    print_array_response(
        &resp,
        format,
        &["ID", "Subject ID", "Display Name"],
        subject_row,
    )
}

pub fn create_subject(
    client: &ApiClient,
    args: &AuthorizationSubjectCreateArgs,
    format: Format,
) -> Result<()> {
    let subject = canonical_service_account_subject(&args.subject)?;
    let mut body = object();
    if let Some(id) = subject.strip_prefix(SUBJECT_PREFIX) {
        body.insert("id".to_string(), json!(id));
    }
    insert_if_some(&mut body, "displayName", args.display_name.as_deref());
    insert_if_some(&mut body, "description", args.description.as_deref());

    let resp = client
        .post("/api/v1/authorization/subjects", &body)
        .context("failed to create service account")?;
    print_single_response(
        &resp,
        format,
        &["ID", "Subject ID", "Display Name"],
        subject_row,
    )
}

pub fn get_subject(client: &ApiClient, subject: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&subject_path(subject)?)
        .context("failed to get service account")?;
    print_single_response(
        &resp,
        format,
        &["ID", "Subject ID", "Display Name"],
        subject_row,
    )
}

pub fn update_subject(
    client: &ApiClient,
    args: &AuthorizationSubjectUpdateArgs,
    format: Format,
) -> Result<()> {
    let mut body = object();
    insert_if_some(&mut body, "displayName", args.display_name.as_deref());
    insert_if_some(&mut body, "description", args.description.as_deref());
    if body.is_empty() {
        bail!("provide --display-name or --description");
    }
    let resp = client
        .patch(&subject_path(&args.subject)?, &body)
        .context("failed to update service account")?;
    print_single_response(
        &resp,
        format,
        &["ID", "Subject ID", "Display Name"],
        subject_row,
    )
}

pub fn delete_subject(client: &ApiClient, subject: &str, format: Format) -> Result<()> {
    let canonical = canonical_service_account_subject(subject)?;
    let resp = client
        .delete(&encoded_subject_path(&canonical))
        .context("failed to delete service account")?;
    print_status(&resp, format, &format!("Deleted {canonical}."))
}

pub fn list_subject_members(client: &ApiClient, subject: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("{}/members", subject_path(subject)?))
        .context("failed to list service account members")?;
    print_array_response(&resp, format, &["Subject ID", "Role", "Email"], member_row)
}

pub fn set_subject_member(
    client: &ApiClient,
    subject: &str,
    subject_id: Option<&str>,
    email: Option<&str>,
    role: AuthorizationManagedSubjectRole,
    format: Format,
) -> Result<()> {
    let mut body = subject_selector_body(subject_id, email)?;
    body.insert("role".to_string(), json!(managed_subject_role(role)));
    let resp = client
        .put(&format!("{}/members", subject_path(subject)?), &body)
        .context("failed to set service account member")?;
    print_single_response(&resp, format, &["Subject ID", "Role", "Email"], member_row)
}

pub fn remove_subject_member(
    client: &ApiClient,
    subject: &str,
    member_subject_id: &str,
    format: Format,
) -> Result<()> {
    let member_subject_id = canonical_non_system_subject(member_subject_id)?;
    let resp = client
        .delete(&format!(
            "{}/members/{}",
            subject_path(subject)?,
            encode_path_segment(&member_subject_id)
        ))
        .context("failed to remove service account member")?;
    print_status(
        &resp,
        format,
        &format!("Removed {member_subject_id} from {subject}."),
    )
}

pub fn list_subject_grants(client: &ApiClient, subject: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("{}/grants", subject_path(subject)?))
        .context("failed to list service account grants")?;
    print_array_response(
        &resp,
        format,
        &["Plugin", "Role", "Source", "Mutable"],
        grant_row,
    )
}

pub fn set_subject_grant(
    client: &ApiClient,
    subject: &str,
    plugin: &str,
    role: &str,
    format: Format,
) -> Result<()> {
    let role = non_empty("role", role)?;
    let resp = client
        .put(
            &format!(
                "{}/grants/{}",
                subject_path(subject)?,
                encode_path_segment(plugin)
            ),
            &json!({ "role": role }),
        )
        .context("failed to set service account grant")?;
    print_grant_write_response(&resp, format)
}

pub fn remove_subject_grant(
    client: &ApiClient,
    subject: &str,
    plugin: &str,
    format: Format,
) -> Result<()> {
    let resp = client
        .delete(&format!(
            "{}/grants/{}",
            subject_path(subject)?,
            encode_path_segment(plugin)
        ))
        .context("failed to remove service account grant")?;
    print_status(
        &resp,
        format,
        &format!("Removed {plugin} grant from {subject}."),
    )
}

pub fn list_subject_external_identities(
    client: &ApiClient,
    subject: &str,
    format: Format,
) -> Result<()> {
    let resp = client
        .get(&format!("{}/external-identities", subject_path(subject)?))
        .context("failed to list service account external identities")?;
    print_array_response(
        &resp,
        format,
        &["Type", "ID", "Resource ID"],
        external_identity_row,
    )
}

pub fn add_subject_external_identity(
    client: &ApiClient,
    subject: &str,
    identity_type: &str,
    id: &str,
    format: Format,
) -> Result<()> {
    write_subject_external_identity(
        client,
        subject,
        identity_type,
        id,
        http_method::Put,
        format,
        "failed to add service account external identity",
    )
}

pub fn remove_subject_external_identity(
    client: &ApiClient,
    subject: &str,
    identity_type: &str,
    id: &str,
    format: Format,
) -> Result<()> {
    write_subject_external_identity(
        client,
        subject,
        identity_type,
        id,
        http_method::Delete,
        format,
        "failed to remove service account external identity",
    )
}

pub fn list_subject_integrations(client: &ApiClient, subject: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("{}/apps", subject_path(subject)?))
        .context("failed to list service account integrations")?;
    print_array_response(
        &resp,
        format,
        &["Name", "Description", "Status", "Connections"],
        integration_row,
    )
}

pub fn connect_subject_integration(
    client: &ApiClient,
    subject: &str,
    name: &str,
    connection: Option<&str>,
    instance: Option<&str>,
) -> Result<()> {
    let subject = canonical_service_account_subject(subject)?;
    apps::connect_managed_subject(client, &subject, name, connection, instance)
}

pub fn disconnect_subject_integration(
    client: &ApiClient,
    subject: &str,
    name: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    format: Format,
) -> Result<()> {
    let normalized_connection = connection.map(apps::canonical_connection_name);
    let mut params = Vec::new();
    if let Some(connection) = normalized_connection {
        params.push(("_connection".to_string(), connection.to_string()));
    }
    if let Some(instance) = instance {
        params.push(("_instance".to_string(), instance.to_string()));
    }
    let path = append_query(
        &format!(
            "{}/apps/{}",
            subject_path(subject)?,
            encode_path_segment(name)
        ),
        &params,
    )?;
    let resp = client
        .delete(&path)
        .context("failed to disconnect service account integration")?;
    print_status(&resp, format, &format!("Disconnected {name}."))
}

pub fn list_subject_tokens(client: &ApiClient, subject: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("{}/tokens", subject_path(subject)?))
        .context("failed to list service account tokens")?;
    print_array_response(
        &resp,
        format,
        &["ID", "Name", "Scopes", "Permissions", "Created"],
        token_row,
    )
}

pub fn create_subject_token(
    client: &ApiClient,
    args: &AuthorizationSubjectTokenCreateArgs,
    format: Format,
) -> Result<()> {
    let mut body = object();
    body.insert("name".to_string(), json!(non_empty("name", &args.name)?));
    if let Some(scopes) = args.scopes.as_deref() {
        body.insert("scopes".to_string(), json!(non_empty("scopes", scopes)?));
    }
    if let Some(permissions) = token_permissions(args)? {
        body.insert("permissions".to_string(), Value::Array(permissions));
    }

    let resp = client
        .post(&format!("{}/tokens", subject_path(&args.subject)?), &body)
        .context("failed to create service account token")?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            if let Some(token) = resp["token"].as_str() {
                output::print_success("Token created. Save it now; it won't be shown again.");
                println!("{token}");
            } else {
                output::print_json(&resp);
            }
        }
    }
    Ok(())
}

pub fn revoke_subject_token(
    client: &ApiClient,
    subject: &str,
    id: &str,
    format: Format,
) -> Result<()> {
    let resp = client
        .delete(&format!(
            "{}/tokens/{}",
            subject_path(subject)?,
            encode_path_segment(id)
        ))
        .context("failed to revoke service account token")?;
    print_status(&resp, format, &format!("Revoked token {id}."))
}

pub fn revoke_all_subject_tokens(client: &ApiClient, subject: &str, format: Format) -> Result<()> {
    let resp = client
        .delete(&format!("{}/tokens", subject_path(subject)?))
        .context("failed to revoke service account tokens")?;
    print_status(&resp, format, "Revoked all service account tokens.")
}

pub fn list_plugins(client: &ApiClient, format: Format) -> Result<()> {
    let resp = client
        .get("/admin/api/v1/authorization/apps")
        .context("failed to list authorization plugins")?;
    print_array_response(
        &resp,
        format,
        &["Name", "Policy", "Mounted UI"],
        authorization_plugin_row,
    )
}

pub fn list_plugin_members(client: &ApiClient, plugin: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!(
            "/admin/api/v1/authorization/apps/{}/members",
            encode_path_segment(plugin)
        ))
        .context("failed to list app members")?;
    print_array_response(
        &resp,
        format,
        &["Subject", "Role", "Source", "Effective", "Mutable"],
        admin_member_row,
    )
}

pub fn set_plugin_member(
    client: &ApiClient,
    plugin: &str,
    subject_id: Option<&str>,
    email: Option<&str>,
    role: &str,
    format: Format,
) -> Result<()> {
    let mut body = subject_selector_body(subject_id, email)?;
    body.insert("role".to_string(), json!(non_empty("role", role)?));
    let resp = client
        .put(
            &format!(
                "/admin/api/v1/authorization/apps/{}/members",
                encode_path_segment(plugin)
            ),
            &body,
        )
        .context("failed to set app member")?;
    print_admin_write_response(&resp, format)
}

pub fn remove_plugin_member(
    client: &ApiClient,
    plugin: &str,
    subject_id: &str,
    format: Format,
) -> Result<()> {
    let subject_id = canonical_non_system_subject(subject_id)?;
    let resp = client
        .delete(&format!(
            "/admin/api/v1/authorization/apps/{}/members/{}",
            encode_path_segment(plugin),
            encode_path_segment(&subject_id)
        ))
        .context("failed to remove app member")?;
    print_status(
        &resp,
        format,
        &format!("Removed {subject_id} from {plugin}."),
    )
}

pub fn list_admin_members(client: &ApiClient, format: Format) -> Result<()> {
    let resp = client
        .get("/admin/api/v1/authorization/admins/members")
        .context("failed to list admin members")?;
    print_array_response(
        &resp,
        format,
        &["Subject", "Role", "Source", "Effective", "Mutable"],
        admin_member_row,
    )
}

pub fn set_admin_member(
    client: &ApiClient,
    subject_id: Option<&str>,
    email: Option<&str>,
    role: &str,
    format: Format,
) -> Result<()> {
    let mut body = subject_selector_body(subject_id, email)?;
    body.insert("role".to_string(), json!(non_empty("role", role)?));
    let resp = client
        .put("/admin/api/v1/authorization/admins/members", &body)
        .context("failed to set admin member")?;
    print_admin_write_response(&resp, format)
}

pub fn remove_admin_member(client: &ApiClient, subject_id: &str, format: Format) -> Result<()> {
    let subject_id = canonical_non_system_subject(subject_id)?;
    let resp = client
        .delete(&format!(
            "/admin/api/v1/authorization/admins/members/{}",
            encode_path_segment(&subject_id)
        ))
        .context("failed to remove admin member")?;
    print_status(
        &resp,
        format,
        &format!("Removed admin member {subject_id}."),
    )
}

pub fn get_provider(client: &ApiClient, format: Format) -> Result<()> {
    let resp = client
        .get("/admin/api/v1/authorization/provider")
        .context("failed to get authorization provider")?;
    print_response(&resp, format)
}

pub fn list_models(client: &ApiClient, args: &AuthorizationPageArgs, format: Format) -> Result<()> {
    let path = append_query(
        "/admin/api/v1/authorization/models",
        &page_params(args.page_size, args.page_token.as_deref()),
    )?;
    let resp = client
        .get(&path)
        .context("failed to list authorization models")?;
    match format {
        Format::Json => {
            output::print_json(&resp);
            Ok(())
        }
        Format::Table => {
            if let Some(next_page_token) = resp["nextPageToken"].as_str() {
                eprintln!("Next page token: {next_page_token}");
            }
            print_array_response(
                resp.get("models").unwrap_or(&Value::Null),
                Format::Table,
                &["ID", "Version", "Created"],
                model_row,
            )
        }
    }
}

pub fn list_relationships(
    client: &ApiClient,
    args: &AuthorizationRelationshipListArgs,
    format: Format,
) -> Result<()> {
    let mut params = page_params(args.page_size, args.page_token.as_deref());
    push_param(&mut params, "subjectType", args.subject_type.as_deref());
    push_param(&mut params, "subjectId", args.subject_id.as_deref());
    push_param(&mut params, "relation", args.relation.as_deref());
    push_param(&mut params, "resourceType", args.resource_type.as_deref());
    push_param(&mut params, "resourceId", args.resource_id.as_deref());
    push_param(&mut params, "modelId", args.model_id.as_deref());
    let path = append_query("/admin/api/v1/authorization/relationships", &params)?;
    let resp = client
        .get(&path)
        .context("failed to list authorization relationships")?;
    match format {
        Format::Json => {
            output::print_json(&resp);
            Ok(())
        }
        Format::Table => {
            if let Some(model_id) = resp["modelId"].as_str() {
                eprintln!("Model: {model_id}");
            }
            if let Some(next_page_token) = resp["nextPageToken"].as_str() {
                eprintln!("Next page token: {next_page_token}");
            }
            print_array_response(
                resp.get("relationships").unwrap_or(&Value::Null),
                Format::Table,
                &["Subject", "Relation", "Resource", "Managed"],
                relationship_row,
            )
        }
    }
}

fn write_subject_external_identity(
    client: &ApiClient,
    subject: &str,
    identity_type: &str,
    id: &str,
    method: http_method,
    format: Format,
    context: &str,
) -> Result<()> {
    let body = json!({
        "type": non_empty("type", identity_type)?,
        "id": non_empty("id", id)?,
    });
    let path = format!("{}/external-identities", subject_path(subject)?);
    let resp = match method {
        http_method::Put => client.put(&path, &body),
        http_method::Delete => client.delete_json(&path, &body),
    }
    .context(context.to_string())?;
    match method {
        http_method::Put => print_single_response(
            &resp,
            format,
            &["Type", "ID", "Resource ID"],
            external_identity_row,
        ),
        http_method::Delete => print_status(&resp, format, "Removed external identity."),
    }
}

#[derive(Clone, Copy)]
#[allow(non_camel_case_types)]
enum http_method {
    Put,
    Delete,
}

fn subject_path(subject: &str) -> Result<String> {
    Ok(encoded_subject_path(&canonical_service_account_subject(
        subject,
    )?))
}

fn encoded_subject_path(subject: &str) -> String {
    format!(
        "/api/v1/authorization/subjects/{}",
        encode_path_segment(subject)
    )
}

fn canonical_service_account_subject(subject: &str) -> Result<String> {
    let subject = subject.trim();
    if subject.is_empty() {
        bail!("subject is required");
    }
    if let Some(local_id) = subject.strip_prefix(SUBJECT_PREFIX) {
        validate_service_account_local_id(local_id)?;
        return Ok(format!("{SUBJECT_PREFIX}{local_id}"));
    }
    validate_service_account_local_id(subject)?;
    Ok(format!("{SUBJECT_PREFIX}{subject}"))
}

fn validate_service_account_local_id(id: &str) -> Result<()> {
    if id.is_empty() || id.len() > 128 {
        bail!("service account id must be 1 to 128 characters");
    }
    if id
        .chars()
        .all(|ch| ch.is_ascii_alphanumeric() || matches!(ch, '.' | '_' | '-'))
    {
        Ok(())
    } else {
        bail!(
            "service account id may only contain letters, numbers, dots, underscores, and hyphens"
        )
    }
}

fn canonical_non_system_subject(subject_id: &str) -> Result<String> {
    let subject_id = subject_id.trim();
    let Some((kind, id)) = subject_id.split_once(':') else {
        bail!("subjectId must be a canonical subject ID");
    };
    if kind.trim().is_empty() || id.trim().is_empty() {
        bail!("subjectId must be a canonical subject ID");
    }
    if kind == "system" {
        bail!("subjectId must not use system:<id>");
    }
    Ok(format!("{kind}:{id}"))
}

fn subject_selector_body(
    subject_id: Option<&str>,
    email: Option<&str>,
) -> Result<Map<String, Value>> {
    match (subject_id, email) {
        (Some(_), Some(_)) => bail!("provide either --subject-id or --email, not both"),
        (Some(subject_id), None) => {
            let mut body = object();
            body.insert(
                "subjectId".to_string(),
                json!(canonical_non_system_subject(subject_id)?),
            );
            Ok(body)
        }
        (None, Some(email)) => {
            let email = non_empty("email", email)?;
            let mut body = object();
            body.insert("email".to_string(), json!(email));
            Ok(body)
        }
        (None, None) => bail!("provide --subject-id or --email"),
    }
}

fn managed_subject_role(role: AuthorizationManagedSubjectRole) -> &'static str {
    match role {
        AuthorizationManagedSubjectRole::Viewer => "viewer",
        AuthorizationManagedSubjectRole::Editor => "editor",
        AuthorizationManagedSubjectRole::Admin => "admin",
    }
}

fn token_permissions(args: &AuthorizationSubjectTokenCreateArgs) -> Result<Option<Vec<Value>>> {
    if let Some(path) = args.permissions_file.as_deref() {
        let value: Value = serde_json::from_str(&read_input(path)?)
            .with_context(|| format!("failed to parse permissions JSON from {path}"))?;
        let Value::Array(items) = value else {
            bail!("--permissions-file must contain a JSON permissions array");
        };
        return Ok(Some(items));
    }

    if args.permission.is_empty() && args.action.is_empty() {
        return Ok(None);
    }

    let mut by_app: BTreeMap<String, (Vec<String>, Vec<String>)> = BTreeMap::new();
    for value in &args.permission {
        let (plugin, operation) = parse_scoped_value(value, "permission")?;
        by_app.entry(plugin).or_default().0.push(operation);
    }
    for value in &args.action {
        let (plugin, action) = parse_scoped_value(value, "action")?;
        by_app.entry(plugin).or_default().1.push(action);
    }

    let permissions = by_app
        .into_iter()
        .map(|(plugin, (operations, actions))| {
            let mut permission = object();
            permission.insert("app".to_string(), json!(plugin));
            if !operations.is_empty() {
                permission.insert("operations".to_string(), json!(operations));
            }
            if !actions.is_empty() {
                permission.insert("actions".to_string(), json!(actions));
            }
            Value::Object(permission)
        })
        .collect();
    Ok(Some(permissions))
}

fn parse_scoped_value(value: &str, label: &str) -> Result<(String, String)> {
    let Some((plugin, name)) = value.split_once(':') else {
        bail!("--{label} must use plugin:name form");
    };
    Ok((
        non_empty("app", plugin)?.to_string(),
        non_empty(label, name)?.to_string(),
    ))
}

fn read_input(path: &str) -> Result<String> {
    if path == "-" {
        let mut input = String::new();
        std::io::stdin()
            .read_to_string(&mut input)
            .context("failed to read permissions from stdin")?;
        return Ok(input);
    }
    std::fs::read_to_string(path).with_context(|| format!("failed to read {path}"))
}

fn append_query(path: &str, params: &[(String, String)]) -> Result<String> {
    if params.is_empty() {
        return Ok(path.to_string());
    }
    Ok(format!(
        "{}?{}",
        path,
        serde_urlencoded::to_string(params).context("failed to encode query")?
    ))
}

fn page_params(page_size: Option<u32>, page_token: Option<&str>) -> Vec<(String, String)> {
    let mut params = Vec::new();
    if let Some(page_size) = page_size {
        params.push(("pageSize".to_string(), page_size.to_string()));
    }
    push_param(&mut params, "pageToken", page_token);
    params
}

fn push_param(params: &mut Vec<(String, String)>, name: &str, value: Option<&str>) {
    if let Some(value) = value.map(str::trim).filter(|value| !value.is_empty()) {
        params.push((name.to_string(), value.to_string()));
    }
}

fn object() -> Map<String, Value> {
    Map::new()
}

fn insert_if_some(body: &mut Map<String, Value>, key: &str, value: Option<&str>) {
    if let Some(value) = value {
        body.insert(key.to_string(), json!(value));
    }
}

fn non_empty<'a>(label: &str, value: &'a str) -> Result<&'a str> {
    let value = value.trim();
    if value.is_empty() {
        bail!("{label} is required");
    }
    Ok(value)
}

fn print_response(resp: &Value, format: Format) -> Result<()> {
    match format {
        Format::Json => output::print_json(resp),
        Format::Table => output::print_json_table(resp),
    }
    Ok(())
}

fn print_single_response(
    resp: &Value,
    format: Format,
    headers: &[&str],
    row: fn(&Value) -> Vec<String>,
) -> Result<()> {
    match format {
        Format::Json => output::print_json(resp),
        Format::Table => output::print_table(headers, &[row(resp)]),
    }
    Ok(())
}

fn print_array_response(
    resp: &Value,
    format: Format,
    headers: &[&str],
    row: fn(&Value) -> Vec<String>,
) -> Result<()> {
    match format {
        Format::Json => output::print_json(resp),
        Format::Table => {
            let Some(items) = resp.as_array() else {
                output::print_json_table(resp);
                return Ok(());
            };
            let rows = items.iter().map(row).collect::<Vec<_>>();
            output::print_table(headers, &rows);
        }
    }
    Ok(())
}

fn print_admin_write_response(resp: &Value, format: Format) -> Result<()> {
    match format {
        Format::Json => output::print_json(resp),
        Format::Table => {
            if let Some(membership) = resp.get("membership") {
                print_single_response(
                    membership,
                    Format::Table,
                    &["Subject", "Role", "Source", "Effective", "Mutable"],
                    admin_member_row,
                )?;
            } else {
                output::print_json_table(resp);
            }
        }
    }
    Ok(())
}

fn print_grant_write_response(resp: &Value, format: Format) -> Result<()> {
    match format {
        Format::Json => output::print_json(resp),
        Format::Table => {
            if resp["reloaded"].as_bool() == Some(false) {
                output::print_warning(
                    "Grant persisted, but the local authorization snapshot has not reloaded yet.",
                );
            }
            if let Some(grant) = resp.get("grant") {
                print_single_response(
                    grant,
                    Format::Table,
                    &["Plugin", "Role", "Source", "Mutable"],
                    grant_row,
                )?;
            } else {
                print_single_response(
                    resp,
                    Format::Table,
                    &["Plugin", "Role", "Source", "Mutable"],
                    grant_row,
                )?;
            }
        }
    }
    Ok(())
}

fn print_status(resp: &Value, format: Format, message: &str) -> Result<()> {
    match format {
        Format::Json => output::print_json(resp),
        Format::Table => output::print_success(message),
    }
    Ok(())
}

fn subject_row(item: &Value) -> Vec<String> {
    vec![
        string_cell(item, "id"),
        string_cell(item, "subjectId"),
        string_cell(item, "displayName"),
    ]
}

fn member_row(item: &Value) -> Vec<String> {
    vec![
        string_cell(item, "subjectId"),
        string_cell(item, "role"),
        string_cell(item, "email"),
    ]
}

fn grant_row(item: &Value) -> Vec<String> {
    vec![
        string_cell(item, "app"),
        string_cell(item, "role"),
        string_cell(item, "source"),
        bool_cell(item, "mutable"),
    ]
}

fn external_identity_row(item: &Value) -> Vec<String> {
    vec![
        string_cell(item, "type"),
        string_cell(item, "id"),
        string_cell(item, "resourceId"),
    ]
}

fn integration_row(item: &Value) -> Vec<String> {
    vec![
        string_cell(item, "name"),
        string_cell(item, "description"),
        string_cell(item, "status"),
        item["connections"]
            .as_array()
            .map(|connections| {
                connections
                    .iter()
                    .map(|connection| {
                        format!(
                            "{}: {}",
                            string_cell(connection, "name"),
                            string_cell(connection, "status")
                        )
                    })
                    .collect::<Vec<_>>()
                    .join(", ")
            })
            .filter(|value| !value.is_empty())
            .unwrap_or_else(|| "-".to_string()),
    ]
}

fn token_row(item: &Value) -> Vec<String> {
    vec![
        string_cell(item, "id"),
        string_cell(item, "name"),
        string_cell(item, "scopes"),
        permissions_cell(item),
        string_cell(item, "createdAt"),
    ]
}

fn permissions_cell(item: &Value) -> String {
    let Some(permissions) = item["permissions"]
        .as_array()
        .filter(|items| !items.is_empty())
    else {
        return "-".to_string();
    };
    let formatted = permissions
        .iter()
        .map(permission_cell)
        .filter(|value| !value.is_empty())
        .collect::<Vec<_>>()
        .join("; ");
    if formatted.is_empty() {
        "-".to_string()
    } else {
        formatted
    }
}

fn permission_cell(item: &Value) -> String {
    let app = string_cell(item, "app");
    let operations = string_array_cell(item, "operations");
    let actions = string_array_cell(item, "actions");
    let mut parts = Vec::new();
    if operations != "-" {
        parts.push(format!("operations={operations}"));
    }
    if actions != "-" {
        parts.push(format!("actions={actions}"));
    }
    if parts.is_empty() {
        app
    } else {
        format!("{} ({})", app, parts.join(", "))
    }
}

fn authorization_plugin_row(item: &Value) -> Vec<String> {
    vec![
        string_cell(item, "name"),
        string_cell(item, "authorizationPolicy"),
        string_cell(item, "mountedUiPath"),
    ]
}

fn admin_member_row(item: &Value) -> Vec<String> {
    vec![
        string_cell(item, "selectorValue"),
        string_cell(item, "role"),
        string_cell(item, "source"),
        bool_cell(item, "effective"),
        bool_cell(item, "mutable"),
    ]
}

fn model_row(item: &Value) -> Vec<String> {
    vec![
        string_cell(item, "id"),
        string_cell(item, "version"),
        string_cell(item, "createdAt"),
    ]
}

fn relationship_row(item: &Value) -> Vec<String> {
    vec![
        ref_cell(item.get("subject")),
        string_cell(item, "relation"),
        ref_cell(item.get("resource")),
        bool_cell(item, "managed"),
    ]
}

fn ref_cell(value: Option<&Value>) -> String {
    let Some(value) = value else {
        return "-".to_string();
    };
    let kind = string_cell(value, "type");
    let id = string_cell(value, "id");
    if kind == "-" && id == "-" {
        "-".to_string()
    } else {
        format!("{kind}:{id}")
    }
}

fn string_cell(item: &Value, key: &str) -> String {
    item.get(key)
        .and_then(Value::as_str)
        .filter(|value| !value.is_empty())
        .unwrap_or("-")
        .to_string()
}

fn string_array_cell(item: &Value, key: &str) -> String {
    let Some(values) = item[key].as_array().filter(|items| !items.is_empty()) else {
        return "-".to_string();
    };
    let joined = values
        .iter()
        .filter_map(Value::as_str)
        .filter(|value| !value.is_empty())
        .collect::<Vec<_>>()
        .join(",");
    if joined.is_empty() {
        "-".to_string()
    } else {
        joined
    }
}

fn bool_cell(item: &Value, key: &str) -> String {
    item.get(key)
        .and_then(Value::as_bool)
        .map(|value| value.to_string())
        .unwrap_or_else(|| "-".to_string())
}
