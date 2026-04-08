use std::collections::BTreeSet;
use std::path::Path;

use schemars::JsonSchema;
use serde_json::{Map as JsonMap, Value as JsonValue, json};
use serde_yaml::{Mapping as YamlMapping, Value as YamlValue};

use crate::error::{Error, Result};

#[derive(Clone, Debug, Default, PartialEq)]
pub struct Catalog {
    pub name: String,
    pub display_name: String,
    pub description: String,
    pub icon_svg: String,
    pub operations: Vec<CatalogOperation>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct CatalogOperation {
    pub id: String,
    pub method: String,
    pub title: String,
    pub description: String,
    pub input_schema: Option<JsonValue>,
    pub output_schema: Option<JsonValue>,
    pub annotations: Option<OperationAnnotations>,
    pub parameters: Vec<CatalogParameter>,
    pub required_scopes: Vec<String>,
    pub tags: Vec<String>,
    pub read_only: bool,
    pub visible: Option<bool>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct OperationAnnotations {
    pub read_only_hint: Option<bool>,
    pub idempotent_hint: Option<bool>,
    pub destructive_hint: Option<bool>,
    pub open_world_hint: Option<bool>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct CatalogParameter {
    pub name: String,
    pub kind: String,
    pub description: String,
    pub required: bool,
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
    let yaml = serde_yaml::to_string(&catalog_yaml_value(catalog))?;
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

pub(crate) fn catalog_json(catalog: &Catalog) -> Result<String> {
    serde_json::to_string(&catalog_json_value(catalog)).map_err(Error::from)
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

fn catalog_json_value(catalog: &Catalog) -> JsonValue {
    let mut map = JsonMap::new();
    map.insert("name".to_owned(), json!(catalog.name));
    insert_json_string(&mut map, "displayName", &catalog.display_name);
    insert_json_string(&mut map, "description", &catalog.description);
    insert_json_string(&mut map, "iconSvg", &catalog.icon_svg);
    map.insert(
        "operations".to_owned(),
        JsonValue::Array(
            catalog
                .operations
                .iter()
                .map(operation_json_value)
                .collect(),
        ),
    );
    JsonValue::Object(map)
}

fn operation_json_value(operation: &CatalogOperation) -> JsonValue {
    let mut map = JsonMap::new();
    map.insert("id".to_owned(), json!(operation.id));
    map.insert("method".to_owned(), json!(operation.method));
    map.insert("path".to_owned(), json!(""));
    map.insert("transport".to_owned(), json!("plugin"));
    insert_json_string(&mut map, "title", &operation.title);
    insert_json_string(&mut map, "description", &operation.description);
    if let Some(schema) = &operation.input_schema {
        map.insert("inputSchema".to_owned(), schema.clone());
    }
    if let Some(schema) = &operation.output_schema {
        map.insert("outputSchema".to_owned(), schema.clone());
    }
    if let Some(annotations) = &operation.annotations {
        let value = annotations_json_value(annotations);
        if !is_empty_json_object(&value) {
            map.insert("annotations".to_owned(), value);
        }
    }
    if !operation.parameters.is_empty() {
        map.insert(
            "parameters".to_owned(),
            JsonValue::Array(
                operation
                    .parameters
                    .iter()
                    .map(parameter_json_value)
                    .collect(),
            ),
        );
    }
    if !operation.required_scopes.is_empty() {
        map.insert(
            "requiredScopes".to_owned(),
            JsonValue::Array(
                operation
                    .required_scopes
                    .iter()
                    .map(|scope| json!(scope))
                    .collect(),
            ),
        );
    }
    if !operation.tags.is_empty() {
        map.insert(
            "tags".to_owned(),
            JsonValue::Array(operation.tags.iter().map(|tag| json!(tag)).collect()),
        );
    }
    if operation.read_only {
        map.insert("readOnly".to_owned(), json!(true));
    }
    if let Some(visible) = operation.visible {
        map.insert("visible".to_owned(), json!(visible));
    }
    JsonValue::Object(map)
}

fn annotations_json_value(annotations: &OperationAnnotations) -> JsonValue {
    let mut map = JsonMap::new();
    insert_json_bool(&mut map, "readOnlyHint", annotations.read_only_hint);
    insert_json_bool(&mut map, "idempotentHint", annotations.idempotent_hint);
    insert_json_bool(&mut map, "destructiveHint", annotations.destructive_hint);
    insert_json_bool(&mut map, "openWorldHint", annotations.open_world_hint);
    JsonValue::Object(map)
}

fn parameter_json_value(parameter: &CatalogParameter) -> JsonValue {
    let mut map = JsonMap::new();
    map.insert("name".to_owned(), json!(parameter.name));
    map.insert("type".to_owned(), json!(parameter.kind));
    insert_json_string(&mut map, "description", &parameter.description);
    if parameter.required {
        map.insert("required".to_owned(), json!(true));
    }
    if let Some(default) = &parameter.default {
        map.insert("default".to_owned(), default.clone());
    }
    JsonValue::Object(map)
}

fn catalog_yaml_value(catalog: &Catalog) -> YamlValue {
    let mut map = YamlMapping::new();
    insert_yaml_value(&mut map, "name", YamlValue::String(catalog.name.clone()));
    insert_yaml_string(&mut map, "display_name", &catalog.display_name);
    insert_yaml_string(&mut map, "description", &catalog.description);
    insert_yaml_string(&mut map, "icon_svg", &catalog.icon_svg);
    insert_yaml_value(
        &mut map,
        "operations",
        YamlValue::Sequence(
            catalog
                .operations
                .iter()
                .map(operation_yaml_value)
                .collect(),
        ),
    );
    YamlValue::Mapping(map)
}

fn operation_yaml_value(operation: &CatalogOperation) -> YamlValue {
    let mut map = YamlMapping::new();
    insert_yaml_value(&mut map, "id", YamlValue::String(operation.id.clone()));
    insert_yaml_value(
        &mut map,
        "method",
        YamlValue::String(operation.method.clone()),
    );
    insert_yaml_string(&mut map, "title", &operation.title);
    insert_yaml_string(&mut map, "description", &operation.description);
    if let Some(annotations) = &operation.annotations {
        let value = annotations_yaml_value(annotations);
        if !is_empty_yaml_mapping(&value) {
            insert_yaml_value(&mut map, "annotations", value);
        }
    }
    if !operation.parameters.is_empty() {
        insert_yaml_value(
            &mut map,
            "parameters",
            YamlValue::Sequence(
                operation
                    .parameters
                    .iter()
                    .map(parameter_yaml_value)
                    .collect(),
            ),
        );
    }
    if !operation.required_scopes.is_empty() {
        insert_yaml_value(
            &mut map,
            "required_scopes",
            YamlValue::Sequence(
                operation
                    .required_scopes
                    .iter()
                    .map(|scope| YamlValue::String(scope.clone()))
                    .collect(),
            ),
        );
    }
    if !operation.tags.is_empty() {
        insert_yaml_value(
            &mut map,
            "tags",
            YamlValue::Sequence(
                operation
                    .tags
                    .iter()
                    .map(|tag| YamlValue::String(tag.clone()))
                    .collect(),
            ),
        );
    }
    if operation.read_only {
        insert_yaml_value(&mut map, "read_only", YamlValue::Bool(true));
    }
    if let Some(visible) = operation.visible {
        insert_yaml_value(&mut map, "visible", YamlValue::Bool(visible));
    }
    YamlValue::Mapping(map)
}

fn annotations_yaml_value(annotations: &OperationAnnotations) -> YamlValue {
    let mut map = YamlMapping::new();
    insert_yaml_bool(&mut map, "read_only_hint", annotations.read_only_hint);
    insert_yaml_bool(&mut map, "idempotent_hint", annotations.idempotent_hint);
    insert_yaml_bool(&mut map, "destructive_hint", annotations.destructive_hint);
    insert_yaml_bool(&mut map, "open_world_hint", annotations.open_world_hint);
    YamlValue::Mapping(map)
}

fn parameter_yaml_value(parameter: &CatalogParameter) -> YamlValue {
    let mut map = YamlMapping::new();
    insert_yaml_value(&mut map, "name", YamlValue::String(parameter.name.clone()));
    insert_yaml_value(&mut map, "type", YamlValue::String(parameter.kind.clone()));
    insert_yaml_string(&mut map, "description", &parameter.description);
    if parameter.required {
        insert_yaml_value(&mut map, "required", YamlValue::Bool(true));
    }
    if let Some(default) = &parameter.default {
        let value = serde_yaml::to_value(default).unwrap_or(YamlValue::Null);
        insert_yaml_value(&mut map, "default", value);
    }
    YamlValue::Mapping(map)
}

fn insert_json_string(map: &mut JsonMap<String, JsonValue>, key: &str, value: &str) {
    if !value.is_empty() {
        map.insert(key.to_owned(), json!(value));
    }
}

fn insert_json_bool(map: &mut JsonMap<String, JsonValue>, key: &str, value: Option<bool>) {
    if let Some(value) = value {
        map.insert(key.to_owned(), json!(value));
    }
}

fn insert_yaml_value(map: &mut YamlMapping, key: &str, value: YamlValue) {
    map.insert(YamlValue::String(key.to_owned()), value);
}

fn insert_yaml_string(map: &mut YamlMapping, key: &str, value: &str) {
    if !value.is_empty() {
        insert_yaml_value(map, key, YamlValue::String(value.to_owned()));
    }
}

fn insert_yaml_bool(map: &mut YamlMapping, key: &str, value: Option<bool>) {
    if let Some(value) = value {
        insert_yaml_value(map, key, YamlValue::Bool(value));
    }
}

fn is_empty_json_object(value: &JsonValue) -> bool {
    matches!(value, JsonValue::Object(map) if map.is_empty())
}

fn is_empty_yaml_mapping(value: &YamlValue) -> bool {
    matches!(value, YamlValue::Mapping(map) if map.is_empty())
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
