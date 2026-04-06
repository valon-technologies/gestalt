use std::io::IsTerminal;

use colored::Colorize;
use comfy_table::{
    Cell, ContentArrangement, Table,
    modifiers::UTF8_ROUND_CORNERS,
    presets::{UTF8_BORDERS_ONLY, UTF8_FULL_CONDENSED},
};

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

    println!(
        "{}",
        render_table_with_style(headers, rows, TableStyle::BordersOnly)
    );
}

#[cfg_attr(not(test), allow(dead_code))]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum TableStyle {
    BordersOnly,
    FullCondensed,
}

fn render_table_with_style(headers: &[&str], rows: &[Vec<String>], style: TableStyle) -> String {
    let term_width = terminal_size::terminal_size()
        .map(|(w, _)| w.0)
        .unwrap_or(80);
    render_table_with_style_and_width(headers, rows, style, term_width)
}

fn render_table_with_style_and_width(
    headers: &[&str],
    rows: &[Vec<String>],
    style: TableStyle,
    width: u16,
) -> String {
    let mut table = Table::new();
    let header: Vec<Cell> = headers.iter().map(|header| Cell::new(*header)).collect();
    let data_rows: Vec<Vec<Cell>> = rows
        .iter()
        .map(|row| row.iter().map(Cell::new).collect())
        .collect();

    table
        .load_preset(match style {
            TableStyle::BordersOnly => UTF8_BORDERS_ONLY,
            TableStyle::FullCondensed => UTF8_FULL_CONDENSED,
        })
        .apply_modifier(UTF8_ROUND_CORNERS)
        .set_content_arrangement(ContentArrangement::Dynamic)
        .set_width(width)
        .set_header(header)
        .add_rows(data_rows);

    table.trim_fmt()
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

#[cfg(test)]
mod tests {
    use super::{TableStyle, render_table_with_style_and_width};

    #[test]
    fn table_style_presets_render_with_utf8_chrome() {
        let headers = ["Name", "Description", "Connected"];
        let rows = vec![vec![
            "bigquery".to_string(),
            "Google BigQuery data warehouse".to_string(),
            "yes".to_string(),
        ]];

        let borders_only =
            render_table_with_style_and_width(&headers, &rows, TableStyle::BordersOnly, 72);
        let full_condensed =
            render_table_with_style_and_width(&headers, &rows, TableStyle::FullCondensed, 72);

        let left_lines: Vec<&str> = borders_only.lines().collect();
        let right_lines: Vec<&str> = full_condensed.lines().collect();
        let left_width = left_lines
            .iter()
            .map(|line| line.chars().count())
            .max()
            .unwrap_or(0);

        println!();
        println!(
            "{:<left_width$}    {}",
            "Borders only",
            "Full condensed",
            left_width = left_width
        );

        for idx in 0..left_lines.len().max(right_lines.len()) {
            let left = left_lines.get(idx).copied().unwrap_or("");
            let right = right_lines.get(idx).copied().unwrap_or("");
            println!(
                "{:<left_width$}    {}",
                left,
                right,
                left_width = left_width
            );
        }
        println!();

        assert!(borders_only.contains('╭'));
        assert!(borders_only.contains('╰'));
        assert!(full_condensed.contains('╭'));
        assert!(full_condensed.contains('╰'));
        assert!(full_condensed.contains('┆'));
        assert!(!borders_only.contains('+'));
        assert!(!full_condensed.contains('+'));
    }
}
