use std::fmt::Write;
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

    let term_width = terminal_size::terminal_size()
        .map(|(w, _)| w.0 as usize)
        .unwrap_or(80);

    let output = format_table(headers, rows, term_width);
    let mut lines = output.lines();

    if let Some(header) = lines.next() {
        if std::io::stdout().is_terminal() {
            println!("{}", header.bold());
        } else {
            println!("{}", header);
        }
    }

    for line in lines {
        println!("{}", line);
    }
}

pub(crate) fn format_table(headers: &[&str], rows: &[Vec<String>], term_width: usize) -> String {
    let gap = 2;

    let mut widths: Vec<usize> = headers.iter().map(|h| h.chars().count()).collect();
    for row in rows {
        for (i, cell) in row.iter().enumerate() {
            if i < widths.len() {
                widths[i] = widths[i].max(cell.chars().count());
            }
        }
    }

    let total_gap = gap * headers.len().saturating_sub(1);
    let min_col_width = 10;
    let available = term_width.saturating_sub(total_gap);

    let mut excess = widths.iter().sum::<usize>().saturating_sub(available);
    while excess > 0 {
        let max_width = *widths.iter().max().unwrap();
        if max_width <= min_col_width {
            break;
        }
        let max_count = widths.iter().filter(|&&w| w == max_width).count();
        let second_max = widths
            .iter()
            .filter(|&&w| w < max_width)
            .max()
            .copied()
            .unwrap_or(min_col_width)
            .max(min_col_width);
        let shrink_each = (max_width - second_max).min(excess / max_count.max(1));
        let shrink_remainder = excess - shrink_each * max_count;
        let mut extra_given = 0;
        for w in &mut widths {
            if *w == max_width {
                let extra = if extra_given < shrink_remainder { 1 } else { 0 };
                let s = shrink_each + extra;
                *w -= s;
                excess = excess.saturating_sub(s);
                extra_given += extra;
            }
        }
    }

    let total_content: usize = widths.iter().sum();
    let slack = available.saturating_sub(total_content);
    if slack > 0 && !widths.is_empty() {
        let max_width = *widths.iter().max().unwrap_or(&0);
        let max_indices: Vec<usize> = widths
            .iter()
            .enumerate()
            .filter(|&(_, &w)| w == max_width)
            .map(|(i, _)| i)
            .collect();
        let per_col = slack / max_indices.len();
        let remainder = slack % max_indices.len();
        for (j, &i) in max_indices.iter().enumerate() {
            widths[i] += per_col + if j < remainder { 1 } else { 0 };
        }
    }

    let mut out = String::new();

    for (i, h) in headers.iter().enumerate() {
        if i > 0 {
            out.push_str("  ");
        }

        let _ = write!(out, "{:<width$}", h.to_uppercase(), width = widths[i]);
    }
    out.push('\n');

    for (i, w) in widths.iter().enumerate() {
        if i > 0 {
            out.push_str("  ");
        }
        for _ in 0..*w {
            out.push('-');
        }
    }
    out.push('\n');

    for row in rows {
        let wrapped: Vec<Vec<String>> = row
            .iter()
            .enumerate()
            .map(|(i, cell)| wrap_text(cell, widths.get(i).copied().unwrap_or(0)))
            .collect();

        let max_lines = wrapped.iter().map(|c| c.len()).max().unwrap_or(1);
        for line_idx in 0..max_lines {
            for (i, cell) in wrapped.iter().enumerate() {
                if i > 0 {
                    out.push_str("  ");
                }
                let w = widths.get(i).copied().unwrap_or(0);
                let text = cell.get(line_idx).map(|s| s.as_str()).unwrap_or("");

                let _ = write!(out, "{:<width$}", text, width = w);
            }
            out.push('\n');
        }
    }

    out.truncate(out.trim_end_matches('\n').len());
    out
}

fn char_width(s: &str) -> usize {
    s.chars().count()
}

fn split_at_char(s: &str, n: usize) -> (&str, &str) {
    let byte_idx = s.char_indices().nth(n).map(|(i, _)| i).unwrap_or(s.len());
    (&s[..byte_idx], &s[byte_idx..])
}

fn wrap_text(text: &str, width: usize) -> Vec<String> {
    if width == 0 || char_width(text) <= width {
        return vec![text.to_string()];
    }

    let mut lines = Vec::new();
    let mut current = String::new();
    let mut current_width: usize = 0;

    for word in text.split_whitespace() {
        let wlen = char_width(word);
        if wlen > width {
            if !current.is_empty() {
                lines.push(current);
                current = String::new();
                current_width = 0;
            }
            let mut remaining = word;
            while char_width(remaining) > width {
                let (chunk, rest) = split_at_char(remaining, width);
                lines.push(chunk.to_string());
                remaining = rest;
            }
            if !remaining.is_empty() {
                current = remaining.to_string();
                current_width = char_width(remaining);
            }
        } else if current.is_empty() {
            current = word.to_string();
            current_width = wlen;
        } else if current_width + 1 + wlen <= width {
            current.push(' ');
            current.push_str(word);
            current_width += 1 + wlen;
        } else {
            lines.push(current);
            current = word.to_string();
            current_width = wlen;
        }
    }

    if !current.is_empty() {
        lines.push(current);
    }

    if lines.is_empty() {
        vec![String::new()]
    } else {
        lines
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
