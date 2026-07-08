//! IndexedDB index pagination helpers for provider-side IndexedDB clients.

use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine;
use serde_json::{Map, Value};

pub struct Page<T> {
    pub items: Vec<T>,
    pub limit: usize,
    pub has_more: bool,
    pub next_cursor: Option<String>,
}

pub struct IndexPageCursor {
    pub index_key: Value,
    pub primary_key: String,
}

pub fn decode_page_cursor(cursor: Option<&str>) -> Result<Option<IndexPageCursor>, String> {
    let text = cursor.unwrap_or("").trim();
    if text.is_empty() {
        return Ok(None);
    }
    let raw = URL_SAFE_NO_PAD
        .decode(text)
        .map_err(|_| "invalid pagination cursor".to_string())?;
    let payload: Value =
        serde_json::from_slice(&raw).map_err(|_| "invalid pagination cursor".to_string())?;
    let obj = payload
        .as_object()
        .ok_or_else(|| "invalid pagination cursor".to_string())?;
    if !obj.contains_key("index_key") {
        let recorded_at = obj.get("recorded_at").and_then(Value::as_str).unwrap_or("");
        let row_id = obj.get("row_id").and_then(Value::as_str).unwrap_or("");
        if recorded_at.is_empty() && row_id.is_empty() {
            return Err("invalid pagination cursor".to_string());
        }
        return Ok(Some(IndexPageCursor {
            index_key: Value::Array(vec![
                Value::String(recorded_at.to_string()),
                Value::String(row_id.to_string()),
            ]),
            primary_key: row_id.to_string(),
        }));
    }
    Ok(Some(IndexPageCursor {
        index_key: obj.get("index_key").cloned().unwrap_or(Value::Null),
        primary_key: obj
            .get("primary_key")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string(),
    }))
}

pub fn encode_page_cursor(index_key: &Value, primary_key: &str) -> Result<String, String> {
    let payload = serde_json::json!({
        "index_key": index_key,
        "primary_key": primary_key,
    });
    let raw = serde_json::to_vec(&payload).map_err(|_| "invalid pagination cursor".to_string())?;
    Ok(URL_SAFE_NO_PAD.encode(raw))
}

pub fn prefix_index_bounds(
    prefix: &[Value],
    after_cursor: Option<&str>,
    upper_sentinels: Option<&[Value]>,
) -> Result<(Value, Value, bool), String> {
    let sentinels = upper_sentinels.unwrap_or(&[Value::String("\u{ffff}".to_string())]);
    let mut upper_parts = prefix.to_vec();
    upper_parts.extend_from_slice(sentinels);
    let upper = if upper_parts.len() == 1 {
        upper_parts[0].clone()
    } else {
        Value::Array(upper_parts)
    };
    let mut lower = Value::Array(prefix.to_vec());
    let mut lower_open = false;
    if let Some(decoded) = decode_page_cursor(after_cursor)? {
        lower = decoded.index_key;
        if let Value::Array(parts) = &lower {
            if prefix.len() == 1 && !parts.is_empty() && parts[0] != prefix[0] {
                let mut merged = vec![prefix[0].clone()];
                merged.extend(parts.clone());
                lower = Value::Array(merged);
            }
        }
        lower_open = true;
    }
    Ok((lower, upper, lower_open))
}

fn primary_key_from_record(row: &Map<String, Value>) -> String {
    for name in ["id", "record_id"] {
        if let Some(value) = row.get(name).and_then(Value::as_str) {
            if !value.is_empty() {
                return value.to_string();
            }
        }
    }
    String::new()
}

fn index_key_from_record(row: &Map<String, Value>, index_key_path: &[String]) -> Value {
    if index_key_path.len() == 1 {
        return row
            .get(&index_key_path[0])
            .cloned()
            .unwrap_or(Value::Null);
    }
    Value::Array(
        index_key_path
            .iter()
            .map(|name| row.get(name).cloned().unwrap_or(Value::Null))
            .collect(),
    )
}

pub fn paginate_index_get_all(
    rows: Vec<Map<String, Value>>,
    limit: usize,
    index_key_path: Option<&[String]>,
) -> Page<Map<String, Value>> {
    let page_limit = limit.max(1);
    let has_more = rows.len() > page_limit;
    let items = rows.into_iter().take(page_limit).collect::<Vec<_>>();
    let next_cursor = if has_more {
        items.last().and_then(|last| {
            let index_key = match index_key_path {
                Some(path) => index_key_from_record(last, path),
                None => Value::String(primary_key_from_record(last)),
            };
            encode_page_cursor(&index_key, &primary_key_from_record(last)).ok()
        })
    } else {
        None
    };
    Page {
        items,
        limit: page_limit,
        has_more,
        next_cursor,
    }
}
