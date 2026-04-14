use std::collections::BTreeSet;
use std::path::Path;

use schemars::JsonSchema;
use serde_json::{Value as JsonValue, json};

use crate::error::{Error, Result};
use crate::generated::v1;

pub type Catalog = v1::Catalog;
pub type CatalogOperation = v1::CatalogOperation;
pub type CatalogParameter = v1::CatalogParameter;

impl Catalog {
    pub fn with_name(mut self, name: impl Into<String>) -> Self {
        let name = name.into();
        if !name.trim().is_empty() {
            self.name = name;
        }
        self
    }
}

pub fn write_catalog(catalog: &Catalog, path: impl AsRef<Path>) -> Result<()> {
    let path = path.as_ref();
    if let Some(parent) = path.parent()
        && !parent.as_os_str().is_empty()
    {
        std::fs::create_dir_all(parent)?;
    }
    let json = serde_json::to_string_pretty(&catalog_to_json_value(catalog))?;
    std::fs::write(path, json)?;
    Ok(())
}

fn catalog_to_json_value(catalog: &Catalog) -> JsonValue {
    let mut obj = serde_json::Map::new();
    obj.insert("name".to_owned(), json!(catalog.name));
    if !catalog.display_name.is_empty() {
        obj.insert("displayName".to_owned(), json!(catalog.display_name));
    }
    if !catalog.description.is_empty() {
        obj.insert("description".to_owned(), json!(catalog.description));
    }
    if !catalog.icon_svg.is_empty() {
        obj.insert("iconSvg".to_owned(), json!(catalog.icon_svg));
    }
    let ops: Vec<JsonValue> = catalog
        .operations
        .iter()
        .map(operation_to_json_value)
        .collect();
    obj.insert("operations".to_owned(), json!(ops));
    JsonValue::Object(obj)
}

