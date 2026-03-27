use anyhow::{bail, Context, Result};
use std::io::Read;

use crate::catalog::OperationsCatalog;

#[derive(Debug, Clone, PartialEq)]
pub enum ParamValue {
    StringVal(String),
    JsonVal(serde_json::Value),
}

#[derive(Debug, Clone)]
pub struct ParamEntry {
    pub key: String,
    pub value: ParamValue,
}

pub fn parse_param_entry(s: &str) -> Result<ParamEntry, String> {
    if let Some(pos) = s.find(":=") {
        let key = &s[..pos];
        let raw_json = &s[pos + 2..];
        if key.is_empty() {
            return Err(format!("invalid KEY:=JSON: empty key in '{s}'"));
        }
        let value: serde_json::Value = serde_json::from_str(raw_json)
            .map_err(|e| format!("invalid JSON in '{key}:={raw_json}': {e}"))?;
        return Ok(ParamEntry {
            key: key.to_string(),
            value: ParamValue::JsonVal(value),
        });
    }

    let pos = s
        .find('=')
        .ok_or_else(|| format!("invalid param: no '=' or ':=' found in '{s}'"))?;
    let key = &s[..pos];
    if key.is_empty() {
        return Err(format!("invalid KEY=VALUE: empty key in '{s}'"));
    }
    Ok(ParamEntry {
        key: key.to_string(),
        value: ParamValue::StringVal(s[pos + 1..].to_string()),
    })
}

fn param_to_json(value: &ParamValue) -> serde_json::Value {
    match value {
        ParamValue::StringVal(s) => serde_json::Value::String(s.clone()),
        ParamValue::JsonVal(v) => v.clone(),
    }
}

pub fn assemble_params(
    entries: &[ParamEntry],
    catalog: Option<&OperationsCatalog>,
    operation: &str,
) -> Result<serde_json::Map<String, serde_json::Value>> {
    let mut grouped: Vec<(String, Vec<serde_json::Value>)> = Vec::new();

    for entry in entries {
        let json_val = param_to_json(&entry.value);
        if let Some((_, vals)) = grouped.iter_mut().find(|(k, _)| k == &entry.key) {
            vals.push(json_val);
        } else {
            grouped.push((entry.key.clone(), vec![json_val]));
        }
    }

    let mut map = serde_json::Map::new();
    for (key, vals) in grouped {
        let is_array = catalog.and_then(|c| c.is_array_param(operation, &key));

        if vals.len() == 1 {
            let val = vals.into_iter().next().unwrap();
            match is_array {
                Some(true) => map.insert(key, serde_json::Value::Array(vec![val])),
                _ => map.insert(key, val),
            };
        } else {
            match is_array {
                Some(false) => {
                    bail!(
                        "parameter '{}' is not an array type but was specified multiple times",
                        key
                    );
                }
                _ => {
                    map.insert(key, serde_json::Value::Array(vals));
                }
            }
        }
    }

    Ok(map)
}

pub fn load_input_file(path: &str) -> Result<serde_json::Map<String, serde_json::Value>> {
    let contents = if path == "-" {
        let mut buf = String::new();
        std::io::stdin()
            .read_to_string(&mut buf)
            .context("failed to read from stdin")?;
        buf
    } else {
        std::fs::read_to_string(path)
            .with_context(|| format!("failed to read input file '{}'", path))?
    };

    let value: serde_json::Value =
        serde_json::from_str(&contents).context("failed to parse input file as JSON")?;

    match value {
        serde_json::Value::Object(map) => Ok(map),
        _ => bail!("input file must contain a JSON object"),
    }
}

pub fn merge_params(
    file_map: serde_json::Map<String, serde_json::Value>,
    param_map: serde_json::Map<String, serde_json::Value>,
) -> serde_json::Map<String, serde_json::Value> {
    let mut result = file_map;
    for (k, v) in param_map {
        result.insert(k, v);
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_string_param() {
        let entry = parse_param_entry("key=value").unwrap();
        assert_eq!(entry.key, "key");
        assert_eq!(entry.value, ParamValue::StringVal("value".to_string()));
    }

    #[test]
    fn test_parse_json_number() {
        let entry = parse_param_entry("key:=42").unwrap();
        assert_eq!(entry.key, "key");
        assert_eq!(entry.value, ParamValue::JsonVal(serde_json::json!(42)));
    }

    #[test]
    fn test_parse_json_bool() {
        let entry = parse_param_entry("key:=true").unwrap();
        assert_eq!(entry.key, "key");
        assert_eq!(entry.value, ParamValue::JsonVal(serde_json::json!(true)));
    }

    #[test]
    fn test_parse_json_array() {
        let entry = parse_param_entry("key:=[1,2]").unwrap();
        assert_eq!(entry.key, "key");
        assert_eq!(entry.value, ParamValue::JsonVal(serde_json::json!([1, 2])));
    }

    #[test]
    fn test_parse_json_object() {
        let entry = parse_param_entry(r#"key:={"a":1}"#).unwrap();
        assert_eq!(entry.key, "key");
        assert_eq!(
            entry.value,
            ParamValue::JsonVal(serde_json::json!({"a": 1}))
        );
    }

    #[test]
    fn test_parse_json_null() {
        let entry = parse_param_entry("key:=null").unwrap();
        assert_eq!(entry.key, "key");
        assert_eq!(
            entry.value,
            ParamValue::JsonVal(serde_json::Value::Null)
        );
    }

    #[test]
    fn test_parse_invalid_json() {
        let result = parse_param_entry("key:=invalid");
        assert!(result.is_err());
    }

    #[test]
    fn test_parse_value_with_equals() {
        let entry = parse_param_entry("url=http://x.com?a=1").unwrap();
        assert_eq!(entry.key, "url");
        assert_eq!(
            entry.value,
            ParamValue::StringVal("http://x.com?a=1".to_string())
        );
    }

    #[test]
    fn test_parse_empty_key() {
        let result = parse_param_entry("=value");
        assert!(result.is_err());
    }

    #[test]
    fn test_parse_no_delimiter() {
        let result = parse_param_entry("nodelimiter");
        assert!(result.is_err());
    }
}
