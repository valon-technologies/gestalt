use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::collections::HashMap;
use std::io::{self, BufRead, Write};

pub const PROTOCOL_VERSION: &str = "gestalt-plugin/1";
const JSONRPC_VERSION: &str = "2.0";

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct HostInfo {
    pub name: String,
    pub version: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IntegrationInfo {
    pub name: String,
    pub config: Value,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ParameterDef {
    pub name: String,
    #[serde(rename = "type")]
    pub type_: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub required: bool,
    #[serde(default)]
    pub default: Value,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OperationDef {
    pub name: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub method: String,
    #[serde(default)]
    pub parameters: Vec<ParameterDef>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CatalogOperationDef {
    pub id: String,
    #[serde(default)]
    pub title: String,
    #[serde(default)]
    pub description: String,
    #[serde(rename = "inputSchema", default)]
    pub input_schema: Value,
    #[serde(default)]
    pub parameters: Vec<ParameterDef>,
    #[serde(rename = "requiredScopes", default)]
    pub required_scopes: Vec<String>,
    #[serde(default)]
    pub tags: Vec<String>,
    #[serde(rename = "readOnly", default)]
    pub read_only: bool,
    #[serde(default)]
    pub visible: Option<bool>,
    #[serde(default)]
    pub transport: String,
    #[serde(default)]
    pub query: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CatalogDef {
    pub name: String,
    #[serde(rename = "displayName", default)]
    pub display_name: String,
    #[serde(default)]
    pub description: String,
    #[serde(rename = "iconSvg", default)]
    pub icon_svg: String,
    #[serde(rename = "baseUrl", default)]
    pub base_url: String,
    #[serde(rename = "authStyle", default)]
    pub auth_style: String,
    #[serde(default)]
    pub headers: HashMap<String, String>,
    #[serde(default)]
    pub operations: Vec<CatalogOperationDef>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProviderManifest {
    #[serde(rename = "displayName")]
    pub display_name: String,
    pub description: String,
    #[serde(rename = "connectionMode")]
    pub connection_mode: String,
    pub operations: Vec<OperationDef>,
    #[serde(default)]
    pub catalog: Option<Value>,
    #[serde(default)]
    pub auth: Option<Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PluginInfo {
    pub name: String,
    pub version: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Capabilities {
    #[serde(default)]
    pub catalog: bool,
    #[serde(default)]
    pub oauth: bool,
    #[serde(rename = "manualAuth", default)]
    pub manual_auth: bool,
    #[serde(default)]
    pub cancellation: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct InitializeRequest {
    #[serde(rename = "protocolVersion")]
    pub protocol_version: String,
    #[serde(rename = "hostInfo")]
    pub host_info: HostInfo,
    pub integration: IntegrationInfo,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct InitializeResult {
    #[serde(rename = "protocolVersion")]
    pub protocol_version: String,
    #[serde(rename = "pluginInfo")]
    pub plugin_info: PluginInfo,
    pub provider: ProviderManifest,
    #[serde(default)]
    pub capabilities: Option<Capabilities>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExecuteRequest {
    pub operation: String,
    pub params: Value,
    pub token: String,
    #[serde(default)]
    pub meta: Option<Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExecuteResult {
    pub status: i32,
    pub body: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuthStartRequest {
    pub state: String,
    pub scopes: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuthStartResult {
    #[serde(rename = "authUrl")]
    pub auth_url: String,
    #[serde(default)]
    pub verifier: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuthExchangeRequest {
    pub code: String,
    #[serde(default)]
    pub verifier: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TokenResult {
    #[serde(rename = "accessToken")]
    pub access_token: String,
    #[serde(rename = "refreshToken", default)]
    pub refresh_token: Option<String>,
    #[serde(rename = "expiresIn", default)]
    pub expires_in: Option<i64>,
    #[serde(rename = "tokenType", default)]
    pub token_type: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuthRefreshRequest {
    #[serde(rename = "refreshToken")]
    pub refresh_token: String,
}

#[derive(Debug)]
pub struct PluginError {
    pub message: String,
}

impl std::fmt::Display for PluginError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.message)
    }
}

impl std::error::Error for PluginError {}

impl From<&str> for PluginError {
    fn from(message: &str) -> Self {
        Self { message: message.to_string() }
    }
}

impl From<String> for PluginError {
    fn from(message: String) -> Self {
        Self { message }
    }
}

#[derive(Deserialize)]
struct JsonRpcRequest {
    #[serde(rename = "jsonrpc")]
    _jsonrpc: Option<String>,
    method: String,
    #[serde(default)]
    params: Value,
    #[serde(default)]
    id: Option<Value>,
}

#[derive(Serialize)]
struct JsonRpcResponse {
    jsonrpc: &'static str,
    id: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    result: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<JsonRpcError>,
}

#[derive(Serialize)]
struct JsonRpcError {
    code: i32,
    message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    data: Option<Value>,
}

type InitializeHandler = Box<dyn Fn(InitializeRequest) -> Result<InitializeResult, PluginError> + Send + Sync>;
type ExecuteHandler = Box<dyn Fn(ExecuteRequest) -> Result<ExecuteResult, PluginError> + Send + Sync>;
type AuthStartHandler = Box<dyn Fn(AuthStartRequest) -> Result<AuthStartResult, PluginError> + Send + Sync>;
type AuthExchangeHandler = Box<dyn Fn(AuthExchangeRequest) -> Result<TokenResult, PluginError> + Send + Sync>;
type AuthRefreshHandler = Box<dyn Fn(AuthRefreshRequest) -> Result<TokenResult, PluginError> + Send + Sync>;
type ShutdownHandler = Box<dyn Fn() -> Result<(), PluginError> + Send + Sync>;

pub struct Plugin {
    plugin_info: PluginInfo,
    provider: ProviderManifest,
    capabilities: Option<Capabilities>,
    initialize_handler: Option<InitializeHandler>,
    execute_handler: ExecuteHandler,
    auth_start_handler: Option<AuthStartHandler>,
    auth_exchange_handler: Option<AuthExchangeHandler>,
    auth_refresh_handler: Option<AuthRefreshHandler>,
    shutdown_handler: Option<ShutdownHandler>,
}

impl Plugin {
    pub fn new<F>(
        plugin_info: PluginInfo,
        provider: ProviderManifest,
        execute: F,
    ) -> Self
    where
        F: Fn(ExecuteRequest) -> Result<ExecuteResult, PluginError> + Send + Sync + 'static,
    {
        Self {
            plugin_info,
            provider,
            capabilities: None,
            initialize_handler: None,
            execute_handler: Box::new(execute),
            auth_start_handler: None,
            auth_exchange_handler: None,
            auth_refresh_handler: None,
            shutdown_handler: None,
        }
    }

    pub fn capabilities(mut self, capabilities: Capabilities) -> Self {
        self.capabilities = Some(capabilities);
        self
    }

    pub fn on_initialize<F>(mut self, handler: F) -> Self
    where
        F: Fn(InitializeRequest) -> Result<InitializeResult, PluginError> + Send + Sync + 'static,
    {
        self.initialize_handler = Some(Box::new(handler));
        self
    }

    pub fn on_auth_start<F>(mut self, handler: F) -> Self
    where
        F: Fn(AuthStartRequest) -> Result<AuthStartResult, PluginError> + Send + Sync + 'static,
    {
        self.auth_start_handler = Some(Box::new(handler));
        self
    }

    pub fn on_auth_exchange_code<F>(mut self, handler: F) -> Self
    where
        F: Fn(AuthExchangeRequest) -> Result<TokenResult, PluginError> + Send + Sync + 'static,
    {
        self.auth_exchange_handler = Some(Box::new(handler));
        self
    }

    pub fn on_auth_refresh_token<F>(mut self, handler: F) -> Self
    where
        F: Fn(AuthRefreshRequest) -> Result<TokenResult, PluginError> + Send + Sync + 'static,
    {
        self.auth_refresh_handler = Some(Box::new(handler));
        self
    }

    pub fn on_shutdown<F>(mut self, handler: F) -> Self
    where
        F: Fn() -> Result<(), PluginError> + Send + Sync + 'static,
    {
        self.shutdown_handler = Some(Box::new(handler));
        self
    }

    pub fn serve(self) -> Result<(), Box<dyn std::error::Error>> {
        let stdin = io::stdin();
        let stdout = io::stdout();
        let mut reader = stdin.lock();
        let mut writer = stdout.lock();
        self.serve_with_io(&mut reader, &mut writer)
    }

    pub fn serve_with_io<R: BufRead, W: Write>(
        self,
        reader: &mut R,
        writer: &mut W,
    ) -> Result<(), Box<dyn std::error::Error>> {
        let plugin = self;
        loop {
            let body = match read_frame(reader)? {
                Some(body) => body,
                None => return Ok(()),
            };
            let request: JsonRpcRequest = match serde_json::from_slice(&body) {
                Ok(request) => request,
                Err(err) => {
                    write_response(
                        writer,
                        JsonRpcResponse {
                            jsonrpc: JSONRPC_VERSION,
                            id: None,
                            result: None,
                            error: Some(JsonRpcError {
                                code: -32700,
                                message: "invalid JSON payload".to_string(),
                                data: Some(json!(err.to_string())),
                            }),
                        },
                    )?;
                    continue;
                }
            };

            if request.id.is_none() {
                if request.method == "exit" {
                    plugin.shutdown()?;
                    return Ok(());
                }
                continue;
            }

            let id = request.id.clone();
            let response = match request.method.as_str() {
                "initialize" => match plugin.handle_initialize(request.params) {
                    Ok(result) => JsonRpcResponse {
                        jsonrpc: JSONRPC_VERSION,
                        id,
                        result: Some(serde_json::to_value(result)?),
                        error: None,
                    },
                    Err(err) => error_response(id, -32603, err.to_string()),
                },
                "provider.execute" => match plugin.handle_execute(request.params) {
                    Ok(result) => JsonRpcResponse {
                        jsonrpc: JSONRPC_VERSION,
                        id,
                        result: Some(serde_json::to_value(result)?),
                        error: None,
                    },
                    Err(err) => error_response(id, -32603, err.to_string()),
                },
                "auth.start" => match plugin.handle_auth_start(request.params) {
                    Ok(result) => JsonRpcResponse {
                        jsonrpc: JSONRPC_VERSION,
                        id,
                        result: Some(serde_json::to_value(result)?),
                        error: None,
                    },
                    Err(err) => error_response(id, -32603, err.to_string()),
                },
                "auth.exchange_code" => match plugin.handle_auth_exchange_code(request.params) {
                    Ok(result) => JsonRpcResponse {
                        jsonrpc: JSONRPC_VERSION,
                        id,
                        result: Some(serde_json::to_value(result)?),
                        error: None,
                    },
                    Err(err) => error_response(id, -32603, err.to_string()),
                },
                "auth.refresh_token" => match plugin.handle_auth_refresh_token(request.params) {
                    Ok(result) => JsonRpcResponse {
                        jsonrpc: JSONRPC_VERSION,
                        id,
                        result: Some(serde_json::to_value(result)?),
                        error: None,
                    },
                    Err(err) => error_response(id, -32603, err.to_string()),
                },
                "shutdown" => JsonRpcResponse {
                    jsonrpc: JSONRPC_VERSION,
                    id,
                    result: Some(Value::Null),
                    error: None,
                }
                _ => error_response(id, -32601, format!("method not found: {}", request.method)),
            };

            write_response(writer, response)?;
            if request.method == "shutdown" {
                plugin.shutdown()?;
                return Ok(());
            }
        }
    }

    fn shutdown(&self) -> Result<(), Box<dyn std::error::Error>> {
        if let Some(handler) = &self.shutdown_handler {
            handler()?;
        }
        Ok(())
    }

    fn handle_initialize(&self, params: Value) -> Result<InitializeResult, PluginError> {
        let request = deserialize_params::<InitializeRequest>(params, "initialize")?;
        if let Some(handler) = &self.initialize_handler {
            return handler(request);
        }
        Ok(InitializeResult {
            protocol_version: PROTOCOL_VERSION.to_string(),
            plugin_info: self.plugin_info.clone(),
            provider: self.provider.clone(),
            capabilities: self.capabilities.clone(),
        })
    }

    fn handle_execute(&self, params: Value) -> Result<ExecuteResult, PluginError> {
        let request = deserialize_params::<ExecuteRequest>(params, "provider.execute")?;
        (self.execute_handler)(request)
    }

    fn handle_auth_start(&self, params: Value) -> Result<AuthStartResult, PluginError> {
        let request = deserialize_params::<AuthStartRequest>(params, "auth.start")?;
        let handler = self
            .auth_start_handler
            .as_ref()
            .ok_or_else(|| PluginError::from("auth.start is not implemented"))?;
        handler(request)
    }

    fn handle_auth_exchange_code(&self, params: Value) -> Result<TokenResult, PluginError> {
        let request = deserialize_params::<AuthExchangeRequest>(params, "auth.exchange_code")?;
        let handler = self
            .auth_exchange_handler
            .as_ref()
            .ok_or_else(|| PluginError::from("auth.exchange_code is not implemented"))?;
        handler(request)
    }

    fn handle_auth_refresh_token(&self, params: Value) -> Result<TokenResult, PluginError> {
        let request = deserialize_params::<AuthRefreshRequest>(params, "auth.refresh_token")?;
        let handler = self
            .auth_refresh_handler
            .as_ref()
            .ok_or_else(|| PluginError::from("auth.refresh_token is not implemented"))?;
        handler(request)
    }
}

fn deserialize_params<T: for<'de> Deserialize<'de>>(params: Value, method: &str) -> Result<T, PluginError> {
    if !params.is_object() {
        return Err(PluginError::from(format!("{} requires a JSON object", method)));
    }
    serde_json::from_value(params).map_err(|err| PluginError::from(err.to_string()))
}

fn read_frame<R: BufRead>(reader: &mut R) -> io::Result<Option<Vec<u8>>> {
    let mut content_length: Option<usize> = None;
    loop {
        let mut line = String::new();
        let bytes = reader.read_line(&mut line)?;
        if bytes == 0 {
            return Ok(None);
        }
        let trimmed = line.trim_end_matches(['\r', '\n']);
        if trimmed.is_empty() {
            break;
        }
        if let Some((key, value)) = trimmed.split_once(':') {
            if key.trim().eq_ignore_ascii_case("Content-Length") {
                content_length = Some(value.trim().parse().map_err(|_| {
                    io::Error::new(io::ErrorKind::InvalidData, "invalid Content-Length header")
                })?);
            }
        }
    }
    let length = match content_length {
        Some(length) => length,
        None => {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "missing Content-Length header",
            ))
        }
    };
    let mut body = vec![0u8; length];
    reader.read_exact(&mut body)?;
    Ok(Some(body))
}

fn write_response<W: Write>(writer: &mut W, response: JsonRpcResponse) -> io::Result<()> {
    let body = serde_json::to_vec(&response)
        .map_err(|err| io::Error::new(io::ErrorKind::InvalidData, err.to_string()))?;
    write!(writer, "Content-Length: {}\r\n\r\n", body.len())?;
    writer.write_all(&body)?;
    writer.flush()?;
    Ok(())
}

fn error_response(id: Option<Value>, code: i32, message: String) -> JsonRpcResponse {
    JsonRpcResponse {
        jsonrpc: JSONRPC_VERSION,
        id,
        result: None,
        error: Some(JsonRpcError {
            code,
            message,
            data: None,
        }),
    }
}
