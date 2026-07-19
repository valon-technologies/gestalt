use anyhow::{Context, Result};
use serde_json::Value;

use crate::cli::{
    AuthorizationActiveModelCommands, AuthorizationActiveModelResourceTypeCommands,
    AuthorizationCommands, AuthorizationModelCommands, AuthorizationRelationshipCommands,
};
use crate::output::{self, Format};

use gestalt_sdk::public::generated::app_client::AuthorizationClient;
use gestalt_sdk::public::generated::authorization::source_layer::{
    SOURCE_LAYER_RUNTIME, SOURCE_LAYER_STATIC_CONFIG, SOURCE_LAYER_UNSPECIFIED,
};
use gestalt_sdk::public::generated::authorization::{
    Action, AuthorizationModelResourceTypeFilter, CheckAccessRequest,
    ListActiveModelResourceTypesRequest, ListRelationshipsRequest, RelationshipFilter,
    RelationshipTarget, RelationshipTargetKind, Resource, Subject,
};
use gestalt_sdk::public::rest_transport::SyncRestTransport;

pub fn dispatch(
    authz: &AuthorizationClient<SyncRestTransport>,
    command: AuthorizationCommands,
    format: Format,
) -> Result<()> {
    match command {
        AuthorizationCommands::CheckAccess(args) => {
            let request = CheckAccessRequest {
                subject: Some(Subject {
                    r#type: args.subject_type.trim().to_string(),
                    id: args.subject_id.trim().to_string(),
                    properties: None,
                }),
                action: Some(Action {
                    name: args.action.trim().to_string(),
                    properties: None,
                }),
                resource: Some(Resource {
                    r#type: args.resource_type.trim().to_string(),
                    id: args.resource_id.trim().to_string(),
                    properties: None,
                }),
            };
            let resp = authz
                .check_access_sync(request)
                .context("failed to check authorization access")?;
            print_value(&serde_json::to_value(&resp)?, format);
            Ok(())
        }
        AuthorizationCommands::Relationships { command } => match command {
            AuthorizationRelationshipCommands::List(args) => {
                let request = build_list_relationships_request(&args);
                let resp = authz
                    .list_relationships_sync(request)
                    .context("failed to list authorization relationships")?;
                print_value(&serde_json::to_value(&resp)?, format);
                Ok(())
            }
        },
        AuthorizationCommands::Models { command } => match command {
            AuthorizationModelCommands::Active { command } => match command {
                AuthorizationActiveModelCommands::Get => {
                    let resp = authz
                        .get_active_model_ref_sync()
                        .context("failed to get active authorization model")?;
                    print_value(&serde_json::to_value(&resp)?, format);
                    Ok(())
                }
                AuthorizationActiveModelCommands::ResourceTypes { command } => match command {
                    AuthorizationActiveModelResourceTypeCommands::List(args) => {
                        let request = ListActiveModelResourceTypesRequest {
                            filter: Some(AuthorizationModelResourceTypeFilter {
                                name: args.name.as_deref().unwrap_or_default().trim().to_string(),
                                source_layer: parse_source_layer(args.source_layer.as_deref()),
                            }),
                            page_size: args.page_size.unwrap_or(0) as i32,
                            page_token: args
                                .page_token
                                .as_deref()
                                .unwrap_or_default()
                                .trim()
                                .to_string(),
                        };
                        let resp = authz
                            .list_active_model_resource_types_sync(request)
                            .context("failed to list active authorization model resource types")?;
                        print_value(&serde_json::to_value(&resp)?, format);
                        Ok(())
                    }
                },
            },
        },
    }
}

fn build_list_relationships_request(
    args: &crate::cli::AuthorizationRelationshipListArgs,
) -> ListRelationshipsRequest {
    let mut filter = RelationshipFilter {
        relation: args
            .relation
            .as_deref()
            .unwrap_or_default()
            .trim()
            .to_string(),
        target: None,
        resource: None,
        target_type: 0,
        target_entity_type: String::new(),
        resource_type: String::new(),
        source_layer: parse_source_layer(args.source_layer.as_deref()),
    };
    if let (Some(subject_type), Some(subject_id)) =
        (args.subject_type.as_deref(), args.subject_id.as_deref())
    {
        filter.target = Some(RelationshipTarget {
            kind: Some(RelationshipTargetKind::Subject(Subject {
                r#type: subject_type.trim().to_string(),
                id: subject_id.trim().to_string(),
                properties: None,
            })),
        });
    }
    if let (Some(resource_type), Some(resource_id)) =
        (args.resource_type.as_deref(), args.resource_id.as_deref())
    {
        filter.resource = Some(Resource {
            r#type: resource_type.trim().to_string(),
            id: resource_id.trim().to_string(),
            properties: None,
        });
    }
    ListRelationshipsRequest {
        filter: Some(filter),
        page_size: args.page_size.unwrap_or(0) as i32,
        page_token: args
            .page_token
            .as_deref()
            .unwrap_or_default()
            .trim()
            .to_string(),
    }
}

fn parse_source_layer(value: Option<&str>) -> i32 {
    match value.map(str::trim).filter(|v| !v.is_empty()) {
        Some("static_config") | Some("staticconfig") => SOURCE_LAYER_STATIC_CONFIG,
        Some("runtime") => SOURCE_LAYER_RUNTIME,
        _ => SOURCE_LAYER_UNSPECIFIED,
    }
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
