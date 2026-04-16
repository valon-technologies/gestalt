use anyhow::{Context, Result};
use reqwest::Method;
use std::collections::BTreeMap;

use crate::api::ApiClient;
use crate::catalog::{
    self, CatalogOperation, CatalogParameter, CatalogSelectors, OperationsCatalog,
    ResolvedOperation,
};
use crate::output::{self, Format};
use crate::params::{self, ParamEntry};

#[derive(Default, Clone, Copy)]
pub struct InvokeOptions<'a> {
    pub connection: Option<&'a str>,
    pub instance: Option<&'a str>,
    pub select: Option<&'a str>,
    pub input_file: Option<&'a str>,
}

pub fn run(
    client: &ApiClient,
    plugin: &str,
    segments: &[String],
    params: &[ParamEntry],
    options: InvokeOptions<'_>,
    format: Format,
) -> Result<()> {
    let cat = fetch_catalog_for_invoke(client, plugin, options)?;
    let query = segments.join(".");

    match cat.resolve(&query)? {
        ResolvedOperation::All(ops) => {
            warn_ignored_params(params, "no operation specified");
            display_operations(ops, format)
        }
        ResolvedOperation::Exact(_) => {
            execute(client, &cat, plugin, &query, params, options, format)
        }
        ResolvedOperation::Prefix(matches) => {
            let n = matches.len();
            let reason = format!(
                "prefix matched {} operation{}",
                n,
                if n == 1 { "" } else { "s" }
            );
            warn_ignored_params(params, &reason);
            display_operations(matches, format)
        }
    }
}

pub fn invoke(
    client: &ApiClient,
    plugin: &str,
    operation: &str,
    params: &[ParamEntry],
    options: InvokeOptions<'_>,
    format: Format,
) -> Result<()> {
    let cat = fetch_catalog_for_invoke(client, plugin, options)?;
    execute(client, &cat, plugin, operation, params, options, format)
}

pub fn list_operations(client: &ApiClient, plugin: &str, format: Format) -> Result<()> {
    let cat = catalog::fetch_catalog_with_selectors(client, plugin, CatalogSelectors::default())?;
    display_operations(cat.operations(), format)
}

fn execute(
    client: &ApiClient,
    cat: &OperationsCatalog,
    plugin: &str,
    operation: &str,
    params: &[ParamEntry],
    options: InvokeOptions<'_>,
    format: Format,
) -> Result<()> {
    let mut param_map = params::assemble_params(params, Some(cat), operation)?;

    if let Some(file_path) = options.input_file {
        let file_map = params::load_input_file(file_path)?;
        param_map = params::merge_params(file_map, param_map);
    }

    let request_method = request_method(
        cat.find_operation(operation),
        &param_map,
        options.connection.is_some() || options.instance.is_some(),
    );

    let path = format!("/api/v1/{}/{}", plugin, operation);
    let resp = match request_method {
        Method::GET => {
            let query_path = operation_query_path(&path, &param_map, options)?;
            client.get(&query_path)
        }
        Method::POST => {
            if let Some(connection) = options.connection {
                param_map.insert(
                    "_connection".to_string(),
                    serde_json::Value::String(connection.to_string()),
                );
            }
            if let Some(instance) = options.instance {
                param_map.insert(
                    "_instance".to_string(),
                    serde_json::Value::String(instance.to_string()),
                );
            }
            client.post(&path, &serde_json::Value::Object(param_map))
        }
        _ => unreachable!("unsupported method already rejected"),
    }
    .map_err(|err| rewrite_connect_error(err, plugin, options))
    .with_context(|| format!("failed to invoke {}.{}", plugin, operation))?;

    let output_value = match options.select {
        Some(sel_path) => output::select_path(&resp, sel_path)?,
        None => resp,
    };

    match format {
        Format::Json => output::print_json(&output_value),
        Format::Table => output::print_json_table(&output_value),
    }

    Ok(())
}

