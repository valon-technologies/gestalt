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
            let Some(first) = arr.first().and_then(|v| v.as_object()) else {
                print_json(value);
                return;
            };
            let headers: Vec<&str> = first.keys().map(|k| k.as_str()).collect();
            let rows: Vec<Vec<String>> = arr
                .iter()
                .filter_map(|v| v.as_object())
                .map(|obj| headers.iter().map(|h| value_to_cell(obj.get(*h))).collect())
                .collect();
            print_table(&headers, &rows);
        }
        serde_json::Value::Object(obj) => {
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
        .unwrap_or(80);
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
