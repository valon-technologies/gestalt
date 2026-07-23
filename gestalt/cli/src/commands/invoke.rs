use anyhow::{Context, Result};

use crate::api::ApiClient;
use crate::catalog::{
    self, CatalogOperation, CatalogParameter, OperationsCatalog, ResolvedOperation,
};
use crate::output::{self, Format};
use crate::params::{self, ParamEntry};

use super::app_errors;

#[derive(Clone, Copy, Default)]
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
    let resolved_selector = match app_errors::resolve_selector(
        client,
        plugin,
        &query,
        options.connection,
        options.instance,
        app_errors::SelectorCommand::Invoke,
    ) {
        app_errors::SelectorResolution::Selected(selector) => Some(selector),
        app_errors::SelectorResolution::Message(message) => {
            anyhow::bail!(message);
        }
        app_errors::SelectorResolution::Unchanged => None,
    };
    let options = InvokeOptions {
        connection: resolved_selector
            .as_ref()
            .map(|selector| selector.connection.as_str())
            .or(options.connection),
        instance: resolved_selector
            .as_ref()
            .map(|selector| selector.instance.as_str())
            .or(options.instance),
        select: options.select,
        input_file: options.input_file,
    };

    let mut cat =
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
    if cat.operations().is_empty() && (options.connection.is_some() || options.instance.is_some()) {
        let fallback_cat = load_catalog_for_invoke(client, plugin, &query, None, None)?;
        if !fallback_cat.operations().is_empty() {
            cat = fallback_cat;
        }
    }

    let handle_resolved =
        |cat: &OperationsCatalog, resolved: ResolvedOperation<'_>, options: InvokeOptions<'_>| {
            match resolved {
                ResolvedOperation::All(ops) => {
                    warn_ignored_params(params, "no operation specified");
                    display_operations(ops, format, options.connection, options.instance)
                }
                ResolvedOperation::Exact(_) => {
                    execute(client, Some(cat), plugin, &query, params, options, format)
                }
                ResolvedOperation::Prefix(matches) => {
                    let n = matches.len();
                    let reason = format!(
                        "prefix matched {} operation{}",
                        n,
                        if n == 1 { "" } else { "s" }
                    );
                    warn_ignored_params(params, &reason);
                    display_operations(matches, format, options.connection, options.instance)
                }
            }
        };

    match cat.resolve(&query) {
        Ok(resolved) => handle_resolved(&cat, resolved, options),
        Err(err)
            if !query.is_empty()
                && (options.connection.is_some() || options.instance.is_some()) =>
        {
            match load_catalog_for_invoke(client, plugin, &query, None, None) {
                Ok(fallback_cat) => match fallback_cat.resolve(&query) {
                    Ok(resolved) => handle_resolved(&fallback_cat, resolved, options),
                    Err(_) => Err(err),
                },
                Err(_) => Err(err),
            }
        }
        Err(err) => Err(err),
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
    let selector_resolution = app_errors::resolve_selector(
        client,
        plugin,
        operation,
        options.connection,
        options.instance,
        app_errors::SelectorCommand::Invoke,
    );
    let resolved_selector = match selector_resolution {
        app_errors::SelectorResolution::Selected(selector) => Some(selector),
        app_errors::SelectorResolution::Message(message) => anyhow::bail!(message),
        app_errors::SelectorResolution::Unchanged => None,
    };
    let options = InvokeOptions {
        connection: resolved_selector
            .as_ref()
            .map(|selector| selector.connection.as_str())
            .or(options.connection),
        instance: resolved_selector
            .as_ref()
            .map(|selector| selector.instance.as_str())
            .or(options.instance),
        select: options.select,
        input_file: options.input_file,
    };

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
    list_operations_with_selector(client, plugin, None, None, format)
}

pub fn list_operations_with_selector(
    client: &ApiClient,
    plugin: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    format: Format,
) -> Result<()> {
    let cat = app_errors::map_catalog_error(
        client,
        plugin,
        "",
        connection,
        instance,
        app_errors::SelectorCommand::Invoke,
        catalog::fetch_catalog(client, plugin, connection, instance),
    )?;
    display_operations(cat.operations(), format, connection, instance)
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

    let transport = gestalt_sdk::public::rest_transport::SyncRestTransport::new(
        client.base_url(),
        std::sync::Arc::new(gestalt_sdk::public::auth::BearerAuth::new(client.token())),
    )
    .with_timeout(std::time::Duration::from_secs(30));
    let app_client = gestalt_sdk::public::generated::app_client::AppClient::new(transport);

    let request = gestalt_sdk::public::generated::app::AppInvokeRequest {
        app: plugin.to_string(),
        operation: operation.to_string(),
        params: if param_map.is_empty() {
            None
        } else {
            Some(param_map)
        },
        connection: options.connection.unwrap_or_default().to_string(),
        instance: options.instance.unwrap_or_default().to_string(),
        ..Default::default()
    };
    let resp = app_client
        .invoke_sync(request)
        .map_err(|err| {
            app_errors::rewrite_connect_error(
                client,
                anyhow::Error::new(err),
                plugin,
                operation,
                options.connection,
                options.instance,
                app_errors::SelectorCommand::Invoke,
            )
        })
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
    app_errors::should_retry_without_catalog(err, operation)
}

fn display_operations<'a>(
    operations: impl IntoIterator<Item = &'a CatalogOperation>,
    format: Format,
    connection: Option<&str>,
    instance: Option<&str>,
) -> Result<()> {
    let ops: Vec<&CatalogOperation> = operations.into_iter().collect();

    match format {
        Format::Json => {
            output::print_json(&serde_json::to_value(&ops).unwrap());
        }
        Format::Table => {
            if connection.is_some() || instance.is_some() {
                if let Some(connection) = connection {
                    println!("Connection: {connection}");
                }
                if let Some(instance) = instance {
                    println!("Instance:   {instance}");
                }
                println!();
            }
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
    app_errors::map_catalog_error(
        client,
        plugin,
        operation,
        connection,
        instance,
        app_errors::SelectorCommand::Invoke,
        catalog::fetch_catalog(client, plugin, connection, instance),
    )
    .with_context(|| format!("failed to invoke {}", invoke_target(plugin, operation)))
}

fn invoke_target(plugin: &str, operation: &str) -> String {
    if operation.is_empty() {
        plugin.to_string()
    } else {
        format!("{plugin}.{operation}")
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
