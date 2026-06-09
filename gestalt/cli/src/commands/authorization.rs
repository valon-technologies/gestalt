use anyhow::{Context, Result};
use serde_json::{Map, Value, json};

use crate::api::ApiClient;
use crate::cli::{
    AuthorizationActiveModelCommands, AuthorizationActiveModelResourceTypeCommands,
    AuthorizationCommands, AuthorizationModelCommands, AuthorizationRelationshipCommands,
};
use crate::output::{self, Format};
use crate::params;
use crate::query;

const CHECK_ACCESS_PATH: &str = "/api/v1/authorization/check-access";
const RELATIONSHIPS_PATH: &str = "/api/v1/authorization/relationships";
const ACTIVE_MODEL_PATH: &str = "/api/v1/authorization/models/active";
const ACTIVE_MODEL_RESOURCE_TYPES_PATH: &str = "/api/v1/authorization/models/active/resource-types";

pub fn dispatch(client: &ApiClient, command: AuthorizationCommands, format: Format) -> Result<()> {
    match command {
        AuthorizationCommands::CheckAccess(args) => {
            let body = json!({
                "subject": {
                    "type": args.subject_type,
                    "id": args.subject_id,
                },
                "action": {
                    "name": args.action,
                },
                "resource": {
                    "type": args.resource_type,
                    "id": args.resource_id,
                },
            });
            let resp = client
                .post(CHECK_ACCESS_PATH, &body)
                .context("failed to check authorization access")?;
            print_value(&resp, format);
            Ok(())
        }
        AuthorizationCommands::Relationships { command } => match command {
            AuthorizationRelationshipCommands::List(args) => {
                let path = relationships_list_path(&args)?;
                let resp = client
                    .get(&path)
                    .context("failed to list authorization relationships")?;
                print_value(&resp, format);
                Ok(())
            }
            AuthorizationRelationshipCommands::Add(args) => {
                let body = Value::Object(params::load_input_file(&args.input_file)?);
                let resp = client
                    .post(RELATIONSHIPS_PATH, &body)
                    .context("failed to add authorization relationship")?;
                print_value(&resp, format);
                Ok(())
            }
            AuthorizationRelationshipCommands::Delete(args) => {
                let body = json!({
                    "relationshipTuple": {
                        "target": {
                            "subject": {
                                "type": args.target_subject_type,
                                "id": args.target_subject_id,
                            },
                        },
                        "relation": args.relation,
                        "resource": {
                            "type": args.resource_type,
                            "id": args.resource_id,
                        },
                    },
                });
                let resp = client
                    .delete_json(RELATIONSHIPS_PATH, &body)
                    .context("failed to delete authorization relationship")?;
                print_value(&resp, format);
                Ok(())
            }
        },
        AuthorizationCommands::Models { command } => match command {
            AuthorizationModelCommands::Active { command } => match command {
                AuthorizationActiveModelCommands::Get => {
                    let resp = client
                        .get(ACTIVE_MODEL_PATH)
                        .context("failed to get active authorization model")?;
                    print_value(&resp, format);
                    Ok(())
                }
                AuthorizationActiveModelCommands::ResourceTypes { command } => match command {
                    AuthorizationActiveModelResourceTypeCommands::List(args) => {
                        let path = active_model_resource_types_path(
                            args.name.as_deref(),
                            args.source_layer.as_deref(),
                            args.page_size,
                            args.page_token.as_deref(),
                        )?;
                        let resp = client
                            .get(&path)
                            .context("failed to list active authorization model resource types")?;
                        print_value(&resp, format);
                        Ok(())
                    }
                },
            },
        },
    }
}

fn relationships_list_path(args: &crate::cli::AuthorizationRelationshipListArgs) -> Result<String> {
    let input = match args.input_file.as_deref() {
        Some(path) => Some(Value::Object(params::load_input_file(path)?)),
        None => None,
    };
    let mut params = Vec::new();
    query::push_opt_param(
        &mut params,
        "subjectId",
        args.subject_id
            .as_deref()
            .or_else(|| json_path_str(input.as_ref(), &["filter", "target", "subject", "id"])),
    );
    query::push_opt_param(
        &mut params,
        "subjectType",
        args.subject_type
            .as_deref()
            .or_else(|| json_path_str(input.as_ref(), &["filter", "target", "subject", "type"])),
    );
    query::push_opt_param(
        &mut params,
        "relation",
        args.relation
            .as_deref()
            .or_else(|| json_path_str(input.as_ref(), &["filter", "relation"])),
    );
    query::push_opt_param(
        &mut params,
        "resourceType",
        args.resource_type
            .as_deref()
            .or_else(|| json_path_str(input.as_ref(), &["filter", "resourceType"]))
            .or_else(|| json_path_str(input.as_ref(), &["filter", "resource", "type"])),
    );
    query::push_opt_param(
        &mut params,
        "resourceId",
        args.resource_id
            .as_deref()
            .or_else(|| json_path_str(input.as_ref(), &["filter", "resource", "id"])),
    );
    query::push_opt_param(
        &mut params,
        "sourceLayer",
        args.source_layer
            .as_deref()
            .or_else(|| json_path_str(input.as_ref(), &["filter", "sourceLayer"])),
    );
    query::push_opt_u32(
        &mut params,
        "pageSize",
        args.page_size
            .or_else(|| json_path_u32(input.as_ref(), &["pageSize"])),
    );
    query::push_opt_param(
        &mut params,
        "pageToken",
        args.page_token
            .as_deref()
            .or_else(|| json_path_str(input.as_ref(), &["pageToken"])),
    );
    query::append_query(RELATIONSHIPS_PATH, &params)
}

fn active_model_resource_types_path(
    name: Option<&str>,
    source_layer: Option<&str>,
    page_size: Option<u32>,
    page_token: Option<&str>,
) -> Result<String> {
    let mut params = Vec::new();
    query::push_opt_param(&mut params, "name", name);
    query::push_opt_param(&mut params, "sourceLayer", source_layer);
    query::push_opt_u32(&mut params, "pageSize", page_size);
    query::push_opt_param(&mut params, "pageToken", page_token);
    query::append_query(ACTIVE_MODEL_RESOURCE_TYPES_PATH, &params)
}

fn print_value(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => output::print_json_table(&table_value(value)),
    }
}

fn table_value(value: &Value) -> Value {
    if let Some(items) = value.get("relationships").and_then(Value::as_array) {
        return Value::Array(items.clone());
    }
    if let Some(items) = value.get("resourceTypes").and_then(Value::as_array) {
        return Value::Array(items.clone());
    }
    if let Some(model) = value.get("model").and_then(Value::as_object) {
        return Value::Object(model.clone());
    }
    Value::Object(value.as_object().cloned().unwrap_or_else(Map::new))
}

fn json_path_str<'a>(value: Option<&'a Value>, path: &[&str]) -> Option<&'a str> {
    let mut current = value?;
    for segment in path {
        current = current.get(*segment)?;
    }
    current.as_str()
}

fn json_path_u32(value: Option<&Value>, path: &[&str]) -> Option<u32> {
    let mut current = value?;
    for segment in path {
        current = current.get(*segment)?;
    }
    current.as_u64().and_then(|value| u32::try_from(value).ok())
}
