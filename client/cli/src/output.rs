use std::io::IsTerminal;

use colored::Colorize;

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

    let mut widths: Vec<usize> = headers.iter().map(|h| h.len()).collect();
    for row in rows {
        for (i, cell) in row.iter().enumerate() {
            if i < widths.len() {
                widths[i] = widths[i].max(cell.len());
            }
        }
    }

    let header_line: Vec<String> = headers
        .iter()
        .enumerate()
        .map(|(i, h)| format!("{:<width$}", h.to_uppercase(), width = widths[i]))
        .collect();

    if std::io::stdout().is_terminal() {
        println!("{}", header_line.join("  ").bold());
    } else {
        println!("{}", header_line.join("  "));
    }

    let separator: Vec<String> = widths.iter().map(|w| "-".repeat(*w)).collect();
    println!("{}", separator.join("  "));

    for row in rows {
        let line: Vec<String> = row
            .iter()
            .enumerate()
            .map(|(i, cell)| {
                let w = widths.get(i).copied().unwrap_or(0);
                format!("{:<width$}", cell, width = w)
            })
            .collect();
        println!("{}", line.join("  "));
    }
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
    use super::*;

    #[test]
    fn test_format_value_enum() {
        use clap::ValueEnum;
        let json = Format::from_str("json", false).unwrap();
        assert_eq!(json, Format::Json);
        let table = Format::from_str("table", false).unwrap();
        assert_eq!(table, Format::Table);
        assert!(Format::from_str("other", false).is_err());
    }
}
