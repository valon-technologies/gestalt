//! Protobuf-JSON request mapping (compatibility re-exports).

use serde_json::{Map, Value};

use crate::public::generated::metadata::PublicField;

pub use crate::public::generated::transport_kernel::{
    append_query_params, build_rest_body, build_rest_path, build_rest_query, substitute_path,
};

fn is_path_field(field: &str, path_fields: &[PublicField]) -> bool {
    path_fields
        .iter()
        .any(|path_field| path_field.name == field || path_field.json_name == field)
}

fn retained_fields<'a>(
    request: &'a Map<String, Value>,
    path_fields: &[PublicField],
) -> impl Iterator<Item = (&'a String, &'a Value)> {
    request
        .iter()
        .filter(move |(key, value)| !value.is_null() && !is_path_field(key, path_fields))
}

/// Builds query-string pairs from a protobuf JSON request object.
pub fn build_query_pairs(
    request: &Map<String, Value>,
    path_fields: &[PublicField],
) -> Vec<(String, String)> {
    let mut pairs = Vec::new();
    for (key, value) in retained_fields(request, path_fields) {
        encode_query_value(key, value, &mut pairs);
    }
    pairs
}

/// Builds the JSON request body for REST calls.
pub fn build_body_map(
    request: &Map<String, Value>,
    path_fields: &[PublicField],
) -> Map<String, Value> {
    retained_fields(request, path_fields)
        .map(|(key, value)| (key.clone(), value.clone()))
        .collect()
}

fn encode_query_value(key: &str, value: &Value, out: &mut Vec<(String, String)>) {
    match value {
        Value::Null => {}
        Value::Array(items) => {
            for item in items {
                encode_query_value(key, item, out);
            }
        }
        Value::Object(nested) => {
            for (nested_key, nested_value) in nested {
                encode_query_value(&format!("{key}.{nested_key}"), nested_value, out);
            }
        }
        _ => {
            if let Some(text) = scalar_to_string(value) {
                out.push((key.to_string(), text));
            }
        }
    }
}

/// Encodes query pairs as an `application/x-www-form-urlencoded` string.
pub fn encode_query_string(pairs: &[(String, String)]) -> String {
    pairs
        .iter()
        .map(|(key, value)| {
            format!(
                "{}={}",
                urlencoding::encode(key),
                urlencoding::encode(value)
            )
        })
        .collect::<Vec<_>>()
        .join("&")
}

fn scalar_to_string(value: &Value) -> Option<String> {
    match value {
        Value::String(text) => Some(text.clone()),
        Value::Number(number) => Some(number.to_string()),
        Value::Bool(flag) => Some(flag.to_string()),
        _ => None,
    }
}
