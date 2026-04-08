use std::collections::BTreeSet;
use std::path::Path;

use schemars::JsonSchema;
use serde::Serialize;
use serde_json::{Map as JsonMap, Value as JsonValue, json};

use crate::error::{Error, Result};

#[derive(Clone, Debug, Default, PartialEq, Serialize)]
pub struct Catalog {
    pub name: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub display_name: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub description: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub icon_svg: String,
    pub operations: Vec<CatalogOperation>,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize)]
pub struct CatalogOperation {
    pub id: String,
    pub method: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub title: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub description: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub input_schema: Option<JsonValue>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub output_schema: Option<JsonValue>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub annotations: Option<OperationAnnotations>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub parameters: Vec<CatalogParameter>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub required_scopes: Vec<String>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub tags: Vec<String>,
    #[serde(skip_serializing_if = "std::ops::Not::not")]
    pub read_only: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub visible: Option<bool>,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize)]
pub struct OperationAnnotations {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub read_only_hint: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub idempotent_hint: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub destructive_hint: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub open_world_hint: Option<bool>,
}

impl OperationAnnotations {
    pub fn is_empty(&self) -> bool {
        self.read_only_hint.is_none()
            && self.idempotent_hint.is_none()
            && self.destructive_hint.is_none()
            && self.open_world_hint.is_none()
    }
}

#[derive(Clone, Debug, Default, PartialEq, Serialize)]
pub struct CatalogParameter {
    pub name: String,
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub description: String,
    #[serde(skip_serializing_if = "std::ops::Not::not")]
    pub required: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub default: Option<JsonValue>,
}

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
    let mut yaml_catalog = catalog.clone();
    for op in &mut yaml_catalog.operations {
        op.input_schema = None;
        op.output_schema = None;
    }
    let yaml = serde_yaml::to_string(&yaml_catalog)?;
    std::fs::write(path, yaml)?;
    Ok(())
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
            kind: schema_type(property),
            description: property
                .get("description")
                .and_then(JsonValue::as_str)
                .unwrap_or_default()
                .trim()
                .to_owned(),
            required: required.contains(name),
            default: property.get("default").cloned(),
        })
        .collect()
}

fn is_false(value: &&bool) -> bool {
    !**value
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct WireCatalog<'a> {
    name: &'a str,
    #[serde(skip_serializing_if = "str::is_empty")]
    display_name: &'a str,
    #[serde(skip_serializing_if = "str::is_empty")]
    description: &'a str,
    #[serde(skip_serializing_if = "str::is_empty")]
    icon_svg: &'a str,
    operations: Vec<WireOperation<'a>>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct WireOperation<'a> {
    id: &'a str,
    method: &'a str,
    path: &'a str,
    transport: &'a str,
    #[serde(skip_serializing_if = "str::is_empty")]
    title: &'a str,
    #[serde(skip_serializing_if = "str::is_empty")]
    description: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    input_schema: &'a Option<JsonValue>,
    #[serde(skip_serializing_if = "Option::is_none")]
    output_schema: &'a Option<JsonValue>,
    #[serde(skip_serializing_if = "Option::is_none")]
    annotations: Option<WireAnnotations<'a>>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    parameters: Vec<WireParameter<'a>>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    required_scopes: &'a Vec<String>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    tags: &'a Vec<String>,
    #[serde(skip_serializing_if = "is_false")]
    read_only: &'a bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    visible: &'a Option<bool>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct WireAnnotations<'a> {
    #[serde(skip_serializing_if = "Option::is_none")]
    read_only_hint: &'a Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    idempotent_hint: &'a Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    destructive_hint: &'a Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    open_world_hint: &'a Option<bool>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct WireParameter<'a> {
    name: &'a str,
    #[serde(rename = "type")]
    kind: &'a str,
    #[serde(skip_serializing_if = "str::is_empty")]
    description: &'a str,
    #[serde(skip_serializing_if = "is_false")]
    required: &'a bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    default: &'a Option<JsonValue>,
}

pub(crate) fn catalog_json(catalog: &Catalog) -> Result<String> {
    let wire = WireCatalog {
        name: &catalog.name,
        display_name: &catalog.display_name,
        description: &catalog.description,
        icon_svg: &catalog.icon_svg,
        operations: catalog
            .operations
            .iter()
            .map(|op| WireOperation {
                id: &op.id,
                method: &op.method,
                path: "",
                transport: "plugin",
                title: &op.title,
                description: &op.description,
                input_schema: &op.input_schema,
                output_schema: &op.output_schema,
                annotations: op
                    .annotations
                    .as_ref()
                    .filter(|a| !a.is_empty())
                    .map(|a| WireAnnotations {
                        read_only_hint: &a.read_only_hint,
                        idempotent_hint: &a.idempotent_hint,
                        destructive_hint: &a.destructive_hint,
                        open_world_hint: &a.open_world_hint,
                    }),
                parameters: op
                    .parameters
                    .iter()
                    .map(|p| WireParameter {
                        name: &p.name,
                        kind: &p.kind,
                        description: &p.description,
                        required: &p.required,
                        default: &p.default,
                    })
                    .collect(),
                required_scopes: &op.required_scopes,
                tags: &op.tags,
                read_only: &op.read_only,
                visible: &op.visible,
            })
            .collect(),
    };
    serde_json::to_string(&wire).map_err(Error::from)
}

pub(crate) fn object_map(value: Option<prost_types::Struct>) -> JsonMap<String, JsonValue> {
    value
        .map(|structure| {
            structure
                .fields
                .into_iter()
                .map(|(key, value)| (key, proto_value_to_json(value)))
                .collect::<JsonMap<_, _>>()
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

    #[test]
    fn catalog_json_includes_plugin_transport() {
        let catalog = Catalog {
            name: "example".to_owned(),
            operations: vec![CatalogOperation {
                id: "echo".to_owned(),
                method: "POST".to_owned(),
                ..CatalogOperation::default()
            }],
            ..Catalog::default()
        };
        let raw = catalog_json(&catalog).expect("catalog json");
        assert!(raw.contains("\"transport\":\"plugin\""));
        assert!(raw.contains("\"path\":\"\""));
    }
}
