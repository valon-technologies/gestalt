use std::collections::BTreeSet;
use std::path::Path;

use schemars::JsonSchema;
use serde_json::{Value as JsonValue, json};

use crate::error::{Error, Result};
use crate::generated::v1;

/// Catalog schema used by the provider runtime.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct Catalog {
    /// The `name` field.
    pub name: String,
    /// The `display_name` field.
    pub display_name: String,
    /// The `description` field.
    pub description: String,
    /// The `icon_svg` field.
    pub icon_svg: String,
    /// The `operations` field.
    pub operations: Vec<CatalogOperation>,
}

/// One operation exposed by a catalog.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct CatalogOperation {
    /// The `id` field.
    pub id: String,
    /// The `method` field.
    pub method: String,
    /// The `title` field.
    pub title: String,
    /// The `description` field.
    pub description: String,
    /// The `input_schema` field.
    pub input_schema: String,
    /// The `output_schema` field (deprecated; use `response`).
    pub output_schema: String,
    /// The `response` field; when set, takes precedence over `output_schema`.
    pub response: Option<OperationResponseSpec>,
    /// The `annotations` field.
    pub annotations: Option<OperationAnnotations>,
    /// The `parameters` field.
    pub parameters: Vec<CatalogParameter>,
    /// The `required_scopes` field.
    pub required_scopes: Vec<String>,
    /// The `tags` field.
    pub tags: Vec<String>,
    /// The `read_only` field.
    pub read_only: bool,
    /// The `visible` field.
    pub visible: Option<bool>,
    /// The `transport` field.
    pub transport: String,
    /// The `allowed_roles` field.
    pub allowed_roles: Vec<String>,
}

/// One input parameter surfaced in a generated catalog operation.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct CatalogParameter {
    /// The `name` field.
    pub name: String,
    /// The `type` field.
    pub r#type: String,
    /// The `description` field.
    pub description: String,
    /// The `required` field.
    pub required: bool,
    /// The `default` field.
    pub default: Option<JsonValue>,
}

/// Optional host hints attached to a catalog operation.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct OperationAnnotations {
    /// The `read_only_hint` field.
    pub read_only_hint: Option<bool>,
    /// The `idempotent_hint` field.
    pub idempotent_hint: Option<bool>,
    /// The `destructive_hint` field.
    pub destructive_hint: Option<bool>,
    /// The `open_world_hint` field.
    pub open_world_hint: Option<bool>,
}

/// UnaryResponseSpec describes a unary (fully materialized) operation response.
/// `schema` is a JSON-encoded schema object (JSON Schema shape).
#[derive(Clone, Debug, Default, PartialEq)]
pub struct UnaryResponseSpec {
    /// The `schema` field; a JSON-encoded schema string.
    pub schema: String,
}

/// StreamResponseSpec describes a streaming operation response. `media_type`
/// names the representation (for example `application/x-ndjson`); `item_schema`
/// is an optional JSON-encoded schema describing one yielded item.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct StreamResponseSpec {
    /// The `media_type` field.
    pub media_type: String,
    /// The `item_schema` field; a JSON-encoded schema string.
    pub item_schema: String,
}

/// OperationResponseSpec declares how an operation responds. Either `unary` or
/// `stream` is set; both `None` means unary with no schema. When set on a
/// `CatalogOperation`, it takes precedence over the legacy `output_schema`.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct OperationResponseSpec {
    /// The `unary` variant.
    pub unary: Option<UnaryResponseSpec>,
    /// The `stream` variant.
    pub stream: Option<StreamResponseSpec>,
}

impl OperationResponseSpec {
    /// Reports whether this response spec declares a streaming response.
    pub fn is_stream(&self) -> bool {
        self.stream.is_some()
    }
}

impl Catalog {
    /// Returns a copy of the catalog with a non-empty name override applied.
    pub fn with_name(mut self, name: impl Into<String>) -> Self {
        let name = name.into();
        if !name.trim().is_empty() {
            self.name = name;
        }
        self
    }
}

/// Writes catalog to path using the JSON shape expected by `gestaltd`.
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

pub(crate) fn catalog_to_proto(catalog: &Catalog) -> v1::Catalog {
    v1::Catalog {
        name: catalog.name.clone(),
        display_name: catalog.display_name.clone(),
        description: catalog.description.clone(),
        icon_svg: catalog.icon_svg.clone(),
        operations: catalog.operations.iter().map(operation_to_proto).collect(),
    }
}

