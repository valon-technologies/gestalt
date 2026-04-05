use std::collections::{BTreeMap, BTreeSet};
use std::io::IsTerminal;

use colored::Colorize;
use comfy_table::{Cell, ContentArrangement, Table, presets::ASCII_NO_BORDERS};

#[derive(Debug, Clone, Copy, PartialEq, Eq, clap::ValueEnum)]
pub enum Format {
    Json,
    Table,
}

pub fn print_json(value: &serde_json::Value) {
    let output = serde_json::to_string_pretty(value).unwrap_or_else(|_| value.to_string());
    println!("{}", output);
}

pub fn print_json_table(value: &serde_json::Value) {
    match value {
        serde_json::Value::Array(arr) => print_json_array_table(arr),
        serde_json::Value::Object(obj) => print_json_object_table(obj),
        _ => print_json(value),
    }
}

fn print_json_array_table(arr: &[serde_json::Value]) {
    if arr.is_empty() {
        println!("No results.");
        return;
    }

    if arr.iter().all(|value| value.is_object()) {
        let flattened_rows: Vec<BTreeMap<String, String>> = arr
            .iter()
            .map(|value| flatten_object(value.as_object().expect("checked object")))
            .collect();
        let headers: Vec<String> = flattened_rows
            .iter()
            .flat_map(|row| row.keys().cloned())
            .collect::<BTreeSet<_>>()
            .into_iter()
            .collect();
        let display_headers = unique_header_suffixes(&headers);
        let header_refs: Vec<&str> = display_headers.iter().map(String::as_str).collect();
        let rows: Vec<Vec<String>> = flattened_rows
            .iter()
            .map(|row| {
                headers
                    .iter()
                    .map(|header| row.get(header).cloned().unwrap_or_default())
                    .collect()
            })
            .collect();
        print_table(&header_refs, &rows);
        return;
    }

    if arr
        .iter()
        .all(|value| !value.is_object() && !value.is_array())
    {
        let rows: Vec<Vec<String>> = arr
            .iter()
            .map(|value| vec![value_to_cell(Some(value))])
            .collect();
        print_table(&["value"], &rows);
        return;
    }

    print_json(&serde_json::Value::Array(arr.to_vec()));
}

fn unique_header_suffixes(paths: &[String]) -> Vec<String> {
    paths
        .iter()
        .enumerate()
        .map(|(index, path)| shortest_unique_suffix(paths, index).unwrap_or_else(|| path.clone()))
        .collect()
}

fn shortest_unique_suffix(paths: &[String], index: usize) -> Option<String> {
    let target_parts: Vec<&str> = paths[index].split('.').collect();
    for suffix_len in 1..=target_parts.len() {
        let suffix = &target_parts[target_parts.len() - suffix_len..];
        let is_unique = paths.iter().enumerate().all(|(other_index, other_path)| {
            other_index == index || !path_has_suffix(other_path, suffix)
        });
        if is_unique {
            return Some(suffix.join("."));
        }
    }
    None
}

fn path_has_suffix(path: &str, suffix: &[&str]) -> bool {
    let parts: Vec<&str> = path.split('.').collect();
    parts.len() >= suffix.len() && parts[parts.len() - suffix.len()..] == *suffix
}

fn print_json_object_table(obj: &serde_json::Map<String, serde_json::Value>) {
    if let Some((array_key, arr)) = single_array_field(obj) {
        println!("{}", array_key.to_uppercase());
        print_json_array_table(arr);

        let metadata_rows = object_to_key_value_rows(
            obj.iter()
                .filter(|(key, _)| key.as_str() != array_key)
                .map(|(key, value)| (key.as_str(), value)),
        );
        if !metadata_rows.is_empty() {
            println!();
            println!("METADATA");
            print_table(&["key", "value"], &metadata_rows);
        }
        return;
    }

    let rows = object_to_key_value_rows(obj.iter().map(|(key, value)| (key.as_str(), value)));
    print_table(&["key", "value"], &rows);
}

