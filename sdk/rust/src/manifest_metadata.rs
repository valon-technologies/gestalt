use std::collections::BTreeMap;
use std::path::Path;

use serde::{Deserialize, Serialize};
use serde_json::Value as JsonValue;

use crate::error::{Error, Result};

/// Optional manifest metadata emitted for code-defined hosted HTTP bindings.
#[derive(Clone, Debug, Default, Eq, PartialEq, Serialize, Deserialize)]
pub struct PluginManifestMetadata {
    #[serde(
        rename = "securitySchemes",
        default,
        skip_serializing_if = "BTreeMap::is_empty"
    )]
    pub security_schemes: BTreeMap<String, HTTPSecurityScheme>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub http: BTreeMap<String, HTTPBinding>,
}

impl PluginManifestMetadata {
    /// Creates empty manifest metadata.
    pub fn new() -> Self {
        Self::default()
    }

    /// Returns `true` when no manifest metadata has been configured.
    pub fn is_empty(&self) -> bool {
        self.security_schemes.is_empty() && self.http.is_empty()
    }

    /// Inserts or replaces one named HTTP security scheme.
    pub fn security_scheme(mut self, name: impl Into<String>, scheme: HTTPSecurityScheme) -> Self {
        let name = name.into().trim().to_owned();
        if !name.is_empty() {
            self.security_schemes.insert(name, scheme);
        }
        self
    }

    /// Inserts or replaces one named hosted HTTP binding.
    pub fn http_binding(mut self, name: impl Into<String>, binding: HTTPBinding) -> Self {
        let name = name.into().trim().to_owned();
        if !name.is_empty() {
            self.http.insert(name, binding);
        }
        self
    }
}

/// Supported manifest HTTP security scheme kinds.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum HTTPSecuritySchemeType {
    #[serde(rename = "hmac")]
    Hmac,
    #[serde(rename = "apiKey")]
    ApiKey,
    #[serde(rename = "http")]
    Http,
    #[serde(rename = "none")]
    None,
}

/// Location for HTTP API key credentials.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum HTTPIn {
    Header,
    Query,
}

/// HTTP authorization scheme kinds supported by hosted bindings.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum HTTPAuthScheme {
    Basic,
    Bearer,
}

/// Credential modes supported by hosted HTTP bindings.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum HTTPBindingCredentialMode {
    None,
    User,
}

/// Secret reference used by generated hosted HTTP bindings.
#[derive(Clone, Debug, Default, Eq, PartialEq, Serialize, Deserialize)]
pub struct HTTPSecretRef {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub env: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub secret: Option<String>,
}

impl HTTPSecretRef {
    /// Creates an empty secret reference.
    pub fn new() -> Self {
        Self::default()
    }

    /// References a process environment variable.
    pub fn env(mut self, env: impl Into<String>) -> Self {
        let env = env.into().trim().to_owned();
        if !env.is_empty() {
            self.env = Some(env);
        }
        self
    }

    /// References a named host secret.
    pub fn secret(mut self, secret: impl Into<String>) -> Self {
        let secret = secret.into().trim().to_owned();
        if !secret.is_empty() {
            self.secret = Some(secret);
        }
        self
    }
}

/// One hosted HTTP security scheme definition.
#[derive(Clone, Debug, Default, Eq, PartialEq, Serialize, Deserialize)]
pub struct HTTPSecurityScheme {
    #[serde(rename = "type", skip_serializing_if = "Option::is_none")]
    pub kind: Option<HTTPSecuritySchemeType>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,
    #[serde(rename = "signatureHeader", skip_serializing_if = "Option::is_none")]
    pub signature_header: Option<String>,
    #[serde(rename = "signaturePrefix", skip_serializing_if = "Option::is_none")]
    pub signature_prefix: Option<String>,
    #[serde(rename = "payloadTemplate", skip_serializing_if = "Option::is_none")]
    pub payload_template: Option<String>,
    #[serde(rename = "timestampHeader", skip_serializing_if = "Option::is_none")]
    pub timestamp_header: Option<String>,
    #[serde(rename = "maxAgeSeconds", skip_serializing_if = "Option::is_none")]
    pub max_age_seconds: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(rename = "in", skip_serializing_if = "Option::is_none")]
    pub location: Option<HTTPIn>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub scheme: Option<HTTPAuthScheme>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub secret: Option<HTTPSecretRef>,
}

