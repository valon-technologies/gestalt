use std::collections::{BTreeMap, BTreeSet};
use std::time::Duration;

use tonic::codegen::async_trait;

use crate::api::RuntimeMetadata;
use crate::error::Result;

#[derive(Debug, Clone, PartialEq, Eq)]
/// One cache entry written through [`CacheProvider::set_many`].
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