fn single_array_field<'a>(
    obj: &'a serde_json::Map<String, serde_json::Value>,
) -> Option<(&'a str, &'a Vec<serde_json::Value>)> {
    let mut array_fields = obj
        .iter()
        .filter_map(|(key, value)| value.as_array().map(|arr| (key.as_str(), arr)));
    let first = array_fields.next()?;
    if array_fields.next().is_some() {
        return None;
    }
    Some(first)
}

fn flatten_object(obj: &serde_json::Map<String, serde_json::Value>) -> BTreeMap<String, String> {
    let mut flattened = BTreeMap::new();
    for (key, value) in obj {
        flatten_value(key, value, &mut flattened);
    }
    flattened
}

fn object_to_key_value_rows<'a>(
    entries: impl IntoIterator<Item = (&'a str, &'a serde_json::Value)>,
) -> Vec<Vec<String>> {
    let mut flattened = BTreeMap::new();
    for (key, value) in entries {
        flatten_value(key, value, &mut flattened);
    }
    flattened
        .into_iter()
        .map(|(key, value)| vec![key, value])
        .collect()
}

fn flatten_value(path: &str, value: &serde_json::Value, out: &mut BTreeMap<String, String>) {
    match value {
        serde_json::Value::Object(map) => {
            if map.is_empty() {
                out.insert(path.to_string(), "{}".to_string());
                return;
            }

            for (key, nested_value) in map {
                let nested_path = format!("{path}.{key}");
                flatten_value(&nested_path, nested_value, out);
            }
        }
        _ => {
            out.insert(path.to_string(), value_to_cell(Some(value)));
        }
    }
}

fn value_to_cell(v: Option<&serde_json::Value>) -> String {
    match v {
        Some(serde_json::Value::String(s)) => s.clone(),
        Some(v) => v.to_string(),
        None => String::new(),
    }
}

pub fn print_table(headers: &[&str], rows: &[Vec<String>]) {
    if rows.is_empty() {
        println!("No results.");
        return;
    }

    let term_width = terminal_size::terminal_size()
        .map(|(w, _)| w.0)
        .or_else(|| {
            std::env::var("COLUMNS")
                .ok()
                .and_then(|width| width.parse::<u16>().ok())
        })
        .unwrap_or(120);
    let mut table = Table::new();
    let header: Vec<Cell> = headers
        .iter()
        .map(|header| Cell::new(header.to_uppercase()))
        .collect();
    let data_rows: Vec<Vec<Cell>> = rows
        .iter()
        .map(|row| row.iter().map(Cell::new).collect())
        .collect();

    table
        .load_preset(ASCII_NO_BORDERS)
        .set_content_arrangement(ContentArrangement::Dynamic)
        .set_width(term_width)
        .set_header(header)
        .add_rows(data_rows);
    println!("{}", table.trim_fmt());
}

pub fn select_path(value: &serde_json::Value, path: &str) -> anyhow::Result<serde_json::Value> {
    let mut current = value;
    for segment in path.split('.') {
        if segment.is_empty() {
            continue;
        }
        current = match current {
            serde_json::Value::Object(map) => map
                .get(segment)
                .ok_or_else(|| anyhow::anyhow!("key '{}' not found in response", segment))?,
            serde_json::Value::Array(arr) => {
                let idx: usize = segment.parse().map_err(|_| {
                    anyhow::anyhow!("expected numeric index for array, got '{}'", segment)
                })?;
                arr.get(idx)
                    .ok_or_else(|| anyhow::anyhow!("index {} out of bounds", idx))?
            }
            _ => anyhow::bail!("cannot traverse into non-object/non-array value"),
        };
    }
    Ok(current.clone())
}

pub fn print_success(msg: &str) {
    if std::io::stderr().is_terminal() {
        eprintln!("{}", msg.green());
    } else {
        eprintln!("{}", msg);
    }
}

pub fn print_warning(msg: &str) {
    if std::io::stderr().is_terminal() {
        eprintln!("{}: {}", "warning".yellow().bold(), msg);
    } else {
        eprintln!("warning: {}", msg);
    }
}

pub fn print_error(msg: &str) {
    if std::io::stderr().is_terminal() {
        eprintln!("{}: {}", "error".red().bold(), msg);
    } else {
        eprintln!("error: {}", msg);
    }
}
