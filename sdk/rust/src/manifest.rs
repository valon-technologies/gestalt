use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};
use serde_json::Value;

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct ManifestMetadata {
    #[serde(
        rename = "securitySchemes",
        default,
        skip_serializing_if = "BTreeMap::is_empty"
    )]
    pub security_schemes: BTreeMap<String, WebhookSecurityScheme>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub webhooks: BTreeMap<String, WebhookDef>,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct WebhookSecurityScheme {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub description: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub name: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub r#in: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub scheme: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub secret: Option<SecretRef>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub signature: Option<SignatureConfig>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub replay: Option<ReplayConfig>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub mtls: Option<MTLSConfig>,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct SecretRef {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub env: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub secret: String,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct SignatureConfig {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub algorithm: String,
    #[serde(
        rename = "signatureHeader",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub signature_header: String,
    #[serde(
        rename = "timestampHeader",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub timestamp_header: String,
    #[serde(
        rename = "deliveryIdHeader",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub delivery_id_header: String,
    #[serde(
        rename = "payloadTemplate",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub payload_template: String,
    #[serde(
        rename = "digestPrefix",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub digest_prefix: String,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct ReplayConfig {
    #[serde(rename = "maxAge", default, skip_serializing_if = "String::is_empty")]
    pub max_age: String,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct MTLSConfig {
    #[serde(
        rename = "subjectAltName",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub subject_alt_name: String,
    #[serde(
        rename = "forwardedBy",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub forwarded_by: String,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct WebhookDef {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub summary: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub description: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub path: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub get: Option<WebhookOperation>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub post: Option<WebhookOperation>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub put: Option<WebhookOperation>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub delete: Option<WebhookOperation>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub target: Option<WebhookTarget>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub execution: Option<WebhookExecution>,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct WebhookOperation {
    #[serde(
        rename = "operationId",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub operation_id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub summary: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub description: String,
    #[serde(
        rename = "requestBody",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub request_body: Option<WebhookRequestBody>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub responses: BTreeMap<String, WebhookResponse>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub security: Vec<SecurityRequirement>,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct WebhookRequestBody {
    #[serde(default)]
    pub required: bool,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub content: BTreeMap<String, WebhookMediaType>,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct WebhookMediaType {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub schema: Option<Value>,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct WebhookResponse {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub description: String,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub headers: BTreeMap<String, String>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub content: BTreeMap<String, WebhookMediaType>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub body: Option<Value>,
}

pub type SecurityRequirement = BTreeMap<String, Vec<String>>;

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct WebhookTarget {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub operation: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub workflow: Option<WebhookWorkflowTarget>,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct WebhookExecution {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub mode: String,
    #[serde(
        rename = "acceptedResponse",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub accepted_response: String,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct WebhookWorkflowTarget {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub provider: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub plugin: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub operation: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub connection: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub instance: String,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub input: BTreeMap<String, Value>,
}
