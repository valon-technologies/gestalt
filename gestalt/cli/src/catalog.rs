use anyhow::{Context, Result, ensure};
use serde::{Deserialize, Serialize};

use crate::api::ApiClient;

pub enum ResolvedOperation<'a> {
    All(&'a [CatalogOperation]),
    Exact(&'a CatalogOperation),
    Prefix(Vec<&'a CatalogOperation>),
}

fn default_type() -> String {
    "string".to_string()
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct CatalogOperation {
    pub id: String,
    #[serde(default)]
    pub title: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub method: String,
    #[serde(default)]
    pub parameters: Vec<CatalogParameter>,
    #[serde(default)]
    pub input_schema: Option<serde_json::Value>,
    #[serde(default)]
    pub transport: String,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct CatalogParameter {
    pub name: String,
    #[serde(default = "default_type")]
    pub r#type: String,
    #[serde(default)]
    pub location: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub required: bool,
}

pub struct OperationsCatalog {
    operations: Vec<CatalogOperation>,
}

impl OperationsCatalog {
    pub fn find_operation(&self, name: &str) -> Option<&CatalogOperation> {
        self.operations.iter().find(|op| op.id == name)
    }

    pub fn is_array_param(&self, operation: &str, param_name: &str) -> Option<bool> {
        let op = self.find_operation(operation)?;
        let param = op.parameters.iter().find(|p| p.name == param_name)?;
        Some(param.r#type == "array")
    }

    pub fn operations(&self) -> &[CatalogOperation] {
        &self.operations
    }

    pub fn resolve(&self, query: &str) -> Result<ResolvedOperation<'_>> {
        if query.is_empty() {
            return Ok(ResolvedOperation::All(&self.operations));
        }

        ensure!(
            is_valid_operation_query(query),
            "invalid operation '{}': segments must be alphanumeric, underscore, or hyphen (no leading/trailing hyphen)",
            query,
        );

        if let Some(op) = self.find_operation(query) {
            return Ok(ResolvedOperation::Exact(op));
        }

        let prefix = format!("{}.", query);
        let matches: Vec<_> = self
            .operations
            .iter()
            .filter(|op| op.id.starts_with(&prefix))
            .collect();

        ensure!(
            !matches.is_empty(),
            "no operation matching '{}' found",
            query
        );

        Ok(ResolvedOperation::Prefix(matches))
    }
}

fn is_valid_operation_query(query: &str) -> bool {
    query.split('.').all(|seg| {
        !seg.is_empty()
            && !seg.starts_with('-')
            && !seg.ends_with('-')
            && seg
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
    })
}

pub fn fetch_catalog(
    client: &ApiClient,
    plugin: &str,
    connection: Option<&str>,
    instance: Option<&str>,
) -> Result<OperationsCatalog> {
    let mut params = Vec::new();
    if let Some(connection) = connection {
        params.push(("_connection", connection));
    }
    if let Some(instance) = instance {
        params.push(("_instance", instance));
    }

    let path = if params.is_empty() {
        format!("/api/v1/integrations/{plugin}/operations")
    } else {
        let query =
            serde_urlencoded::to_string(params).context("failed to encode catalog selectors")?;
        format!("/api/v1/integrations/{plugin}/operations?{query}")
    };
    let resp = client.get(&path)?;
    let operations: Vec<CatalogOperation> =
        serde_json::from_value(resp).context("failed to parse operations response")?;
    Ok(OperationsCatalog { operations })
}
