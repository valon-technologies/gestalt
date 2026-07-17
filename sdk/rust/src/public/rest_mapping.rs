//! Protobuf-JSON request mapping for the public REST transport.

use serde_json::{Map, Value};

use crate::rpc_support::{GestaltError, gestalt_error_code};

/// Converts a snake_case field name to lowerCamelCase.
pub fn snake_to_camel(name: &str) -> String {
    let mut parts = name.split('_');
    let Some(first) = parts.next() else {
        return name.to_string();
    };
    let mut out = first.to_string();
    for part in parts {
        if part.is_empty() {
            continue;
        }
        let mut chars = part.chars();
        if let Some(initial) = chars.next() {
            out.push(initial.to_ascii_uppercase());
            out.push_str(chars.as_str());
        }
    }
    out
}

/// Resolves a path parameter from protobuf JSON using snake_case or camelCase keys.
pub fn resolve_path_value<'a>(
    request: &'a Map<String, Value>,
    field_name: &str,
) -> Option<&'a Value> {
    request
        .get(field_name)
        .or_else(|| request.get(&snake_to_camel(field_name)))
}

/// Substitutes `{param}` placeholders in an HTTP path template.
pub fn substitute_path(pattern: &str, request: &Value) -> Result<String, GestaltError> {
    let Some(object) = request.as_object() else {
        return Ok(pattern.to_string());
    };
    let mut out = pattern.to_string();
    for placeholder in path_param_names(pattern) {
        let token = format!("{{{placeholder}}}");
        if !out.contains(&token) {
            continue;
        }
        let value = resolve_path_value(object, &placeholder).ok_or_else(|| {
            GestaltError::new(
                gestalt_error_code::INVALID_ARGUMENT,
                format!("missing path parameter {placeholder}"),
            )
        })?;
        let segment = scalar_to_string(value).ok_or_else(|| {
            GestaltError::new(
                gestalt_error_code::INVALID_ARGUMENT,
                format!("unsupported path parameter type for {placeholder}"),
            )
        })?;
        out = out.replace(&token, &urlencoding::encode(&segment));
    }
    Ok(out)
}

/// Returns path parameter names from an HTTP path template.
pub fn path_param_names(pattern: &str) -> Vec<String> {
    let mut names = Vec::new();
    let mut rest = pattern;
    while let Some(start) = rest.find('{') {
        let after = &rest[start + 1..];
        if let Some(end) = after.find('}') {
            names.push(after[..end].to_string());
            rest = &after[end + 1..];
        } else {
            break;
        }
    }
    names
}

fn is_path_field(field: &str, path_params: &[String]) -> bool {
    path_params
        .iter()
        .any(|name| name == field || snake_to_camel(name) == field)
}

/// Builds query-string pairs from a protobuf JSON request object.
pub fn build_query_pairs(
    request: &Map<String, Value>,
    path_params: &[String],
) -> Vec<(String, String)> {
    let mut pairs = Vec::new();
    for (key, value) in request {
        if value.is_null() || is_path_field(key, path_params) {
            continue;
        }
        encode_query_value(key, value, &mut pairs);
    }
    pairs
}

/// Builds the JSON request body for REST calls.
pub fn build_body_map(request: &Map<String, Value>, path_params: &[String]) -> Map<String, Value> {
    let mut body = Map::new();
    for (key, value) in request {
        if value.is_null() || is_path_field(key, path_params) {
            continue;
        }
        body.insert(key.clone(), value.clone());
    }
    body
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