fn operation_to_json_value(op: &CatalogOperation) -> JsonValue {
    let mut obj = serde_json::Map::new();
    obj.insert("id".to_owned(), json!(op.id));
    obj.insert("method".to_owned(), json!(op.method));
    if !op.title.is_empty() {
        obj.insert("title".to_owned(), json!(op.title));
    }
    if !op.description.is_empty() {
        obj.insert("description".to_owned(), json!(op.description));
    }
    if !op.input_schema.is_empty() {
        if let Ok(schema) = serde_json::from_str::<JsonValue>(&op.input_schema) {
            obj.insert("inputSchema".to_owned(), schema);
        }
    }
    if !op.output_schema.is_empty() {
        if let Ok(schema) = serde_json::from_str::<JsonValue>(&op.output_schema) {
            obj.insert("outputSchema".to_owned(), schema);
        }
    }
    if !op.tags.is_empty() {
        obj.insert("tags".to_owned(), json!(op.tags));
    }
    if !op.required_scopes.is_empty() {
        obj.insert("requiredScopes".to_owned(), json!(op.required_scopes));
    }
    if op.read_only {
        obj.insert("readOnly".to_owned(), json!(true));
    }
    if let Some(visible) = op.visible {
        obj.insert("visible".to_owned(), json!(visible));
    }
    if !op.transport.is_empty() {
        obj.insert("transport".to_owned(), json!(op.transport));
    }
    if !op.allowed_roles.is_empty() {
        obj.insert("allowedRoles".to_owned(), json!(op.allowed_roles));
    }
    if !op.parameters.is_empty() {
        let params: Vec<JsonValue> = op
            .parameters
            .iter()
            .map(|p| {
                let mut m = serde_json::Map::new();
                m.insert("name".to_owned(), json!(p.name));
                m.insert("type".to_owned(), json!(p.r#type));
                if !p.description.is_empty() {
                    m.insert("description".to_owned(), json!(p.description));
                }
                if p.required {
                    m.insert("required".to_owned(), json!(true));
                }
                if let Some(ref default) = p.default {
                    let val = proto_value_to_json(default.clone());
                    m.insert("default".to_owned(), val);
                }
                JsonValue::Object(m)
            })
            .collect();
        obj.insert("parameters".to_owned(), json!(params));
    }
    if let Some(ref ann) = op.annotations {
        let mut a = serde_json::Map::new();
        if let Some(v) = ann.read_only_hint {
            a.insert("readOnlyHint".to_owned(), json!(v));
        }
        if let Some(v) = ann.idempotent_hint {
            a.insert("idempotentHint".to_owned(), json!(v));
        }
        if let Some(v) = ann.destructive_hint {
            a.insert("destructiveHint".to_owned(), json!(v));
        }
        if let Some(v) = ann.open_world_hint {
            a.insert("openWorldHint".to_owned(), json!(v));
        }
        if !a.is_empty() {
            obj.insert("annotations".to_owned(), JsonValue::Object(a));
        }
    }
    JsonValue::Object(obj)
}

pub(crate) fn schema_json<T: JsonSchema>() -> Result<JsonValue> {
    serde_json::to_value(schemars::schema_for!(T)).map_err(Error::from)
}

pub(crate) fn schema_parameters(schema: &JsonValue) -> Vec<CatalogParameter> {
    let required = schema
        .get("required")
        .and_then(JsonValue::as_array)
        .map(|items| {
            items
                .iter()
                .filter_map(JsonValue::as_str)
                .map(ToOwned::to_owned)
                .collect::<BTreeSet<_>>()
        })
        .unwrap_or_default();

    let Some(properties) = schema.get("properties").and_then(JsonValue::as_object) else {
        return Vec::new();
    };

    properties
        .iter()
        .map(|(name, property)| CatalogParameter {
            name: name.clone(),
            r#type: schema_type(property),
            description: property
                .get("description")
                .and_then(JsonValue::as_str)
                .unwrap_or_default()
                .trim()
                .to_owned(),
            required: required.contains(name),
            default: property.get("default").map(json_value_to_proto_value),
        })
        .collect()
}

fn json_value_to_proto_value(value: &JsonValue) -> prost_types::Value {
    match value {
        JsonValue::Null => prost_types::Value {
            kind: Some(prost_types::value::Kind::NullValue(0)),
        },
        JsonValue::Bool(b) => prost_types::Value {
            kind: Some(prost_types::value::Kind::BoolValue(*b)),
        },
        JsonValue::Number(n) => prost_types::Value {
            kind: Some(prost_types::value::Kind::NumberValue(
                n.as_f64().unwrap_or(0.0),
            )),
        },
        JsonValue::String(s) => prost_types::Value {
            kind: Some(prost_types::value::Kind::StringValue(s.clone())),
        },
        JsonValue::Array(items) => prost_types::Value {
            kind: Some(prost_types::value::Kind::ListValue(
                prost_types::ListValue {
                    values: items.iter().map(json_value_to_proto_value).collect(),
                },
            )),
        },
        JsonValue::Object(map) => prost_types::Value {
            kind: Some(prost_types::value::Kind::StructValue(prost_types::Struct {
                fields: map
                    .iter()
                    .map(|(k, v)| (k.clone(), json_value_to_proto_value(v)))
                    .collect(),
            })),
        },
    }
}

pub(crate) fn object_map(value: Option<prost_types::Struct>) -> serde_json::Map<String, JsonValue> {
    value
        .map(|structure| {
            structure
                .fields
                .into_iter()
                .map(|(key, value)| (key, proto_value_to_json(value)))
                .collect::<serde_json::Map<_, _>>()
        })
        .unwrap_or_default()
}

pub(crate) fn proto_value_to_json(value: prost_types::Value) -> JsonValue {
    match value.kind {
        Some(prost_types::value::Kind::NullValue(_)) | None => JsonValue::Null,
        Some(prost_types::value::Kind::NumberValue(number)) => json!(number),
        Some(prost_types::value::Kind::StringValue(text)) => json!(text),
        Some(prost_types::value::Kind::BoolValue(flag)) => json!(flag),
        Some(prost_types::value::Kind::StructValue(structure)) => {
            JsonValue::Object(object_map(Some(structure)))
        }
        Some(prost_types::value::Kind::ListValue(list)) => {
            JsonValue::Array(list.values.into_iter().map(proto_value_to_json).collect())
        }
    }
}

fn schema_type(schema: &JsonValue) -> String {
    if schema.get("properties").is_some() {
        return "object".to_owned();
    }
    if schema.get("items").is_some() {
        return "array".to_owned();
    }
    match schema.get("type") {
        Some(JsonValue::String(value)) => normalize_type(value).to_owned(),
        Some(JsonValue::Array(values)) => values
            .iter()
            .filter_map(JsonValue::as_str)
            .find(|value| *value != "null")
            .map(|value| normalize_type(value).to_owned())
            .unwrap_or_else(|| "object".to_owned()),
        _ => "object".to_owned(),
    }
}

fn normalize_type(value: &str) -> &'static str {
    match value {
        "integer" => "integer",
        "number" => "number",
        "boolean" => "boolean",
        "array" => "array",
        "object" => "object",
        _ => "string",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[derive(serde::Deserialize, schemars::JsonSchema)]
    struct SampleInput {
        #[allow(dead_code)]
        #[schemars(description = "Search query")]
        query: String,
        #[allow(dead_code)]
        #[serde(default)]
        max_items: Option<u32>,
    }

    #[test]
    fn schema_parameters_derive_required_and_optional_fields() {
        let schema = schema_json::<SampleInput>().expect("schema");
        let mut params = schema_parameters(&schema);
        params.sort_by(|left, right| left.name.cmp(&right.name));

        assert_eq!(params.len(), 2);
        assert_eq!(params[0].name, "max_items");
        assert!(!params[0].required);
        assert_eq!(params[1].name, "query");
        assert!(params[1].required);
        assert_eq!(params[1].description, "Search query");
    }
}
