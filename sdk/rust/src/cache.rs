use std::collections::{BTreeMap, BTreeSet};
use std::time::Duration;

use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::Request;
use tonic::codegen::async_trait;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tower::service_fn;

use crate::api::RuntimeMetadata;
use crate::error::Result;
use crate::generated::v1::{self as pb, cache_client::CacheClient};

type CacheTransport = InterceptedService<Channel, RelayTokenInterceptor>;

/// Default Unix-socket environment variable used by [`Cache::connect`].
pub const ENV_CACHE_SOCKET: &str = "GESTALT_CACHE_SOCKET";
/// Default relay-token environment variable used by [`Cache::connect`].
pub const ENV_CACHE_SOCKET_TOKEN: &str = "GESTALT_CACHE_SOCKET_TOKEN";
/// Suffix added to named cache socket variables for relay-token variables.
pub const ENV_CACHE_SOCKET_TOKEN_SUFFIX: &str = "_TOKEN";
const CACHE_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

#[derive(Debug, Clone, PartialEq, Eq)]
/// One cache entry written through [`Cache::set_many`].
pub struct CacheEntry {
    /// Cache key to store.
    pub key: String,
    /// Cache value bytes.
    pub value: Vec<u8>,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
/// Options applied to cache writes.
pub struct CacheSetOptions {
    /// Optional time-to-live for the stored value.
    pub ttl: Option<Duration>,
}

#[derive(Debug, thiserror::Error)]
/// Errors returned by the cache client transport.
pub enum CacheError {
    /// The host-service transport could not be created.
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    /// The host-service RPC returned a gRPC status.
    #[error("{0}")]
    Status(#[from] tonic::Status),
    /// Required environment or target configuration was invalid.
    #[error("{0}")]
    Env(String),
}

#[async_trait]
/// Lifecycle and RPC contract for cache providers.
pub trait CacheProvider: Send + Sync + 'static {
    /// Configures the provider before it starts serving requests.
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> Result<()> {
        Ok(())
    }

    /// Returns runtime metadata that should augment the static manifest.
    fn metadata(&self) -> Option<RuntimeMetadata> {
        None
    }

    /// Returns non-fatal warnings the host should surface to users.
    fn warnings(&self) -> Vec<String> {
        Vec::new()
    }

    /// Performs an optional health check.
    async fn health_check(&self) -> Result<()> {
        Ok(())
    }

    /// Starts provider-owned background work after configuration.
    async fn start(&self) -> Result<()> {
        Ok(())
    }

    /// Shuts the provider down before the runtime exits.
    async fn close(&self) -> Result<()> {
        Ok(())
    }

    /// Loads one cache value.
    async fn get(&self, key: &str) -> Result<Option<Vec<u8>>>;

    /// Loads many cache values, defaulting to repeated [`CacheProvider::get`]
    /// calls.
    async fn get_many(&self, keys: &[String]) -> Result<BTreeMap<String, Vec<u8>>> {
        let mut values = BTreeMap::new();
        for key in keys {
            if let Some(value) = self.get(key).await? {
                values.insert(key.clone(), value);
            }
        }
        Ok(values)
    }

    /// Stores one cache value.
    async fn set(&self, key: &str, value: &[u8], options: CacheSetOptions) -> Result<()>;

    /// Stores many cache values, defaulting to repeated [`CacheProvider::set`]
    /// calls.
    async fn set_many(&self, entries: &[CacheEntry], options: CacheSetOptions) -> Result<()> {
        for entry in entries {
            self.set(&entry.key, &entry.value, options).await?;
        }
        Ok(())
    }

    /// Deletes one cache key.
    async fn delete(&self, key: &str) -> Result<bool>;

    /// Deletes many cache keys, defaulting to repeated
    /// [`CacheProvider::delete`] calls.
    async fn delete_many(&self, keys: &[String]) -> Result<i64> {
        let mut deleted = 0_i64;
        let mut seen = BTreeSet::new();
        for key in keys {
            if !seen.insert(key.as_str()) {
                continue;
            }
            if self.delete(key).await? {
                deleted += 1;
            }
        }
        Ok(deleted)
    }

