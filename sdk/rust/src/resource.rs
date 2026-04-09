use std::collections::BTreeMap;
use std::time::SystemTime;

use serde::{Deserialize, Serialize};
use tonic::codegen::async_trait;

use crate::error::Result;

// ---------------------------------------------------------------------------
// Capability enum
// ---------------------------------------------------------------------------

#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum DatastoreCapability {
    KeyValue,
    Sql,
    BlobStore,
}

// ---------------------------------------------------------------------------
// Key-value types and consumer trait
// ---------------------------------------------------------------------------

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct KvEntry {
    pub key: String,
    pub value: Vec<u8>,
}

#[async_trait]
pub trait KeyValueStore: Send + Sync {
    async fn get(&self, key: &str) -> Result<Option<Vec<u8>>>;
    async fn put(&self, key: &str, value: Vec<u8>) -> Result<()>;
    async fn put_with_ttl(&self, key: &str, value: Vec<u8>, ttl_seconds: i64) -> Result<()>;
    async fn delete(&self, key: &str) -> Result<bool>;
    async fn list(&self, prefix: &str, cursor: &str, limit: i32) -> Result<(Vec<KvEntry>, String)>;
}

// ---------------------------------------------------------------------------
// SQL types and consumer trait
// ---------------------------------------------------------------------------

#[derive(Clone, Debug, PartialEq)]
pub struct SqlRows {
    pub columns: Vec<String>,
    pub rows: Vec<Vec<serde_json::Value>>,
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct SqlExecResult {
    pub rows_affected: i64,
    pub last_insert_id: i64,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct SqlMigration {
    pub version: i32,
    pub description: String,
    pub up_sql: String,
}

#[async_trait]
pub trait SqlStore: Send + Sync {
    async fn query(&self, query: &str, params: Vec<serde_json::Value>) -> Result<SqlRows>;
    async fn exec(&self, query: &str, params: Vec<serde_json::Value>) -> Result<SqlExecResult>;
    async fn migrate(&self, migrations: Vec<SqlMigration>) -> Result<()>;
}

// ---------------------------------------------------------------------------
// Blob types and consumer trait
// ---------------------------------------------------------------------------

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BlobEntry {
    pub key: String,
    pub size: i64,
    pub content_type: String,
    pub last_modified: Option<SystemTime>,
}

#[async_trait]
pub trait BlobStore: Send + Sync {
    async fn get(&self, key: &str) -> Result<Option<(Vec<u8>, String)>>;
    async fn put(&self, key: &str, data: Vec<u8>, content_type: &str) -> Result<()>;
    async fn delete(&self, key: &str) -> Result<()>;
    async fn list(
        &self,
        prefix: &str,
        cursor: &str,
        limit: i32,
    ) -> Result<(Vec<BlobEntry>, String)>;
}

// ---------------------------------------------------------------------------
// Provider-facing traits (with namespace parameter)
// ---------------------------------------------------------------------------

#[async_trait]
pub trait DatastoreProvider: Send + Sync + 'static {
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> Result<()> {
        Ok(())
    }

    fn capabilities(&self) -> Vec<DatastoreCapability>;

    async fn health_check(&self) -> Result<()>;

    async fn close(&self) -> Result<()> {
        Ok(())
    }
}

#[async_trait]
pub trait KeyValueDatastoreProvider: DatastoreProvider {
    async fn kv_get(&self, namespace: &str, key: &str) -> Result<Option<Vec<u8>>>;
    async fn kv_put(
        &self,
        namespace: &str,
        key: &str,
        value: Vec<u8>,
        ttl_seconds: i64,
    ) -> Result<()>;
    async fn kv_delete(&self, namespace: &str, key: &str) -> Result<bool>;
    async fn kv_list(
        &self,
        namespace: &str,
        prefix: &str,
        cursor: &str,
        limit: i32,
    ) -> Result<(Vec<KvEntry>, String)>;
    async fn kv_migrate(&self, namespace: &str) -> Result<()>;
}

#[async_trait]
pub trait SqlDatastoreProvider: DatastoreProvider {
    async fn sql_query(
        &self,
        namespace: &str,
        query: &str,
        params: Vec<serde_json::Value>,
    ) -> Result<SqlRows>;
    async fn sql_exec(
        &self,
        namespace: &str,
        query: &str,
        params: Vec<serde_json::Value>,
    ) -> Result<SqlExecResult>;
    async fn sql_migrate(&self, namespace: &str, migrations: Vec<SqlMigration>) -> Result<()>;
}

#[async_trait]
pub trait BlobStoreDatastoreProvider: DatastoreProvider {
    async fn blob_get(
        &self,
        namespace: &str,
        key: &str,
    ) -> Result<Option<(Vec<u8>, String, BTreeMap<String, String>)>>;
    async fn blob_put(
        &self,
        namespace: &str,
        key: &str,
        data: Vec<u8>,
        content_type: &str,
        metadata: BTreeMap<String, String>,
    ) -> Result<()>;
    async fn blob_delete(&self, namespace: &str, key: &str) -> Result<()>;
    async fn blob_list(
        &self,
        namespace: &str,
        prefix: &str,
        cursor: &str,
        limit: i32,
    ) -> Result<(Vec<BlobEntry>, String)>;
}