/// response_to_proto converts the authoring `OperationResponseSpec` to its proto
/// form. When `response` is `None` but `legacy_output_schema` is non-empty, it
/// is mapped to a unary response with that schema for backward compatibility.
/// An empty/unparseable schema yields `None`.
fn response_to_proto(
    response: &Option<OperationResponseSpec>,
    legacy_output_schema: &str,
) -> Option<v1::OperationResponseSpec> {
    if let Some(spec) = response {
        if let Some(stream) = &spec.stream {
            return Some(v1::OperationResponseSpec {
                kind: Some(v1::operation_response_spec::Kind::Stream(
                    v1::StreamResponseSpec {
                        media_type: stream.media_type.clone(),
                        item_schema: schema_string_to_struct(&stream.item_schema),
                    },
                )),
            });
        }
        if let Some(unary) = &spec.unary {
            return Some(v1::OperationResponseSpec {
                kind: Some(v1::operation_response_spec::Kind::Unary(
                    v1::UnaryResponseSpec {
                        schema: schema_string_to_struct(&unary.schema),
                    },
                )),
            });
        }
    }
    if legacy_output_schema.trim().is_empty() {
        return None;
    }
    let struct_value = schema_string_to_struct(legacy_output_schema)?;
    Some(v1::OperationResponseSpec {
        kind: Some(v1::operation_response_spec::Kind::Unary(
            v1::UnaryResponseSpec {
                schema: Some(struct_value),
            },
        )),
    })
}

/// schema_string_to_struct parses a JSON-encoded schema string into a
/// protobuf Struct. An empty string or parse error yields None.
fn schema_string_to_struct(schema: &str) -> Option<prost_types::Struct> {
    let schema = schema.trim();
    if schema.is_empty() {
        return None;
    }
    let parsed = serde_json::from_str::<JsonValue>(schema).ok()?;
    let mut fields = std::collections::BTreeMap::new();
    if let JsonValue::Object(map) = parsed {
        for (k, v) in map {
            fields.insert(k, json_value_to_prost_value(v));
        }
    }
    Some(prost_types::Struct { fields })
}

/// json_value_to_prost_value converts a serde_json::Value into a prost_types::Value.
fn json_value_to_prost_value(value: JsonValue) -> prost_types::Value {
    use prost_types::value::Kind;
    let kind = match value {
        JsonValue::Null => Kind::NullValue(0),
        JsonValue::Bool(b) => Kind::BoolValue(b),
        JsonValue::Number(n) => {
            if let Some(i) = n.as_i64() {
                Kind::NumberValue(i as f64)
            } else {
                Kind::NumberValue(n.as_f64().unwrap_or(0.0))
            }
        }
        JsonValue::String(s) => Kind::StringValue(s),
        JsonValue::Array(arr) => Kind::ListValue(prost_types::ListValue {
            values: arr.into_iter().map(json_value_to_prost_value).collect(),
        }),
        JsonValue::Object(map) => {
            let mut fields = std::collections::BTreeMap::new();
            for (k, v) in map {
                fields.insert(k, json_value_to_prost_value(v));
            }
            Kind::StructValue(prost_types::Struct { fields })
        }
    };
    prost_types::Value { kind: Some(kind) }
}

fn operation_to_proto(operation: &CatalogOperation) -> v1::CatalogOperation {
    v1::CatalogOperation {
        id: operation.id.clone(),
        method: operation.method.clone(),
        title: operation.title.clone(),
        description: operation.description.clone(),
        input_schema: operation.input_schema.clone(),
        response: response_to_proto(&operation.response, &operation.output_schema),
        annotations: operation.annotations.as_ref().map(annotations_to_proto),
        parameters: operation
            .parameters
            .iter()
            .map(parameter_to_proto)
            .collect(),
        required_scopes: operation.required_scopes.clone(),
        tags: operation.tags.clone(),
        read_only: operation.read_only,
        visible: operation.visible,
        transport: operation.transport.clone(),
        allowed_roles: operation.allowed_roles.clone(),
    }
}

fn annotations_to_proto(annotations: &OperationAnnotations) -> v1::OperationAnnotations {
    v1::OperationAnnotations {
        read_only_hint: annotations.read_only_hint,
        idempotent_hint: annotations.idempotent_hint,
        destructive_hint: annotations.destructive_hint,
        open_world_hint: annotations.open_world_hint,
    }
}

