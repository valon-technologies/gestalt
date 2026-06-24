use anyhow::{Context, Result};
use serde_json::{Value, json};

use crate::api::ApiClient;
use crate::cli::{
    AuthorizationActiveModelCommands, AuthorizationActiveModelResourceTypeCommands,
    AuthorizationCommands, AuthorizationModelCommands, AuthorizationRelationshipCommands,
};
use crate::output::{self, Format};
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
    let mut params = Vec::new();
    query::push_opt_param(&mut params, "subjectId", args.subject_id.as_deref());
    query::push_opt_param(&mut params, "subjectType", args.subject_type.as_deref());
    query::push_opt_param(&mut params, "relation", args.relation.as_deref());
    query::push_opt_param(&mut params, "resourceType", args.resource_type.as_deref());
    query::push_opt_param(&mut params, "resourceId", args.resource_id.as_deref());
    query::push_opt_param(&mut params, "sourceLayer", args.source_layer.as_deref());
    query::push_opt_u32(&mut params, "pageSize", args.page_size);
    query::push_opt_param(&mut params, "pageToken", args.page_token.as_deref());
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
        Format::Table => {
            output::print_json_table(&table_value(value));
            if let Some(token) = next_page_token(value) {
                eprintln!("Next page token: {token}");
            }
            if let Some(model_id) = model_id(value) {
                eprintln!("Model ID: {model_id}");
            }
        }
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
    value.clone()
}

fn next_page_token(value: &Value) -> Option<&str> {
    value
        .get("nextPageToken")
        .and_then(Value::as_str)
        .map(str::trim)
        .filter(|token| !token.is_empty())
}

fn model_id(value: &Value) -> Option<&str> {
    value
        .get("modelId")
        .and_then(Value::as_str)
        .map(str::trim)
        .filter(|model_id| !model_id.is_empty())
}

#[cfg(test)]
mod tests {
    use serde_json::json;

    use super::{model_id, next_page_token};

    #[test]
    fn next_page_token_returns_trimmed_non_empty_token() {
        let value = json!({"nextPageToken": " next "});

        assert_eq!(next_page_token(&value), Some("next"));
    }

    #[test]
    fn next_page_token_ignores_blank_tokens() {
        let value = json!({"nextPageToken": " "});

        assert_eq!(next_page_token(&value), None);
    }

    #[test]
    fn model_id_returns_trimmed_non_empty_id() {
        let value = json!({"modelId": " model-1 "});

        assert_eq!(model_id(&value), Some("model-1"));
    }
}