impl HTTPSecurityScheme {
    /// Creates an empty security scheme definition.
    pub fn new() -> Self {
        Self::default()
    }

    /// Creates an HMAC request-signing scheme.
    pub fn hmac() -> Self {
        Self::new().kind(HTTPSecuritySchemeType::Hmac)
    }

    /// Creates an API key scheme.
    pub fn api_key(name: impl Into<String>, location: HTTPIn) -> Self {
        Self::new()
            .kind(HTTPSecuritySchemeType::ApiKey)
            .name(name)
            .location(location)
    }

    /// Creates an HTTP auth scheme like bearer or basic auth.
    pub fn http(scheme: HTTPAuthScheme) -> Self {
        Self::new()
            .kind(HTTPSecuritySchemeType::Http)
            .scheme(scheme)
    }

    /// Creates a no-auth scheme.
    pub fn none() -> Self {
        Self::new().kind(HTTPSecuritySchemeType::None)
    }

    /// Sets the scheme kind.
    pub fn kind(mut self, kind: HTTPSecuritySchemeType) -> Self {
        self.kind = Some(kind);
        self
    }

    /// Sets a human-readable description.
    pub fn description(mut self, description: impl Into<String>) -> Self {
        let description = description.into().trim().to_owned();
        if !description.is_empty() {
            self.description = Some(description);
        }
        self
    }

    /// Sets the header carrying the transmitted request signature.
    pub fn signature_header(mut self, header: impl Into<String>) -> Self {
        let header = header.into().trim().to_owned();
        if !header.is_empty() {
            self.signature_header = Some(header);
        }
        self
    }

    /// Sets the static prefix prepended to the computed signature.
    pub fn signature_prefix(mut self, prefix: impl Into<String>) -> Self {
        let prefix = prefix.into();
        if !prefix.is_empty() {
            self.signature_prefix = Some(prefix);
        }
        self
    }

    /// Sets the template used to build the signed payload.
    pub fn payload_template(mut self, template: impl Into<String>) -> Self {
        let template = template.into().trim().to_owned();
        if !template.is_empty() {
            self.payload_template = Some(template);
        }
        self
    }

    /// Sets the header carrying the request timestamp.
    pub fn timestamp_header(mut self, header: impl Into<String>) -> Self {
        let header = header.into().trim().to_owned();
        if !header.is_empty() {
            self.timestamp_header = Some(header);
        }
        self
    }

    /// Sets the maximum accepted request age in seconds.
    pub fn max_age_seconds(mut self, seconds: u64) -> Self {
        if seconds > 0 {
            self.max_age_seconds = Some(seconds);
        }
        self
    }

    /// Sets the HTTP parameter name used by the scheme.
    pub fn name(mut self, name: impl Into<String>) -> Self {
        let name = name.into().trim().to_owned();
        if !name.is_empty() {
            self.name = Some(name);
        }
        self
    }

    /// Sets the location used by API key auth.
    pub fn location(mut self, location: HTTPIn) -> Self {
        self.location = Some(location);
        self
    }

    /// Sets the HTTP auth scheme keyword.
    pub fn scheme(mut self, scheme: HTTPAuthScheme) -> Self {
        self.scheme = Some(scheme);
        self
    }

    /// Attaches a secret reference.
    pub fn secret(mut self, secret: HTTPSecretRef) -> Self {
        self.secret = Some(secret);
        self
    }

    /// Attaches an environment-variable secret reference.
    pub fn secret_env(mut self, env: impl Into<String>) -> Self {
        self.secret = Some(HTTPSecretRef::new().env(env));
        self
    }

    /// Attaches a named secret reference.
    pub fn secret_name(mut self, secret: impl Into<String>) -> Self {
        self.secret = Some(HTTPSecretRef::new().secret(secret));
        self
    }
}

/// Marker object for a supported request media type.
#[derive(Clone, Debug, Default, Eq, PartialEq, Serialize, Deserialize)]
pub struct HTTPMediaType {}