fn parameter_to_proto(parameter: &CatalogParameter) -> v1::CatalogParameter {
    v1::CatalogParameter {
        name: parameter.name.clone(),
        r#type: parameter.r#type.clone(),
        description: parameter.description.clone(),
        required: parameter.required,
        default: parameter.default.as_ref().map(json_value_to_proto_value),
    }
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
    if let Some(ref spec) = op.response {
        obj.insert("response".to_owned(), response_to_json_value(spec));
    }
    // Legacy outputSchema is emitted when response is unset, for backward-compatible catalogs.
    if op.response.is_none() && !op.output_schema.is_empty() {
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
                    m.insert("default".to_owned(), default.clone());
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

/// response_to_json_value serializes an authoring OperationResponseSpec to the
/// JSON catalog shape (unary/stream with JSON-parsed schema objects).
fn response_to_json_value(spec: &OperationResponseSpec) -> JsonValue {
    let mut m = serde_json::Map::new();
    if let Some(unary) = &spec.unary {
        if let Ok(schema) = serde_json::from_str::<JsonValue>(&unary.schema) {
            let mut u = serde_json::Map::new();
            u.insert("schema".to_owned(), schema);
            m.insert("unary".to_owned(), JsonValue::Object(u));
        }
    }
    if let Some(stream) = &spec.stream {
        let mut s = serde_json::Map::new();
        s.insert("mediaType".to_owned(), json!(stream.media_type));
        if let Ok(schema) = serde_json::from_str::<JsonValue>(&stream.item_schema) {
            s.insert("itemSchema".to_owned(), schema);
        }
        m.insert("stream".to_owned(), JsonValue::Object(s));
    }
    JsonValue::Object(m)
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
            default: property.get("default").cloned(),
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

    fn op_with_response(response: Option<OperationResponseSpec>) -> CatalogOperation {
        CatalogOperation {
            id: "search".to_owned(),
            method: "POST".to_owned(),
            output_schema: String::new(),
            response,
            ..Default::default()
        }
    }

    #[test]
    fn response_stream_produces_stream_proto_kind() {
        let op = op_with_response(Some(OperationResponseSpec {
            stream: Some(StreamResponseSpec {
                media_type: "application/x-ndjson".to_owned(),
                item_schema: r#"{"type":"object"}"#.to_owned(),
            }),
            unary: None,
        }));
        let proto = operation_to_proto(&op);
        let kind = proto
            .response
            .expect("response set")
            .kind
            .expect("kind set");
        match kind {
            v1::operation_response_spec::Kind::Stream(s) => {
                assert_eq!(s.media_type, "application/x-ndjson");
                assert!(s.item_schema.is_some());
            }
            other => panic!("expected stream, got {other:?}"),
        }
    }

    #[test]
    fn response_unary_produces_unary_proto_kind() {
        let op = op_with_response(Some(OperationResponseSpec {
            unary: Some(UnaryResponseSpec {
                schema: r#"{"type":"object"}"#.to_owned(),
            }),
            stream: None,
        }));
        let proto = operation_to_proto(&op);
        let kind = proto
            .response
            .expect("response set")
            .kind
            .expect("kind set");
        assert!(matches!(kind, v1::operation_response_spec::Kind::Unary(_)));
    }

    #[test]
    fn legacy_output_schema_falls_back_to_unary_when_response_is_none() {
        let op = CatalogOperation {
            id: "get".to_owned(),
            method: "POST".to_owned(),
            output_schema: r#"{"type":"object"}"#.to_owned(),
            response: None,
            ..Default::default()
        };
        let proto = operation_to_proto(&op);
        let kind = proto
            .response
            .expect("response set")
            .kind
            .expect("kind set");
        assert!(matches!(kind, v1::operation_response_spec::Kind::Unary(_)));
    }

    #[test]
    fn empty_response_and_empty_output_schema_yield_none() {
        let op = op_with_response(None);
        let proto = operation_to_proto(&op);
        assert!(proto.response.is_none());
    }

    #[test]
    fn json_value_emits_response_and_skips_legacy_output_schema() {
        let op = CatalogOperation {
            id: "search".to_owned(),
            method: "POST".to_owned(),
            output_schema: r#"{"type":"object"}"#.to_owned(),
            response: Some(OperationResponseSpec {
                stream: Some(StreamResponseSpec {
                    media_type: "application/x-ndjson".to_owned(),
                    item_schema: r#"{"type":"object"}"#.to_owned(),
                }),
                unary: None,
            }),
            ..Default::default()
        };
        let json = operation_to_json_value(&op);
        assert!(json.get("response").is_some(), "response should be emitted");
        assert!(
            json.get("outputSchema").is_none(),
            "legacy outputSchema should be skipped when response is set"
        );
        let stream = json
            .get("response")
            .and_then(|v| v.get("stream"))
            .expect("stream variant");
        assert_eq!(stream["mediaType"], "application/x-ndjson");
    }

    #[test]
    fn is_stream_reports_stream_variant() {
        let stream_spec = OperationResponseSpec {
            stream: Some(StreamResponseSpec::default()),
            unary: None,
        };
        assert!(stream_spec.is_stream());
        let unary_spec = OperationResponseSpec {
            unary: Some(UnaryResponseSpec::default()),
            stream: None,
        };
        assert!(!unary_spec.is_stream());
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
