use anyhow::{Context, Result};

pub fn append_query(path: &str, params: &[(String, String)]) -> Result<String> {
    if params.is_empty() {
        return Ok(path.to_string());
    }
    Ok(format!(
        "{}?{}",
        path,
        serde_urlencoded::to_string(params).context("failed to encode query")?
    ))
}

pub fn page_params(page_size: Option<u32>, page_token: Option<&str>) -> Vec<(String, String)> {
    let mut params = Vec::new();
    if let Some(page_size) = page_size {
        params.push(("pageSize".to_string(), page_size.to_string()));
    }
    push_opt_param(&mut params, "pageToken", page_token);
    params
}

pub fn push_opt_param(params: &mut Vec<(String, String)>, name: &str, value: Option<&str>) {
    if let Some(value) = value.map(str::trim).filter(|value| !value.is_empty()) {
        params.push((name.to_string(), value.to_string()));
    }
}

pub fn push_opt_u32(params: &mut Vec<(String, String)>, name: &str, value: Option<u32>) {
    if let Some(value) = value {
        params.push((name.to_string(), value.to_string()));
    }
}

pub fn push_opt_u64(params: &mut Vec<(String, String)>, name: &str, value: Option<u64>) {
    if let Some(value) = value {
        params.push((name.to_string(), value.to_string()));
    }
}

pub fn with_query(path: &str, params: &[(String, String)]) -> String {
    append_query(path, params).unwrap_or_else(|_| path.to_string())
}
