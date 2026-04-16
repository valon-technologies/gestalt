use anyhow::{Context, Result};
use reqwest::Method;

use crate::api::ApiClient;
use crate::catalog::{
    self, CatalogOperation, CatalogParameter, CatalogSelectors, OperationsCatalog,
    ResolvedOperation,
};
use crate::output::{self, Format};
use crate::params::{self, ParamEntry};

#[derive(Default)]
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
    let cat = catalog::fetch_catalog(
        client,
        plugin,
        CatalogSelectors {
            connection: options.connection,
            instance: options.instance,
        },
    )?;
    let query = segments.join(".");

    match cat.resolve(&query)? {
        ResolvedOperation::All(ops) => {
            warn_ignored_params(params, "no operation specified");
            display_operations(ops, format)
        }
        ResolvedOperation::Exact(op) => execute(client, &cat, op, plugin, params, options, format),
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
    let cat = catalog::fetch_catalog(
        client,
        plugin,
        CatalogSelectors {
            connection: options.connection,
            instance: options.instance,
        },
    )?;
    let op = cat
        .find_operation(operation)
        .ok_or_else(|| anyhow::anyhow!("operation '{}' not found", operation))?;
    execute(client, &cat, op, plugin, params, options, format)
}

pub fn list_operations(client: &ApiClient, plugin: &str, format: Format) -> Result<()> {
    let cat = catalog::fetch_catalog(client, plugin, CatalogSelectors::default())?;
    display_operations(cat.operations(), format)
}

fn execute(
    client: &ApiClient,
    cat: &OperationsCatalog,
    op: &CatalogOperation,
    plugin: &str,
    params: &[ParamEntry],
    options: InvokeOptions<'_>,
    format: Format,
) -> Result<()> {
    let mut param_map = params::assemble_params(params, Some(cat), &op.id)?;

    if let Some(file_path) = options.input_file {
        let file_map = params::load_input_file(file_path)?;
        param_map = params::merge_params(file_map, param_map);
    }

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

    let path = format!("/api/v1/{}/{}", plugin, op.id);
    let method = resolve_invocation_method(op, param_map.is_empty())?;
    let resp = match method {
        Method::GET => client.get_with_query(&path, &build_query_pairs(&param_map)?),
        Method::POST => client.post(&path, &serde_json::Value::Object(param_map)),
        _ => unreachable!("only GET and POST are supported"),
    }
    .map_err(|err| {
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
    })
    .with_context(|| format!("failed to invoke {}.{}", plugin, op.id))?;

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

fn resolve_invocation_method(op: &CatalogOperation, params_empty: bool) -> Result<Method> {
    let method = op.method.trim();
    if method.eq_ignore_ascii_case("GET") {
        return Ok(Method::GET);
    }
    if method.eq_ignore_ascii_case("POST") {
        return Ok(Method::POST);
    }
    if method.is_empty() {
        // Compatibility fallback for older catalogs that omit method metadata.
        return Ok(if params_empty {
            Method::GET
        } else {
            Method::POST
        });
    }
    anyhow::bail!(
        "unsupported operation method '{}' for '{}'; expected GET or POST",
        op.method,
        op.id
    );
}

fn build_query_pairs(
    params: &serde_json::Map<String, serde_json::Value>,
) -> Result<Vec<(String, String)>> {
    let mut query = Vec::new();
    for (key, value) in params {
        append_query_pairs(&mut query, key, value)?;
    }
    Ok(query)
}

fn append_query_pairs(
    query: &mut Vec<(String, String)>,
    key: &str,
    value: &serde_json::Value,
) -> Result<()> {
    match value {
        serde_json::Value::Array(items) => {
            for item in items {
                query.push((key.to_string(), query_value(key, item)?));
            }
        }
        _ => query.push((key.to_string(), query_value(key, value)?)),
    }
    Ok(())
}

fn query_value(key: &str, value: &serde_json::Value) -> Result<String> {
    match value {
        serde_json::Value::String(s) => Ok(s.clone()),
        serde_json::Value::Bool(_) | serde_json::Value::Number(_) => Ok(value.to_string()),
        serde_json::Value::Null => anyhow::bail!(
            "GET query parameter '{}' cannot be null; omit it or use POST",
            key
        ),
        serde_json::Value::Array(_) => anyhow::bail!(
            "GET query parameter '{}' must be a scalar or flat array of scalars",
            key
        ),
        serde_json::Value::Object(_) => anyhow::bail!(
            "GET query parameter '{}' cannot be an object; use POST for structured input",
            key
        ),
    }
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
