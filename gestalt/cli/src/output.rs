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
        serde_json::Value::Array(arr) => {
            let rows: Vec<serde_json::Value> = arr.iter().map(flatten_table_object).collect();
            let Some(first) = rows.first().and_then(|v| v.as_object()) else {
                print_json(value);
                return;
            };
            let headers: Vec<&str> = first.keys().map(|k| k.as_str()).collect();
            let rows: Vec<Vec<String>> = rows
                .iter()
                .filter_map(|v| v.as_object())
                .map(|obj| headers.iter().map(|h| value_to_cell(obj.get(*h))).collect())
                .collect();
            print_table(&headers, &rows);
        }
        serde_json::Value::Object(obj) => {
            if let Some((array_key, arr)) = single_array_field(obj) {
                println!("{}", array_key.to_uppercase());
                print_json_table(&serde_json::Value::Array(
                    arr.iter().map(flatten_table_object).collect(),
                ));

                let rows: Vec<Vec<String>> = obj
                    .iter()
                    .filter(|(key, _)| key.as_str() != array_key)
                    .map(|(key, value)| vec![key.clone(), value_to_cell(Some(value))])
                    .collect();
                if !rows.is_empty() {
                    println!();
                    println!("METADATA");
                    print_table(&["key", "value"], &rows);
                }
                return;
            }

            let headers = vec!["key", "value"];
            let rows: Vec<Vec<String>> = obj
                .iter()
                .map(|(k, v)| vec![k.clone(), value_to_cell(Some(v))])
                .collect();
            print_table(&headers, &rows);
        }
        _ => print_json(value),
    }
}

fn single_array_field(
    obj: &serde_json::Map<String, serde_json::Value>,
) -> Option<(&str, &Vec<serde_json::Value>)> {
    let mut array_fields = obj
        .iter()
        .filter_map(|(key, value)| value.as_array().map(|arr| (key.as_str(), arr)));
    let first = array_fields.next()?;
    if array_fields.next().is_some() {
        return None;
    }
    Some(first)
}

fn flatten_table_object(value: &serde_json::Value) -> serde_json::Value {
    let serde_json::Value::Object(obj) = value else {
        return value.clone();
    };
    serde_json::Value::Object(
        obj.iter()
            .flat_map(|(key, value)| match value {
                serde_json::Value::Object(nested) if !nested.is_empty() => nested
                    .iter()
                    .map(|(nested_key, nested_value)| {
                        (format!("{key}.{nested_key}"), nested_value.clone())
                    })
                    .collect::<Vec<_>>(),
                _ => vec![(key.clone(), value.clone())],
            })
            .collect(),
    )
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
        .unwrap_or(160);
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
