use anyhow::{Context, Result};

use crate::api::{ApiClient, ApiError};
use crate::catalog::{
    self, CatalogOperation, CatalogParameter, OperationsCatalog, ResolvedOperation,
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
    let query = segments.join(".");
    let cat =
        match load_catalog_for_invoke(client, plugin, &query, options.connection, options.instance)
        {
            Ok(cat) => cat,
            Err(err) => {
                if should_retry_without_catalog(&err, &query) {
                    return execute(client, None, plugin, &query, params, options, format);
                }
                return Err(err);
            }
        };

    match cat.resolve(&query)? {
        ResolvedOperation::All(ops) => {
            warn_ignored_params(params, "no operation specified");
            display_operations(ops, format)
        }
        ResolvedOperation::Exact(_) => {
            execute(client, Some(&cat), plugin, &query, params, options, format)
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
    let cat = match load_catalog_for_invoke(
        client,
        plugin,
        operation,
        options.connection,
        options.instance,
    ) {
        Ok(cat) => Some(cat),
        Err(err) => {
            if should_retry_without_catalog(&err, operation) {
                return execute(client, None, plugin, operation, params, options, format);
            }
            return Err(err);
        }
    };
    execute(
        client,
        cat.as_ref(),
        plugin,
        operation,
        params,
        options,
        format,
    )
}

pub fn list_operations(client: &ApiClient, plugin: &str, format: Format) -> Result<()> {
    let cat = catalog::fetch_catalog(client, plugin, None, None)?;
    display_operations(cat.operations(), format)
}

fn execute(
    client: &ApiClient,
    cat: Option<&OperationsCatalog>,
    plugin: &str,
    operation: &str,
    params: &[ParamEntry],
    options: InvokeOptions<'_>,
    format: Format,
) -> Result<()> {
    let mut param_map = params::assemble_params(params, cat, operation)?;

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

    let path = format!("/api/v1/{}/{}", plugin, operation);
    let resp = (if param_map.is_empty() {
        client.get(&path)
    } else {
        client.post(&path, &serde_json::Value::Object(param_map))
    })
    .map_err(|err| rewrite_connect_error(err, plugin, options.connection, options.instance))
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

fn should_retry_without_catalog(err: &anyhow::Error, operation: &str) -> bool {
    !operation.is_empty() && connect_error_kind(err).is_some()
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

fn load_catalog_for_invoke(
    client: &ApiClient,
    plugin: &str,
    operation: &str,
    connection: Option<&str>,
    instance: Option<&str>,
) -> Result<OperationsCatalog> {
    catalog::fetch_catalog(client, plugin, connection, instance)
        .map_err(|err| rewrite_connect_error(err, plugin, connection, instance))
        .with_context(|| format!("failed to invoke {}", invoke_target(plugin, operation)))
}

fn rewrite_connect_error(
    err: anyhow::Error,
    plugin: &str,
    connection: Option<&str>,
    instance: Option<&str>,
) -> anyhow::Error {
    let connect_command = connect_command(plugin, connection, instance);

    match connect_error_kind(&err) {
        Some(ConnectErrorKind::NotConnected) => anyhow::anyhow!(
            "plugin {:?} is not connected. Connect it first with `{}`",
            plugin,
            connect_command,
        ),
        Some(ConnectErrorKind::ReconnectRequired) => anyhow::anyhow!(
            "token for plugin {:?} expired or was revoked. Reconnect it with `{}`",
            plugin,
            connect_command,
        ),
        None => err,
    }
}

fn connect_command(plugin: &str, connection: Option<&str>, instance: Option<&str>) -> String {
    let mut connect_command = format!("gestalt plugin connect {}", plugin);
    if let Some(connection) = connection {
        connect_command.push_str(" --connection ");
        connect_command.push_str(connection);
    }
    if let Some(instance) = instance {
        connect_command.push_str(" --instance ");
        connect_command.push_str(instance);
    }
    connect_command
}

fn invoke_target(plugin: &str, operation: &str) -> String {
    if operation.is_empty() {
        plugin.to_string()
    } else {
        format!("{plugin}.{operation}")
    }
}

enum ConnectErrorKind {
    NotConnected,
    ReconnectRequired,
}

fn connect_error_kind(err: &anyhow::Error) -> Option<ConnectErrorKind> {
    for cause in err.chain() {
        if let Some(api_error) = cause.downcast_ref::<ApiError>() {
            match api_error.code() {
                Some("not_connected") => return Some(ConnectErrorKind::NotConnected),
                Some("reconnect_required") => return Some(ConnectErrorKind::ReconnectRequired),
                _ => {}
            }
        }

        let message = cause.to_string();
        if message.contains("no token stored for integration") {
            return Some(ConnectErrorKind::NotConnected);
        }
        if message.contains("is not connected. Connect it first with `") {
            return Some(ConnectErrorKind::NotConnected);
        }
        if message.contains("expired or was revoked") {
            return Some(ConnectErrorKind::ReconnectRequired);
        }
    }
    None
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
