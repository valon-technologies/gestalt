use anyhow::{Context, Result, bail};
use std::collections::BTreeMap;
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

impl ParamValue {
    fn to_json(&self) -> serde_json::Value {
        match self {
            Self::StringVal(value) => serde_json::Value::String(value.clone()),
            Self::JsonVal(value) => value.clone(),
        }
    }
}

pub fn parse_param_entry(s: &str) -> Result<ParamEntry, String> {
    let eq_pos = s.find('=');
    let colon_eq_pos = s.find(":=");

    let use_json = matches!(
        (eq_pos, colon_eq_pos),
        (_, Some(ce)) if eq_pos.is_none() || ce < eq_pos.unwrap()
    );

    if use_json {
        let pos = colon_eq_pos.unwrap();
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

    let pos = eq_pos.ok_or_else(|| format!("invalid param: no '=' or ':=' found in '{s}'"))?;
    let key = &s[..pos];
    if key.is_empty() {
        return Err(format!("invalid KEY=VALUE: empty key in '{s}'"));
    }
    Ok(ParamEntry {
        key: key.to_string(),
        value: ParamValue::StringVal(s[pos + 1..].to_string()),
    })
}

pub fn assemble_params(
    entries: &[ParamEntry],
    catalog: Option<&OperationsCatalog>,
    operation: &str,
) -> Result<serde_json::Map<String, serde_json::Value>> {
    let mut grouped: BTreeMap<String, Vec<serde_json::Value>> = BTreeMap::new();
    for entry in entries {
        grouped
            .entry(entry.key.clone())
            .or_default()
            .push(entry.value.to_json());
    }

    let mut map = serde_json::Map::new();
    for (key, vals) in grouped {
        let is_array = catalog.and_then(|c| c.is_array_param(operation, &key));

        if vals.len() == 1 {
            let val = vals.into_iter().next().unwrap();
            match is_array {
                Some(true) if !val.is_array() => {
                    map.insert(key, serde_json::Value::Array(vec![val]))
                }
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
