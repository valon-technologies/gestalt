use std::collections::{BTreeMap, BTreeSet};
use std::time::Duration;

use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::codegen::async_trait;
use tonic::transport::{Channel, Endpoint, Uri};
use tower::service_fn;

use crate::api::RuntimeMetadata;
use crate::error::Result;
use crate::generated::v1::{self as pb, cache_client::CacheClient};

pub const ENV_CACHE_SOCKET: &str = "GESTALT_CACHE_SOCKET";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CacheEntry {
    pub key: String,
    pub value: Vec<u8>,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct CacheSetOptions {
    pub ttl: Option<Duration>,
}

#[derive(Debug, thiserror::Error)]
pub enum CacheError {
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    #[error("{0}")]
    Status(#[from] tonic::Status),
    #[error("{0}")]
    Env(String),
}

#[async_trait]
pub trait CacheProvider: Send + Sync + 'static {
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> Result<()> {
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        None
    }

    fn warnings(&self) -> Vec<String> {
        Vec::new()
    }

    async fn health_check(&self) -> Result<()> {
        Ok(())
    }

    async fn close(&self) -> Result<()> {
        Ok(())
    }

    async fn get(&self, key: &str) -> Result<Option<Vec<u8>>>;

    async fn get_many(&self, keys: &[String]) -> Result<BTreeMap<String, Vec<u8>>> {
        let mut values = BTreeMap::new();
        for key in keys {
            if let Some(value) = self.get(key).await? {
                values.insert(key.clone(), value);
            }
        }
        Ok(values)
    }

    async fn set(&self, key: &str, value: &[u8], options: CacheSetOptions) -> Result<()>;

    async fn set_many(&self, entries: &[CacheEntry], options: CacheSetOptions) -> Result<()> {
        for entry in entries {
            self.set(&entry.key, &entry.value, options).await?;
        }
        Ok(())
    }

    async fn delete(&self, key: &str) -> Result<bool>;

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

    async fn touch(&self, key: &str, ttl: Duration) -> Result<bool>;
}

pub struct Cache {
    client: CacheClient<Channel>,
}

impl Cache {
    pub async fn connect() -> std::result::Result<Self, CacheError> {
        Self::connect_named("").await
    }

    pub async fn connect_named(name: &str) -> std::result::Result<Self, CacheError> {
        let env_name = cache_socket_env(name);
        let socket_path = std::env::var(&env_name)
            .map_err(|_| CacheError::Env(format!("{env_name} is not set")))?;

        let channel = Endpoint::try_from("http://[::]:50051")?
            .connect_with_connector(service_fn(move |_: Uri| {
                let path = socket_path.clone();
                async move { UnixStream::connect(path).await.map(TokioIo::new) }
            }))
            .await?;

        Ok(Self {
            client: CacheClient::new(channel),
        })
    }

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

fn duration_to_proto(ttl: Option<Duration>) -> Option<prost_types::Duration> {
    let ttl = ttl.filter(|ttl| !ttl.is_zero())?;
    Some(prost_types::Duration {
        seconds: i64::try_from(ttl.as_secs()).unwrap_or(i64::MAX),
        nanos: i32::try_from(ttl.subsec_nanos()).unwrap_or(i32::MAX),
    })
}
