use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};

use crate::api::ApiClient;

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
    pub fn new(operations: Vec<CatalogOperation>) -> Self {
        Self { operations }
    }

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
}

pub fn fetch_catalog(client: &ApiClient, integration: &str) -> Result<OperationsCatalog> {
    let path = format!("/api/v1/integrations/{}/operations", integration);
    let resp = client.get(&path)?;
    let operations: Vec<CatalogOperation> =
        serde_json::from_value(resp).context("failed to parse operations response")?;
    Ok(OperationsCatalog { operations })
}