    /// Updates the TTL for one cache key.
    async fn touch(&self, key: &str, ttl: Duration) -> Result<bool>;
}

/// Client for a running cache provider.
pub struct Cache {
    client: CacheClient<CacheTransport>,
}

impl Cache {
    /// Connects to the default cache transport socket.
    pub async fn connect() -> std::result::Result<Self, CacheError> {
        Self::connect_named("").await
    }

    /// Connects to a named cache transport socket.
    pub async fn connect_named(name: &str) -> std::result::Result<Self, CacheError> {
        let env_name = cache_socket_env(name);
        let target = std::env::var(&env_name)
            .map_err(|_| CacheError::Env(format!("{env_name} is not set")))?;
        let relay_token =
            std::env::var(cache_socket_token_env(name)).unwrap_or_else(|_| String::new());

        let channel = match parse_cache_target(&target)? {
            CacheTarget::Unix(path) => {
                Endpoint::try_from("http://[::]:50051")?
                    .connect_with_connector(service_fn(move |_: Uri| {
                        let path = path.clone();
                        async move { UnixStream::connect(path).await.map(TokioIo::new) }
                    }))
                    .await?
            }
            CacheTarget::Tcp(address) => {
                Endpoint::from_shared(format!("http://{address}"))?
                    .connect()
                    .await?
            }
            CacheTarget::Tls(address) => {
                Endpoint::from_shared(format!("https://{address}"))?
                    .tls_config(ClientTlsConfig::new().with_native_roots())?
                    .connect()
                    .await?
            }
        };

        Ok(Self {
            client: CacheClient::with_interceptor(
                channel,
                relay_token_interceptor(relay_token.trim())?,
            ),
        })
    }

    /// Loads one cache value.
    pub async fn get(&mut self, key: &str) -> std::result::Result<Option<Vec<u8>>, CacheError> {
        let response = self
            .client
            .get(pb::CacheGetRequest {
                key: key.to_string(),
            })
            .await?
            .into_inner();
        if !response.found {
            return Ok(None);
        }
        Ok(Some(response.value))
    }

    /// Loads all present values for keys.
    pub async fn get_many<S>(
        &mut self,
        keys: &[S],
    ) -> std::result::Result<BTreeMap<String, Vec<u8>>, CacheError>
    where
        S: AsRef<str>,
    {
        let request_keys: Vec<String> = keys.iter().map(|key| key.as_ref().to_string()).collect();
        let response = self
            .client
            .get_many(pb::CacheGetManyRequest { keys: request_keys })
            .await?
            .into_inner();
        let mut values = BTreeMap::new();
        for entry in response.entries {
            if entry.found {
                values.insert(entry.key, entry.value);
            }
        }
        Ok(values)
    }

    /// Stores one cache value.
    pub async fn set(
        &mut self,
        key: &str,
        value: &[u8],
        options: CacheSetOptions,
    ) -> std::result::Result<(), CacheError> {
        self.client
            .set(pb::CacheSetRequest {
                key: key.to_string(),
                value: value.to_vec(),
                ttl: duration_to_proto(options.ttl),
            })
            .await?;
        Ok(())
    }

    /// Stores multiple cache values in one RPC.
    pub async fn set_many(
        &mut self,
        entries: &[CacheEntry],
        options: CacheSetOptions,
    ) -> std::result::Result<(), CacheError> {
        self.client
            .set_many(pb::CacheSetManyRequest {
                entries: entries
                    .iter()
                    .map(|entry| pb::CacheSetEntry {
                        key: entry.key.clone(),
                        value: entry.value.clone(),
                    })
                    .collect(),
                ttl: duration_to_proto(options.ttl),
            })
            .await?;
        Ok(())
    }

    /// Deletes one cache key.
    pub async fn delete(&mut self, key: &str) -> std::result::Result<bool, CacheError> {
        let response = self
            .client
            .delete(pb::CacheDeleteRequest {
                key: key.to_string(),
            })
            .await?
            .into_inner();
        Ok(response.deleted)
    }