fn catalog_selectors(options: InvokeOptions<'_>) -> CatalogSelectors<'_> {
    CatalogSelectors {
        connection: options.connection,
        instance: options.instance,
    }
}

fn fetch_catalog_for_invoke(
    client: &ApiClient,
    plugin: &str,
    options: InvokeOptions<'_>,
) -> Result<OperationsCatalog> {
    catalog::fetch_catalog_with_selectors(client, plugin, catalog_selectors(options))
        .map_err(|err| rewrite_connect_error(err, plugin, options))
        .with_context(|| format!("failed to load catalog for plugin '{}'", plugin))
}

fn request_method(
    operation: Option<&CatalogOperation>,
    params: &serde_json::Map<String, serde_json::Value>,
    has_selectors: bool,
) -> Method {
    let Some(operation) = operation else {
        return if !params.is_empty() || has_selectors {
            Method::POST
        } else {
            Method::GET
        };
    };

    let method = operation.method.trim();
    if method.is_empty() {
        return if !params.is_empty() || has_selectors {
            Method::POST
        } else {
            Method::GET
        };
    }

    if method.eq_ignore_ascii_case("GET") && params.values().all(query_value_is_safe) {
        Method::GET
    } else {
        Method::POST
    }
}

fn operation_query_path(
    path: &str,
    params: &serde_json::Map<String, serde_json::Value>,
    options: InvokeOptions<'_>,
) -> Result<String> {
    let mut query_pairs = catalog::selector_query_pairs(catalog_selectors(options));
    let mut sorted_params = BTreeMap::new();
    for (key, value) in params {
        let encoded = query_value(value)
            .with_context(|| format!("failed to encode query parameter '{}'", key))?;
        sorted_params.insert(key.clone(), encoded);
    }
    query_pairs.extend(sorted_params);
    catalog::append_query_pairs(path, query_pairs)
}

fn query_value_is_safe(value: &serde_json::Value) -> bool {
    matches!(value, serde_json::Value::String(_))
}

fn query_value(value: &serde_json::Value) -> Result<String> {
    match value {
        serde_json::Value::String(value) => Ok(value.clone()),
        _ => serde_json::to_string(value).context("failed to encode query parameter"),
    }
}

fn rewrite_connect_error(
    err: anyhow::Error,
    plugin: &str,
    options: InvokeOptions<'_>,
) -> anyhow::Error {
    let message = err.to_string();
    if !message.contains("no token stored for integration") {
        return err;
    }

    let mut connect_command = format!("gestalt plugins connect {}", plugin);
    if let Some(connection) = options.connection {
        connect_command.push_str(" --connection ");
        connect_command.push_str(connection);
    }
    if let Some(instance) = options.instance {
        connect_command.push_str(" --instance ");
        connect_command.push_str(instance);
    }

    anyhow::anyhow!(
        "plugin {:?} is not connected. Connect it first with `{}`",
        plugin,
        connect_command,
    )
}

fn display_operations<'a>(
    operations: impl IntoIterator<Item = &'a CatalogOperation>,
    format: Format,
) -> Result<()> {
    let ops: Vec<&CatalogOperation> = operations.into_iter().collect();

    match format {
        Format::Json => {
            output::print_json(&serde_json::to_value(&ops).unwrap());
        }
        Format::Table => {
            let rows: Vec<Vec<String>> = ops
                .iter()
                .map(|op| {
                    vec![
                        op.id.clone(),
                        op.description.clone(),
                        op.method.clone(),
                        format_parameters(&op.parameters),
                    ]
                })
                .collect();
            output::print_table(&["Name", "Description", "Method", "Parameters"], &rows);
        }
    }

    Ok(())
}

fn warn_ignored_params(params: &[ParamEntry], reason: &str) {
    if !params.is_empty() {
        output::print_warning(&format!("parameters ignored; {}", reason));
    }
}

fn format_parameters(params: &[CatalogParameter]) -> String {
    params
        .iter()
        .map(|p| {
            let location_hint = if p.location.is_empty() {
                String::new()
            } else {
                format!(" [{}]", p.location)
            };
            let mut s = format!("-p {}=<{}>{}", p.name, p.r#type, location_hint);
            if p.required {
                s.push_str(" (required)");
            }
            s
        })
        .collect::<Vec<_>>()
        .join(", ")
}
