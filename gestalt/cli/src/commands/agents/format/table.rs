use serde_json::Value;

use crate::output::{self, Format};

use super::transcript::{
    fallback_turn_event_data_summary, turn_event_data_summary,
    turn_event_display_summary_from_value,
};

pub(crate) fn print_session(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows = vec![session_row(value)];
            output::print_table(&session_headers(), &rows);
        }
    }
}

pub(crate) fn print_sessions(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items.iter().map(session_row).collect();
            output::print_table(&session_headers(), &rows);
        }
    }
}

pub(crate) fn print_turn(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows = vec![turn_row(value)];
            output::print_table(&turn_headers(), &rows);
        }
    }
}

pub(crate) fn print_turns(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items.iter().map(turn_row).collect();
            output::print_table(&turn_headers(), &rows);
        }
    }
}

pub(crate) fn print_turn_events(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items.iter().map(turn_event_row).collect();
            output::print_table(&event_headers(), &rows);
        }
    }
}

fn session_headers() -> [&'static str; 6] {
    ["ID", "Provider", "Model", "State", "Client Ref", "Updated"]
}

fn turn_headers() -> [&'static str; 6] {
    ["ID", "Session", "Provider", "Model", "Status", "Created"]
}

fn event_headers() -> [&'static str; 7] {
    [
        "Seq",
        "Type",
        "Source",
        "Visibility",
        "Turn",
        "Created",
        "Display",
    ]
}

pub(crate) fn session_row(value: &Value) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        value["provider"].as_str().unwrap_or("-").to_string(),
        value["model"].as_str().unwrap_or("-").to_string(),
        value["state"].as_str().unwrap_or("-").to_string(),
        value["clientRef"].as_str().unwrap_or("-").to_string(),
        value["updatedAt"].as_str().unwrap_or("-").to_string(),
    ]
}

pub(crate) fn turn_row(value: &Value) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        value["sessionId"].as_str().unwrap_or("-").to_string(),
        value["provider"].as_str().unwrap_or("-").to_string(),
        value["model"].as_str().unwrap_or("-").to_string(),
        value["status"].as_str().unwrap_or("-").to_string(),
        value["createdAt"].as_str().unwrap_or("-").to_string(),
    ]
}

pub(crate) fn turn_event_row(value: &Value) -> Vec<String> {
    let display = turn_event_display_summary_from_value(value)
        .unwrap_or_else(|| fallback_turn_event_data_summary(value));
    vec![
        value["seq"]
            .as_i64()
            .map(|seq| seq.to_string())
            .unwrap_or_else(|| "-".to_string()),
        value["type"].as_str().unwrap_or("-").to_string(),
        value["source"].as_str().unwrap_or("-").to_string(),
        value["visibility"].as_str().unwrap_or("-").to_string(),
        value["turnId"].as_str().unwrap_or("-").to_string(),
        value["createdAt"].as_str().unwrap_or("-").to_string(),
        display,
    ]
}