    /// Deletes many cache keys.
    pub async fn delete_many<S>(&mut self, keys: &[S]) -> std::result::Result<i64, CacheError>
    where
        S: AsRef<str>,
    {
        let response = self
            .client
            .delete_many(pb::CacheDeleteManyRequest {
                keys: keys.iter().map(|key| key.as_ref().to_string()).collect(),
            })
            .await?
            .into_inner();
        Ok(response.deleted)
    }

    /// Updates the TTL for one cache key.
    pub async fn touch(
        &mut self,
        key: &str,
        ttl: Duration,
    ) -> std::result::Result<bool, CacheError> {
        let response = self
            .client
            .touch(pb::CacheTouchRequest {
                key: key.to_string(),
                ttl: duration_to_proto(Some(ttl)),
            })
            .await?
            .into_inner();
        Ok(response.touched)
    }
}

/// Returns the environment variable used for a named cache socket.
pub fn cache_socket_env(name: &str) -> String {
    let trimmed = name.trim();
    if trimmed.is_empty() {
        return ENV_CACHE_SOCKET.to_string();
    }
    let mut env = String::from(ENV_CACHE_SOCKET);
    env.push('_');
    for ch in trimmed.chars() {
        if ch.is_ascii_alphanumeric() {
            env.push(ch.to_ascii_uppercase());
        } else {
            env.push('_');
        }
    }
    env
}

/// Returns the environment variable used for a named cache relay token.
pub fn cache_socket_token_env(name: &str) -> String {
    format!(
        "{env}{}",
        ENV_CACHE_SOCKET_TOKEN_SUFFIX,
        env = cache_socket_env(name)
    )
}

enum CacheTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_cache_target(raw_target: &str) -> std::result::Result<CacheTarget, CacheError> {
    let target = raw_target.trim();
    if target.is_empty() {
        return Err(CacheError::Env(
            "cache: transport target is required".to_string(),
        ));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(CacheError::Env(format!(
                "cache: tcp target {raw_target:?} is missing host:port"
            )));
        }
        return Ok(CacheTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(CacheError::Env(format!(
                "cache: tls target {raw_target:?} is missing host:port"
            )));
        }
        return Ok(CacheTarget::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(CacheError::Env(format!(
                "cache: unix target {raw_target:?} is missing a socket path"
            )));
        }
        return Ok(CacheTarget::Unix(path.to_string()));
    }
    if target.contains("://") {
        let scheme = target.split("://").next().unwrap_or_default();
        return Err(CacheError::Env(format!(
            "cache: unsupported target scheme {scheme:?}"
        )));
    }
    Ok(CacheTarget::Unix(target.to_string()))
}

fn relay_token_interceptor(token: &str) -> std::result::Result<RelayTokenInterceptor, CacheError> {
    let header =
        if token.trim().is_empty() {
            None
        } else {
            Some(MetadataValue::try_from(token.to_string()).map_err(|err| {
                CacheError::Env(format!("invalid cache relay token metadata: {err}"))
            })?)
        };
    Ok(RelayTokenInterceptor { header })
}

#[derive(Clone)]
struct RelayTokenInterceptor {
    header: Option<MetadataValue<tonic::metadata::Ascii>>,
}

impl Interceptor for RelayTokenInterceptor {
    fn call(
        &mut self,
        mut request: Request<()>,
    ) -> std::result::Result<Request<()>, tonic::Status> {
        if let Some(header) = self.header.clone() {
            request
                .metadata_mut()
                .insert(CACHE_RELAY_TOKEN_HEADER, header);
        }
        Ok(request)
    }
}

fn duration_to_proto(ttl: Option<Duration>) -> Option<prost_types::Duration> {
    let ttl = ttl.filter(|ttl| !ttl.is_zero())?;
    Some(prost_types::Duration {
        seconds: i64::try_from(ttl.as_secs()).unwrap_or(i64::MAX),
        nanos: i32::try_from(ttl.subsec_nanos()).unwrap_or(i32::MAX),
    })
}