/// Hosted HTTP request-body description.
#[derive(Clone, Debug, Default, Eq, PartialEq, Serialize, Deserialize)]
pub struct HTTPRequestBody {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub required: Option<bool>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub content: BTreeMap<String, HTTPMediaType>,
}

impl HTTPRequestBody {
    /// Creates an empty request-body definition.
    pub fn new() -> Self {
        Self::default()
    }

    /// Marks whether the request body is required.
    pub fn required(mut self, required: bool) -> Self {
        self.required = Some(required);
        self
    }

    /// Adds a media type entry with an empty schema marker.
    pub fn content_type(self, content_type: impl Into<String>) -> Self {
        self.media_type(content_type, HTTPMediaType::default())
    }

    /// Adds a media type entry.
    pub fn media_type(
        mut self,
        content_type: impl Into<String>,
        media_type: HTTPMediaType,
    ) -> Self {
        let content_type = content_type.into().trim().to_owned();
        if !content_type.is_empty() {
            self.content.insert(content_type, media_type);
        }
        self
    }
}

/// Immediate acknowledgement for a hosted HTTP binding.
#[derive(Clone, Debug, Default, Eq, PartialEq, Serialize, Deserialize)]
pub struct HTTPAck {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<u16>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub headers: BTreeMap<String, String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub body: Option<JsonValue>,
}

impl HTTPAck {
    /// Creates an empty acknowledgement response.
    pub fn new() -> Self {
        Self::default()
    }

    /// Sets the acknowledgement HTTP status.
    pub fn status(mut self, status: u16) -> Self {
        self.status = Some(status);
        self
    }

    /// Adds a response header.
    pub fn header(mut self, name: impl Into<String>, value: impl Into<String>) -> Self {
        let name = name.into().trim().to_owned();
        let value = value.into();
        if !name.is_empty() {
            self.headers.insert(name, value);
        }
        self
    }

    /// Sets the acknowledgement body.
    pub fn body(mut self, body: JsonValue) -> Self {
        self.body = Some(body);
        self
    }
}

/// Hosted HTTP binding definition.
#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct HTTPBinding {
    pub path: String,
    pub method: String,
    #[serde(rename = "credentialMode", skip_serializing_if = "Option::is_none")]
    pub credential_mode: Option<HTTPBindingCredentialMode>,
    #[serde(rename = "requestBody", skip_serializing_if = "Option::is_none")]
    pub request_body: Option<HTTPRequestBody>,
    pub security: String,
    pub target: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub ack: Option<HTTPAck>,
}

impl HTTPBinding {
    /// Creates one hosted HTTP binding.
    pub fn new(
        path: impl Into<String>,
        method: impl AsRef<str>,
        security: impl Into<String>,
        target: impl Into<String>,
    ) -> Self {
        Self {
            path: path.into().trim().to_owned(),
            method: normalize_method(method.as_ref()),
            credential_mode: None,
            request_body: None,
            security: security.into().trim().to_owned(),
            target: target.into().trim().to_owned(),
            ack: None,
        }
    }

    /// Overrides provider credential resolution for this binding.
    pub fn credential_mode(mut self, credential_mode: HTTPBindingCredentialMode) -> Self {
        self.credential_mode = Some(credential_mode);
        self
    }

    /// Attaches request-body metadata.
    pub fn request_body(mut self, request_body: HTTPRequestBody) -> Self {
        self.request_body = Some(request_body);
        self
    }

    /// Attaches an acknowledgement response.
    pub fn ack(mut self, ack: HTTPAck) -> Self {
        self.ack = Some(ack);
        self
    }
}

pub(crate) fn write_manifest_metadata(
    metadata: &PluginManifestMetadata,
    path: impl AsRef<Path>,
) -> Result<()> {
    let path = path.as_ref();
    if let Some(parent) = path.parent()
        && !parent.as_os_str().is_empty()
    {
        std::fs::create_dir_all(parent)?;
    }
    let yaml = serde_yaml::to_string(metadata)
        .map_err(|error| Error::hidden_internal(error.to_string()))?;
    std::fs::write(path, yaml)?;
    Ok(())
}

fn normalize_method(method: &str) -> String {
    let method = method.trim().to_ascii_uppercase();
    if method.is_empty() {
        "POST".to_owned()
    } else {
        method
    }
}
