use anyhow::{Context, Result, ensure};
use serde::{Deserialize, Serialize};

use crate::api::ApiClient;

#[derive(Default, Serialize)]
pub struct CatalogSelectors<'a> {
    #[serde(rename = "_connection")]
    #[serde(skip_serializing_if = "Option::is_none")]
    pub connection: Option<&'a str>,
    #[serde(rename = "_instance")]
    #[serde(skip_serializing_if = "Option::is_none")]
    pub instance: Option<&'a str>,
}

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
    selectors: CatalogSelectors<'_>,
) -> Result<OperationsCatalog> {
    let path = format!("/api/v1/integrations/{}/operations", plugin);
    let resp = if selectors.connection.is_some() || selectors.instance.is_some() {
        client.get_with_query(&path, &selectors)?
    } else {
        client.get(&path)?
    };
    let operations: Vec<CatalogOperation> =
        serde_json::from_value(resp).context("failed to parse operations response")?;
    Ok(OperationsCatalog { operations })
}
